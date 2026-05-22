package main

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// --- Constants ---

const (
	fbDevice    = "/dev/fb0"
	inputDir    = "/dev/input"
	cfgPath     = "/userdata/app/gk/printer_mutable.cfg"
	watchdogSec = 300 // auto-exit after 5 minutes (safety)

	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_ABS = 0x03

	BTN_TOUCH         = 0x14a
	ABS_X             = 0x00
	ABS_Y             = 0x01
	ABS_MT_POSITION_X = 0x35
	ABS_MT_POSITION_Y = 0x36
)

// --- Types ---

// InputEvent matches the Linux input_event struct (32-bit ARM)
type InputEvent struct {
	Sec   uint32
	Usec  uint32
	Type  uint16
	Code  uint16
	Value int32
}

type inputAbsInfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
}

type Button struct {
	X, Y, W, H  int
	Label       string
	BgColor     color.RGBA
	BorderColor color.RGBA
	Radius      int
	OnClick     func()
}

// JSON structure of printer_mutable.cfg
type PrinterConfig struct {
	BedMesh map[string]string `json:"bed_mesh default"`
}

// --- Globals ---

//go:embed VERSION
var versionFS string

func getVersion() string {
	return strings.TrimSpace(versionFS)
}

// Display rotation: 0 = none, 180 = vflip+hflip (KS1/KS1M)
var displayRotation int = 0

var (
	screenW        int  = 480
	screenH        int  = 272
	fbW            int  = 480
	fbH            int  = 272
	fbMem          []byte
	fbBackup       []byte // backup original screen content
	buttons        []Button
	exitFlag       int32 // atomic flag
	mu             sync.Mutex
	touchMaxX      int  = 4095
	touchMaxY      int  = 4095
	touchIsLogical bool = false
	hasTouchLimits bool = false
	modelCode      string = ""
	touchCalX0     int  = 25
	touchCalY0     int  = 235
	touchCalX1     int  = 460
	touchCalY1     int  = 25
)

func getAbsInfo(fd uintptr, axis int) (inputAbsInfo, error) {
	var info inputAbsInfo
	// 0x80184540 corresponds to EVIOCGABS(0)
	ioctlCode := uintptr(0x80184540) + uintptr(axis)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlCode, uintptr(unsafe.Pointer(&info)))
	if errno != 0 {
		return info, errno
	}
	return info, nil
}

func detectRotation() {
	// Try to detect model from environment or system files
	model := os.Getenv("KOBRA_MODEL_CODE")
	if model == "" {
		// Try reading model from common firmware locations
		for _, path := range []string{
			"/userdata/app/gk/.model_code",
			"/useremain/.model_code",
		} {
			if data, err := os.ReadFile(path); err == nil {
				model = strings.TrimSpace(string(data))
				log.Printf("Model read from %s: %s", path, model)
				break
			}
		}
	}

	switch model {
	case "KS1", "KS1M":
		displayRotation = 180
		touchCalX0, touchCalY0, touchCalX1, touchCalY1 = 0, 0, 800, 480
	case "K3M":
		displayRotation = 270
		touchCalX0, touchCalY0, touchCalX1, touchCalY1 = 25, 235, 460, 25
	case "K3", "K3V2":
		displayRotation = 90
		touchCalX0, touchCalY0, touchCalX1, touchCalY1 = 25, 235, 460, 25
	default:
		// Unknown model - try 180° as safest default for landscape screens
		displayRotation = 180
		touchCalX0, touchCalY0, touchCalX1, touchCalY1 = 25, 235, 460, 25
		log.Printf("WARN: Unknown model '%s', defaulting to 180° rotation", model)
	}
	modelCode = model

	// Manual override via environment variable for displayRotation
	if v := os.Getenv("BEDMESH_ROTATE"); v != "" {
		if r, err := strconv.Atoi(v); err == nil {
			displayRotation = r
			log.Printf("Display rotation override: %d°", displayRotation)
		}
	}

	log.Printf("Display rotation: %d° (model=%s)", displayRotation, modelCode)
}

// --- Logging ---

func setupLogging() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
}

// --- Framebuffer ---

func initFramebuffer() (*os.File, error) {
	fb, err := os.OpenFile(fbDevice, os.O_RDWR, 0660)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", fbDevice, err)
	}

	// Read screen dimensions from sysfs
	if data, err := os.ReadFile("/sys/class/graphics/fb0/virtual_size"); err == nil {
		parts := strings.Split(strings.TrimSpace(string(data)), ",")
		if len(parts) == 2 {
			if w, e := strconv.Atoi(parts[0]); e == nil && w > 0 {
				fbW = w
				screenW = w
			}
			if h, e := strconv.Atoi(parts[1]); e == nil && h > 0 {
				fbH = h
				screenH = h
			}
		}
	}

	if displayRotation == 90 || displayRotation == 270 {
		screenW, screenH = fbH, fbW
	}
	log.Printf("Screen: fb=%dx%d logical=%dx%d", fbW, fbH, screenW, screenH)

	size := fbW * fbH * 4 // BGRA32
	fbMem, err = syscall.Mmap(int(fb.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		fb.Close()
		return nil, fmt.Errorf("mmap failed: %w", err)
	}

	// Backup current screen content so we can restore on exit
	fbBackup = make([]byte, len(fbMem))
	copy(fbBackup, fbMem)
	log.Println("Framebuffer initialized, backup saved")

	return fb, nil
}

func restoreFramebuffer() {
	if fbBackup != nil && fbMem != nil {
		copy(fbMem, fbBackup)
		log.Println("Framebuffer restored from backup")
	}
}

func closeFramebuffer(fb *os.File) {
	if fbMem != nil {
		syscall.Munmap(fbMem)
		fbMem = nil
	}
	if fb != nil {
		fb.Close()
	}
}

func setPixel(x, y int, c color.RGBA) {
	if x < 0 || x >= screenW || y < 0 || y >= screenH {
		return
	}
	// Apply display rotation
	px, py := x, y
	switch displayRotation {
	case 90:
		px = fbW - 1 - y
		py = x
	case 180:
		px = fbW - 1 - x
		py = fbH - 1 - y
	case 270:
		px = y
		py = fbH - 1 - x
	}
	if px < 0 || px >= fbW || py < 0 || py >= fbH {
		return
	}
	off := (py*fbW + px) * 4
	fbMem[off+0] = c.B
	fbMem[off+1] = c.G
	fbMem[off+2] = c.R
	fbMem[off+3] = c.A
}

func drawRect(x, y, w, h int, c color.RGBA) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			setPixel(col, row, c)
		}
	}
}

func drawRectOutline(x, y, w, h int, c color.RGBA) {
	for col := x; col < x+w; col++ {
		setPixel(col, y, c)
		setPixel(col, y+h-1, c)
	}
	for row := y; row < y+h; row++ {
		setPixel(x, row, c)
		setPixel(x+w-1, row, c)
	}
}

// drawRoundedRect draws a filled rectangle with rounded corners
func drawRoundedRect(x, y, w, h, r int, c color.RGBA) {
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	// Center body
	drawRect(x+r, y, w-2*r, h, c)
	// Left strip
	drawRect(x, y+r, r, h-2*r, c)
	// Right strip
	drawRect(x+w-r, y+r, r, h-2*r, c)
	// Four corner arcs (filled)
	for cy := 0; cy < r; cy++ {
		for cx := 0; cx < r; cx++ {
			if cx*cx+cy*cy <= r*r {
				// Top-left
				setPixel(x+r-1-cx, y+r-1-cy, c)
				// Top-right
				setPixel(x+w-r+cx, y+r-1-cy, c)
				// Bottom-left
				setPixel(x+r-1-cx, y+h-r+cy, c)
				// Bottom-right
				setPixel(x+w-r+cx, y+h-r+cy, c)
			}
		}
	}
}

// drawRoundedRectOutline draws just the border of a rounded rectangle
func drawRoundedRectOutline(x, y, w, h, r int, c color.RGBA) {
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	// Top edge
	for col := x + r; col < x+w-r; col++ {
		setPixel(col, y, c)
	}
	// Bottom edge
	for col := x + r; col < x+w-r; col++ {
		setPixel(col, y+h-1, c)
	}
	// Left edge
	for row := y + r; row < y+h-r; row++ {
		setPixel(x, row, c)
	}
	// Right edge
	for row := y + r; row < y+h-r; row++ {
		setPixel(x+w-1, row, c)
	}
	// Corner arcs (Bresenham circle outline)
	px, py := r, 0
	d := 1 - r
	for px >= py {
		// Top-left corner
		setPixel(x+r-px, y+r-py, c)
		setPixel(x+r-py, y+r-px, c)
		// Top-right corner
		setPixel(x+w-1-r+px, y+r-py, c)
		setPixel(x+w-1-r+py, y+r-px, c)
		// Bottom-left corner
		setPixel(x+r-px, y+h-1-r+py, c)
		setPixel(x+r-py, y+h-1-r+px, c)
		// Bottom-right corner
		setPixel(x+w-1-r+px, y+h-1-r+py, c)
		setPixel(x+w-1-r+py, y+h-1-r+px, c)
		py++
		if d < 0 {
			d += 2*py + 1
		} else {
			px--
			d += 2*(py-px) + 1
		}
	}
}

// Simple 5x7 bitmap font for digits, minus, dot, space
var font5x7 = map[byte][7]uint8{
	'0': {0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E},
	'1': {0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E},
	'2': {0x0E, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1F},
	'3': {0x1F, 0x02, 0x04, 0x02, 0x01, 0x11, 0x0E},
	'4': {0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02},
	'5': {0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E},
	'6': {0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E},
	'7': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08},
	'8': {0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
	'9': {0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C},
	'-': {0x00, 0x00, 0x00, 0x1F, 0x00, 0x00, 0x00},
	'.': {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04},
	' ': {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	':': {0x00, 0x00, 0x04, 0x00, 0x04, 0x00, 0x00},
	'A': {0x0E, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11},
	'B': {0x1E, 0x11, 0x11, 0x1E, 0x11, 0x11, 0x1E},
	'D': {0x1E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x1E},
	'E': {0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x1F},
	'M': {0x11, 0x1B, 0x15, 0x15, 0x11, 0x11, 0x11},
	'U': {0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
	'a': {0x00, 0x00, 0x0E, 0x01, 0x0F, 0x11, 0x0F},
	'd': {0x01, 0x01, 0x0D, 0x13, 0x11, 0x11, 0x0F},
	'e': {0x00, 0x00, 0x0E, 0x11, 0x1F, 0x10, 0x0E},
	'g': {0x00, 0x0F, 0x11, 0x0F, 0x01, 0x11, 0x0E},
	'h': {0x10, 0x10, 0x16, 0x19, 0x11, 0x11, 0x11},
	'i': {0x04, 0x00, 0x0C, 0x04, 0x04, 0x04, 0x0E},
	'l': {0x0C, 0x04, 0x04, 0x04, 0x04, 0x04, 0x0E},
	'n': {0x00, 0x00, 0x16, 0x19, 0x11, 0x11, 0x11},
	'o': {0x00, 0x00, 0x0E, 0x11, 0x11, 0x11, 0x0E},
	's': {0x00, 0x00, 0x0F, 0x10, 0x0E, 0x01, 0x1E},
	't': {0x08, 0x08, 0x1C, 0x08, 0x08, 0x09, 0x06},
	'v': {0x00, 0x00, 0x11, 0x11, 0x11, 0x0A, 0x04},
	'w': {0x00, 0x00, 0x11, 0x11, 0x15, 0x1F, 0x0A},
	'x': {0x00, 0x00, 0x11, 0x0A, 0x04, 0x0A, 0x11},
}

func drawChar(x, y int, ch byte, c color.RGBA, scale int) {
	glyph, ok := font5x7[ch]
	if !ok {
		return
	}
	for row := 0; row < 7; row++ {
		for col := 0; col < 5; col++ {
			if glyph[row]&(1<<uint(4-col)) != 0 {
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						setPixel(x+col*scale+sx, y+row*scale+sy, c)
					}
				}
			}
		}
	}
}

func drawText(x, y int, text string, c color.RGBA, scale int) {
	cx := x
	for i := 0; i < len(text); i++ {
		drawChar(cx, y, text[i], c, scale)
		cx += 6 * scale // 5 pixels + 1 gap, scaled
	}
}

func drawCharVertical(x, y int, ch byte, c color.RGBA, scale int) {
	glyph, ok := font5x7[ch]
	if !ok {
		return
	}
	for row := 0; row < 7; row++ {
		for col := 0; col < 5; col++ {
			if glyph[row]&(1<<uint(4-col)) != 0 {
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						setPixel(x+(6-row)*scale+sx, y+col*scale+sy, c)
					}
				}
			}
		}
	}
}

func drawTextVertical(x, y int, text string, c color.RGBA, scale int) {
	cy := y
	for i := 0; i < len(text); i++ {
		drawCharVertical(x, cy, text[i], c, scale)
		cy += 6 * scale // 5 pixels height + 1 gap
	}
}

// --- Mesh Parsing (JSON format) ---

func parseMesh(path string) ([][]float64, int, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("ERROR: cannot read config %s: %v", path, err)
		return nil, 0, 0
	}

	var cfg PrinterConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("ERROR: cannot parse JSON: %v", err)
		return nil, 0, 0
	}

	if cfg.BedMesh == nil {
		log.Println("ERROR: no 'bed_mesh default' section found")
		return nil, 0, 0
	}

	pointsStr, ok := cfg.BedMesh["points"]
	if !ok || pointsStr == "" {
		log.Println("ERROR: no 'points' key in bed_mesh")
		return nil, 0, 0
	}

	xCount := 0
	yCount := 0
	if v, ok := cfg.BedMesh["x_count"]; ok {
		xCount, _ = strconv.Atoi(v)
	}
	if v, ok := cfg.BedMesh["y_count"]; ok {
		yCount, _ = strconv.Atoi(v)
	}

	// Points are lines separated by \n, values separated by ", "
	lines := strings.Split(pointsStr, "\n")
	var mesh [][]float64
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		var row []float64
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			val, err := strconv.ParseFloat(p, 64)
			if err != nil {
				log.Printf("WARN: cannot parse float '%s': %v", p, err)
				continue
			}
			row = append(row, val)
		}
		if len(row) > 0 {
			mesh = append(mesh, row)
		}
	}

	if len(mesh) == 0 {
		log.Println("ERROR: parsed 0 mesh rows")
		return nil, 0, 0
	}

	log.Printf("Parsed mesh: %d rows x %d cols (x_count=%d, y_count=%d)",
		len(mesh), len(mesh[0]), xCount, yCount)
	return mesh, xCount, yCount
}

// --- Heatmap Color ---

func getColorForZ(z, minZ, maxZ float64) color.RGBA {
	if maxZ-minZ < 0.0001 {
		return color.RGBA{R: 0, G: 200, B: 0, A: 255} // flat = green
	}
	norm := (z - minZ) / (maxZ - minZ) // 0.0 .. 1.0

	// Blue (low) -> Green (mid) -> Red (high)
	var r, g, b uint8
	if norm < 0.5 {
		t := norm * 2.0
		r = 0
		g = uint8(255 * t)
		b = uint8(255 * (1 - t))
	} else {
		t := (norm - 0.5) * 2.0
		r = uint8(255 * t)
		g = uint8(255 * (1 - t))
		b = 0
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

// --- Drawing ---

func drawUI(mesh [][]float64) {
	white := color.RGBA{255, 255, 255, 255}
	bgColor := color.RGBA{30, 30, 30, 255}

	// Clear screen
	drawRect(0, 0, screenW, screenH, bgColor)

	// Title
	if screenW < 300 {
		drawText(8, 12, "Bed Mesh", white, 2)
	} else {
		drawText(screenW/2-60, 5, "Bed Mesh", white, 2)
	}

	// Draw buttons (rounded corners, dark bg, subtle border)
	for _, b := range buttons {
		radius := b.Radius
		if radius <= 0 {
			radius = 4
		}
		drawRoundedRect(b.X, b.Y, b.W, b.H, radius, b.BgColor)
		borderColor := b.BorderColor
		if borderColor.A == 0 {
			borderColor = color.RGBA{100, 100, 100, 255}
		}
		drawRoundedRectOutline(b.X, b.Y, b.W, b.H, radius, borderColor)
		// Center label in button (scale 2)
		textW := len(b.Label) * 6 * 2
		tx := b.X + (b.W-textW)/2
		ty := b.Y + (b.H-7*2)/2
		drawText(tx, ty, b.Label, white, 2)
	}

	if mesh == nil || len(mesh) == 0 {
		drawText(40, screenH/2, "no data", color.RGBA{255, 80, 80, 255}, 2)
		return
	}

	rows := len(mesh)
	cols := len(mesh[0])

	// Find min/max/average
	minZ, maxZ := mesh[0][0], mesh[0][0]
	sumZ := 0.0
	count := 0
	for _, row := range mesh {
		for _, z := range row {
			if z < minZ {
				minZ = z
			}
			if z > maxZ {
				maxZ = z
			}
			sumZ += z
			count++
		}
	}
	avgZ := sumZ / float64(count)
	deltaZ := maxZ - minZ

	// Layout: title bar + info bar + mesh + bottom stats
	infoBarY := 30
	infoBarH := 22
	margin := 15
	fs := 2 // font scale for info/stats

	if screenW < 300 {
		infoBarY = 50 // Push down to clear the 45px tall Exit button
		infoBarH = 14
		margin = 10
		fs = 1
	}

	topY := infoBarY + infoBarH + 4
	bottomBarH := 26
	if screenW < 300 {
		bottomBarH = 20
	}
	availW := screenW - margin*2
	availH := screenH - topY - bottomBarH - 4

	cellW := availW / cols
	cellH := availH / rows

	// Ensure cells are not too small
	if cellW < 4 {
		cellW = 4
	}
	if cellH < 4 {
		cellH = 4
	}

	gap := 1
	if cellW > 20 && cellH > 20 {
		gap = 2
	}

	// --- INFO BAR (above mesh) ---
	yellow := color.RGBA{255, 220, 50, 255}
	cyan := color.RGBA{80, 220, 255, 255}
	lightGray := color.RGBA{180, 180, 180, 255}

	cw := 6 * fs // character width at this scale

	gridStr := fmt.Sprintf("%dx%d", cols, rows)
	drawText(margin, infoBarY, gridStr, cyan, fs)

	minStr := fmt.Sprintf("Min:%.3f", minZ)
	gapBetweenInfo := 12
	if screenW < 300 {
		gapBetweenInfo = 8
	}
	drawText(margin+len(gridStr)*cw+gapBetweenInfo, infoBarY, minStr, color.RGBA{80, 80, 255, 255}, fs)

	maxStr := fmt.Sprintf("Max:%.3f", maxZ)
	drawText(margin+len(gridStr)*cw+gapBetweenInfo+len(minStr)*cw+gapBetweenInfo, infoBarY, maxStr, color.RGBA{255, 80, 80, 255}, fs)

	// --- MESH GRID ---
	// Draw with Y flipped: row 0 (Y_min = front of bed) at BOTTOM,
	// last row (Y_max = back of bed) at TOP
	for i, row := range mesh {
		for j, z := range row {
			x := margin + j*cellW
			y := topY + (rows-1-i)*cellH // flip Y axis
			c := getColorForZ(z, minZ, maxZ)
			drawRect(x, y, cellW-gap, cellH-gap, c)

			// Draw Z value text centered in cell
			label := fmt.Sprintf("%.3f", z)

			// Decide if we should draw vertically or horizontally
			// Use vertical if cell is too narrow for horizontal text
			if cellW < 42 {
				maxChars := (cellH - 4) / 6
				if maxChars > 0 && len(label) > maxChars {
					label = label[:maxChars]
				}
				if maxChars >= 4 {
					textH := len(label) * 6
					textW := 7
					tx := x + (cellW-gap-textW)/2
					ty := y + (cellH-gap-textH)/2
					drawTextVertical(tx, ty, label, white, 1)
				}
			} else {
				if cellH >= 12 {
					maxChars := (cellW - 4) / 6
					if maxChars > 0 && len(label) > maxChars {
						label = label[:maxChars]
					}
					if maxChars >= 4 {
						textW := len(label) * 6
						tx := x + (cellW-gap-textW)/2
						ty := y + (cellH-gap-7)/2
						drawText(tx, ty, label, white, 1)
					}
				}
			}
		}
	}

	// --- BOTTOM BAR (below mesh) ---
	bottomY := screenH - bottomBarH

	// Delta (spread)
	deltaStr := fmt.Sprintf("Delta:%.3f", deltaZ)
	drawText(margin, bottomY, deltaStr, yellow, fs)

	// Average
	avgStr := fmt.Sprintf("Avg:%.3f", avgZ)
	gapBetweenBottom := 12
	if screenW < 300 {
		gapBetweenBottom = 8
	}
	drawText(margin+len(deltaStr)*cw+gapBetweenBottom, bottomY, avgStr, lightGray, fs)

	// Color legend gradient bar
	legendW := 100
	if screenW < 300 {
		legendW = 70
	}
	legendX := screenW - margin - legendW
	legendH := 12
	legendY := bottomY + 3
	if screenW < 300 {
		legendY = bottomY + 1
		legendH = 8
	}
	for i := 0; i < legendW; i++ {
		norm := float64(i) / float64(legendW-1)
		fakeZ := minZ + norm*deltaZ
		c := getColorForZ(fakeZ, minZ, maxZ)
		drawRect(legendX+i, legendY, 1, legendH, c)
	}
	// Legend labels
	lowTextX := legendX - 30
	hiTextX := legendX + legendW + 4
	if screenW < 300 {
		lowTextX = legendX - 22
		hiTextX = legendX + legendW + 3
	}
	drawText(lowTextX, legendY-1, "low", color.RGBA{80, 80, 255, 255}, 1)
	drawText(hiTextX, legendY-1, "hi", color.RGBA{255, 80, 80, 255}, 1)

	log.Printf("UI drawn: %dx%d mesh, Z range [%.4f, %.4f], delta=%.4f, avg=%.4f",
		rows, cols, minZ, maxZ, deltaZ, avgZ)
}

// --- Touch Input ---

func handleInput() {
	files, err := os.ReadDir(inputDir)
	if err != nil {
		log.Printf("WARN: cannot read %s: %v", inputDir, err)
		return
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "event") {
			path := filepath.Join(inputDir, f.Name())
			go readInputEvents(path)
			log.Printf("Listening on %s", path)
		}
	}
}

// EVIOCGRAB ioctl to exclusively grab/release an input device
// This prevents K3SysUi from receiving touch events while our app runs
const EVIOCGRAB = 0x40044590

var grabbedFiles []*os.File // track grabbed files for cleanup

func grabInput(f *os.File) {
	val := int32(1)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), EVIOCGRAB, uintptr(unsafe.Pointer(&val)))
	if errno != 0 {
		log.Printf("WARN: EVIOCGRAB failed on %s: %v", f.Name(), errno)
	} else {
		log.Printf("Grabbed input device %s exclusively", f.Name())
		mu.Lock()
		grabbedFiles = append(grabbedFiles, f)
		mu.Unlock()
	}
}

func releaseAllInputs() {
	mu.Lock()
	defer mu.Unlock()
	val := int32(0)
	for _, f := range grabbedFiles {
		syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), EVIOCGRAB, uintptr(unsafe.Pointer(&val)))
		log.Printf("Released input device %s", f.Name())
	}
	grabbedFiles = nil
}

func readInputEvents(path string) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("WARN: cannot open %s: %v", path, err)
		return
	}
	defer f.Close()

	// Grab the device exclusively so K3SysUi doesn't get events
	grabInput(f)

	// Try to query touch limits
	if infoX, err := getAbsInfo(f.Fd(), ABS_X); err == nil && infoX.Maximum > 0 {
		if infoY, err := getAbsInfo(f.Fd(), ABS_Y); err == nil && infoY.Maximum > 0 {
			mu.Lock()
			touchMaxX = int(infoX.Maximum)
			touchMaxY = int(infoY.Maximum)
			hasTouchLimits = true
			
			// Detect if coordinates already map to the screen's logical portrait orientation
			if (touchMaxX == screenW-1 && touchMaxY == screenH-1) || (touchMaxX == screenW && touchMaxY == screenH) {
				touchIsLogical = true
				log.Printf("Touch device %s is ALREADY in logical orientation: %dx%d", path, touchMaxX, touchMaxY)
			} else {
				touchIsLogical = false
				log.Printf("Touch device %s raw limits: X[0..%d], Y[0..%d]", path, touchMaxX, touchMaxY)
			}
			mu.Unlock()
		}
	}

	var event InputEvent
	var lastX, lastY int

	for {
		if atomic.LoadInt32(&exitFlag) != 0 {
			return
		}
		err := binary.Read(f, binary.LittleEndian, &event)
		if err != nil {
			return
		}

		switch event.Type {
		case EV_ABS:
			switch event.Code {
			case ABS_X, ABS_MT_POSITION_X:
				lastX = int(event.Value)
			case ABS_Y, ABS_MT_POSITION_Y:
				lastY = int(event.Value)
			}
		case EV_KEY:
			if event.Code == BTN_TOUCH && event.Value == 0 { // release
				tx, ty := scaleTouch(lastX, lastY)
				log.Printf("Touch at raw=(%d,%d) scaled=(%d,%d)", lastX, lastY, tx, ty)
				mu.Lock()
				for _, b := range buttons {
					if tx >= b.X && tx < b.X+b.W && ty >= b.Y && ty < b.Y+b.H {
						log.Printf("Button '%s' clicked", b.Label)
						b.OnClick()
					}
				}
				mu.Unlock()
			}
		}
	}
}

func scaleTouch(rawX, rawY int) (int, int) {
	mu.Lock()
	isLogical := touchIsLogical
	limitX, limitY := touchMaxX, touchMaxY
	calX0, calY0, calX1, calY1 := touchCalX0, touchCalY0, touchCalX1, touchCalY1
	mu.Unlock()

	// If the touch driver reports logical coordinates already
	if isLogical {
		x := (rawX * screenW) / (limitX + 1)
		y := (rawY * screenH) / (limitY + 1)
		return x, y
	}

	// Apply touch calibration to get physical landscape coordinates
	var x, y int
	if calX1 != calX0 {
		x = ((rawX - calX0) * fbW) / (calX1 - calX0)
	} else {
		x = rawX
	}
	if calY1 != calY0 {
		y = ((rawY - calY0) * fbH) / (calY1 - calY0)
	} else {
		y = rawY
	}

	// Constrain to physical screen bounds
	if x < 0 {
		x = 0
	} else if x >= fbW {
		x = fbW - 1
	}
	if y < 0 {
		y = 0
	} else if y >= fbH {
		y = fbH - 1
	}

	// Apply display rotation to physical landscape coordinates
	switch displayRotation {
	case 90:
		x, y = y, fbW-1-x
	case 180:
		x = fbW - 1 - x
		y = fbH - 1 - y
	case 270:
		x, y = fbH-1-y, x
	}
	return x, y
}

// --- Main ---

func main() {
	setupLogging()
	log.Printf("=== Bed Mesh Visualizer v%s starting ===", getVersion())
	detectRotation()

	// Signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down", sig)
		atomic.StoreInt32(&exitFlag, 1)
	}()

	// Watchdog timer - auto-exit after watchdogSec
	go func() {
		time.Sleep(time.Duration(watchdogSec) * time.Second)
		log.Printf("Watchdog: %d seconds elapsed, auto-exiting", watchdogSec)
		atomic.StoreInt32(&exitFlag, 1)
	}()

	// Init framebuffer
	fb, err := initFramebuffer()
	if err != nil {
		log.Fatalf("FATAL: framebuffer init failed: %v", err)
	}
	defer func() {
		restoreFramebuffer()
		closeFramebuffer(fb)
	}()

	// Setup buttons (larger for touchscreen)
	btnW := 140
	btnH := 45
	buttons = []Button{
		{
			X: screenW - btnW - 8, Y: 0, W: btnW, H: btnH,
			Label:       "Exit",
			BgColor:     color.RGBA{60, 60, 60, 255},
			BorderColor: color.RGBA{120, 120, 120, 255},
			Radius:      6,
			OnClick: func() {
				atomic.StoreInt32(&exitFlag, 1)
			},
		},
	}

	// Parse mesh
	mesh, _, _ := parseMesh(cfgPath)

	// Draw everything
	drawUI(mesh)

	// Start touch listeners
	handleInput()

	// Main loop
	for atomic.LoadInt32(&exitFlag) == 0 {
		time.Sleep(100 * time.Millisecond)
	}

	log.Println("Exiting")
	// IMPORTANT: os.Exit() skips deferred functions!
	// We must clean up manually here.
	releaseAllInputs()
	restoreFramebuffer()
	closeFramebuffer(fb)
	os.Exit(0)
}

# Bed Mesh App for Anycubic Kobra printers

Visualization of the Bed Mesh on the printer's screen. Reads data from `printer_mutable.cfg` and displays a colorful heatmap directly on the screen.

<img width="800" height="561" alt="photo" src="https://github.com/user-attachments/assets/8920b4ee-913b-49ba-8b0a-a113fc6c0273" />

## Supported Models

| Printer Model | Tested     |
|---------------|------------|
| Kobra 3 V2    | ✅         |
| Kobra 3 Max   | ✅ 2.5.0.8 |
| Kobra S1      | ✅ 2.7.0.9 |
| Kobra S1 Max  | ❓         |

## Grid Orientation

The visualization on the screen **always corresponds directly to the physical X/Y coordinate system of the bed** (top-down view):
- **Top** of the screen = **Back** of the bed (Y-max)
- **Bottom** of the screen = **Front** of the bed (Y-min)
- **Left** side = **Left** of the bed (X-min)
- **Right** side = **Right** of the bed (X-max)

> [!NOTE]
> On vertical/portrait displays (e.g., Kobra 3 V2), the Z-value text inside the grid cells is rendered vertically (rotated 90°) to fit inside the narrow columns. **The grid itself is NOT rotated or tilted**—its physical orientation remains aligned with the Cartesian XY coordinates of the bed.

### Understanding Z-Values & How to Level

The heatmap uses colors to represent height offsets relative to the ideal center Z-height:
- **Positive Z-value (Red / Warm colors)**: The bed is **too high** (closer to the nozzle) at this point.
- **Negative Z-value (Blue / Cool colors)**: The bed is **too low** (further from the nozzle) at this point.
- **Near-zero Z-value (Green / Neutral)**: The bed is **level** and perfect.

## Download

If you prefer not to compile the project yourself, you can download the ready-to-use `.swu` packages from the [Releases](https://github.com/Aroslavv/BedMeshApp/releases/latest) page.

## Execution

### SWU - one-time execution from a USB drive

1. Download the appropriate `.swu` package for your printer from the [Releases](https://github.com/Aroslavv/BedMeshApp/releases/latest) page (or build it yourself)
2. Copy the downloaded `.swu` file to the `aGVscF9zb3Nf` folder on the USB drive ( formatted as FAT32 ) and rename it to `update.swu`
3. Insert the USB drive into the printer
4. The firmware will automatically recognize the file and launch the viewer
5. After clicking `Exit` (or after 5 minutes), the viewer will close


Available packages per model:
- `bedmesh-swu-k3v2.swu` - Kobra 3 V2
- `bedmesh-swu-k3m.swu` - Kobra 3 Max
- `bedmesh-swu-ks1.swu` - Kobra S1
- `bedmesh-swu-ks1m.swu` - Kobra S1 Max

## Compilation

### Windows
```cmd
cd app

:: Only binary
build.bat

:: Binary + SWU packages (requires 7-Zip)
build.bat swu
```

Requirements:
- **Go** >= 1.21 ([go.dev/dl](https://go.dev/dl/))
- **7-Zip** in `C:\Program Files\7-Zip` (Windows, only for SWU mode)


## Debugging and Logs

### Log files
- **SWU**: `/tmp/bedmesh/bedmesh.log` (temporary, disappears after restart)

### Testing on the printer via SSH
```bash
scp bedmesh_viewer root@PRINTER_IP:/tmp/
ssh root@PRINTER_IP
chmod +x /tmp/bedmesh_viewer
/tmp/bedmesh_viewer
```

## Acknowledgments

Special thanks to the author of [Rinkhals](https://github.com/jbatonnet/Rinkhals). This tool would not have been possible without his foundational work.


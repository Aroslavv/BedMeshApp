# Bed Mesh App for Anycubic Kobra printers

Visualization of the Bed Mesh on the printer's screen. Reads data from `printer_mutable.cfg` and displays a colorful heatmap directly on the framebuffer.

## Supported Models

| Printer Model | Tested     |
|---------------|------------|
| Kobra 3 V2    | ❓         |
| Kobra 3 Max   | ❓         |
| Kobra S1      | ✅ 2.7.0.9 |
| Kobra S1 Max  | ❓         |

## Download (Pre-built Assets)

If you prefer not to compile the project yourself, you can download the ready-to-use `.swu` packages directly from the `assets/` folder in this repository.

## Installation Methods

### SWU - one-time execution from a USB drive

The simplest way - requires no printer modifications.

1. Build the SWU package (see [Compilation](#compilation)) or use pre-built assets
2. Copy the appropriate `.swu` file to the `aGVscF9zb3Nf` folder on the USB drive as `update.swu`
3. Insert the USB drive into the printer
4. The firmware will automatically recognize the file and launch the viewer
5. After clicking `Exit` (or after 5 minutes), the viewer will close
6. **Zero traces left** - nothing is installed permanently

SWU files per model:
| File                                                  | Models       |
|-------------------------------------------------------|--------------|
| [`bedmesh-swu-k3v2.swu`](assets/bedmesh-swu-k3v2.swu) | Kobra 3 V2   |
| [`bedmesh-swu-k3m.swu`](assets/bedmesh-swu-k3m.swu)   | Kobra 3 Max  |
| [`bedmesh-swu-ks1.swu`](assets/bedmesh-swu-ks1.swu)   | Kobra S1     |
| [`bedmesh-swu-ks1m.swu`](assets/bedmesh-swu-ks1m.swu) | Kobra S1 Max |

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

## Security

### Watchdog Timer
The application automatically closes after **5 minutes** of inactivity (`watchdogSec` constant).

### Screen Restoration
At startup, the application saves the current framebuffer content. Upon closing, it restores it, so the screen returns to its state before execution.

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


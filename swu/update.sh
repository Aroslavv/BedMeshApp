#!/bin/sh
# ============================================
#  Bed Mesh Viewer - SWU One-Shot Launcher
# ============================================
# This script is executed automatically by the Kobra firmware
# when a USB drive with the .swu file is inserted.
#
# It runs the bedmesh_viewer once from /tmp (RAM),
# leaves NO permanent changes on the printer.
# ============================================

function beep() {
    echo 1 > /sys/class/pwm/pwmchip0/pwm0/enable
    usleep $(($1 * 1000))
    echo 0 > /sys/class/pwm/pwmchip0/pwm0/enable
}

UPDATE_PATH="/useremain/update_swu"
TMP_PATH="/tmp/bedmesh"
LOG_FILE="/tmp/bedmesh/bedmesh.log"

echo "$(date) [swu] Bed Mesh Viewer SWU starting..." > $LOG_FILE

# Create temp directory
mkdir -p $TMP_PATH

# Copy binary to /tmp (RAM)
cp $UPDATE_PATH/bedmesh_viewer $TMP_PATH/bedmesh_viewer
chmod +x $TMP_PATH/bedmesh_viewer

# Clean up SWU source files (firmware expects this)
rm -rf $UPDATE_PATH

echo "$(date) [swu] Binary copied to $TMP_PATH" >> $LOG_FILE

# Detect model for rotation
if [ -f /userdata/app/gk/config/api.cfg ]; then
    MODEL_ID=$(cat /userdata/app/gk/config/api.cfg | sed -nr 's/.*"modelId"\s*:\s*"([0-9]+)".*/\1/p')
    case "$MODEL_ID" in
        20025) export KOBRA_MODEL_CODE="KS1" ;;
        20029) export KOBRA_MODEL_CODE="KS1M" ;;
        20026) export KOBRA_MODEL_CODE="K3M" ;;
        20027) export KOBRA_MODEL_CODE="K3V2" ;;
    esac
    echo "$(date) [swu] Detected model: $KOBRA_MODEL_CODE (ID=$MODEL_ID)" >> $LOG_FILE
fi

# Signal readiness
beep 300

echo "$(date) [swu] Launching bedmesh_viewer..." >> $LOG_FILE

# Run the viewer (blocks until user clicks Exit or watchdog expires)
$TMP_PATH/bedmesh_viewer >> $LOG_FILE 2>&1
EXIT_CODE=$?

echo "$(date) [swu] Viewer exited with code $EXIT_CODE" >> $LOG_FILE

# Cleanup
rm -rf $TMP_PATH/bedmesh_viewer

# Signal completion
beep 200
usleep 200000
beep 200

echo "$(date) [swu] Done." >> $LOG_FILE

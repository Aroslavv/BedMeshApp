#!/bin/bash
# ============================================
#  Bed Mesh Viewer - Universal Build Script
# ============================================
#  Usage:
#    ./build.sh           - Compile binary only
#    ./build.sh swu       - Compile + create SWU packages for all models
#    ./build.sh swu       - Compile + create SWU packages for all models
#
#  Requirements:
#    - Go >= 1.21
#    - zip (for SWU mode)
#    - tar, gzip, md5sum (standard Linux tools)
# ============================================

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_MODE="${1:-binary}"

echo "=== Bed Mesh Viewer Build (Linux) ==="
echo "Mode: $BUILD_MODE"
echo ""

# --- Find Go ---
GO_BIN="go"
if ! command -v go &>/dev/null; then
    if [ -x "/usr/local/go/bin/go" ]; then
        GO_BIN="/usr/local/go/bin/go"
    else
        echo "ERROR: Go is not installed!"
        echo "Download from: https://go.dev/dl/"
        exit 1
    fi
fi

# --- Clean and create assets directory ---
rm -rf "$SCRIPT_DIR/../assets"
mkdir -p "$SCRIPT_DIR/../assets"

# --- Compile ---
echo "[1/3] Compiling bedmesh_viewer..."

cd "$SCRIPT_DIR"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    "$GO_BIN" build -ldflags="-s -w" -o "$SCRIPT_DIR/../assets/bedmesh_viewer" main.go

echo "      Done! Size: $(stat -c%s "$SCRIPT_DIR/../assets/bedmesh_viewer" 2>/dev/null || stat -f%z "$SCRIPT_DIR/../assets/bedmesh_viewer") bytes"

# --- Helper: build SWU for a model ---
build_swu_for_model() {
    local MODEL_CODE="$1"
    local PASSWORD="$2"
    local OUTPUT_NAME="$3"
    local STAGE_DIR="$4"

    echo "      Packing: $OUTPUT_NAME"

    cd "$STAGE_DIR"
    zip -0 -P "$PASSWORD" -r "$SCRIPT_DIR/../assets/$OUTPUT_NAME" update_swu >/dev/null
}

# --- SWU Mode ---
if [ "$BUILD_MODE" = "swu" ]; then
    echo ""
    echo "[2/3] Creating SWU packages..."

    if ! command -v zip &>/dev/null; then
        echo "ERROR: 'zip' is not installed!"
        echo "Install with: sudo apt install zip"
        exit 1
    fi

    # Create staging area
    STAGE=$(mktemp -d)
    mkdir -p "$STAGE/update_swu"

    # Copy files
    cp -f "$SCRIPT_DIR/../assets/bedmesh_viewer" "$STAGE/update_swu/bedmesh_viewer"
    cp -f "$SCRIPT_DIR/../swu/update.sh" "$STAGE/update_swu/update.sh"

    # Create setup.tar.gz
    cd "$STAGE/update_swu"
    tar -cf "$STAGE/setup.tar" .
    gzip "$STAGE/setup.tar"
    mv "$STAGE/setup.tar.gz" "$STAGE/update_swu/setup.tar.gz"

    # Calculate MD5
    md5sum "$STAGE/update_swu/setup.tar.gz" | awk '{ print $1 }' > "$STAGE/update_swu/setup.tar.gz.md5"

    # Remove source files (keep only tar.gz and md5)
    rm -f "$STAGE/update_swu/bedmesh_viewer"
    rm -f "$STAGE/update_swu/update.sh"

    # Build SWU for each model
    # K3V2
    build_swu_for_model "K3V2" "U2FsdGVkX19deTfqpXHZnB5GeyQ/dtlbHjkUnwgCi+w=" "bedmesh-swu-k3v2.swu" "$STAGE"

    # K3M
    build_swu_for_model "K3M" "4DKXtEGStWHpPgZm8Xna9qluzAI8VJzpOsEIgd8brTLiXs8fLSu3vRx8o7fMf4h6" "bedmesh-swu-k3m.swu" "$STAGE"

    # KS1
    build_swu_for_model "KS1" "U2FsdGVkX1+lG6cHmshPLI/LaQr9cZCjA8HZt6Y8qmbB7riY" "bedmesh-swu-ks1.swu" "$STAGE"

    # KS1M
    build_swu_for_model "KS1M" "U2FsdGVkX1+lG6cHmshPLI/LaQr9cZCjA8HZt6Y8qmbB7riY" "bedmesh-swu-ks1m.swu" "$STAGE"

    # Cleanup
    rm -rf "$STAGE"

    echo "      SWU packages ready in assets/"
fi

echo ""
echo "=== Build finished ==="
echo ""
echo "Files in assets/ directory:"
ls -la "$SCRIPT_DIR/../assets/" 2>/dev/null || true

#!/usr/bin/env bash
set -euo pipefail

# File4Base Desktop Build & Packaging Script (macOS / Windows / Linux)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
CLIENT_DIR="${ROOT_DIR}/client"
DIST_DIR="${ROOT_DIR}/dist"

mkdir -p "${DIST_DIR}"

echo "=========================================="
echo "   File4Base Desktop Client Builder       "
echo "=========================================="

OS="$(uname -s)"
case "${OS}" in
    Darwin*)
        PLATFORM="macos"
        ;;
    Linux*)
        PLATFORM="linux"
        ;;
    MINGW*|MSYS*|CYGWIN*)
        PLATFORM="windows"
        ;;
    *)
        PLATFORM="unknown"
        ;;
esac

echo "Detected host OS: ${OS} (${PLATFORM})"

build_macos() {
    echo "==> Building for macOS..."
    if ! command -v xcodebuild >/dev/null 2>&1; then
        echo "Error: xcodebuild is not installed or in PATH."
        echo "Please install Xcode from the Mac App Store and run:"
        echo "  sudo xcode-select -s /Applications/Xcode.app/Contents/Developer"
        exit 1
    fi

    cd "${CLIENT_DIR}"
    flutter build macos --release
    echo "==> Packaging macOS application..."
    APP_PATH="${CLIENT_DIR}/build/macos/Build/Products/Release/File4Base.app"
    if [ -d "${APP_PATH}" ]; then
        ditto -c -k --sequesterRsrc --keepParent "${APP_PATH}" "${DIST_DIR}/File4Base-macOS-universal.zip"
        echo "Packaging complete: ${DIST_DIR}/File4Base-macOS-universal.zip"
    else
        echo "Error: File4Base.app not found at ${APP_PATH}"
        exit 1
    fi
}

build_linux() {
    echo "==> Building for Linux..."
    cd "${CLIENT_DIR}"
    flutter build linux --release
    echo "==> Packaging Linux application..."
    BUNDLE_DIR="${CLIENT_DIR}/build/linux/x64/release/bundle"
    if [ -d "${BUNDLE_DIR}" ]; then
        tar -czf "${DIST_DIR}/File4Base-Linux-x64.tar.gz" -C "${BUNDLE_DIR}" .
        echo "Packaging complete: ${DIST_DIR}/File4Base-Linux-x64.tar.gz"
    else
        echo "Error: Linux release bundle not found at ${BUNDLE_DIR}"
        exit 1
    fi
}

build_windows() {
    echo "==> Building for Windows..."
    cd "${CLIENT_DIR}"
    flutter build windows --release
    echo "==> Packaging Windows application..."
    BUNDLE_DIR="${CLIENT_DIR}/build/windows/x64/runner/Release"
    if [ -d "${BUNDLE_DIR}" ]; then
        if command -v tar >/dev/null 2>&1; then
            tar -a -c -f "${DIST_DIR}/File4Base-Windows-x64.zip" -C "${BUNDLE_DIR}" .
        else
            (cd "${BUNDLE_DIR}" && zip -r -q "${DIST_DIR}/File4Base-Windows-x64.zip" .)
        fi
        echo "Packaging complete: ${DIST_DIR}/File4Base-Windows-x64.zip"
    else
        echo "Error: Windows release bundle not found at ${BUNDLE_DIR}"
        exit 1
    fi
}

TARGET="${1:-${PLATFORM}}"

case "${TARGET}" in
    macos)
        build_macos
        ;;
    linux)
        build_linux
        ;;
    windows)
        build_windows
        ;;
    *)
        echo "Usage: $0 [macos|linux|windows]"
        exit 1
        ;;
esac

echo "Build process finished successfully."

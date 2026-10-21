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
        cp "${ROOT_DIR}/LICENSE" "${APP_PATH}/Contents/Resources/LICENSE"
        ditto -c -k --sequesterRsrc --keepParent "${APP_PATH}" "${DIST_DIR}/File4Base-macOS-universal.zip"
        "${SCRIPT_DIR}/verify_desktop_archive.sh" "${DIST_DIR}/File4Base-macOS-universal.zip"
        if command -v hdiutil >/dev/null 2>&1; then
            # Stage the .app next to a symlink to /Applications, so the mounted
            # volume shows both icons and the user installs by dragging one onto
            # the other, the way macOS users expect a .dmg to behave.
            DMG_STAGE="$(mktemp -d)"
            cp -R "${APP_PATH}" "${DMG_STAGE}/"
            ln -s /Applications "${DMG_STAGE}/Applications"
            hdiutil create -volname "File4Base" -srcfolder "${DMG_STAGE}" -ov -format UDZO "${DIST_DIR}/File4Base-macOS.dmg"
            rm -rf "${DMG_STAGE}"
            echo "macOS DMG installer created: ${DIST_DIR}/File4Base-macOS.dmg"
        fi
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
        "${SCRIPT_DIR}/verify_desktop_archive.sh" \
            "${DIST_DIR}/File4Base-Linux-x64.tar.gz" \
            "${CLIENT_DIR}/build/linux/x64/release/pdfium-src"
        if command -v dpkg-deb >/dev/null 2>&1; then
            VERSION="$(tr -d '[:space:]' < "${ROOT_DIR}/VERSION")"
            DEB_ROOT="${DIST_DIR}/deb-pkg"
            mkdir -p "${DEB_ROOT}/DEBIAN" "${DEB_ROOT}/opt/file4base" "${DEB_ROOT}/usr/bin" "${DEB_ROOT}/usr/share/applications"
            cp -r "${BUNDLE_DIR}/"* "${DEB_ROOT}/opt/file4base/"
            ln -sf /opt/file4base/file4base_client "${DEB_ROOT}/usr/bin/file4base"
            cat > "${DEB_ROOT}/DEBIAN/control" <<EOF
Package: file4base
Version: ${VERSION}
Section: database
Priority: optional
Architecture: amd64
Depends: libgtk-3-0
Maintainer: File4Base Team <support@file4base.io>
Description: File4Base Native Desktop Client
EOF
            cat > "${DEB_ROOT}/usr/share/applications/file4base.desktop" <<EOF
[Desktop Entry]
Name=File4Base
Comment=File4Base Relational Database Platform
Exec=/opt/file4base/file4base_client
Terminal=false
Type=Application
Categories=Development;Database;Office;
EOF
            dpkg-deb --build --root-owner-group "${DEB_ROOT}" "${DIST_DIR}/File4Base-Linux-x64.deb"
            rm -rf "${DEB_ROOT}"
            echo "Linux DEB installer created: ${DIST_DIR}/File4Base-Linux-x64.deb"
        fi
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
        "${SCRIPT_DIR}/verify_desktop_archive.sh" \
            "${DIST_DIR}/File4Base-Windows-x64.zip" \
            "${CLIENT_DIR}/build/windows/x64/pdfium-src"
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

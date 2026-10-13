#!/usr/bin/env bash
# Verifies that a packaged File4Base desktop release archive contains:
#   1. The project GPL-3.0 LICENSE file matching the repository root LICENSE.
#   2. Flutter's generated package notices (NOTICES.Z).
#   3. Whenever a native PDFium binary (lib/libpdfium.so or pdfium.dll) is
#      shipped (Linux and Windows builds), the complete matching upstream
#      PDFium LICENSE and VERSION files under THIRD_PARTY_NOTICES/pdfium/,
#      verified by SHA-256 correspondence against the build's pdfium-src
#      directory and the repository's THIRD_PARTY_NOTICES/pdfium/ bundle.
#
# Usage:
#   ./scripts/verify_desktop_archive.sh <archive-path> [pdfium-src-dir]

set -euo pipefail

if [ $# -lt 1 ]; then
    echo "Usage: $0 <archive-path> [pdfium-src-dir]" >&2
    exit 1
fi

ARCHIVE="$1"
PDFIUM_SRC_DIR="${2:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if [ ! -f "${ARCHIVE}" ]; then
    echo "ERROR: archive not found: ${ARCHIVE}" >&2
    exit 1
fi

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        openssl dgst -sha256 "$1" | awk '{print $NF}'
    fi
}

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

case "${ARCHIVE}" in
    *.tar.gz|*.tgz)
        tar -xzf "${ARCHIVE}" -C "${TMP_DIR}"
        ;;
    *.zip)
        if command -v unzip >/dev/null 2>&1; then
            unzip -q "${ARCHIVE}" -d "${TMP_DIR}"
        else
            tar -xf "${ARCHIVE}" -C "${TMP_DIR}"
        fi
        ;;
    *)
        echo "ERROR: unsupported archive format: ${ARCHIVE}" >&2
        exit 1
        ;;
esac

echo "==> Inspecting desktop archive: ${ARCHIVE} (SHA-256: $(sha256_file "${ARCHIVE}"))"

# 1. Verify project LICENSE
if [ -d "${TMP_DIR}/File4Base.app" ]; then
    ARCH_TYPE="macos"
    PACKAGED_LICENSE="${TMP_DIR}/File4Base.app/Contents/Resources/LICENSE"
    FLUTTER_NOTICES="${TMP_DIR}/File4Base.app/Contents/Frameworks/App.framework/Resources/flutter_assets/NOTICES.Z"
else
    PACKAGED_LICENSE="${TMP_DIR}/LICENSE"
    FLUTTER_NOTICES="${TMP_DIR}/data/flutter_assets/NOTICES.Z"
    if [ -f "${TMP_DIR}/lib/libpdfium.so" ]; then
        ARCH_TYPE="linux"
    elif [ -f "${TMP_DIR}/pdfium.dll" ]; then
        ARCH_TYPE="windows"
    else
        ARCH_TYPE="other"
    fi
fi

if [ ! -s "${PACKAGED_LICENSE}" ]; then
    echo "ERROR: project LICENSE is missing from ${ARCHIVE} (expected at ${PACKAGED_LICENSE#${TMP_DIR}/})" >&2
    exit 1
fi

if [ "$(sha256_file "${PACKAGED_LICENSE}")" != "$(sha256_file "${ROOT_DIR}/LICENSE")" ]; then
    echo "ERROR: packaged LICENSE hash does not match repository root LICENSE" >&2
    exit 1
fi
echo "  [OK] Project LICENSE present and matches root LICENSE."

# 2. Verify Flutter generated package notices (NOTICES.Z)
if [ ! -s "${FLUTTER_NOTICES}" ]; then
    echo "ERROR: Flutter NOTICES.Z is missing or empty in ${ARCHIVE}" >&2
    exit 1
fi
echo "  [OK] Flutter package notices present (${FLUTTER_NOTICES#${TMP_DIR}/})."

# 3. Verify native PDFium binary and matching notices
PDFIUM_BIN=""
if [ -f "${TMP_DIR}/lib/libpdfium.so" ]; then
    PDFIUM_BIN="${TMP_DIR}/lib/libpdfium.so"
elif [ -f "${TMP_DIR}/pdfium.dll" ]; then
    PDFIUM_BIN="${TMP_DIR}/pdfium.dll"
fi

case "$(basename "${ARCHIVE}")" in
    *Linux*|*linux*|*Windows*|*windows*)
        if [ -z "${PDFIUM_BIN}" ]; then
            echo "ERROR: expected native PDFium library in ${ARCHIVE}, but none was found" >&2
            exit 1
        fi
        ;;
esac

if [ -n "${PDFIUM_BIN}" ]; then
    PDFIUM_NOTICE="${TMP_DIR}/THIRD_PARTY_NOTICES/pdfium/LICENSE"
    PDFIUM_VERSION_FILE="${TMP_DIR}/THIRD_PARTY_NOTICES/pdfium/VERSION"

    if [ ! -s "${PDFIUM_NOTICE}" ]; then
        echo "ERROR: ${PDFIUM_BIN#${TMP_DIR}/} is bundled in ${ARCHIVE} without THIRD_PARTY_NOTICES/pdfium/LICENSE" >&2
        exit 1
    fi
    if [ ! -s "${PDFIUM_VERSION_FILE}" ]; then
        echo "ERROR: ${PDFIUM_BIN#${TMP_DIR}/} is bundled in ${ARCHIVE} without THIRD_PARTY_NOTICES/pdfium/VERSION" >&2
        exit 1
    fi

    if ! grep -q "Copyright 2014 PDFium Authors" "${PDFIUM_NOTICE}"; then
        echo "ERROR: THIRD_PARTY_NOTICES/pdfium/LICENSE does not contain the PDFium copyright notice" >&2
        exit 1
    fi

    # Verify normalized (LF) correspondence with the repository's reference copy.
    LC_ALL=C tr -d '\r' < "${ROOT_DIR}/THIRD_PARTY_NOTICES/pdfium/LICENSE" > "${TMP_DIR}/repo_pdfium_license.lf"
    LC_ALL=C tr -d '\r' < "${PDFIUM_NOTICE}" > "${TMP_DIR}/pkg_pdfium_license.lf"
    REPO_NOTICE_LF="$(sha256_file "${TMP_DIR}/repo_pdfium_license.lf")"
    PKG_NOTICE_LF="$(sha256_file "${TMP_DIR}/pkg_pdfium_license.lf")"
    if [ "${PKG_NOTICE_LF}" != "${REPO_NOTICE_LF}" ]; then
        echo "ERROR: packaged THIRD_PARTY_NOTICES/pdfium/LICENSE differs from repository THIRD_PARTY_NOTICES/pdfium/LICENSE" >&2
        exit 1
    fi

    # When pdfium-src-dir is provided, verify exact hash correspondence with the downloaded build artifact.
    if [ -n "${PDFIUM_SRC_DIR}" ]; then
        if [ ! -f "${PDFIUM_SRC_DIR}/LICENSE" ]; then
            echo "ERROR: pdfium-src LICENSE not found at ${PDFIUM_SRC_DIR}/LICENSE" >&2
            exit 1
        fi
        if [ "$(sha256_file "${PDFIUM_NOTICE}")" != "$(sha256_file "${PDFIUM_SRC_DIR}/LICENSE")" ]; then
            echo "ERROR: packaged PDFium LICENSE hash does not match ${PDFIUM_SRC_DIR}/LICENSE" >&2
            exit 1
        fi
        SRC_BIN=""
        if [ -f "${PDFIUM_SRC_DIR}/lib/libpdfium.so" ]; then
            SRC_BIN="${PDFIUM_SRC_DIR}/lib/libpdfium.so"
        elif [ -f "${PDFIUM_SRC_DIR}/bin/pdfium.dll" ]; then
            SRC_BIN="${PDFIUM_SRC_DIR}/bin/pdfium.dll"
        fi
        if [ -n "${SRC_BIN}" ] && [ "$(sha256_file "${PDFIUM_BIN}")" != "$(sha256_file "${SRC_BIN}")" ]; then
            echo "ERROR: packaged PDFium binary hash does not match ${SRC_BIN}" >&2
            exit 1
        fi
    fi

    echo "  [OK] PDFium binary (${PDFIUM_BIN#${TMP_DIR}/}, SHA-256: $(sha256_file "${PDFIUM_BIN}")) paired with matching THIRD_PARTY_NOTICES/pdfium/LICENSE (SHA-256: $(sha256_file "${PDFIUM_NOTICE}"))."
else
    echo "  [OK] No native PDFium binary in archive (${ARCH_TYPE})."
fi

# PDFium Redistribution Notices and Corrective Notice Bundle

## Overview

The File4Base desktop client uses the [`printing`](https://pub.dev/packages/printing) Flutter plugin (locked at `5.15.1` in [`client/pubspec.lock`](../../client/pubspec.lock)). During native desktop builds on **Linux** and **Windows**, `printing`'s CMake build downloads precompiled PDFium binaries (`chromium/5200`, PDFium `106.0.5200.0`) from [`bblanchon/pdfium-binaries`](https://github.com/bblanchon/pdfium-binaries/releases/tag/chromium%2F5200) and bundles the shared library alongside the executable.

Because PDFium is downloaded by CMake rather than declared as a Dart package, Flutter's generated `NOTICES.Z` file does not automatically include PDFium's consolidated license and bundled-component notices.

This directory provides:
1. **[`LICENSE`](LICENSE)** and **[`VERSION`](VERSION)** — the complete consolidated upstream license and notice bundle (59,354 bytes) from `bblanchon/pdfium-binaries` release `chromium/5200` covering PDFium and its bundled third-party components (FreeType, HarfBuzz, ICU, libjpeg-turbo, libpng, OpenJPEG, zlib, agg23, Foxit/Google PDFium).
2. **Corrective notice coverage** for previously published File4Base Linux and Windows desktop archives (`v0.9.1` through `v0.11.0`), without silently modifying or replacing previously published release archives under their existing SHA-256 hashes.

---

## Platform & Artifact Correspondence (`printing 5.15.1` / `chromium/5200`)

| Target Platform | Bundles PDFium? | Upstream Archive | Shipped Binary Path | Binary SHA-256 | Upstream `LICENSE` SHA-256 |
| :--- | :---: | :--- | :--- | :--- | :--- |
| **Linux x64** | **Yes** | [`pdfium-linux-x64.tgz`](https://github.com/bblanchon/pdfium-binaries/releases/download/chromium/5200/pdfium-linux-x64.tgz) | `lib/libpdfium.so` | `d585300f4a8ce25cfb3a290fdd16caeedf102bb89194a0d3835f27a9ca09bd24` | `0f00d0bb0e8a07d499439de08e1192a88eae06b055877db098b28dc3ca0892ce` (59,354 bytes, LF) |
| **Windows x64** | **Yes** | [`pdfium-win-x64.tgz`](https://github.com/bblanchon/pdfium-binaries/releases/download/chromium/5200/pdfium-win-x64.tgz) | `pdfium.dll` (from `bin/pdfium.dll`) | `0c88ebacc0393fd45fc3e7b35e31e72c9e55b633a846a7ecf4085694dba68abd` | `18d26db25f264c3b8bec19398ed9dd95845e38d1ac992d75ac356c317ed8528a` (60,003 bytes, CRLF; identical text to Linux LF file) |
| **macOS Universal** | **No** | N/A (uses Apple's system `PDFKit` via `FlutterMacOS` in `printing.podspec`) | None | N/A | N/A |
| **File4Base Web Client** | **No** | N/A (uses browser printing APIs) | None | N/A | N/A |

---

## Corrective Notice for Existing Releases (`v0.9.1` – `v0.11.0`)

Published Linux (`File4Base-Linux-x64.tar.gz`, including `v0.9.1` SHA-256 `a131f49890118afe059c9db0ff9f5f60f218a49b5a259e7bde9f69302bfb8e78`) and Windows (`File4Base-Windows-x64.zip`) release archives up to and including `v0.11.0` shipped `lib/libpdfium.so` or `pdfium.dll` from `bblanchon/pdfium-binaries` (`chromium/5200`) with Flutter's `data/flutter_assets/NOTICES.Z`, but omitted the upstream `LICENSE` file from the PDFium archive.

To preserve artifact provenance, existing release downloads on GitHub Releases are **not** silently replaced under old version tags. Recipients of any `v0.9.1`–`v0.11.0` Linux or Windows desktop archive should treat [`THIRD_PARTY_NOTICES/pdfium/LICENSE`](LICENSE) and [`THIRD_PARTY_NOTICES/pdfium/VERSION`](VERSION) in this directory as the accompanying copyright, license, and disclaimer notice material for the bundled `libpdfium.so` / `pdfium.dll` binary.

---

## Packaging & Verification in Subsequent Releases

Starting after `v0.11.0`:
- [`client/linux/CMakeLists.txt`](../../client/linux/CMakeLists.txt) and [`client/windows/CMakeLists.txt`](../../client/windows/CMakeLists.txt) install the project's top-level `LICENSE` (`GPL-3.0-only`) plus `THIRD_PARTY_NOTICES/pdfium/LICENSE` and `THIRD_PARTY_NOTICES/pdfium/VERSION` directly from `${CMAKE_BINARY_DIR}/pdfium-src/` into the release bundle alongside Flutter's `data/flutter_assets/NOTICES.Z`.
- [`scripts/verify_desktop_archive.sh`](../../scripts/verify_desktop_archive.sh) runs in both [`scripts/build_desktop.sh`](../../scripts/build_desktop.sh) and [`.github/workflows/desktop_release.yml`](../../.github/workflows/desktop_release.yml) to inspect every built desktop archive, verifying that any bundled `libpdfium.so` or `pdfium.dll` is accompanied by its matching `THIRD_PARTY_NOTICES/pdfium/LICENSE` and `VERSION` from the same `pdfium-src` extraction.

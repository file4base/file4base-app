# Project Rules: Versioning, Changelog & Documentation Governance

## 1. Semantic Versioning Standard (SemVer 2.0.0)
All components of File4Base follow strict Semantic Versioning (`MAJOR.MINOR.PATCH`):
- **MAJOR**: Breaking schema migrations, incompatible REST API changes, or architectural shifts.
- **MINOR**: New capabilities (e.g., new operational mode, new DBAL dialect, portal rendering, calculation engine).
- **PATCH**: Bug fixes, performance optimizations, UI polish, or minor non-breaking enhancements.

---

## 2. Mandatory Version Synchronization
The root `VERSION` file is the primary single source of truth for the project version. Whenever the version is changed, the AI assistant and developers MUST synchronize:
1. `VERSION`: The canonical version string (e.g., `0.2.0`).
2. `server/cmd/server/main.go`: `const AppVersion = "X.Y.Z"` (exposed via `/healthz`).
3. `client/pubspec.yaml`: `version: X.Y.Z+BUILD_NUMBER`.
4. `client/windows/runner/Runner.rc`: `VERSION_AS_NUMBER` and `VERSION_AS_STRING`.
5. `client/lib/features/about/about_dialog.dart`: Displayed version.
6. Git Tags: `vX.Y.Z`.

---

## 3. Mandatory Changelog Maintenance (`CHANGELOG.md`)
Every pull request, feature completion, or significant commit MUST update `CHANGELOG.md` adhering to [Keep a Changelog](https://keepachangelog.com/) standards.
Categories to use:
- `Added`: New features, endpoints, or UI capabilities.
- `Changed`: Changes in existing functionality.
- `Deprecated`: Soon-to-be removed features.
- `Removed`: Now removed features.
- `Fixed`: Any bug fixes or correction of errors.
- `Security`: Vulnerability mitigations or security enhancements.

---

## 4. Living Documentation Rule
The AI and developers must keep project documentation synchronized with code:
1. **API Endpoints**: Any added or altered REST route or payload MUST be documented in `docs/api/API_REFERENCE.md`.
2. **Schema & DBAL**: Any new dialect, data type, or catalog table MUST be documented in `docs/specs/DBAL_SPECIFICATION.md` or `docs/specs/layout_schema.json`.
3. **Roadmap**: Ticked progress checkboxes in `docs/specs/ROADMAP.md` MUST match actual merged code.
4. **Desktop & Platforms**: Changes in platform targets, ports, or preflight logic MUST be documented in `README.md` and `docs/specs/ARCHITECTURE.md`.

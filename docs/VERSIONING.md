# File4Base Versioning & Release Governance

## 1. Semantic Versioning Specification (SemVer 2.0.0)

File4Base adheres strictly to [Semantic Versioning 2.0.0](https://semver.org/):

$$\text{Version} = \text{MAJOR} . \text{MINOR} . \text{PATCH}$$

| Level | When to Increment | Examples |
|---|---|---|
| **MAJOR** | Incompatible API changes, breaking schema migrations, or fundamental architecture reworks. | System catalog restructuring, protocol changes breaking client compatibility. |
| **MINOR** | Backwards-compatible new features and major module completions. | New operational mode, calculation engine parser, portal component, new DB engine dialect. |
| **PATCH** | Backwards-compatible bug fixes, UI styling refinements, and documentation corrections. | Layout inspector bug fix, preflight dialog timeout adjustment, minor test update. |

---

## 2. Single Source of Truth (`VERSION`)

The canonical project version is maintained in the plain-text file `/VERSION` in the repository root.

Whenever a version is bumped, the following targets **must** be kept in exact synchronization:

1. **Root File**: `VERSION`
2. **Go Backend**: `server/cmd/server/main.go` (`const AppVersion = "X.Y.Z"`, served on `GET /healthz`)
3. **Flutter Client**: `client/pubspec.yaml` (`version: X.Y.Z+BUILD`)
4. **macOS Config**: `client/macos/Runner/Configs/AppInfo.xcconfig`
5. **Windows Config**: `client/windows/runner/Runner.rc` (`VERSION_AS_NUMBER` and `VERSION_AS_STRING`)
6. **Changelog**: `CHANGELOG.md` with release date and categorized notes.
7. **Git Tag**: Tagged as `vX.Y.Z`.

---

## 3. Automated Version Bumping

A helper script is provided at `scripts/bump_version.sh` to update all version targets simultaneously:

```bash
# Bump patch version (e.g., 0.2.0 -> 0.2.1)
./scripts/bump_version.sh patch

# Bump minor version (e.g., 0.2.0 -> 0.3.0)
./scripts/bump_version.sh minor

# Bump major version (e.g., 0.2.0 -> 1.0.0)
./scripts/bump_version.sh major

# Set explicit version
./scripts/bump_version.sh 0.3.0
```

---

## 4. Git Branching & Tagging Strategy

- `main`: Production-ready branch. All commits must pass Go unit tests, Flutter widget tests, and docker container builds.
- Feature branches: `feat/<feature-name>`, `fix/<bug-name>`.
- Release tags: `v<MAJOR>.<MINOR>.<PATCH>` (e.g., `v0.2.0`).
- Triggering a Git tag pushes multiplatform desktop release builds via `.github/workflows/desktop_release.yml`.

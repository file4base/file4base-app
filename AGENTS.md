# AGENTS.md

## Repository Instructions & Guidelines

### Language Standard
- **All documentation, specifications, code, comments, commit messages, and schemas MUST be in English.**
- When translating or creating new files, maintain consistent naming across backend (Go), frontend (Flutter), and database (PostgreSQL).
- Detailed rule: `.agents/rules/language.md`.

### Versioning & Changelog Governance
- **Semantic Versioning (SemVer 2.0.0)** is strictly mandatory (`MAJOR.MINOR.PATCH`).
- Single source of truth is the root `VERSION` file.
- **Always update `CHANGELOG.md`** whenever a feature, fix, or breaking change is committed.
- When bumping version, synchronize across `VERSION`, `server/cmd/server/main.go` (`AppVersion`), `client/pubspec.yaml`, `client/windows/runner/Runner.rc`, and about dialogs.
- Detailed rule: `.agents/rules/versioning.md`.

### Documentation Maintenance
- Any new or updated REST API endpoint MUST be documented in `docs/api/API_REFERENCE.md`.
- Roadmaps (`docs/specs/ROADMAP.md`) and specs (`docs/specs/`) must reflect actual implementation status.

### Architecture & Tech Stack
- **Backend**: Go (Clean Architecture, standard library / chi, pgx/v5, Dockerized).
- **Frontend**: Flutter desktop (desktop target: macOS/Linux/Windows, WebDirect via Nginx, Riverpod, JSON layout renderer).
- **Database**: PostgreSQL 16 default, optional MariaDB profile (docker-compose).
- **Style**: Strict error handling, deterministic JSON schemas (`docs/specs/layout_schema.json`), unit tests for all layers.

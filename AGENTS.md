# AGENTS.md

## Repository Instructions & Guidelines

### Language Standard
- **All documentation, specifications, code, comments, commit messages, and schemas MUST be in English.**
- When translating or creating new files, maintain consistent naming across backend (Go), frontend (Flutter), and database (PostgreSQL).
- Detailed rule: `.agents/rules/language.md`.

### Independent Development and Rights
- Read `CONTRIBUTING.md` and `docs/LEGAL.md` for reference/asset provenance and licensing review.
- Express compatibility requirements as behavior; use File4Base's own visual composition, prose, artwork and examples.
- Identify reference titles and versions accurately. Do not erase provenance or claim an undocumented clean-room process.
- Review rights for external and AI-assisted material, preserving applicable licenses and notices, including native build downloads.
- Use **File4Base Web Client** for the browser component; preserve compatibility identifiers when required and flag remaining display names for review.

### Versioning & Changelog Governance
- **Semantic Versioning (SemVer 2.0.0)** is strictly mandatory (`MAJOR.MINOR.PATCH`).
- Single source of truth is the root `VERSION` file.
- **Always update `CHANGELOG.md`** whenever a feature, fix, or breaking change is committed.
- When bumping version, synchronize across `VERSION`, `server/cmd/server/main.go` (`AppVersion`), `client/pubspec.yaml`, `client/windows/runner/Runner.rc`, about dialogs, `openapi.json`, and the Docker image tags in `docker-compose.yml` (use `scripts/bump_version.sh`).
- Detailed rule: `.agents/rules/versioning.md`.

### Documentation Maintenance
- Any new or updated REST API endpoint MUST be documented in `docs/api/API_REFERENCE.md`.
- Roadmaps (`docs/specs/ROADMAP.md`) and specs (`docs/specs/`) must reflect actual implementation status.

### Architecture & Tech Stack
- **Backend**: Go (Clean Architecture, standard library / chi, pgx/v5, Dockerized).
- **Frontend**: Flutter desktop (desktop target: macOS/Linux/Windows, Web Client via Nginx, Riverpod, JSON layout renderer).
- **Database**: PostgreSQL 16 default, optional MariaDB profile (docker-compose).
- **Docker Naming Convention**: All Docker containers, images, volumes, and networks MUST strictly use the `file4base-` prefix (`file4base-api`, `file4base-web`, `file4base-postgres`, `file4base-mariadb`, `file4base-net`). Never omit `container_name:`.
- **Style**: Strict error handling, deterministic JSON schemas (`docs/specs/layout_schema.json`), unit tests for all layers.

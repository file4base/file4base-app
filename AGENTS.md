# AGENTS.md

## Repository Instructions & Guidelines

### Language Standard
- **All documentation, specifications, code, comments, commit messages, and schemas MUST be in English.**
- When translating or creating new files, maintain consistent naming across backend (Go), frontend (Flutter), and database (PostgreSQL).

### Architecture & Tech Stack
- **Backend**: Go (Clean Architecture, standard library / chi, pgx/v5, Dockerized).
- **Frontend**: Flutter desktop (desktop target: macOS/Linux/Windows, Riverpod/Bloc, JSON layout renderer).
- **Database**: PostgreSQL 16 (docker-compose).
- **Style**: Strict error handling, deterministic JSON schemas (`docs/specs/layout_schema.json`), unit tests for all layers.

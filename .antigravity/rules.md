# Antigravity Agent Rules: File4Base

You are the Lead Fullstack Systems Architect and Software Engineer building File4Base.
File4Base is an open-source, modern alternative to datase Editor.

## 1. Tech Stack Boundaries
- **Backend**: Go (1.22+). Clean Architecture / Ports and Adapters. Standard library + `net/http` (or `chi`), `pgx/v5` for PostgreSQL, Dockerized.
- **Frontend**: Flutter (desktop target: macOS/Linux/Windows). State management: Riverpod or Bloc. Dynamic rendering based on JSON layout schema.
- **Database**: PostgreSQL 16 (running via Docker Compose). Support dynamic table/column metadata.
- **Communication**: REST API + WebSockets for real-time record events.

## 2. Coding Principles & Guidelines
### Go (Backend):
- Strict error handling; do NOT use `panic` in production paths.
- All database migrations must be explicit SQL files.
- Separate business logic (domain/use cases) from HTTP transport and SQL persistence.
- Keep binaries lightweight; avoid bloat and heavy frameworks.

### Flutter (Frontend):
- Modular widget tree. Separate UI layout engine from state logic.
- Responsive to desktop window resizing and multi-monitor setups.
- Use strict typing for JSON serialization (freezed / json_serializable).
- Decouple API communication via an abstracted HTTP/WebSocket repository interface.

## 3. Execution Rules for Antigravity
1. **Incremental building**: Implement and verify one module at a time. Do not generate massive unverified boilerplates in a single pass.
2. **Deterministic Schemas**: All dynamic layout and table definitions must strictly match `docs/specs/layout_schema.json`.
3. **Tests**: Include Go unit tests for repository/usecase layers and Flutter widget/unit tests for parser logic.
4. **Language Rule**: All documentation, code, schema definitions, comments, error messages, and commits MUST be written strictly in English.


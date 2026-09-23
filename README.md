# File4Base

File4Base is an open-source, modern alternative to database and layout editors, built with a decoupled Go backend and Flutter desktop frontend.

## Architecture Highlights
- **Backend (Go 1.22+)**: REST & WebSockets, Clean Architecture, `pgx/v5`, PostgreSQL 16.
- **Frontend (Flutter Desktop)**: Native desktop application targeting macOS (Apple Silicon M-series & Intel), Windows, and Linux.
- **Environment & Preflight Engine**: Automated startup check verifying Docker presence, daemon state, and host architecture, with direct download and auto-launch prompts.
- **Data Persistence**: Relational storage in PostgreSQL/MariaDB running locally in Docker with dynamic schema metadata tables.


## Repository Documentation
- [Architecture Specification](docs/specs/ARCHITECTURE.md)
- [Implementation Roadmap](docs/specs/ROADMAP.md)
- [Database Abstraction Layer (DBAL) Spec](docs/specs/DBAL_SPECIFICATION.md)
- [Functional Specification (Feature Mapping)](docs/specs/FUNCTIONAL_SPECIFICATION.md)
- [Dynamic Layout JSON Schema](docs/specs/layout_schema.json)
- [Menu Bar & Functional Command Reference](docs/specs/file4base_menu_reference_guide.md)


## Development Setup
```bash
# Start PostgreSQL database and services
docker compose up -d
```
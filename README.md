# File4Base

File4Base is an open-source, modern alternative to database and layout editors, built with a decoupled Go backend and Flutter desktop frontend.

## Architecture Highlights
- **Backend (Go 1.22+)**: REST & WebSockets, Clean Architecture, `pgx/v5`, PostgreSQL 16.
- **Frontend (Flutter Desktop)**: Dynamic layout rendering engine using deterministic JSON schemas.
- **Data Persistence**: Relational storage in PostgreSQL with dynamic schema metadata tables.

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
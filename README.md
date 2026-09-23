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


## Execution Modalities

### 1. Server Deployment (Docker Compose)
When launched via Docker Compose, File4Base acts as a full multi-user application server:
- **PostgreSQL 16**: Port `5432` (or MariaDB via `--profile mariadb`)
- **Backend Core API (Go)**: Port `8080` (`/healthz`, `/api/v1/schemas`, `/api/v1/data`)
- **WebDirect Web Client (Nginx)**: Port `3000` (browser-accessible client)

```bash
# Start complete File4Base server stack
docker compose up -d

# Check server health
curl http://localhost:8080/healthz

# Access web application
open http://localhost:3000
```

### 2. Standalone Desktop Client (macOS / Windows / Linux)
The native desktop client runs locally on developer workstations (including Apple Silicon M-series Macs):
- Automatically performs preflight system checks on launch.
- Verifies Docker installation and engine daemon status.
- Prompts for automatic launch or download if missing.

```bash
cd client
flutter run -d macos    # or windows, linux
```
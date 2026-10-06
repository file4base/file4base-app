# File4Base

File4Base is an open-source, modern alternative to database and layout editors, built with a decoupled Go backend and Flutter desktop frontend.

## Architecture Highlights
- **Backend (Go 1.26+)**: REST API with per-database sessions and role-based authorization, Clean Architecture, `pgx/v5`, PostgreSQL 16. Real-time WebSocket sync is planned (Roadmap Phase 6).
- **Frontend (Flutter Desktop)**: Native desktop application targeting macOS (Apple Silicon M-series & Intel), Windows, and Linux.
- **Environment & Preflight Engine**: Automated startup check verifying Docker presence, daemon state, and host architecture, with direct download and auto-launch prompts.
- **Data Persistence**: Relational storage in PostgreSQL/MariaDB running locally in Docker with dynamic schema metadata tables.


## Repository Documentation
- [Architecture Specification](docs/specs/ARCHITECTURE.md)
- [API Best Practices & Telemetry Specification](docs/specs/API_BEST_PRACTICES.md)
- [REST API Reference](docs/api/API_REFERENCE.md)
- [Implementation Roadmap](docs/specs/ROADMAP.md)
- [Database Abstraction Layer (DBAL) Spec](docs/specs/DBAL_SPECIFICATION.md)
- [Functional Specification (Feature Mapping)](docs/specs/FUNCTIONAL_SPECIFICATION.md)
- [Dynamic Layout JSON Schema](docs/specs/layout_schema.json)
- [Menu Bar & Functional Command Reference](docs/specs/file4base_menu_reference_guide.md)


## Security Model
- **Sessions**: `POST /api/v1/auth/login` returns a bearer token bound to one user and one database. Every schema, data, security and solution endpoint requires it; only the health probes, Swagger UI, sign-in and the database selector are public.
- **Per-session database**: the server keeps no global "active database". Two clients signed in to different databases never affect each other.
- **Roles enforced by the server**: `owner`, `admin` and `user`, plus per-layout permissions (`read_write`, `read_only`, `none`) that also govern record access through the data API.
- **No default accounts**: a database gets its first owner with the credentials chosen when it is created.
- **Configuration** (environment variables of the API server): `DATABASE_URL`, `SESSION_TTL` (default `12h`), `CORS_ALLOWED_ORIGINS` (default `*`), `ALLOW_PUBLIC_DATABASE_CREATION` (default `true`; set to `false` on shared servers).

Details: [REST API Reference - Authentication & Authorization](docs/api/API_REFERENCE.md#authentication--authorization).

## Execution Modalities

### 1. Server Deployment (Docker Compose)
When launched via Docker Compose, File4Base acts as a full multi-user application server:
- **PostgreSQL 16**: Port `5432`, published on `127.0.0.1` only by default (or MariaDB via `--profile mariadb`)
- **Backend Core API (Go)**: Port `8080` (`/healthz`, `/api/v1/schemas`, `/api/v1/data`)
- **WebDirect Web Client (Nginx)**: Port `3000` (browser-accessible client)

```bash
# Optional: set your own database password and server options
cp .env.example .env

# Start complete File4Base server stack (builds the API and the web client images)
docker compose up -d

# Check server health
curl http://localhost:8080/healthz

# Access web application
open http://localhost:3000
```

On a new Docker volume, PostgreSQL starts with its administrative `postgres`
database and no File4Base solution database. Create a solution database from
the client's database login screen or import a `.f4p` solution. Docker's
`POSTGRES_DB` setting only applies when PostgreSQL initializes an empty data
volume: existing volumes keep their databases and data, which remain available
in the database selector. Do not remove a volume to change this behavior unless
you intend to delete its persisted data.

#### Images and versions
The images are published on Docker Hub for `linux/amd64`, `linux/arm64` and
`linux/arm/v7`, so they run on macOS and Windows (Docker Desktop, Intel or Apple
Silicon / ARM), Linux on Intel/AMD or ARM, and Raspberry Pi with a 64-bit or
32-bit OS:
[`cloudresources/file4base-api`](https://hub.docker.com/r/cloudresources/file4base-api) and
[`cloudresources/file4base-web`](https://hub.docker.com/r/cloudresources/file4base-web),
tagged with the version in the root `VERSION` file (and `latest`).

```bash
docker compose pull && docker compose up -d   # run the published images
docker compose up -d --build                  # or build them locally
./scripts/publish_images.sh                   # maintainers: build and push a release
```

Set `FILE4BASE_VERSION` (tag) or `FILE4BASE_REGISTRY` (Docker Hub namespace)
in `.env` to run another release or registry.

#### Data persistence and backups
PostgreSQL stores all its databases in the named Docker volume
`file4base-postgres-data`. It survives `docker compose down` and image rebuilds;
only `docker compose down -v` or `docker volume rm` delete it.

```bash
# Back up every database (and roles) to backups/file4base-postgres-v<version>-<date>.sql.gz
./scripts/backup_postgres.sh backup

# List backups / restore one (asks for confirmation, stops the API meanwhile)
./scripts/backup_postgres.sh list
./scripts/backup_postgres.sh restore backups/<file>.sql.gz
```

### 2. Standalone Desktop Client (macOS / Windows / Linux)
The native desktop client runs locally on developer workstations (including Apple Silicon M-series Macs, Windows, and Linux):
- Connects through configurable port (default: `http://localhost:8080`), with interactive Host & Port Settings dialog (accessible by clicking the server status pill in the AppBar).
- Performs generic preflight checks (connectivity, Docker daemon, database engine status).
- Official adaptive light/dark branding icons integrated into macOS `.appiconset`, Windows `app_icon.ico`, and Linux headers.

```bash
# Run locally in development
cd client
flutter run -d macos    # or windows, linux

# Build native release bundles with packaging script
./scripts/build_desktop.sh macos     # outputs dist/File4Base-macOS-universal.zip
./scripts/build_desktop.sh windows   # outputs dist/File4Base-Windows-x64.zip
./scripts/build_desktop.sh linux     # outputs dist/File4Base-Linux-x64.tar.gz
```

### 3. Automated CI/CD Workflows
A GitHub Actions workflow (`.github/workflows/desktop_release.yml`) runs the Go server checks (`go vet` and the test suite against a PostgreSQL service) and the Flutter tests, and automatically compiles and packages native release builds for macOS, Windows, and Linux on every release tag or workflow dispatch.

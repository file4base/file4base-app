# File4Base Architecture Specification

## 1. System Overview
File4Base replicates the integrated database application paradigm using a modern decoupled architecture:
- **Server**: Go (1.26+) application running in Docker or standalone. Features a database abstraction layer (DBAL) supporting PostgreSQL (primary) and MariaDB (alternative engine), dynamic DDL/metadata management, a dynamic CRUD API, and session-based authentication with role and layout-permission enforcement. WebSocket event streaming (Roadmap Phase 6) and server-side calculation execution (Phase 7) are planned and not implemented yet.
- **Client**: Cross-platform Flutter desktop application (macOS, Windows, Linux) that fetches schema metadata and JSON layout definitions to dynamically render forms, interactive relationship graphs, grids, and block-based scripts.

## 2. Core Pillars (File4Base Equivalences)

| File4Base Concept | File4Base Architectural Solution | Tech Implementation |
|---|---|---|
| **Integrated Database Engine** | Multi-engine DBAL with dynamic DDL execution | Go (`dbal` package) + PostgreSQL / MariaDB |
| **Manage Database (Tables & Fields)** | Metadata Catalog (`sys_tables`, `sys_columns`) + REST API | Dynamic SQL generator (`CREATE/ALTER TABLE`) |
| **Relationship Graph** | Occurrence & Relationship Catalog (`sys_relationships`) | Flutter Visual Node Canvas (`flutter_graphview` / Custom Painter) |
| **Layouts & Visual Form Designer** | JSON Layout Engine + Drag-and-Drop Canvas | Flutter `InteractiveViewer` + JSON Schema Renderer |
| **Calculation Engine** | Formula parser for calculated fields and auto-enter values | Go expression evaluator / embedded engine (Expr / Lua) |
| **Script Workspace** | Block-based automation engine and action runner | Go workflow runner + Flutter block drag-and-drop IDE |
| **Browse / Find / Layout / Preview Modes** | Client-side Mode State Machine & Dynamic Toolbar | Flutter Bloc/Riverpod Mode Controller |
| **Real-time multi-user sync** | Event Hub via Pub/Sub & WebSockets | PostgreSQL `LISTEN/NOTIFY` or Redis/Go Channels -> WS |

## 3. Layered System Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                        Flutter Desktop Client                          │
│                                                                        │
│  ┌─────────────────────────┐  ┌─────────────────────────────────────┐  │
│  │   UI & Layout Engine    │  │   Visual Schema & Script Editors    │  │
│  │  - Form & List Renderers │  │  - Table & Column Inspector         │  │
│  │  - Interactive Grid     │  │  - Relationship Graph (Node Canvas) │  │
│  │  - 4 Modes Controller   │  │  - Script Workspace (Action Blocks) │  │
│  └────────────┬────────────┘  └──────────────────┬──────────────────┘  │
│               │                                  │                     │
│               └────────────────┬─────────────────┘                     │
│                                │ State / Repository Layer               │
└────────────────────────────────┼───────────────────────────────────────┘
                                 │ HTTP REST (JSON) + WebSockets
┌────────────────────────────────▼───────────────────────────────────────┐
│                           Go Backend Core                              │
│                                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ API Handlers: /api/v1/schemas, /api/v1/data, /api/v1/scripts     │  │
│  └─────────────────────────────┬────────────────────────────────────┘  │
│                                │                                       │
│  ┌─────────────────────────────▼────────────────────────────────────┐  │
│  │ Domain & Services: Schema Service, CRUD Service, Formula Engine  │  │
│  └─────────────────────────────┬────────────────────────────────────┘  │
│                                │                                       │
│  ┌─────────────────────────────▼────────────────────────────────────┐  │
│  │ DBAL (Database Abstraction Layer & Dialect Gateway)             │  │
│  │ - Agnostic Models & Query AST                                   │  │
│  │ - Dialect Translator (PostgreSQL, MariaDB/MySQL, SQLite)        │  │
│  │ - Connection Pool & Transaction Manager                          │  │
│  └─────────────────────────────┬────────────────────────────────────┘  │
└────────────────────────────────┼───────────────────────────────────────┘
                                 │ Native SQL Connections
             ┌───────────────────┴───────────────────┐
             │                                       │
      ┌──────▼──────┐                         ┌──────▼──────┐
      │ PostgreSQL  │                         │   MariaDB   │
      │ (Primary)   │                         │(Alternative)│
      └─────────────┘                         └─────────────┘
```

## 4. Repository Structure (Monorepo)

```
file4base-app/
├── .agents/
│   ├── rules/                        # Language and versioning rules
│   └── skills/                       # Agent skills (Go, Flutter, Docker)
├── .antigravity/
│   └── rules.md                      # Antigravity operational guidelines
├── .github/workflows/
│   ├── desktop_release.yml           # Go checks, Flutter tests, desktop builds, releases
│   └── deploy_docs.yml               # GitHub Pages documentation site
├── AGENTS.md                         # Root agent guidelines
├── CHANGELOG.md
├── VERSION                           # Single source of truth for the version
├── docker-compose.yml                # PostgreSQL + API + WebDirect services
├── docker-compose.mariadb.yml        # the same stack on MariaDB
├── .env.example                      # Template for docker-compose configuration
├── docs/
│   ├── index.html                    # GitHub Pages landing page
│   ├── VERSIONING.md
│   ├── api/
│   │   └── API_REFERENCE.md          # REST API reference
│   └── specs/
│       ├── ARCHITECTURE.md           # This document
│       ├── ROADMAP.md                # Phase-by-phase roadmap
│       ├── API_BEST_PRACTICES.md     # Telemetry, probes and error format rules
│       ├── DBAL_SPECIFICATION.md     # DBAL & multi-engine dialect specs
│       ├── FUNCTIONAL_SPECIFICATION.md
│       ├── script_workspace_spec.md
│       ├── file4base_menu_reference_guide.md # Menu bar & command guide
│       └── layout_schema.json        # JSON schema for layout renderer
├── assets/
│   └── branding/
├── scripts/
│   ├── build_desktop.sh              # Native desktop packaging
│   └── bump_version.sh               # Version synchronization
├── server/                           # Go Backend
│   ├── cmd/server/main.go            # Configuration, middleware chain, lifecycle
│   ├── migrations/
│   ├── internal/
│   │   ├── api/                      # HTTP handlers, route mounting, auth middleware, Swagger
│   │   ├── auth/                     # Session store (bearer tokens bound to user + database)
│   │   ├── dbal/                     # Database Abstraction Layer
│   │   │   ├── dialect.go            # Dialect interfaces and agnostic types
│   │   │   ├── factory.go            # Dialect registry and driver factory
│   │   │   ├── multi_db.go           # Per-database connection pools, create/drop/list
│   │   │   ├── context.go            # Request-scoped database driver
│   │   │   ├── postgres/             # PostgreSQL dialect implementation
│   │   │   └── mariadb/              # MariaDB dialect implementation
│   │   ├── schema/                   # sys_* catalog: tables, relationships, layouts, scripts, users, solutions
│   │   ├── data/                     # Generic dynamic CRUD and Find Mode translator
│   │   └── telemetry/                # Trace context, structured logs, RFC 9457 errors
│   ├── Dockerfile
│   └── go.mod
└── client/                           # Flutter Desktop & Web Client
    ├── lib/
    │   ├── main.dart                 # Workspace shell and mode controller
    │   ├── core/                     # API client, models, providers, services, theme, widgets
    │   └── features/
    │       ├── auth/                 # Database login
    │       ├── connection/           # Server host & port settings
    │       ├── preflight/            # Environment / Docker checks
    │       ├── data_browser/         # Browse & Find modes
    │       ├── layout_engine/        # Layout designer, preview and renderer
    │       ├── schema_manager/       # Manage Database & relationship graph
    │       ├── script_workspace/     # Script editor & calculation builder
    │       ├── security/             # Users and layout permissions
    │       ├── solution_manager/     # New / open / save solution, file options, page setup
    │       ├── theme_manager/
    │       ├── about/
    │       └── help/
    ├── Dockerfile                    # Multi-stage: Flutter web build + Nginx
    ├── nginx.conf
    └── pubspec.yaml
```

Planned packages that do not exist yet: `server/internal/websocket/` (live sync hub, Phase 6) and `server/internal/calc/` (calculation engine, Phase 7).

## 5. Docker Infrastructure & Naming Standards

To guarantee predictable container orchestration, monitoring, and avoid namespace collisions across development, staging, and production hosts, **all Docker resources MUST strictly use the `file4base-` prefix**:

| Resource Type | Service / Target | Name / Identifier | Host Port | Purpose |
|---|---|---|---|---|
| **Container & Image** | Go API Server | `file4base-api` | `8080:8080` | Core Go REST engine |
| **Container & Image** | WebDirect Client | `file4base-web` | `3000:80` | Nginx reverse proxy & Flutter Web |
| **Container & Image** | PostgreSQL DB | `file4base-postgres` | `5432:5432` | Primary relational database engine |
| **Container & Image** | MariaDB DB | `file4base-mariadb` | `3306:3306` | Alternative engine (`docker-compose.mariadb.yml`) |
| **Bridge Network** | System Network | `file4base-net` | N/A | Isolated inter-container communications |
| **Named Volume** | PostgreSQL Storage | `file4base-postgres-data` | N/A | Persistent PostgreSQL data catalog |
| **Named Volume** | MariaDB Storage | `file4base-mariadb-data` | N/A | Persistent MariaDB data catalog |

### Strict Enforcement Rules
1. **Explicit Container Names**: Never omit `container_name:` in `docker-compose.yml`. Relying on Docker Compose default naming generates irregular names like `<dir>-api-1` or `api`. The API container must always be explicitly named `file4base-api`.
2. **Versioned Images**: Images are tagged with the application version (`cloudresources/file4base-api:X.Y.Z`, `cloudresources/file4base-web:X.Y.Z`), never left untagged. The default tag in `docker-compose.yml` follows the root `VERSION` file and can be overridden with `FILE4BASE_VERSION` (namespace: `FILE4BASE_REGISTRY`). `scripts/publish_images.sh` publishes multi-arch (`linux/amd64`, `linux/arm64`, `linux/arm/v7`) images (macOS, Windows, Linux, Intel and Raspberry Pi) to Docker Hub with the version tag and `latest`.
3. **Persistent Volumes**: Database data lives only in the named volumes above, which survive `docker compose down` (only `down -v` removes them). Backups are taken with `scripts/backup_postgres.sh`.
4. **Network Aliases**: The `file4base-api` container must declare network aliases `api`, `server`, and `file4base-api` within `file4base-net` so internal reverse proxies and services can resolve it deterministically.

## 6. Security Architecture

### Sessions and database binding
- `POST /api/v1/auth/login` verifies a username and password against the `sys_users` table of one database and returns an opaque bearer token. The session records the user, role and **database**.
- `AuthMiddleware.Authenticate` resolves the token on every protected request and places the session and the connection driver of the session's database in the request context (`dbal.WithDriver`). Handlers build their services from that driver, so a request can only reach the database it signed in to.
- `MultiDatabaseManager` holds no caller state: its default (administrative) database is fixed at startup and only used to list, create and drop databases. There is no global "active database".
- Sessions are kept in memory (`internal/auth`), expire after `SESSION_TTL` of inactivity and are revoked when the user's credentials, role or status change, when the user is deleted, or when the database is dropped. Only the SHA-256 digest of each token is stored.

### Authorization
- Every protected route group installs `RequireSession` (deny by default), and mutating routes add `RequireAdmin` / `RequireOwner`.
- Layout permissions (`sys_user_permissions`) are enforced by the server: layouts with `none` access are hidden from regular users, saving a layout requires `read_write`, and the data API derives a per-table access level from the layouts built on each table.
- The data API only serves tables registered in `sys_tables` and fields registered in `sys_columns`. System tables are unreachable through it, including for owners. Data imports are restricted to catalog tables as well.

### Account provisioning
- There are no default accounts. `POST /api/v1/databases` creates a database together with its first owner, using the credentials supplied by the caller, and never modifies an existing database.
- Signing in never creates tables or accounts.

### Known limits
- Sessions do not survive a server restart and are not shared between server replicas.
- Transport security (TLS) is expected to be provided by a reverse proxy in front of the API.
- There is no sign-in rate limiting yet.

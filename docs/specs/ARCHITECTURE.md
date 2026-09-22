# File4Base Architecture Specification

## 1. System Overview
File4Base replicates the integrated database application paradigm using a modern decoupled architecture:
- **Server**: Go (1.22+) application running in Docker or standalone. Features a database abstraction layer (DBAL) supporting PostgreSQL (primary) and MariaDB/MySQL/SQLite (alternative engines), dynamic DDL/metadata management, dynamic CRUD API, WebSocket event streaming, and calculation execution.
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
file4base/
├── .agents/
│   └── rules/
│       └── language.md               # English language standard
├── .antigravity/
│   └── rules.md                      # Antigravity operational guidelines
├── AGENTS.md                         # Root agent guidelines
├── docker-compose.yml                # PostgreSQL + Server services
├── Makefile
├── docs/
│   ├── ARCHITECTURE.md               # High-level architecture
│   ├── ROADMAP.md                    # Detailed phase-by-phase roadmap
│   └── specs/
│       ├── DBAL_SPECIFICATION.md     # DBAL & multi-engine dialect specs
│       ├── file4base_menu_reference_guide.md # Menu bar & command guide
│       └── layout_schema.json        # JSON schema for layout renderer
├── assets/
│   └── branding/
│       ├── file4base-icon.jpg
│       ├── file4base-dark.svg
│       └── file4base-light.svg
├── server/                           # Go Backend
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── dbal/                     # Database Abstraction Layer
│   │   │   ├── dialect.go            # Dialect interfaces
│   │   │   ├── postgres/             # PostgreSQL dialect implementation
│   │   │   ├── mariadb/              # MariaDB dialect implementation
│   │   │   └── query_builder.go      # Agnostic AST to SQL builder
│   │   ├── schema/                   # sys_tables, sys_columns, DDL
│   │   ├── data/                     # Generic dynamic CRUD
│   │   ├── calc/                     # Calculation engine
│   │   └── websocket/                # Live sync hub
│   └── go.mod
└── client/                           # Flutter Desktop Client
    ├── lib/
    │   ├── core/                     # API client, theme, router
    │   ├── features/
    │   │   ├── menu/                 # Canonical file4base menu bar
    │   │   ├── modes/                # Browse / Find / Layout / Preview modes
    │   │   ├── layout_engine/        # Dynamic JSON layout parser
    │   │   ├── schema_manager/       # Relationship graph & field inspector
    │   │   └── script_workspace/     # Visual action block editor
    └── pubspec.yaml
```

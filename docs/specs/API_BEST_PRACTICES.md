# File4Base API Best Practices & Architecture Specification

## 1. Introduction & Core Principles

This specification establishes the official engineering standards, operational best practices, and architectural requirements for all RESTful and cloud-native APIs within the **File4Base** ecosystem.

All backend services (written in Go), API gateways, external client connectors, and third-party integrations must strictly comply with these guidelines.

### Core Architectural Pillars
1. **Predictability**: Deterministic resource modeling, strict HTTP semantics, uniform error envelopes, and consistent pagination.
2. **Observability**: First-class OpenTelemetry (OTel) instrumentation for distributed tracing, metrics (RED/USE), and structured logging.
3. **Resilience & Cloud-Native Lifecycle**: Kubernetes/Docker-native health checks (Liveness, Readiness, Startup) and graceful shutdown procedures.
4. **Backward Compatibility**: Non-breaking contract evolution backed by Semantic Versioning (SemVer 2.0.0) and standardized deprecation lifecycles.
5. **Security-by-Design**: Zero-trust authorization, input sanitization, rate limiting, and defensive HTTP security headers.

---

## 2. API Versioning & Lifecycle Management

### 2.1 Versioning Strategy: URI Path Prefixing
File4Base enforces **URI path prefixing** for major API versions to provide clear contract boundaries, straightforward CDN/proxy routing, and unambiguous client targeting:

```
http(s)://{host}:{port}/api/v{MAJOR}/{resource}
```

*Example*: `/api/v1/schemas/tables`, `/api/v1/data/customers/records`

### 2.2 Semantic Versioning (SemVer 2.0.0) Rules
- **MAJOR (e.g. `/api/v1` -> `/api/v2`)**: Reserved for incompatible breaking changes:
  - Removing or renaming an endpoint.
  - Removing a field from a request or response payload.
  - Altering the data type or semantic meaning of an existing field.
  - Making a previously optional field required.
  - Changing error code structures or authentication protocols.
- **MINOR**: Backward-compatible additive enhancements (retains current `/api/v1` prefix):
  - Adding new optional request query parameters or request body fields.
  - Adding new response fields.
  - Introducing new resource endpoints.
- **PATCH**: Backward-compatible internal bug fixes, performance optimizations, and security patches.

### 2.3 Non-Breaking Evolution Rules
To maintain forward and backward compatibility within the same major version:
- **Clients must be lenient readers**: Clients should ignore unrecognized fields in JSON responses (`tolerant reader` pattern).
- **Servers must be conservative writers**: Never alter the meaning of existing JSON keys.
- **Default values**: Any newly introduced request field must be optional and accompanied by a sensible default value.

### 2.4 Standardized Deprecation & Sunset (RFC 8594)
When an API endpoint or version is marked for deprecation, the server must emit the standardized IETF RFC 8594 headers on all responses:

```http
HTTP/1.1 200 OK
Deprecation: @1790240000
Sunset: Wed, 24 Mar 2027 00:00:00 GMT
Link: <https://api.file4base.com/api/v2/data/records>; rel="successor-version"
Content-Type: application/json
```

- **`Deprecation`**: Unix timestamp (`@...`) or HTTP date marking when the endpoint was deprecated.
- **`Sunset`**: Mandatory future HTTP date indicating when the endpoint will be permanently decommissioned.
- **`Link`**: Link header with `rel="successor-version"` pointing to the migration path or new endpoint.
- **Grace Period**: The sunset date must be at least **6 months** (preferably 12 months) from the announcement date.

---

## 3. Cloud-Native Health Checks & Lifecycle Probes

Health checks enable container orchestrators (Kubernetes, Docker Swarm, Nomad) and load balancers (Nginx, Envoy, Traefik) to manage container lifecycles safely.

File4Base implements a **three-probe architecture** plus a detailed diagnostic endpoint.

### 3.1 Probe Architecture Overview

| Probe | Endpoint | Target Audience | Checks Performed | Fail Action |
|---|---|---|---|---|
| **Liveness** | `GET /healthz/liveness` (or `/livez`) | Kubelet / Container Engine | Deadlock, process unresponsive, fatal runtime panic | Restart container |
| **Readiness** | `GET /healthz/readiness` (or `/readyz`) | Ingress / Service Load Balancer | DB connection pool, active migrations, cache ready | Route traffic away from instance |
| **Startup** | `GET /healthz/startup` (or `/startupz`) | Kubelet during slow boots | Schema verification, heavy data migrations | Delays liveness checking |
| **Diagnostic** | `GET /healthz` | DevOps, SREs, Monitoring Agents | Deep system diagnostics, DB latency, pool stats | Status visibility |

---

### 3.2 Liveness Probe (`GET /healthz/liveness`)
- **Golden Rule**: **NEVER check external dependencies (database, cache, third-party APIs) in the liveness probe.**
- If the database goes down, restarting 50 application pods simultaneously will trigger a cascading failure (thundering herd).
- **Logic**: Returns `200 OK` immediately if the Go HTTP server loop is functioning and memory is within limits. Execution time must be `< 5ms`.

#### Response:
```json
{
  "status": "alive"
}
```

---

### 3.3 Readiness Probe (`GET /healthz/readiness`)
- **Purpose**: Asserts whether this specific replica can accept user requests right now.
- **Checks**:
  1. Primary Database connection pool (`db.PingContext(ctx)` with a 2-second timeout).
  2. Database migration state (are schema migrations still executing?).
  3. Instance drain flag (is instance undergoing graceful shutdown?).
- **HTTP Status Codes**:
  - `200 OK`: All critical subsystems ready.
  - `503 Service Unavailable`: Subsystem unready (ingress stops routing traffic).

#### Response (`200 OK`):
```json
{
  "status": "ready",
  "database": "connected",
  "latency_ms": 1.2
}
```

#### Response (`503 Service Unavailable`):
```json
{
  "status": "not_ready",
  "database": "connection_timeout",
  "reason": "PostgreSQL pool ping failed after 2000ms"
}
```

---

### 3.4 Deep Diagnostic Health (`GET /healthz`)
Adheres to the IETF Draft standard for HTTP API Health Checks:

#### Response (`200 OK`):
```json
{
  "status": "pass",
  "version": "0.4.6",
  "release_id": "file4base-server-v0.4.6",
  "service_id": "file4base-core-backend",
  "description": "File4Base Core Engine Health",
  "checks": {
    "database:postgres": [
      {
        "component_type": "datastore",
        "observed_value": "connected",
        "status": "pass",
        "time": "2026-09-24T10:45:00Z",
        "output": "pool_open=5, pool_idle=3, pool_in_use=2, latency_ms=1.1"
      }
    ],
    "memory:runtime": [
      {
        "component_type": "system",
        "observed_value": "34MB",
        "status": "pass",
        "time": "2026-09-24T10:45:00Z"
      }
    ]
  }
}
```

---

### 3.5 Graceful Shutdown Lifecycle
All File4Base API servers must implement graceful shutdown:
1. Intercept `SIGTERM` and `SIGINT`.
2. Immediately flag `/healthz/readiness` as `503 Service Unavailable` so upstream load balancers remove the instance from active pools.
3. Wait for a configured grace period (e.g. 5–10s) to allow existing in-flight HTTP requests and reverse-proxy connections to drain.
4. Call `httpServer.Shutdown(ctx)` with a shutdown timeout (e.g. 30s).
5. Close database connection pools, flush pending audit events and OpenTelemetry buffers.
6. Exit process with code `0`.

---

## 4. OpenTelemetry (OTel) Observability Standards

OpenTelemetry is the sole telemetry collection and propagation standard across File4Base.

```
Incoming Request (W3C traceparent)
       │
       ▼
┌────────────────────────────────────────────────────────┐
│ HTTP Ingress Middleware (Go chi)                       │
│ - Extract W3C Context (`traceparent`, `tracestate`)    │
│ - Start Server Span: `HTTP GET /api/v1/schemas/tables` │
│ - Record Metric: `http.server.request.duration`        │
└──────────────────────────┬─────────────────────────────┘
                           │
       ┌───────────────────┴───────────────────┐
       ▼                                       ▼
┌──────────────────────────────┐   ┌──────────────────────────────┐
│ Service & Domain Span        │   │ DBAL / SQL Span              │
│ - `db.system: postgresql`    │   │ - Query AST translation      │
│ - `db.statement: SELECT ...` │   │ - Connection checkout        │
│ - `db.operation: SELECT`     │   │ - Record: `db.client.dur`    │
└──────────────────────────────┘   └──────────────────────────────┘
                           │
                           ▼
              ┌──────────────────────────┐
              │ JSON Structured Log      │
              │ - Include `trace_id`     │
              │ - Include `span_id`      │
              └──────────────────────────┘
```

---

### 4.1 Distributed Tracing & W3C TraceContext
- All services must support W3C TraceContext specification (`traceparent` and `tracestate` headers):
  ```http
  traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
  ```
- **Span Naming**: Must follow OpenTelemetry Semantic Conventions v1.26+:
  - Server spans: `HTTP {METHOD} {route_template}` (e.g., `HTTP GET /api/v1/schemas/tables/{id}`).
  - Client spans: `HTTP {METHOD}` (e.g., `HTTP POST`).
  - Database spans: `{db.operation} {db.name}.{target}` (e.g., `SELECT sys_tables`).

#### Mandatory Span Attributes

| Category | Attribute Key | Example Value | Description |
|---|---|---|---|
| **HTTP** | `http.request.method` | `"GET"` | HTTP verb |
| **HTTP** | `http.response.status_code`| `200` | HTTP response code |
| **HTTP** | `url.path` | `"/api/v1/data/products"` | Normalized request path |
| **HTTP** | `url.scheme` | `"https"` | Protocol scheme |
| **HTTP** | `network.peer.address` | `"192.168.1.45"` | Client IP address |
| **HTTP** | `user_agent.original` | `"File4Base-Desktop/0.4.6"` | Full user agent string |
| **Database** | `db.system` | `"postgresql"` or `"mariadb"` | Dialect/engine identifier |
| **Database** | `db.name` | `"file4base_dev"` | Active catalog database |
| **Database** | `db.operation` | `"SELECT"`, `"UPDATE"` | SQL operation verb |
| **Database** | `db.statement` | `"SELECT * FROM sys_tables WHERE id = $1"` | Sanitized parameterized SQL (no literals) |
| **File4Base** | `file4base.table.id` | `"366f22cf-0054-4a84..."` | Table GUID |
| **File4Base** | `file4base.user` | `"admin"` | Authenticated identity |

---

### 4.2 Standard OpenTelemetry Metrics (RED Method)
File4Base services must export metrics in OpenTelemetry format (OTLP/gRPC or Prometheus exporter):

1. **Rate**:
   - `http.server.active_requests` (UpDownCounter, `{request}`)
   - `http.server.request.body.size` (Histogram, `By`)
2. **Errors**:
   - Total errors derived from `http.server.request.duration` with tag `http.response.status_code >= 400`
3. **Duration**:
   - `http.server.request.duration` (Histogram, `s` or `ms`):
     - Buckets: `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]`
4. **Database Pool Metrics (USE Method)**:
   - `db.client.connections.usage` (Gauge): Current in-use vs idle connections.
   - `db.client.connections.max` (Gauge): Maximum configured pool size.
   - `db.client.connections.wait_duration` (Histogram): Time spent waiting for an available connection from pool.

---

### 4.3 Structured Logging & Trace-Log Correlation
- Logs must be formatted as **single-line JSON** in production.
- **Mandatory Fields**:
  - `timestamp`: RFC 3339 / ISO 8601 UTC timestamp (`2026-09-24T10:45:00.123456Z`).
  - `level`: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`.
  - `message`: Human-readable description.
  - `trace_id`: Correlated W3C trace ID (16-byte hex).
  - `span_id`: Correlated current span ID (8-byte hex).
  - `service.name`: `"file4base-server"`.
  - `service.version`: `"0.4.6"`.
- **Sensitive Data Redaction**: Passwords, authorization bearer tokens, API keys, and connection strings containing credentials must be masked (`***REDACTED***`).

#### Example Log Record:
```json
{
  "timestamp": "2026-09-24T10:45:00.123456Z",
  "level": "INFO",
  "message": "Switched active database context successfully",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "service.name": "file4base-server",
  "service.version": "0.4.6",
  "http.method": "POST",
  "http.path": "/api/v1/schemas/databases/actions/switch",
  "http.status_code": 200,
  "file4base.database": "file4base_dev",
  "file4base.user": "admin",
  "duration_ms": 14.2
}
```

---

## 5. REST Resource Modeling & HTTP Semantics

### 5.1 Resource-Oriented URL Naming
- Use **plural nouns** for resource collections: `/api/v1/schemas/tables`, `/api/v1/data/records`.
- Use **kebab-case** or **snake_case** consistently (File4Base standard: `snake_case` in JSON properties, `kebab-case` in URL slugs).
- Avoid action verbs in URLs; express actions through HTTP methods whenever possible:
  - Good: `DELETE /api/v1/schemas/tables/{id}`
  - Bad: `POST /api/v1/schemas/deleteTable?id=123`
- Non-CRUD RPC-style actions are nested under an `/actions/` sub-resource:
  - `POST /api/v1/schemas/databases/actions/switch`
  - `POST /api/v1/solutions/actions/export`

### 5.2 HTTP Methods & Idempotency Matrix

| Method | Safe | Idempotent | Usage in File4Base | Success Status |
|---|---|---|---|---|
| `GET` | Yes | Yes | Retrieve resource metadata or record set | `200 OK` |
| `HEAD` | Yes | Yes | Check existence or header metadata | `200 OK` |
| `POST` | No | No | Create record, add column, trigger action | `201 Created` / `200 OK` |
| `PUT` | No | Yes | Replace full resource or update existing | `200 OK` |
| `PATCH` | No | Yes/No | Partial update of specific record fields | `200 OK` |
| `DELETE` | No | Yes | Drop column, drop table, delete record | `204 No Content` / `200 OK` |

### 5.3 Pagination, Filtering, and Sorting
For collections that can grow indefinitely (e.g. data records, audit logs):

1. **Cursor-Based Pagination (Recommended for large datasets)**:
   - Query: `GET /api/v1/data/products/records?cursor=eyJpZCI6MTAwfQ&limit=50`
   - Response includes:
     ```json
     {
       "data": [ ... ],
       "pagination": {
         "limit": 50,
         "has_more": true,
         "next_cursor": "eyJpZCI6MTUwfQ"
       }
     }
     ```
2. **Offset-Based Pagination (Supported for small datasets & admin grids)**:
   - Query: `GET /api/v1/schemas/tables?page=1&page_size=20`
   - Response envelope:
     ```json
     {
       "data": [ ... ],
       "pagination": {
         "page": 1,
         "page_size": 20,
         "total_count": 8,
         "total_pages": 1
       }
     }
     ```
3. **Sorting**:
   - Parameter: `sort=column_name:asc` or `sort=column_name:desc` (comma-separated for multi-column sorts).
4. **Filtering**:
   - Format: `filter[column_name]=value` or operator format `filter[price][gte]=100`.

---

## 6. Standardized Error Handling (RFC 9457 / RFC 7807)

File4Base adopts **RFC 9457 (Problem Details for HTTP APIs)** as the universal error format.

- Content-Type: `application/problem+json`
- Every error response must contain standard machine-readable keys:
  - `type`: URI identifying the error problem type.
  - `title`: Short, human-readable summary of problem.
  - `status`: HTTP status code matching the response.
  - `detail`: Human-readable explanation specific to this occurrence.
  - `instance`: URI reference identifying the specific request.
  - `invalid_params`: Array of parameter validation errors (if applicable).
  - `trace_id`: Correlated OpenTelemetry trace ID for immediate log lookup.

#### Example Validation Error (`422 Unprocessable Content`):
```json
{
  "type": "https://file4base.org/errors/validation-failed",
  "title": "Validation Failed",
  "status": 422,
  "detail": "One or more columns contain invalid data constraints",
  "instance": "/api/v1/schemas/tables/prod-1/columns",
  "invalid_params": [
    {
      "name": "field_type",
      "reason": "'BINARY_BLOB' is not a supported field type in current dialect"
    },
    {
      "name": "name",
      "reason": "Column name contains illegal characters. Only [a-z0-9_] allowed."
    }
  ],
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

#### Example Resource Not Found (`404 Not Found`):
```json
{
  "type": "https://file4base.org/errors/not-found",
  "title": "Resource Not Found",
  "status": 404,
  "detail": "Table with ID 'de9c04e4-03e8-4473-9fa4-9e356f75dc48' does not exist in catalog",
  "instance": "/api/v1/schemas/tables/de9c04e4-03e8-4473-9fa4-9e356f75dc48",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

---

## 7. Security, Rate Limiting & Defense-in-Depth

### 7.1 Authentication & Authorization
- **Bearer Token**: `Authorization: Bearer <JWT>` for stateless, horizontally scalable API authentication.
- **Session Tokens**: Handled via standard HTTP-only, secure, `SameSite=Strict` cookies or explicit `X-Session-Token` headers for WebDirect sessions.
- **Role-Based Access Control (RBAC)**: Privilege sets (`[Full Access]`, `[Data Entry]`, `[Read Only]`) enforced at the DBAL and handler layers before any SQL generation occurs.

### 7.2 Defensive Security Headers
Every HTTP response emitted by File4Base server and Nginx reverse proxy must include:

```http
Strict-Transport-Security: max-age=63072000; includeSubDomains; preload
X-Content-Type-Options: nosniff
X-Frame-Options: SAMEORIGIN
Referrer-Policy: strict-origin-when-cross-origin
Content-Security-Policy: default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data: blob:; connect-src 'self' ws: wss:;
Permissions-Policy: camera=(), microphone=(), geolocation=()
```

### 7.3 Rate Limiting Standards (IETF Draft)
To prevent brute-force attacks and noisy-neighbor database exhaustion:
- Return standard IETF rate limit headers on every response:
  ```http
  RateLimit-Limit: 1000
  RateLimit-Remaining: 987
  RateLimit-Reset: 42
  ```
- When limit is exceeded, return `429 Too Many Requests`:
  ```http
  HTTP/1.1 429 Too Many Requests
  Retry-After: 42
  Content-Type: application/problem+json

  {
    "type": "https://file4base.org/errors/rate-limit-exceeded",
    "title": "Rate Limit Exceeded",
    "status": 429,
    "detail": "Quota of 1000 requests per minute exceeded. Retry in 42 seconds.",
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
  }
  ```

---

## 8. Implementation Checklist for API Endpoints

Before merging or publishing any new File4Base REST API endpoint, engineers must verify:

- [ ] **Routing & Path**: Uses canonical `/api/v{MAJOR}/` prefix with plural nouns.
- [ ] **Semantics**: Uses correct HTTP verbs and status codes (`201` for create with `Location`, `204` for void deletes).
- [ ] **OpenTelemetry Spans**: Middleware extracts W3C trace context, spans have standard semantic attributes (`http.*`, `db.*`).
- [ ] **Metrics**: Duration histogram and active request counters recorded.
- [ ] **Structured Logging**: Emits JSON log with `trace_id` and `span_id` attached.
- [ ] **Health Checks**: Liveness never touches DB; readiness verifies DB pool and returns `503` if dead.
- [ ] **Error Handling**: Formatted as RFC 9457 `application/problem+json` with `trace_id`.
- [ ] **Security**: Parameterized queries only (SQL injection immunity), inputs validated, auth checked.
- [ ] **Documentation**: Documented in `docs/api/API_REFERENCE.md` with request and response payloads.
- [ ] **Tests**: Unit tests for handlers and integration tests verifying HTTP status codes and payloads.

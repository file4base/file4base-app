#!/usr/bin/env bash
# Checks the secure defaults of both compose stacks (#202) with
# `docker compose config`, which only renders the files: nothing is started
# and no Docker daemon is needed.
#
#   - without passwords (or with only .env.example) the stack refuses to
#     start, naming what to set;
#   - with them, the passwords reach the database and the API's DSN, the
#     database publishes no port, CORS allows no other origin, and public
#     database creation is off, also with the shipped .env.example;
#   - docker-compose.first-run.yml publishes every port on 127.0.0.1 only.
#
# A local .env and the caller's environment are ignored.
#
#   ./scripts/tests/compose_defaults_test.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT
FAILURES=0

fail() {
    echo "FAIL: $*" >&2
    FAILURES=$((FAILURES + 1))
}

# compose FILE ENV_FILE ARGS...: docker compose with a clean environment.
# FILE may name several files separated by ":".
compose() {
    local files="$1" env_file="$2" f
    shift 2
    local args=()
    IFS=: read -ra parts <<<"${files}"
    for f in "${parts[@]}"; do args+=(-f "${ROOT}/${f}"); done
    env -i PATH="${PATH}" HOME="${HOME}" \
        docker compose --project-directory "${ROOT}" --env-file "${env_file}" "${args[@]}" "$@"
}

: > "${WORK}/empty.env"
cat > "${WORK}/secrets.env" <<'ENV'
POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef
MARIADB_PASSWORD=fedcba9876543210fedcba9876543210
MARIADB_ROOT_PASSWORD=00112233445566778899aabbccddeeff
ENV
grep -v '^MARIADB_ROOT_PASSWORD=' "${WORK}/secrets.env" > "${WORK}/no-root.env"
grep -v '^MARIADB_PASSWORD=' "${WORK}/secrets.env" > "${WORK}/no-app.env"
# The same, after the shipped example (a later line wins).
for f in secrets no-root no-app; do
    cat "${ROOT}/.env.example" "${WORK}/${f}.env" > "${WORK}/example-${f}.env"
done

# Reads the rendered configuration (JSON) on stdin; argument: the database
# service. Prints each insecure default and fails if there is one.
cat > "${WORK}/check.py" <<'PY'
import json, sys
db, secrets_file = sys.argv[1], sys.argv[2]
secrets = dict(l.strip().split("=", 1) for l in open(secrets_file) if "=" in l)
services = json.load(sys.stdin)["services"]
problems = []
wanted = ["POSTGRES_PASSWORD"] if db == "postgres" else ["MARIADB_PASSWORD", "MARIADB_ROOT_PASSWORD"]
for name in wanted:
    if services[db]["environment"].get(name) != secrets[name]:
        problems.append(f"{db}: {name} is not the value from .env")
dsn = {
    "postgres": f"postgres://file4base:{secrets['POSTGRES_PASSWORD']}@postgres:5432/postgres?sslmode=disable",
    "mariadb": f"file4base:{secrets['MARIADB_PASSWORD']}@tcp(mariadb:3306)/mysql",
}[db]
if services["api"]["environment"].get("DATABASE_URL") != dsn:
    problems.append(f"api: DATABASE_URL is not the expected DSN with {wanted[0]} from .env")
if services[db].get("ports"):
    problems.append(f"{db} publishes {services[db]['ports']}")
env = services["api"]["environment"]
if env.get("CORS_ALLOWED_ORIGINS") != "":
    problems.append(f"CORS_ALLOWED_ORIGINS defaults to {env.get('CORS_ALLOWED_ORIGINS')!r}")
if env.get("ALLOW_PUBLIC_DATABASE_CREATION") != "false":
    problems.append(f"ALLOW_PUBLIC_DATABASE_CREATION defaults to {env.get('ALLOW_PUBLIC_DATABASE_CREATION')!r}")
for name, value in env.items():
    if "dev_password" in str(value) or "root_password" in str(value):
        problems.append(f"{name} still carries a known password")
for p in problems:
    print(p, file=sys.stderr)
sys.exit(1 if problems else 0)
PY

# Reads the rendered configuration (JSON) on stdin and fails if a port is
# published beyond 127.0.0.1.
cat > "${WORK}/first_run.py" <<'PY'
import json, sys
bad = []
for name, svc in json.load(sys.stdin)["services"].items():
    for port in svc.get("ports", []):
        if port.get("host_ip") != "127.0.0.1":
            bad.append(f"{name}: {port.get('host_ip') or 'all interfaces'}:{port['published']}")
for b in bad:
    print(b, file=sys.stderr)
sys.exit(1 if bad else 0)
PY

# check_refused FILE ENV_FILE VARIABLE...
check_refused() {
    local file="$1" env_file="$2"
    shift 2
    local out
    if out="$(compose "${file}" "${env_file}" config -q 2>&1)"; then
        fail "${file} with ${env_file##*/}: starts without ${*:-a password}"
        return
    fi
    # Compose may stop at the first missing variable, so pass one variable
    # per call when several are missing; none only checks the refusal.
    for var in "$@"; do
        if ! grep -q "required variable ${var} is missing a value: set it in .env" <<<"${out}"; then
            fail "${file}: the error does not say how to set ${var}: ${out}"
        fi
    done
}

# check_defaults FILE DB_SERVICE ENV_FILE
check_defaults() {
    local file="$1" db="$2" env_file="$3" json
    if ! json="$(compose "${file}" "${env_file}" config --format json 2>&1)"; then
        fail "${file} with ${env_file##*/}: does not render with the passwords set: ${json}"
        return
    fi
    if ! python3 -I "${WORK}/check.py" "${db}" "${WORK}/secrets.env" <<<"${json}"; then
        fail "${file} with ${env_file##*/}: insecure default (see above)"
    fi
}

# check_first_run FILE: with docker-compose.first-run.yml, no port is
# published beyond 127.0.0.1.
check_first_run() {
    local file="$1" json
    if ! json="$(compose "${file}:docker-compose.first-run.yml" "${WORK}/secrets.env" config --format json 2>&1)"; then
        fail "${file} + first-run: does not render: ${json}"
        return
    fi
    if ! python3 -I "${WORK}/first_run.py" <<<"${json}"; then
        fail "${file} + first-run: a port is published beyond 127.0.0.1 (see above)"
    fi
}

check_refused docker-compose.yml "${WORK}/empty.env" POSTGRES_PASSWORD
check_refused docker-compose.yml "${ROOT}/.env.example" POSTGRES_PASSWORD
check_refused docker-compose.mariadb.yml "${WORK}/empty.env"
check_refused docker-compose.mariadb.yml "${WORK}/no-root.env" MARIADB_ROOT_PASSWORD
check_refused docker-compose.mariadb.yml "${WORK}/no-app.env" MARIADB_PASSWORD
check_refused docker-compose.mariadb.yml "${WORK}/example-no-root.env" MARIADB_ROOT_PASSWORD
check_refused docker-compose.mariadb.yml "${WORK}/example-no-app.env" MARIADB_PASSWORD
for env_file in "${WORK}/secrets.env" "${WORK}/example-secrets.env"; do
    check_defaults docker-compose.yml postgres "${env_file}"
    check_defaults docker-compose.mariadb.yml mariadb "${env_file}"
done
check_first_run docker-compose.yml
check_first_run docker-compose.mariadb.yml

if [ "${FAILURES}" -gt 0 ]; then
    echo "${FAILURES} check(s) failed" >&2
    exit 1
fi
echo "compose defaults: all checks passed"

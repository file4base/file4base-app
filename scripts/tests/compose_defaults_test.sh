#!/usr/bin/env bash
# Checks the secure defaults of both compose stacks (#202) with
# `docker compose config`, which only renders the files: nothing is started
# and no Docker daemon is needed.
#
#   - without passwords the stack refuses to start, naming what to set;
#   - with them, the database publishes no port, CORS allows no other
#     origin, and public database creation is off.
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
compose() {
    local file="$1" env_file="$2"
    shift 2
    env -i PATH="${PATH}" HOME="${HOME}" \
        docker compose --project-directory "${ROOT}" --env-file "${env_file}" -f "${ROOT}/${file}" "$@"
}

: > "${WORK}/empty.env"
cat > "${WORK}/secrets.env" <<'ENV'
POSTGRES_PASSWORD=0123456789abcdef0123456789abcdef
MARIADB_PASSWORD=fedcba9876543210fedcba9876543210
MARIADB_ROOT_PASSWORD=00112233445566778899aabbccddeeff
ENV
grep -v '^MARIADB_ROOT_PASSWORD=' "${WORK}/secrets.env" > "${WORK}/no-root.env"

# Reads the rendered configuration (JSON) on stdin; argument: the database
# service. Prints each insecure default and fails if there is one.
cat > "${WORK}/check.py" <<'PY'
import json, sys
db = sys.argv[1]
services = json.load(sys.stdin)["services"]
problems = []
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

# check_refused FILE ENV_FILE VARIABLE...
check_refused() {
    local file="$1" env_file="$2"
    shift 2
    local out
    if out="$(compose "${file}" "${env_file}" config -q 2>&1)"; then
        fail "${file}: starts without $*"
        return
    fi
    for var in "$@"; do
        if ! grep -q "required variable ${var} is missing a value: set it in .env" <<<"${out}"; then
            fail "${file}: the error does not say how to set ${var}: ${out}"
        fi
    done
}

# check_defaults FILE DB_SERVICE
check_defaults() {
    local file="$1" db="$2" json
    if ! json="$(compose "${file}" "${WORK}/secrets.env" config --format json 2>&1)"; then
        fail "${file}: does not render with the passwords set: ${json}"
        return
    fi
    if ! python3 -I "${WORK}/check.py" "${db}" <<<"${json}"; then
        fail "${file}: insecure default (see above)"
    fi
}

check_refused docker-compose.yml "${WORK}/empty.env" POSTGRES_PASSWORD
check_refused docker-compose.mariadb.yml "${WORK}/empty.env" MARIADB_PASSWORD MARIADB_ROOT_PASSWORD
check_refused docker-compose.mariadb.yml "${WORK}/no-root.env" MARIADB_ROOT_PASSWORD
check_defaults docker-compose.yml postgres
check_defaults docker-compose.mariadb.yml mariadb

if [ "${FAILURES}" -gt 0 ]; then
    echo "${FAILURES} check(s) failed" >&2
    exit 1
fi
echo "compose defaults: all checks passed"

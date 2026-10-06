#!/usr/bin/env bash
# Tests for scripts/backup_postgres.sh restore (#21).
#
# A fake `docker` on PATH stands in for the containers, so no real database
# or container is touched. Each case sets how the fake behaves and checks the
# exit status, the messages and which docker commands ran.
#
#   ./scripts/tests/backup_postgres_test.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${ROOT}/scripts/backup_postgres.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT
mkdir -p "${WORK}/bin"

cat > "${WORK}/bin/docker" <<'FAKE'
#!/usr/bin/env bash
# Fake docker: behaviour comes from FAKE_* variables, calls go to FAKE_LOG.
echo "docker $*" >> "${FAKE_LOG}"
case "$1" in
    inspect)
        name="${@: -1}"
        if [ "${name}" = "file4base-api" ]; then echo "${FAKE_API_RUNNING:-true}"; else echo true; fi ;;
    stop)  [ "${FAKE_STOP_FAIL:-0}" = 1 ] && exit 1; exit 0 ;;
    start) [ "${FAKE_START_FAIL:-0}" = 1 ] && exit 1; exit 0 ;;
    exec)
        cat > "${FAKE_SQL}"
        exit "${FAKE_PSQL_EXIT:-0}" ;;
esac
FAKE
chmod +x "${WORK}/bin/docker"

DUMP="${WORK}/backup.sql.gz"
cat <<'SQL' | gzip > "${DUMP}"
--
-- PostgreSQL database cluster dump
--
SET default_transaction_read_only = off;
DROP DATABASE IF EXISTS invoices_db;
DROP ROLE IF EXISTS file4base;
CREATE ROLE file4base;
ALTER ROLE file4base WITH SUPERUSER INHERIT CREATEROLE CREATEDB LOGIN;
CREATE DATABASE invoices_db WITH TEMPLATE = template0 ENCODING = 'UTF8';
SQL
echo "not gzip" > "${WORK}/corrupt.sql.gz"
echo "CREATE TABLE x();" | gzip > "${WORK}/not_a_dump.sql.gz"

failures=0
run_case() {
    local name="$1" expected_status="$2" expected_text="$3"; shift 3
    : > "${WORK}/log"; : > "${WORK}/sql"
    local out status
    out="$(env PATH="${WORK}/bin:${PATH}" FAKE_LOG="${WORK}/log" FAKE_SQL="${WORK}/sql" RESTORE_CONFIRM=restore "$@" \
        "${SCRIPT}" restore "${ARCHIVE:-${DUMP}}" 2>&1)"
    status=$?
    if [ "${status}" != "${expected_status}" ] || ! grep -q -- "${expected_text}" <<< "${out}"; then
        echo "FAIL ${name}: exit ${status} (want ${expected_status}), output:"
        sed 's/^/    /' <<< "${out}"
        failures=$((failures + 1))
        return 1
    fi
    echo "ok   ${name}"
}
ran()     { grep -q -- "$1" "${WORK}/log"; }
not_ran() { ! grep -q -- "$1" "${WORK}/log"; }
check()   { if ! "$@"; then echo "FAIL     ${CASE}: expected $*"; failures=$((failures + 1)); fi; }

CASE="successful restore"
run_case "${CASE}" 0 "Restore finished from" &&
    { check ran "stop file4base-api"; check ran "start file4base-api"; check ran "ON_ERROR_STOP=1"
      check not_ran "ON_ERROR_STOP=0"
      check grep -q "ALTER ROLE file4base" "${WORK}/sql"
      check grep -q "CREATE DATABASE invoices_db" "${WORK}/sql"
      check bash -c "! grep -q -e 'DROP ROLE IF EXISTS file4base;' -e 'CREATE ROLE file4base;' '${WORK}/sql'"; }

CASE="SQL error"
run_case "${CASE}" 2 "Restore FAILED part-way" FAKE_PSQL_EXIT=3 &&
    { check ran "stop file4base-api"; check not_ran "start file4base-api"
      check bash -c "! grep -q 'Restore finished' <<< ''"; }

CASE="API cannot be stopped"
run_case "${CASE}" 1 "could not be stopped" FAKE_STOP_FAIL=1 &&
    { check not_ran "docker exec"; check not_ran "start file4base-api"; }

CASE="API cannot be started again"
run_case "${CASE}" 3 "could not be started again" FAKE_START_FAIL=1 &&
    check ran "docker exec"

CASE="API was already stopped"
run_case "${CASE}" 0 "Restore finished from" FAKE_API_RUNNING=false &&
    { check not_ran "stop file4base-api"; check not_ran "start file4base-api"; }

CASE="corrupt archive"
ARCHIVE="${WORK}/corrupt.sql.gz" run_case "${CASE}" 1 "not a valid gzip archive" &&
    { check not_ran "stop"; check not_ran "docker exec"; }

CASE="not a pg_dumpall backup"
ARCHIVE="${WORK}/not_a_dump.sql.gz" run_case "${CASE}" 1 "not a pg_dumpall backup" &&
    { check not_ran "stop"; check not_ran "docker exec"; }

CASE="confirmation declined"
run_case "${CASE}" 1 "Aborted, nothing was changed" RESTORE_CONFIRM=no &&
    { check not_ran "stop"; check not_ran "docker exec"; }

# A large archive must not trip the header check (SIGPIPE under pipefail)
BIG="${WORK}/big.sql.gz"
{ gunzip -c "${DUMP}"; for i in $(seq 1 200000); do echo "-- padding line ${i}"; done; } | gzip > "${BIG}"
CASE="large archive"
ARCHIVE="${BIG}" run_case "${CASE}" 0 "Restore finished from"

if [ "${failures}" -gt 0 ]; then
    echo "${failures} check(s) failed"
    exit 1
fi
echo "All restore checks passed"

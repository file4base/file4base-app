#!/usr/bin/env bash
set -euo pipefail

# File4Base PostgreSQL Backup & Restore Script
#
# Works against the running `file4base-postgres` container, whose data lives in
# the named Docker volume `file4base-postgres-data`.
#
#   ./scripts/backup_postgres.sh backup            # all databases + roles
#   ./scripts/backup_postgres.sh restore <file>    # restore a backup file
#   ./scripts/backup_postgres.sh list              # list existing backups
#
# Backups are written to ./backups (ignored by git), or to BACKUP_DIR if set.
# RESTORE_CONFIRM=restore skips the interactive confirmation.
#
# Restore outcome:
#   exit 0   the whole backup was applied and the API runs again (if it ran)
#   exit 1   nothing was changed (bad archive, API could not be stopped, ...)
#   exit 2   the restore failed part-way: the databases may be partially
#            restored and the API is left stopped; restore a backup again
#   exit 3   the backup was applied but the API could not be started again

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-${ROOT_DIR}/backups}"
CONTAINER="${POSTGRES_CONTAINER:-file4base-postgres}"
API_CONTAINER="${API_CONTAINER:-file4base-api}"
DB_USER="${DB_USER:-file4base}"
VERSION="$(tr -d '[:space:]' < "${ROOT_DIR}/VERSION")"

usage() {
    echo "Usage: $0 [backup|restore <file>|list]"
    exit 1
}

container_running() {
    [ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null)" = "true" ]
}

require_container() {
    if ! container_running "${CONTAINER}"; then
        echo "Container ${CONTAINER} is not running. Start it with: docker compose up -d postgres" >&2
        exit 1
    fi
}

backup() {
    require_container
    mkdir -p "${BACKUP_DIR}"
    local file="${BACKUP_DIR}/file4base-postgres-v${VERSION}-$(date +%Y%m%d-%H%M%S).sql.gz"
    local tmp="${file}.partial"

    echo "Backing up all databases from ${CONTAINER} ..."
    # pipefail makes a failing pg_dumpall fail the pipeline; the file only
    # gets its final name once the dump is complete and readable.
    if ! docker exec "${CONTAINER}" pg_dumpall -U "${DB_USER}" --clean --if-exists | gzip > "${tmp}"; then
        rm -f "${tmp}"
        echo "Backup FAILED: pg_dumpall did not complete." >&2
        exit 1
    fi
    if ! gzip -t "${tmp}"; then
        rm -f "${tmp}"
        echo "Backup FAILED: the written archive is not readable." >&2
        exit 1
    fi
    mv "${tmp}" "${file}"
    echo "Backup written to: ${file} ($(du -h "${file}" | cut -f1))"
}

# The dump recreates every role, including the one this restore connects
# with: PostgreSQL refuses to drop it ("current user cannot be dropped") and
# it already exists. Only those two statements are left out; every other
# statement must succeed.
filter_bootstrap_role() {
    grep -v -x -e "DROP ROLE IF EXISTS ${DB_USER};" -e "DROP ROLE ${DB_USER};" -e "CREATE ROLE ${DB_USER};"
}

restore() {
    local file="${1:-}"
    [ -n "${file}" ] || usage
    [ -f "${file}" ] || { echo "Backup file not found: ${file}" >&2; exit 1; }

    # 1. Check the archive before any downtime.
    if ! gzip -t "${file}" 2>/dev/null; then
        echo "Restore aborted, nothing was changed: ${file} is not a valid gzip archive." >&2
        exit 1
    fi
    # Read only the start: under pipefail, `gunzip | grep -q` would fail on a
    # large file when grep closes the pipe early.
    local header
    header="$(gunzip -c "${file}" 2>/dev/null | head -c 4096 || true)"
    if ! grep -q "PostgreSQL database cluster dump" <<< "${header}"; then
        echo "Restore aborted, nothing was changed: ${file} is not a pg_dumpall backup." >&2
        exit 1
    fi
    require_container

    echo "WARNING: this replaces the databases contained in ${file}."
    local answer="${RESTORE_CONFIRM:-}"
    if [ -z "${answer}" ]; then
        read -r -p "Type 'restore' to continue: " answer
    fi
    [ "${answer}" = "restore" ] || { echo "Aborted, nothing was changed."; exit 1; }

    # 2. Stop the API (its sessions would block DROP DATABASE), remembering
    #    whether it was running.
    local api_was_running=false
    if container_running "${API_CONTAINER}"; then
        api_was_running=true
        echo "Stopping ${API_CONTAINER} during the restore ..."
        if ! docker stop "${API_CONTAINER}" >/dev/null; then
            echo "Restore aborted, nothing was changed: ${API_CONTAINER} could not be stopped." >&2
            exit 1
        fi
    fi

    # 3. Apply the dump, stopping at the first SQL error.
    echo "Restoring ${file} ..."
    if ! gunzip -c "${file}" | filter_bootstrap_role |
        docker exec -i "${CONTAINER}" psql -U "${DB_USER}" -d postgres -v ON_ERROR_STOP=1 -q >/dev/null; then
        echo "Restore FAILED part-way: the databases may be partially restored." >&2
        echo "${API_CONTAINER} was left stopped. Fix the cause and restore a backup again before starting it." >&2
        exit 2
    fi

    # 4. Bring the API back only if it was running before.
    if [ "${api_was_running}" = true ]; then
        if ! docker start "${API_CONTAINER}" >/dev/null; then
            echo "Restore finished from: ${file}, but ${API_CONTAINER} could not be started again." >&2
            exit 3
        fi
    fi
    echo "Restore finished from: ${file}"
}

list() {
    ls -lh "${BACKUP_DIR}"/*.sql.gz 2>/dev/null || echo "No backups in ${BACKUP_DIR}"
}

case "${1:-}" in
    backup)  backup ;;
    restore) restore "${2:-}" ;;
    list)    list ;;
    *)       usage ;;
esac

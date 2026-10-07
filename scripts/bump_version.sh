#!/usr/bin/env bash
set -euo pipefail

# File4Base Automated Version Bumping Script

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION_FILE="${ROOT_DIR}/VERSION"
SERVER_MAIN="${ROOT_DIR}/server/cmd/server/main.go"
CLIENT_PUBSPEC="${ROOT_DIR}/client/pubspec.yaml"
ABOUT_DIALOG="${ROOT_DIR}/client/lib/features/about/about_dialog.dart"
# client/windows/runner/Runner.rc is not listed: it takes FLUTTER_VERSION from
# the build, which Flutter derives from pubspec.yaml, so bumping pubspec.yaml
# is what sets the Windows file and product version.
OPENAPI_SPEC="${ROOT_DIR}/server/internal/api/swagger/ui/openapi.json"
COMPOSE_FILES=("${ROOT_DIR}/docker-compose.yml" "${ROOT_DIR}/docker-compose.mariadb.yml")

if [ ! -f "${VERSION_FILE}" ]; then
    echo "0.2.0" > "${VERSION_FILE}"
fi

CURRENT_VERSION="$(cat "${VERSION_FILE}" | tr -d '[:space:]')"
echo "Current File4Base version: ${CURRENT_VERSION}"

if [ $# -lt 1 ]; then
    echo "Usage: $0 [major|minor|patch|<explicit_version>]"
    exit 1
fi

ACTION="$1"

IFS='.' read -r MAJOR MINOR PATCH <<< "${CURRENT_VERSION}"

case "${ACTION}" in
    major)
        NEW_MAJOR=$((MAJOR + 1))
        NEW_VERSION="${NEW_MAJOR}.0.0"
        ;;
    minor)
        NEW_MINOR=$((MINOR + 1))
        NEW_VERSION="${MAJOR}.${NEW_MINOR}.0"
        ;;
    patch)
        NEW_PATCH=$((PATCH + 1))
        NEW_VERSION="${MAJOR}.${MINOR}.${NEW_PATCH}"
        ;;
    *)
        if [[ "${ACTION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
            NEW_VERSION="${ACTION}"
        else
            echo "Invalid version format: ${ACTION}. Expected MAJOR.MINOR.PATCH"
            exit 1
        fi
        ;;
esac

echo "Bumping version: ${CURRENT_VERSION} -> ${NEW_VERSION}"

# 1. Update VERSION file
echo "${NEW_VERSION}" > "${VERSION_FILE}"

# 2. Update Go server AppVersion
if [ -f "${SERVER_MAIN}" ]; then
    sed -i.bak "s/const AppVersion = .*/const AppVersion = \"${NEW_VERSION}\"/" "${SERVER_MAIN}"
    rm -f "${SERVER_MAIN}.bak"
    echo "Updated ${SERVER_MAIN}"
fi

# 3. Update Flutter pubspec.yaml
if [ -f "${CLIENT_PUBSPEC}" ]; then
    # Keep build number incremental
    BUILD_NUM="$(grep -E '^version: ' "${CLIENT_PUBSPEC}" | sed -E 's/.*[+]([0-9]+)/\1/' || echo "1")"
    NEW_BUILD=$((BUILD_NUM + 1))
    sed -i.bak "s/^version: .*/version: ${NEW_VERSION}+${NEW_BUILD}/" "${CLIENT_PUBSPEC}"
    rm -f "${CLIENT_PUBSPEC}.bak"
    echo "Updated ${CLIENT_PUBSPEC}"
fi

# 4. Update About dialog displayed version
if [ -f "${ABOUT_DIALOG}" ]; then
    sed -i.bak "s/_version = '.*';/_version = '${NEW_VERSION}';/" "${ABOUT_DIALOG}"
    rm -f "${ABOUT_DIALOG}.bak"
    echo "Updated ${ABOUT_DIALOG}"
fi

# 5. Update OpenAPI spec version
if [ -f "${OPENAPI_SPEC}" ]; then
    sed -i.bak "s/^    \"version\": \".*\",/    \"version\": \"${NEW_VERSION}\",/" "${OPENAPI_SPEC}"
    rm -f "${OPENAPI_SPEC}.bak"
    echo "Updated ${OPENAPI_SPEC}"
fi

# 6. Update default Docker image tags in the Compose files
for COMPOSE_FILE in "${COMPOSE_FILES[@]}"; do
    if [ -f "${COMPOSE_FILE}" ]; then
        sed -i.bak -E "s/(FILE4BASE_VERSION:-)[^}]+}/\1${NEW_VERSION}}/g" "${COMPOSE_FILE}"
        rm -f "${COMPOSE_FILE}.bak"
        echo "Updated ${COMPOSE_FILE}"
    fi
done

echo "=================================================="
echo "Version successfully bumped to: ${NEW_VERSION}"
echo "Next steps:"
echo "1. Update CHANGELOG.md under [${NEW_VERSION}]"
echo "2. Commit changes: git commit -am \"chore(release): bump version to ${NEW_VERSION}\""
echo "3. Create Git tag: git tag -a \"v${NEW_VERSION}\" -m \"Release v${NEW_VERSION}\""
echo "=================================================="

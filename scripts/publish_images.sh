#!/usr/bin/env bash
set -euo pipefail

# File4Base Docker Image Publishing Script
#
# Builds the file4base-api and file4base-web images for linux/amd64,
# linux/arm64 and linux/arm/v7 (macOS and Windows through Docker Desktop,
# Linux on Intel/AMD and ARM, Raspberry Pi with 64-bit or 32-bit OS) and pushes them to Docker Hub, tagged with the version in the
# root `VERSION` file and with `latest`.
#
#   docker login                    # once, with the Docker Hub account
#   ./scripts/publish_images.sh     # build and push both images
#
# Environment overrides:
#   FILE4BASE_REGISTRY  Docker Hub namespace (default: cloudresources)
#   PLATFORMS           Target platforms (default: linux/amd64,linux/arm64,linux/arm/v7)
#   NO_LATEST=1         Do not move the `latest` tag (e.g. for a hotfix of an older release)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "${ROOT_DIR}/VERSION")"
REGISTRY="${FILE4BASE_REGISTRY:-cloudresources}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64,linux/arm/v7}"
BUILDER="file4base-builder"

if ! [[ "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Invalid version in VERSION file: ${VERSION}"
    exit 1
fi

if [ -n "$(git -C "${ROOT_DIR}" status --porcelain)" ]; then
    echo "WARNING: the working tree has uncommitted changes; the images will include them."
fi

# A docker-container builder is required to build multi-platform images.
if ! docker buildx inspect "${BUILDER}" >/dev/null 2>&1; then
    docker buildx create --name "${BUILDER}" --driver docker-container >/dev/null
fi

echo "=========================================="
echo "   File4Base Image Publisher v${VERSION}"
echo "   Registry : docker.io/${REGISTRY}"
echo "   Platforms: ${PLATFORMS}"
echo "=========================================="

publish() {
    local name="$1" context="$2"
    local image="${REGISTRY}/${name}"
    local tags=(--tag "${image}:${VERSION}")
    [ "${NO_LATEST:-0}" = "1" ] || tags+=(--tag "${image}:latest")

    echo "--> Building and pushing ${image}:${VERSION}"
    docker buildx build \
        --builder "${BUILDER}" \
        --platform "${PLATFORMS}" \
        --label "org.opencontainers.image.title=${name}" \
        --label "org.opencontainers.image.version=${VERSION}" \
        --label "org.opencontainers.image.revision=$(git -C "${ROOT_DIR}" rev-parse HEAD)" \
        --label "org.opencontainers.image.source=https://github.com/file4base/file4base-app" \
        --label "org.opencontainers.image.licenses=GPL-3.0-only" \
        "${tags[@]}" \
        --push \
        "${context}"
}

publish file4base-api "${ROOT_DIR}/server"
publish file4base-web "${ROOT_DIR}/client"

echo "=========================================="
echo "Published:"
echo "  ${REGISTRY}/file4base-api:${VERSION}"
echo "  ${REGISTRY}/file4base-web:${VERSION}"
echo "=========================================="

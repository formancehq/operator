#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-formance-operator-e2e}"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-ghcr.io/formancehq/operator}"
UTILS_IMAGE_REPOSITORY="${UTILS_IMAGE_REPOSITORY:-ghcr.io/formancehq/operator-utils}"
IMAGE_TAG="${IMAGE_TAG:-e2e}"
IMAGE="${IMAGE_REPOSITORY}:${IMAGE_TAG}"
UTILS_IMAGE="${UTILS_IMAGE_REPOSITORY}:${IMAGE_TAG}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:15-alpine}"
NATS_IMAGE="${NATS_IMAGE:-nats:2.10-alpine}"
NATS_BOX_IMAGE="${NATS_BOX_IMAGE:-natsio/nats-box:0.19.2}"
EARTHLY_REPOSITORY="${EARTHLY_REPOSITORY:-ghcr.io}"

if ! command -v earthly >/dev/null 2>&1; then
  echo "earthly is required to build the operator image used by E2E tests" >&2
  exit 1
fi

if ! command -v kind >/dev/null 2>&1; then
  echo "kind is required to load the operator image into the E2E cluster" >&2
  exit 1
fi

cd "${ROOT_DIR}"

load_image_for_node_platform() {
  local image="$1"
  local os
  local arch
  local platform

  os="$(docker image inspect "${image}" --format '{{.Os}}')"
  arch="$(docker image inspect "${image}" --format '{{.Architecture}}')"
  platform="${os}/${arch}"

  for node in $(kind get nodes --name "${KIND_CLUSTER_NAME}"); do
    docker save "${image}" | docker exec --privileged -i "${node}" \
      ctr --namespace=k8s.io images import --platform "${platform}" --digests --snapshotter=overlayfs -
  done
}

earthly +build-image --REPOSITORY="${EARTHLY_REPOSITORY}" --tag="${IMAGE_TAG}"
earthly ./tools/utils+build-image --REPOSITORY="${EARTHLY_REPOSITORY}" --tag="${IMAGE_TAG}"
docker image inspect "${POSTGRES_IMAGE}" >/dev/null 2>&1 || docker pull "${POSTGRES_IMAGE}"
docker image inspect "${NATS_IMAGE}" >/dev/null 2>&1 || docker pull "${NATS_IMAGE}"
docker image inspect "${NATS_BOX_IMAGE}" >/dev/null 2>&1 || docker pull "${NATS_BOX_IMAGE}"
kind load docker-image "${IMAGE}" --name "${KIND_CLUSTER_NAME}"
kind load docker-image "${UTILS_IMAGE}" --name "${KIND_CLUSTER_NAME}"
load_image_for_node_platform "${POSTGRES_IMAGE}"
load_image_for_node_platform "${NATS_IMAGE}"
load_image_for_node_platform "${NATS_BOX_IMAGE}"

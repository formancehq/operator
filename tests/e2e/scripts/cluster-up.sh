#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-formance-operator-e2e}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-}"

if ! command -v kind >/dev/null 2>&1; then
  echo "kind is required to create the E2E cluster" >&2
  exit 1
fi

if kind get clusters | grep -qx "${KIND_CLUSTER_NAME}"; then
  echo "kind cluster ${KIND_CLUSTER_NAME} already exists"
  exit 0
fi

kind_args=(
  --name "${KIND_CLUSTER_NAME}"
  --config "${ROOT_DIR}/tests/e2e/kind/cluster.yaml"
)

if [[ -n "${KIND_NODE_IMAGE}" ]]; then
  kind_args+=(--image "${KIND_NODE_IMAGE}")
fi

kind create cluster "${kind_args[@]}"

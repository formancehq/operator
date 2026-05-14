#!/usr/bin/env bash
set -euo pipefail

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-formance-operator-e2e}"

if ! command -v kind >/dev/null 2>&1; then
  echo "kind is required to delete the E2E cluster" >&2
  exit 1
fi

if kind get clusters | grep -qx "${KIND_CLUSTER_NAME}"; then
  kind delete cluster --name "${KIND_CLUSTER_NAME}"
fi

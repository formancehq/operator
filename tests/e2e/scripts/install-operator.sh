#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
NAMESPACE="${OPERATOR_NAMESPACE:-formance-system}"
RELEASE="${OPERATOR_RELEASE:-formance-operator}"
CRDS_RELEASE="${OPERATOR_CRDS_RELEASE:-formance-operator-crds}"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-ghcr.io/formancehq/operator}"
IMAGE_TAG="${IMAGE_TAG:-e2e}"
TIMEOUT="${HELM_TIMEOUT:-10m}"

if ! command -v helm >/dev/null 2>&1; then
  echo "helm is required to install the operator for E2E tests" >&2
  exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required to install the operator for E2E tests" >&2
  exit 1
fi

cd "${ROOT_DIR}"

helm upgrade --install "${CRDS_RELEASE}" ./helm/crds \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  --wait \
  --timeout "${TIMEOUT}"

helm dependency update ./helm/operator

helm upgrade --install "${RELEASE}" ./helm/operator \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  --wait \
  --timeout "${TIMEOUT}" \
  --set operator-crds.create=false \
  --set image.repository="${IMAGE_REPOSITORY}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy=IfNotPresent \
  --set operator.disableWebhooks=false \
  --set operator.dev=true \
  --set operator.enableLeaderElection=false \
  --set global.licence.createSecret=false \
  --set operator.utils.tag="${IMAGE_TAG}"

kubectl rollout status "deployment/${RELEASE}" \
  --namespace "${NAMESPACE}" \
  --timeout "${TIMEOUT}"

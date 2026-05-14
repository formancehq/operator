#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
NAME="${1:-diagnostics}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
ARTIFACT_DIR="${ARTIFACT_DIR:-${ROOT_DIR}/tests/e2e/artifacts/${STAMP}-${NAME}}"

mkdir -p "${ARTIFACT_DIR}"

run() {
  local file="$1"
  shift
  {
    echo "$ $*"
    "$@"
  } >"${ARTIFACT_DIR}/${file}" 2>&1 || true
}

run namespaces.txt kubectl get namespaces -o wide
run crds.txt kubectl get crds
run formance-crds.yaml kubectl get crds -o yaml
run all-resources.txt kubectl get all -A -o wide
run formance-resources.yaml kubectl get stacks,settings,versions,auths,authclients,brokers,brokertopics,brokerconsumers,databases,gateways,gatewayhttpapis,ledgers,orchestrations,payments,reconciliations,resourcereferences,searches,stargates,transactionplanes,wallets,webhooks -A -o yaml
run events.txt kubectl get events -A --sort-by=.lastTimestamp
run operator-describe.txt kubectl describe deployment -n formance-system formance-operator
run operator-logs.txt kubectl logs -n formance-system deployment/formance-operator --all-containers --tail=-1
run pods-describe.txt kubectl describe pods -A
run jobs.txt kubectl get jobs -A -o yaml

echo "Diagnostics written to ${ARTIFACT_DIR}"

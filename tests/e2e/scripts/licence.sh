#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
E2E_LICENCE_DIR="${E2E_LICENCE_DIR:-${ROOT_DIR}/tests/e2e/artifacts/licence}"
E2E_LICENCE_KEY="${E2E_LICENCE_KEY:-${E2E_LICENCE_DIR}/e2e-licence.key}"
E2E_LICENCE_PUBLIC_KEY="${E2E_LICENCE_PUBLIC_KEY:-${E2E_LICENCE_DIR}/e2e-licence.pub}"
E2E_LICENCE_ISSUER="${E2E_LICENCE_ISSUER:-https://e2e.license.formance.local/keys}"
E2E_LICENCE_SERVICE="${E2E_LICENCE_SERVICE:-operator}"

ensure_e2e_licence_keypair() {
  mkdir -p "${E2E_LICENCE_DIR}"
  if [[ ! -s "${E2E_LICENCE_KEY}" || ! -s "${E2E_LICENCE_PUBLIC_KEY}" ]]; then
    openssl genrsa -out "${E2E_LICENCE_KEY}" 2048 >/dev/null 2>&1
    openssl rsa -in "${E2E_LICENCE_KEY}" -pubout -out "${E2E_LICENCE_PUBLIC_KEY}" >/dev/null 2>&1
  fi
}

e2e_licence_public_key_b64() {
  ensure_e2e_licence_keypair
  base64 <"${E2E_LICENCE_PUBLIC_KEY}" | tr -d '\n'
}

base64url_file() {
  openssl base64 -A -in "$1" | tr '+/' '-_' | tr -d '='
}

base64url_string() {
  local value="$1"
  local tmp
  tmp="$(mktemp)"
  printf '%s' "${value}" >"${tmp}"
  base64url_file "${tmp}"
  rm -f "${tmp}"
}

generate_e2e_licence_token() {
  ensure_e2e_licence_keypair

  local cluster_id="${1:-}"
  local issuer="${2:-${E2E_LICENCE_ISSUER}}"
  local expires_at="${3:-$(( $(date +%s) + 86400 ))}"

  if [[ -z "${cluster_id}" ]]; then
    cluster_id="$(kubectl get namespace kube-system -o jsonpath='{.metadata.uid}')"
  fi

  local header payload signing_input signature_file signature
  header='{"alg":"RS256","typ":"JWT"}'
  payload="$(printf '{"iss":"%s","sub":"%s","aud":"%s","exp":%s}' \
    "${issuer}" "${cluster_id}" "${E2E_LICENCE_SERVICE}" "${expires_at}")"
  signing_input="$(base64url_string "${header}").$(base64url_string "${payload}")"

  signature_file="$(mktemp)"
  printf '%s' "${signing_input}" | openssl dgst -sha256 -sign "${E2E_LICENCE_KEY}" -binary >"${signature_file}"
  signature="$(base64url_file "${signature_file}")"
  rm -f "${signature_file}"

  printf '%s.%s\n' "${signing_input}" "${signature}"
}

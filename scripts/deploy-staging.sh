#!/bin/sh

set -eu

: "${APPLICATION:?APPLICATION is required}"
: "${AUTH_TOKEN:?AUTH_TOKEN is required}"
: "${SERVER:?SERVER is required}"
: "${TAG:?TAG is required}"

argocd_cli() {
	argocd --auth-token="$AUTH_TOKEN" --server="$SERVER" --grpc-web "$@"
}

parameter_value() {
	argocd_cli app get "$APPLICATION" --output json |
		jq -r --arg name "$1" \
		'.spec.source.helm.parameters // [] | map(select(.name == $name)) | last | .value // empty'
}

restore_parameter() {
	name=$1
	value=$2
	if [ -n "$value" ]; then
		argocd_cli app set "$APPLICATION" --parameter "$name=$value"
	else
		argocd_cli app unset "$APPLICATION" --parameter "$name"
	fi
}

mutated=false
previous_image_tag=""
previous_utils_tag=""

rollback() {
	status=$?
	trap - EXIT INT TERM
	if [ "$status" -ne 0 ] && [ "$mutated" = true ]; then
		echo "Staging promotion failed; restoring previous Argo CD parameters" >&2
		restore_parameter image.tag "$previous_image_tag" || true
		restore_parameter operator.utils.tag "$previous_utils_tag" || true
		argocd_cli app sync "$APPLICATION" --timeout 600 || true
		argocd_cli app wait "$APPLICATION" --operation --sync --health --timeout 600 || true
	fi
	exit "$status"
}

trap rollback EXIT INT TERM

# Never mutate parameters while another sync can still consume them.
argocd_cli app wait "$APPLICATION" --operation --timeout 600

previous_image_tag=$(parameter_value image.tag)
previous_utils_tag=$(parameter_value operator.utils.tag)

argocd_cli app set "$APPLICATION" \
	--parameter "image.tag=$TAG" \
	--parameter "operator.utils.tag=$TAG"
mutated=true

argocd_cli app sync "$APPLICATION" --timeout 600
argocd_cli app wait "$APPLICATION" --operation --sync --health --timeout 600

application=$(argocd_cli app get "$APPLICATION" --refresh --output json)
echo "$application" | jq -e --arg tag "$TAG" '
  (.spec.source.helm.parameters // [] | any(.name == "image.tag" and .value == $tag)) and
  (.spec.source.helm.parameters // [] | any(.name == "operator.utils.tag" and .value == $tag)) and
  (.status.sync.status == "Synced") and
  (.status.health.status == "Healthy") and
  (.status.summary.images // [] | any(endswith("/operator:" + $tag)))
' >/dev/null

mutated=false
trap - EXIT INT TERM

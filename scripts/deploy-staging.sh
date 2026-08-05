#!/bin/sh

set -eu

: "${APPLICATION:?APPLICATION is required}"
: "${AUTH_TOKEN:?AUTH_TOKEN is required}"
: "${SERVER:?SERVER is required}"
: "${TAG:?TAG is required}"

argocd_cli() {
	argocd --auth-token="$AUTH_TOKEN" --server="$SERVER" --grpc-web "$@"
}

mutated=false
previous_parameters='[]'

wait_for_operation() {
	if argocd_cli app wait "$APPLICATION" --operation --timeout 600; then
		return 0
	fi

	echo "Argo CD operation did not finish; terminating it before rollback" >&2
	argocd_cli app terminate-op "$APPLICATION"
	argocd_cli app wait "$APPLICATION" --operation --timeout 60
}

rollback() {
	status=$1
	trap - EXIT
	trap '' INT TERM

	if [ "$mutated" = true ]; then
		echo "Staging promotion failed; restoring previous Argo CD parameters" >&2
		if wait_for_operation; then
			patch=$(jq -cn --argjson parameters "$previous_parameters" \
				'{spec:{source:{helm:{parameters:$parameters}}}}')
			if argocd_cli app patch "$APPLICATION" --type merge --patch "$patch"; then
				argocd_cli app sync "$APPLICATION" --timeout 600 || true
				argocd_cli app wait "$APPLICATION" --operation --sync --health --timeout 600 || true
			else
				echo "Unable to restore the previous Argo CD parameters" >&2
			fi
		else
			echo "Unable to stop the active Argo CD operation; parameters were not changed during rollback" >&2
		fi
	fi

	exit "$status"
}

trap 'rollback $?' EXIT
trap 'rollback 130' INT
trap 'rollback 143' TERM

# Never mutate parameters while another sync can still consume them.
argocd_cli app wait "$APPLICATION" --operation --timeout 600

baseline=$(argocd_cli app get "$APPLICATION" --output json)
previous_parameters=$(printf '%s\n' "$baseline" | jq -c '.spec.source.helm.parameters // []')

mutated=true
argocd_cli app set "$APPLICATION" \
	--parameter "image.tag=$TAG" \
	--parameter "operator.utils.tag=$TAG"

argocd_cli app sync "$APPLICATION" --timeout 600
argocd_cli app wait "$APPLICATION" --operation --sync --health --timeout 600

application=$(argocd_cli app get "$APPLICATION" --refresh --output json)
printf '%s\n' "$application" | jq -e --arg tag "$TAG" '
  (.spec.source.helm.parameters // [] | any(.name == "image.tag" and .value == $tag)) and
  (.spec.source.helm.parameters // [] | any(.name == "operator.utils.tag" and .value == $tag)) and
  (.status.sync.status == "Synced") and
  (.status.health.status == "Healthy") and
  (.status.summary.images // [] | any(endswith("/operator:" + $tag)))
' >/dev/null

mutated=false
trap - EXIT INT TERM

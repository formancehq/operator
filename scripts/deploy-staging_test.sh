#!/bin/sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
subject="$script_dir/deploy-staging.sh"
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT INT TERM

cat >"$test_dir/argocd" <<'EOF'
#!/bin/sh
set -eu

while [ "$#" -gt 0 ]; do
	case "$1" in
		--auth-token | --server)
			shift 2
			;;
		--auth-token=* | --server=* | --grpc-web)
			shift
			;;
		*)
			break
			;;
	esac
done

printf '%s\n' "$*" >>"$FAKE_ARGOCD_LOG"

read_state() {
	name=$1
	if [ -f "$FAKE_ARGOCD_STATE.$name" ]; then
		cat "$FAKE_ARGOCD_STATE.$name"
	fi
}

write_state() {
	name=$1
	value=$2
	if [ -n "$value" ]; then
		printf '%s' "$value" >"$FAKE_ARGOCD_STATE.$name"
	else
		rm -f "$FAKE_ARGOCD_STATE.$name"
	fi
}

case "$1 $2" in
	"app get")
		image=$(read_state image)
		utils=$(read_state utils)
		jq -cn --arg image "$image" --arg utils "$utils" '
		  [
		    (if $image == "" then empty else {name:"image.tag",value:$image} end),
		    (if $utils == "" then empty else {name:"operator.utils.tag",value:$utils} end)
		  ] as $parameters |
		  {spec:{source:{helm:{parameters:$parameters}}},status:{sync:{status:"Synced"},health:{status:"Healthy"},summary:{images:["ghcr.io/formancehq/operator:" + $image]}}}
		'
		;;
	"app set")
		for argument in "$@"; do
			case "$argument" in
				image.tag=*) write_state image "${argument#image.tag=}" ;;
				operator.utils.tag=*) write_state utils "${argument#operator.utils.tag=}" ;;
			esac
		done
		;;
	"app patch")
		patch=""
		while [ "$#" -gt 0 ]; do
			if [ "$1" = --patch ]; then
				patch=$2
				break
			fi
			shift
		done
		write_state image "$(printf '%s' "$patch" | jq -r '.spec.source.helm.parameters[]? | select(.name == "image.tag") | .value')"
		write_state utils "$(printf '%s' "$patch" | jq -r '.spec.source.helm.parameters[]? | select(.name == "operator.utils.tag") | .value')"
		;;
	"app sync")
		if [ -f "$FAKE_ARGOCD_FAIL_ONCE" ]; then
			rm "$FAKE_ARGOCD_FAIL_ONCE"
			: >"$FAKE_ARGOCD_OPERATION"
			exit 20
		fi
		;;
	"app wait")
		if [ -f "$FAKE_ARGOCD_OPERATION" ]; then
			exit 1
		fi
		;;
	"app terminate-op")
		rm -f "$FAKE_ARGOCD_OPERATION"
		;;
esac
EOF
chmod +x "$test_dir/argocd"

run_subject() {
	PATH="$test_dir:$PATH" \
		APPLICATION=staging-operator \
		AUTH_TOKEN=test-token \
		SERVER=argocd.example.test \
		TAG=new-sha \
		FAKE_ARGOCD_LOG="$test_dir/log" \
		FAKE_ARGOCD_STATE="$test_dir/state" \
		FAKE_ARGOCD_FAIL_ONCE="$test_dir/fail-once" \
		FAKE_ARGOCD_OPERATION="$test_dir/operation" \
		sh "$subject"
}

reset_test() {
	rm -f "$test_dir"/state.* "$test_dir/fail-once" "$test_dir/operation"
	: >"$test_dir/log"
}

reset_test
printf 'old-image' >"$test_dir/state.image"
printf 'old-utils' >"$test_dir/state.utils"
run_subject
test "$(cat "$test_dir/state.image")" = new-sha
test "$(cat "$test_dir/state.utils")" = new-sha
test "$(sed -n '1p' "$test_dir/log")" = "app wait staging-operator --operation --timeout 600"

reset_test
printf 'old-image' >"$test_dir/state.image"
printf 'old-utils' >"$test_dir/state.utils"
: >"$test_dir/fail-once"
if run_subject; then
	echo "expected failed synchronization" >&2
	exit 1
fi
test "$(cat "$test_dir/state.image")" = old-image
test "$(cat "$test_dir/state.utils")" = old-utils
grep -q 'app terminate-op staging-operator' "$test_dir/log"
grep -q 'app patch staging-operator --type merge --patch' "$test_dir/log"

reset_test
: >"$test_dir/fail-once"
if run_subject; then
	echo "expected failed synchronization with absent baseline parameters" >&2
	exit 1
fi
test ! -e "$test_dir/state.image"
test ! -e "$test_dir/state.utils"

echo "deploy-staging tests passed"

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

case "$1 $2" in
	"app get")
		tag=$(cat "$FAKE_ARGOCD_STATE")
		printf '{"spec":{"source":{"helm":{"parameters":[{"name":"image.tag","value":"%s"},{"name":"operator.utils.tag","value":"%s"}]}}},"status":{"sync":{"status":"Synced"},"health":{"status":"Healthy"},"summary":{"images":["ghcr.io/formancehq/operator:%s"]}}}\n' "$tag" "$tag" "$tag"
		;;
	"app set")
		for argument in "$@"; do
			case "$argument" in
				image.tag=*) printf '%s' "${argument#image.tag=}" >"$FAKE_ARGOCD_STATE" ;;
			esac
		done
		;;
	"app sync")
		if [ -f "$FAKE_ARGOCD_FAIL_ONCE" ]; then
			rm "$FAKE_ARGOCD_FAIL_ONCE"
			exit 20
		fi
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
		sh "$subject"
}

printf 'old-sha' >"$test_dir/state"
: >"$test_dir/log"
run_subject
test "$(cat "$test_dir/state")" = new-sha
test "$(sed -n '1p' "$test_dir/log")" = "app wait staging-operator --operation --timeout 600"

printf 'old-sha' >"$test_dir/state"
: >"$test_dir/log"
: >"$test_dir/fail-once"
if run_subject; then
	echo "expected failed synchronization" >&2
	exit 1
fi
test "$(cat "$test_dir/state")" = old-sha
grep -q 'app set staging-operator --parameter image.tag=old-sha' "$test_dir/log"

echo "deploy-staging tests passed"

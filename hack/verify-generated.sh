#!/usr/bin/env bash
# Fails when the tracked generated files are stale: deepcopy code, CRDs and
# RBAC from controller-gen, and the CRD schemas copied into bundle/manifests.
# Run it after `make generate manifests` (the verify-generated target does).
#
# operator-sdk writes bundle CRDs with a little boilerplate that controller-gen
# does not (a leading "---", "creationTimestamp: null" and an empty status
# stub). Both forms are equivalent, so the bundle comparison ignores it.
set -euo pipefail
cd "$(dirname "$0")/.."

status=0
# Only generator output: hand-written API types are not "out of date".
paths=(api/v1alpha1/zz_generated.deepcopy.go config/crd/bases config/rbac)
if ! git diff --quiet -- "${paths[@]}"; then
	echo "Generated files are out of date. Run 'make generate manifests' and commit the result:" >&2
	git --no-pager diff --stat -- "${paths[@]}" >&2
	status=1
fi

normalize() {
	sed -e '/^---$/d' -e '/^  creationTimestamp: null$/d' -e '/^status:$/,$d' "$1"
}
for crd in config/crd/bases/*.yaml; do
	bundled="bundle/manifests/$(basename "$crd")"
	if [[ ! -f "$bundled" ]]; then
		echo "$bundled is missing. Run 'make sync-bundle-crds' and commit the result." >&2
		status=1
	elif ! diff -q <(normalize "$crd") <(normalize "$bundled") >/dev/null; then
		echo "$bundled does not match $crd. Run 'make sync-bundle-crds' and commit the result." >&2
		status=1
	fi
done

exit "$status"

#!/bin/sh
# Check that every place carrying the release version agrees with the
# release-please manifest: the chart version and appVersion and the Artifact
# Hub image annotation. release-please updates them all in its release PR, so
# a mismatch means an x-release-please-version marker went missing.
#
# Usage: scripts/check-versions.sh [expected-version]
set -eu

expected=${1:-$(sed -n 's/^ *"\." *: *"\([^"]*\)".*/\1/p' .release-please-manifest.json)}
chart=charts/tautulli-exporter/Chart.yaml
status=0

check() {
	if [ "$2" != "$expected" ]; then
		echo "$1: found '$2', expected '$expected'" >&2
		status=1
	fi
}

check "$chart version" "$(sed -n 's/^version: *\([^ #]*\).*/\1/p' "$chart")"
check "$chart appVersion" "$(sed -n 's/^appVersion: *"\{0,1\}\([^" #]*\).*/\1/p' "$chart")"
check "$chart artifacthub.io/images" "$(sed -n 's/^ *image: .*tautulli-exporter:\([^ #]*\).*/\1/p' "$chart")"

if [ "$status" -eq 0 ]; then
	echo "all versions are $expected"
fi
exit "$status"

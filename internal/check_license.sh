#!/usr/bin/env bash
set -ueo pipefail

licRes=$(
    find . -type f -iname '*.go' ! -path '*/vendor/*' -exec \
         sh -c 'head -n3 $1 | grep -Eq "(Copyright|generated|GENERATED)" || echo "$1"' {} {} \;
)

if [ -n "${licRes}" ]; then
	echo -e "License header is missing in:\\n${licRes}"
	exit 255
fi

#!/bin/bash
# Builds both programs into build/. Each is one file with nothing else to
# install, so it can be copied to another Linux machine as it is.
set -e

here="$(cd "$(dirname "$0")" >/dev/null 2>&1 && pwd -P)"
mkdir -p "$here/build"
# An older Go fetches the Go that the code asks for.
export GOTOOLCHAIN=auto CGO_ENABLED=0
(cd "$here/cli" && go build -o "$here/build/gangway" ./cmd/gangway)
(cd "$here/controller" && go build -o "$here/build/gangway-controller" .)
echo "Built: $here/build/gangway"
echo "Built: $here/build/gangway-controller"

#!/bin/bash
# Reads the changed files, one on a line, and prints what to test:
#
#   folders=["cli","shared"]   the folders whose tests must run, as JSON
#   any=true                   whether that list holds a folder
#   docs=true                  whether the tests that tie the docs to the code must run
#
# With "all" as the first argument it prints every folder and reads nothing.
set -eu

everything() {
	echo 'folders=["acceptance","cli","controller","shared"]'
	echo 'any=true'
	echo 'docs=false'
}

if [ "${1:-}" = all ]; then
	everything
	exit 0
fi

files="$(cat)"
has() {
	printf '%s\n' "$files" | grep -qE "$1"
}

# A change to the workflows themselves could break any of them.
if has '^\.github/'; then
	everything
	exit 0
fi

list=""
add() {
	list="$list${list:+,}\"$1\""
}
# The acceptance tests start both real programs and run install.sh.
if has '^(acceptance|cli|controller|shared)/|^install\.sh$'; then add acceptance; fi
if has '^(cli|shared)/'; then add cli; fi
if has '^(controller|shared)/'; then add controller; fi
if has '^shared/'; then add shared; fi

if [ -n "$list" ]; then
	echo "folders=[$list]"
	echo 'any=true'
else
	# A matrix must hold one value even when its job is skipped.
	echo 'folders=["shared"]'
	echo 'any=false'
fi

# These three files are read by tests.
if has '^docs/(design|requirements|releases)\.md$'; then
	echo 'docs=true'
else
	echo 'docs=false'
fi

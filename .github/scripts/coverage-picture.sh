#!/bin/bash
# Runs the tests of cli, controller and shared, and writes a picture with the
# share of lines they run to the file named as the first argument.
#
# Run it from the top of the repo.
set -eu

out="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

text="coverage"
for folder in cli controller shared; do
	(cd "$folder" && go test -count=1 -coverprofile="$work/$folder.out" ./... >&2)
	share="$(cd "$folder" && go tool cover -func="$work/$folder.out" | awk '$1 == "total:" { print $NF }')"
	text="$text · $folder $share"
done

# The width is set from the count of letters, and textLength holds the text
# to it, so a font of another width cannot spill over the edge.
letters=${#text}
wide=$((letters * 7 + 20))
cat > "$out" <<EOF
<svg xmlns="http://www.w3.org/2000/svg" width="$wide" height="24" role="img" aria-label="$text">
  <title>$text</title>
  <rect width="$wide" height="24" rx="6" fill="#11161d" stroke="#232b36"/>
  <text x="10" y="16" textLength="$((wide - 20))" lengthAdjust="spacingAndGlyphs" fill="#6dffa8" font-family="ui-monospace, SFMono-Regular, Menlo, Consolas, monospace" font-size="12">$text</text>
</svg>
EOF
echo "$text"

#!/usr/bin/env bash
# Diffs the C and Go output of one encoder case.
#
# usage: cmp.sh <spec>...
#
# Example: tools/oracle/enc/cmp.sh t=enc fs=24000 ch=1 app=2048 fr=480 n=20 fec=1 loss=5 in=x
# Needs the same environment as gen.sh. The outputs go to $OPUS_WORK/enc/one.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
OPUS_DIR=$(cd "$HERE/../../.." && pwd)
OPUS_GO_DIR=${OPUS_GO_DIR:-$OPUS_DIR}
CC=${CC:-gcc}
: "${OPUS_SRC:?}" "${OPUS_REF:?}" "${OPUS_WORK:?}"
[ $# -gt 0 ] || { echo "usage: $0 <spec>..." >&2; exit 2; }
spec="$*"
D=$OPUS_WORK/enc/one
rm -rf "$D"
mkdir -p "$D"
echo "$spec" >"$D/cases.in"
$CC -O2 -g -ffp-contract=off -Wall -I"$OPUS_SRC/include" -o "$D/drv" "$HERE/drv.c" "$OPUS_REF/.libs/libopus.a" -lm
(cd "$OPUS_GO_DIR" && GOWORK=off OPUS_ENC_INPUTS=$D OPUS_ENC_CASES=$D/cases.in \
	go test -count=1 -run '^TestWriteEncInputs$' . >/dev/null)
# shellcheck disable=SC2086
"$D/drv" "$D/0001.pcm" $spec >"$D/c.txt"
(cd "$OPUS_GO_DIR" && GOWORK=off OPUS_ENC_SPEC="$spec" go test -v -count=1 -run '^TestEncodeOutput$' . |
	grep -v '^ok\|^PASS\|^--- \|^=== ') >"$D/go.txt"
if diff "$D/c.txt" "$D/go.txt" >"$D/diff.txt"; then
	echo "SAME $(wc -l <"$D/c.txt") lines"
else
	echo "DIFF (see $D/diff.txt)"
	head -c 3000 "$D/diff.txt"
	exit 1
fi

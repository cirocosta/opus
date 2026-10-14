#!/usr/bin/env bash
# Regenerates the root encoder goldens (opus/opus_encoder_test.go):
#
#   testdata/encoder_golden.txt   cases.in: encoder, multistream, surround
#                                 and projection cases on the committed
#                                 excerpt internal/celt/testdata/enc_pcm.raw.gz
#                                 and integer synthetic signals
#   testdata/encoder_vectors.txt  vcases.in: cases on the decoded RFC 8251
#                                 vectors
#
# Each line is the line count and sha256[:32] of the drv.c output of one
# case (see the comment at the top of drv.c). gen_cases.py writes
# cases.in and vcases.in; this script checks that the committed files are
# its output. The Go test TestWriteEncInputs writes the input PCM of each
# case. Set OPUS_GO_DIR to run it from a different checkout.
. "$(dirname "$0")/../lib.sh"
oracle_init enc "OPUS_SRC OPUS_REF OPUS_TESTVECTORS" "$@"

$CC $ORACLE_CFLAGS -I"$OPUS_SRC/include" -o "$W/drv" "$HERE/drv.c" "$OPUS_REF/.libs/libopus.a" -lm

mkdir -p "$W/cases"
python3 -I "$HERE/gen_cases.py" "$OPUS_TESTVECTORS" "$W/cases" >/dev/null
for f in cases.in vcases.in; do
	cmp -s "$W/cases/$f" "$HERE/$f" || oracle_die "$HERE/$f is not the output of gen_cases.py ($W/cases/$f)"
done

# golden <cases file> <repo-relative golden> runs every case.
golden() {
	local cases=$HERE/$1 in=$W/in/${1%.in} c=$W/c/${1%.in} spec i=0 n
	rm -rf "$in" "$c"
	mkdir -p "$in" "$c"
	(cd "$OPUS_GO_DIR" && GOWORK=off OPUS_ENC_INPUTS=$in OPUS_ENC_CASES=$cases \
		go test -count=1 -run '^TestWriteEncInputs$' . >/dev/null)
	oracle_out "$2"
	echo "# lines sha256[:32] spec (libopus 1.6.1 default config via tools/oracle/enc/drv.c)" >"$O"
	while read -r spec; do
		[ -n "$spec" ] || continue
		i=$((i + 1))
		n=$(printf %04d $i)
		# shellcheck disable=SC2086
		"$W/drv" "$in/$n.pcm" $spec >"$c/$n.txt"
		echo "$(wc -l <"$c/$n.txt") $(sha256sum "$c/$n.txt" | cut -c1-32) $spec" >>"$O"
	done <"$cases"
}
golden cases.in testdata/encoder_golden.txt
golden vcases.in testdata/encoder_vectors.txt

echo "per-case driver output: $W/c (compare with OPUS_ENC_DUMP output, or use cmp.sh)"
oracle_finish

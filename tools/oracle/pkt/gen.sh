#!/usr/bin/env bash
# Regenerates the root packet-layer goldens (opus/golden_test.go):
# testdata/{parse,softclip,repacketizer,pad,extensions,matrix}.txt.gz,
# testdata/vectors.txt, and mapping_matrix_tables.go.
#
# The Go test TestWriteGoldenInputs writes the input records; golden.c
# runs libopus on them. Set OPUS_GO_DIR to run the input generator from a
# different checkout of the opus module (for example a clean snapshot when
# the working tree does not build).
. "$(dirname "$0")/../lib.sh"
oracle_init pkt "OPUS_SRC OPUS_REF OPUS_TESTVECTORS" "$@"

oracle_out mapping_matrix_tables.go
go_out=$O
python3 -I "$HERE/gen_tables.py" "$OPUS_SRC/src/mapping_matrix.c" >"$go_out"
gofmt -w "$go_out"

$CC $ORACLE_CFLAGS -DHAVE_CONFIG_H -I"$OPUS_REF" -I"$OPUS_SRC/include" -I"$OPUS_SRC/src" \
	-I"$OPUS_SRC/celt" -I"$OPUS_SRC/silk" \
	-o "$W/golden" "$HERE/golden.c" "$OPUS_REF/.libs/libopus.a" -lm

rm -rf "$W/in"
oracle_go_test '^TestWriteGoldenInputs$' "$W/in"
for set in parse softclip repacketizer pad extensions matrix; do
	oracle_out testdata/$set.txt.gz
	"$W/golden" $set "$W/in/$set.bin" | gzip -9n >"$O"
done
oracle_out testdata/vectors.txt
"$W/golden" vectors "$OPUS_TESTVECTORS" >"$O"

oracle_finish

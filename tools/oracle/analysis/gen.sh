#!/usr/bin/env bash
# Regenerates the root tonality analysis goldens (opus/analysis_test.go):
# testdata/analysis.txt.gz, testdata/analysis_vectors.txt, and
# mlp_data.go.
#
# The Go test TestWriteAnalysisSource writes the synthetic source PCM;
# drv.c runs run_analysis from libopus.a with each configuration in
# synth.cfg (full output) and vec.cfg (one hash per test vector). Set
# OPUS_GO_DIR to run the source generator from a different checkout.
. "$(dirname "$0")/../lib.sh"
oracle_init analysis "OPUS_SRC OPUS_REF OPUS_TESTVECTORS" "$@"

oracle_out mlp_data.go
go_out=$O
python3 -I "$HERE/gen_mlp.py" "$OPUS_SRC/src/mlp_data.c" "$go_out"
gofmt -w "$go_out"

$CC $ORACLE_CFLAGS -DHAVE_CONFIG_H -I"$OPUS_REF" -I"$OPUS_SRC/include" -I"$OPUS_SRC/src" \
	-I"$OPUS_SRC/celt" -I"$OPUS_SRC/silk" \
	-o "$W/drv" "$HERE/drv.c" "$OPUS_REF/.libs/libopus.a" -lm

rm -rf "$W/in"
oracle_go_test '^TestWriteAnalysisSource$' "$W/in"
oracle_out testdata/analysis.txt.gz
"$W/drv" synth "$W/in/analysis_src.raw" "$HERE/synth.cfg" | gzip -9n >"$O"
oracle_out testdata/analysis_vectors.txt
"$W/drv" vec "$OPUS_TESTVECTORS" "$HERE/vec.cfg" >"$O"

oracle_finish

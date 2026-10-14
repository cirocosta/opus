#!/usr/bin/env bash
# Regenerates the opus/internal/silk goldens (see internal/silk/testdata/README):
#
# Decoder (dec.c):
#   testdata/vectors_golden.txt  silk_Decode on the RFC 8251 vectors with
#                                SILK or hybrid frames, 8-48 kHz, 1/2
#                                channels, loss modes 0/1/2
#   testdata/mix_golden.txt      the same for the committed testdata/silk_mix.bit
#   testdata/mix_samples_16k_mono_plc.raw
#                                first 4096 samples of silk_mix.bit at
#                                16 kHz mono with loss mode 1
# Encoder (enc.c, unit.c):
#   testdata/enc_golden.txt          synthetic scenarios
#   testdata/enc_vectors_golden.txt  scenarios on the decoded RFC 8251 vectors
#   testdata/enc_unit_golden.txt     encoder helpers on random input
#
# The drivers call silk_Decode and silk_Encode from libopus.a directly.
# They compile with -include config.h, so that the control structures
# have the layout of the reference build. The Go test
# TestWriteEncGoldenInputs (internal/silk) writes the encoder inputs. Set
# OPUS_GO_DIR to run it from a different checkout.
. "$(dirname "$0")/../lib.sh"
oracle_init silk "OPUS_SRC OPUS_REF OPUS_TESTVECTORS" "$@"

V=$OPUS_TESTVECTORS
L=$OPUS_REF/.libs/libopus.a
F="$ORACLE_CFLAGS -include $OPUS_REF/config.h -I$OPUS_SRC/include -I$OPUS_SRC/celt -I$OPUS_SRC/silk -I$OPUS_SRC/silk/float"
$CC $F -o "$W/dec" "$HERE/dec.c" "$L" -lm
$CC $F -o "$W/enc" "$HERE/enc.c" "$L" -lm
$CC $F -o "$W/unit" "$HERE/unit.c" "$L" -lm

TD=internal/silk/testdata
C=$W/c
rm -rf "$C"
mkdir -p "$C/dec" "$C/mix" "$C/enc"

# Decoder.
oracle_out $TD/vectors_golden.txt
{
	echo "# vector api_rate api_channels loss(0 none,1 plc,2 fec) sha256[:32] calls"
	for v in 02 03 04 05 06 08 09 10 12; do
		for r in 8000 12000 16000 24000 48000; do
			for c in 1 2; do
				for l in 0 1 2; do
					f=$C/dec/tv${v}_${r}_${c}_${l}.txt
					"$W/dec" "$V/testvector$v.bit" $r $c $l "$f"
					echo "testvector$v $r $c $l $(sha256sum "$f" | cut -c1-32) $(wc -l <"$f")"
				done
			done
		done
	done
} >"$O"

oracle_out $TD/mix_golden.txt
{
	echo "# stream api_rate api_channels loss(0 none,1 plc,2 fec) sha256[:32] calls"
	for r in 8000 12000 16000 24000 48000; do
		for c in 1 2; do
			for l in 0 1 2; do
				f=$C/mix/mix_${r}_${c}_${l}.txt
				"$W/dec" "$OPUS_DIR/$TD/silk_mix.bit" $r $c $l "$f" "$f.raw"
				echo "mix $r $c $l $(sha256sum "$f" | cut -c1-32) $(wc -l <"$f")"
			done
		done
	done
} >"$O"
oracle_out $TD/mix_samples_16k_mono_plc.raw
head -c 8192 "$C/mix/mix_16000_1_1.txt.raw" >"$O"

# Encoder.
IN=$W/in
rm -rf "$IN"
mkdir -p "$IN"
(cd "$OPUS_GO_DIR" && GOWORK=off OPUS_GOLDEN_INPUTS=$IN OPUS_TESTVECTORS=$V \
	go test -count=1 -run '^TestWriteEncGoldenInputs$' ./internal/silk >/dev/null)

# golden <list> runs every scenario of the list and writes the golden to $O.
golden() {
	local name ch f
	while read -r name ch; do
		"$W/enc" "$IN/$name.pcm" "$IN/$name.txt" "$ch" "$C/enc/$name.out"
	done <"$IN/$1"
	{
		echo "# SILK encoder goldens from tools/oracle/silk/enc.c (libopus 1.6.1 default config)."
		echo "# s name lines sha256[:32]; l name line (first 2 lines of each scenario)"
		while read -r name ch; do
			f=$C/enc/$name.out
			echo "s $name $(wc -l <"$f") $(sha256sum "$f" | cut -c1-32)"
			head -2 "$f" | sed "s/^/l $name /"
		done <"$IN/$1"
	} >"$O"
}
oracle_out $TD/enc_golden.txt
golden list.txt
oracle_out $TD/enc_vectors_golden.txt
golden list_vec.txt
oracle_out $TD/enc_unit_golden.txt
"$W/unit" >"$O"

echo "per-call driver output: $C (compare with SILK_DUMP_DIR / SILK_ENC_DUMP output)"
oracle_finish

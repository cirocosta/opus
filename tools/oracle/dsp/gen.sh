#!/usr/bin/env bash
# Regenerates opus/internal/dsp/static_modes.go and the goldens in
# opus/internal/dsp/testdata/.
#
# The driver is built twice: once with the reference config.h
# (FLOAT_APPROX on, the x86 configure default) and once with
# FLOAT_APPROX removed (libm celt_log2/celt_exp2). The small sweeps go
# into testdata/. A large 2^20-point math sweep goes to $OPUS_WORK/dsp/big
# and is not committed; run it with DSP_GOLDEN_DIR (printed at the end).
. "$(dirname "$0")/../lib.sh"
oracle_init dsp "OPUS_SRC OPUS_REF" "$@"

oracle_out internal/dsp/static_modes.go
go_out=$O
python3 -I "$HERE/gen_static_modes.py" "$OPUS_SRC/celt/static_modes_float.h" "$go_out"
gofmt -w "$go_out"

mkdir -p "$W/cfg_libm"
grep -v "define FLOAT_APPROX" "$OPUS_REF/config.h" >"$W/cfg_libm/config.h"
for v in approx libm; do
	cfg=$OPUS_REF
	[ $v = libm ] && cfg=$W/cfg_libm
	$CC $ORACLE_CFLAGS -DHAVE_CONFIG_H -I"$cfg" -I"$OPUS_SRC/celt" -I"$OPUS_SRC/include" \
		-I"$OPUS_SRC/dnn" -I"$OPUS_SRC/silk" \
		-o "$W/driver_$v" "$HERE/driver.c" "$OPUS_REF/.libs/libopus.a" -lm
done

rm -rf "$W/small_approx" "$W/small_libm" "$W/big"
mkdir -p "$W/small_approx" "$W/small_libm" "$W/big"
"$W/driver_approx" "$W/small_approx" 256
"$W/driver_libm" "$W/small_libm" 256 mathonly
for f in fft lpc math_approx mdct pitch tables; do
	oracle_out internal/dsp/testdata/$f.bin
	cp "$W/small_approx/$f.bin" "$O"
done
oracle_out internal/dsp/testdata/math_libm.bin
cp "$W/small_libm/math_libm.bin" "$O"

"$W/driver_approx" "$W/big" 1048576 mathonly
"$W/driver_libm" "$W/big" 1048576 mathonly

oracle_finish
echo "big sweep: (cd $OPUS_DIR && DSP_GOLDEN_DIR=$W/big go test -run Math ./internal/dsp)"

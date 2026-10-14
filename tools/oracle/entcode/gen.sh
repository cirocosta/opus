#!/usr/bin/env bash
# Regenerates opus/internal/entcode/testdata/golden.txt.
#
# The driver compiles the libopus range coder sources (celt/entcode.c,
# entenc.c, entdec.c, laplace.c) directly. config.h defines
# ENABLE_HARDENING, so celt_assert is active; driver.c defines the
# celt_fatal stub that celt.c would otherwise provide.
. "$(dirname "$0")/../lib.sh"
oracle_init entcode "OPUS_SRC OPUS_REF" "$@"

$CC $ORACLE_CFLAGS -DHAVE_CONFIG_H -I"$OPUS_REF" -I"$OPUS_SRC/celt" -I"$OPUS_SRC/include" \
	-o "$W/driver" "$HERE/driver.c" \
	"$OPUS_SRC/celt/entcode.c" "$OPUS_SRC/celt/entenc.c" \
	"$OPUS_SRC/celt/entdec.c" "$OPUS_SRC/celt/laplace.c" -lm
oracle_out internal/entcode/testdata/golden.txt
"$W/driver" >"$O"

oracle_finish

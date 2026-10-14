#!/usr/bin/env bash
# Regenerates the root decoder goldens (opus/opus_decoder_test.go):
#
#   testdata/dec_mix.bit         opus_demo encodings of excerpts of the
#                                decoded RFC 8251 vectors (mode, bandwidth,
#                                frame size and FEC/DTX switches)
#   testdata/{ms3,ms4i,ms51,amb4,proj4,proj9}.pk
#                                multistream and projection packets
#                                (drv msenc, synthetic input)
#   testdata/decoder_golden.txt  drv dec/msdec for every line of cases.in:
#                                line count and sha256[:32] of the output
#   testdata/decoder_vectors.txt opus_demo -d of every RFC 8251 vector at
#                                every decoder rate, mono and stereo
#
# Cases name streams relative to the opus/ module; $V/ is
# $OPUS_TESTVECTORS. A stream that this script writes is read from its
# output, any other one from the repository.
. "$(dirname "$0")/../lib.sh"
oracle_init dec "OPUS_SRC OPUS_REF OPUS_TESTVECTORS" "$@"

V=$OPUS_TESTVECTORS
DEMO=$OPUS_REF/opus_demo
[ -x "$DEMO" ] || oracle_die "$DEMO is missing (build the reference with make)"

$CC $ORACLE_CFLAGS -I"$OPUS_SRC/include" -o "$W/drv" "$HERE/drv.c" "$OPUS_REF/.libs/libopus.a" -lm

# excerpt <in.dec> <start s> <length s> <channels> <rate> <out.sw> takes
# an excerpt of a 48 kHz stereo decoding, downmixed and decimated by
# averaging.
excerpt() {
	python3 -I - "$@" <<'PY'
import sys, struct
f, st, ln, ch, rate, out = sys.argv[1], float(sys.argv[2]), float(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5]), sys.argv[6]
d = open(f, 'rb').read()
x = struct.unpack('<%dh' % (len(d)//2), d)
a = int(st*48000)*2; n = int(ln*48000)
x = x[a:a+2*n]
dec = 48000//rate
o = []
for i in range(0, len(x)//2 - dec + 1, dec):
    l = sum(x[2*(i+k)] for k in range(dec))//dec
    r = sum(x[2*(i+k)+1] for k in range(dec))//dec
    if ch == 1: o.append((l+r)//2)
    else: o += [l, r]
open(out, 'wb').write(struct.pack('<%dh' % len(o), *o))
PY
}

S=$W/streams
rm -rf "$S"
mkdir -p "$S"
excerpt "$V/testvector07.dec" 1 2.5 2 48000 "$S/a.sw"
excerpt "$V/testvector03.dec" 2 3 2 48000 "$S/b.sw"
excerpt "$V/testvector11.dec" 0 3 2 48000 "$S/c.sw"
excerpt "$V/testvector09.dec" 1 1.5 2 48000 "$S/d.sw"
excerpt "$V/testvector05.dec" 1 2 1 48000 "$S/e.sw"
excerpt "$V/testvector06.dec" 0 5 2 48000 "$S/h.sw"
{
	"$DEMO" -e audio 48000 2 96000 -framesize 20 "$S/a.sw" "$S/a.bit"
	"$DEMO" -e voip 48000 2 24000 -inbandfec -loss 10 "$S/b.sw" "$S/b.bit"
	"$DEMO" -e audio 48000 2 16000 -sweep 1500 -sweep_max 90000 -random_framesize -random_fec "$S/c.sw" "$S/c.bit"
	"$DEMO" -e restricted-lowdelay 48000 2 64000 -framesize 5 "$S/d.sw" "$S/d.bit"
	"$DEMO" -e voip 48000 1 12000 -framesize 60 -inbandfec -loss 20 -dtx "$S/e.sw" "$S/e.bit"
	"$DEMO" -e voip 48000 2 8000 -sweep 3000 -sweep_max 70000 -random_framesize -inbandfec -loss 5 "$S/h.sw" "$S/h.bit"
} >"$W/opus_demo.log" 2>&1
oracle_out testdata/dec_mix.bit
cat "$S/a.bit" "$S/b.bit" "$S/h.bit" "$S/c.bit" "$S/d.bit" "$S/e.bit" >"$O"

# drv msenc: fs ch family bitrate frames framesize
for spec in "ms3 48000 3 1 64000 40 1920" "ms4i 48000 4 255 48000 50 480" \
	"amb4 48000 4 2 64000 50 960" "proj4 48000 4 3 96000 50 960" \
	"ms51 48000 6 1 192000 60 960" "proj9 48000 9 3 128000 40 960"; do
	set -- $spec
	oracle_out testdata/$1.pk
	"$W/drv" msenc "${@:2}" "$O" 2>/dev/null
done

# stream_path <name> resolves a stream of cases.in.
stream_path() {
	case $1 in
	\$V/*) echo "$V/${1#\$V/}" ;;
	*) if [ -f "$OUT/$1" ]; then echo "$OUT/$1"; else echo "$OPUS_DIR/$1"; fi ;;
	esac
}

oracle_out testdata/decoder_golden.txt
G=$O
mkdir -p "$W/cases"
echo "# lines sha256[:32] mode stream options (libopus 1.6.1 default config via tools/oracle/dec/drv.c)" >"$G"
n=0
while read -r mode stream opts; do
	[ -n "$mode" ] || continue
	n=$((n + 1))
	f=$W/cases/c$n.txt
	# shellcheck disable=SC2086
	"$W/drv" "$mode" $opts "$(stream_path "$stream")" >"$f"
	echo "$(wc -l <"$f") $(sha256sum "$f" | cut -c1-32) $mode $stream $opts" >>"$G"
done <"$HERE/cases.in"

oracle_out testdata/decoder_vectors.txt
{
	echo "# vector rate channels bytes sha256[:32] of opus_demo -d rate channels output (default config)"
	for v in 01 02 03 04 05 06 07 08 09 10 11 12; do
		for r in 48000 24000 16000 12000 8000; do
			for c in 2 1; do
				f=$W/vec.out
				"$DEMO" -d $r $c "$V/testvector$v.bit" "$f" >/dev/null 2>&1
				echo "testvector$v $r $c $(stat -c %s "$f") $(sha256sum "$f" | cut -c1-32)"
			done
		done
	done
} >"$O"

echo "per-case driver output: $W/cases (compare with OPUS_DEC_DUMP output of TestDecoderGolden)"
oracle_finish

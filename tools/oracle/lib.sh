# Shared helpers for the oracle generator scripts. Source it from a
# tools/oracle/<pkg>/gen.sh script:
#
#	. "$(dirname "$0")/../lib.sh"
#	oracle_init <pkg> "<needed env vars>" "$@"
#
# Environment (see README.md):
#
#	OPUS_SRC          pristine libopus 1.6.1 source tree
#	OPUS_REF          built reference tree (config.h, .libs/libopus.a)
#	OPUS_TESTVECTORS  RFC 8251 test vector directory
#	OPUS_WORK         scratch output directory
#	OPUS_GO_DIR       Go module that generates inputs (default: the repo opus/ dir)
#	CC                C compiler (default: gcc)
#
# Each gen.sh writes its outputs below $OPUS_WORK/<pkg>/out/, using the
# same relative paths as in the opus/ module. Then it compares each output
# with the committed file. With -w it also copies the outputs into the
# repository.

set -euo pipefail

ORACLE_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
OPUS_DIR=$(cd "$ORACLE_DIR/../.." && pwd)
CC=${CC:-gcc}

# Compiler flags of the reference build. Keep -ffp-contract=off so that
# the drivers do not fuse float products either.
ORACLE_CFLAGS="-O2 -g -ffp-contract=off -Wall"

oracle_die() {
	echo "$(basename "$0"): $*" >&2
	exit 1
}

oracle_usage() {
	cat >&2 <<EOF
usage: $0 [-w]

Regenerates the goldens of package '$ORACLE_PKG' into
\$OPUS_WORK/$ORACLE_PKG/out and compares them with the committed files.

  -w   also copy the regenerated files into the repository
EOF
	exit 2
}

# oracle_init <pkg> "<env var names>" [script args...] parses the flags,
# checks the environment, and sets HERE (script dir), W (work dir) and
# OUT (output root).
oracle_init() {
	ORACLE_PKG=$1
	local needs=$2 a
	shift 2
	ORACLE_WRITE=0
	ORACLE_OUTPUTS=()
	for a in "$@"; do
		case $a in
		-w) ORACLE_WRITE=1 ;;
		*) oracle_usage ;;
		esac
	done
	set -- $needs
	local v
	for v in OPUS_WORK "$@"; do
		[ -n "${!v:-}" ] || oracle_die "$v is not set (see tools/oracle/README.md; build-ref.sh prints the values)"
	done
	case " $* " in
	*" OPUS_SRC "*)
		[ -f "$OPUS_SRC/celt/celt.c" ] || oracle_die "OPUS_SRC=$OPUS_SRC is not a libopus source tree"
		;;
	esac
	case " $* " in
	*" OPUS_REF "*)
		[ -f "$OPUS_REF/config.h" ] && [ -f "$OPUS_REF/.libs/libopus.a" ] ||
			oracle_die "OPUS_REF=$OPUS_REF has no config.h or .libs/libopus.a"
		oracle_check_config "$OPUS_REF/config.h"
		;;
	esac
	case " $* " in
	*" OPUS_TESTVECTORS "*)
		[ -f "$OPUS_TESTVECTORS/testvector01.bit" ] ||
			oracle_die "OPUS_TESTVECTORS=$OPUS_TESTVECTORS has no testvector01.bit"
		;;
	esac
	OPUS_GO_DIR=${OPUS_GO_DIR:-$OPUS_DIR}
	HERE=$ORACLE_DIR/$ORACLE_PKG
	W=$OPUS_WORK/$ORACLE_PKG
	OUT=$W/out
	rm -rf "$OUT"
	mkdir -p "$W" "$OUT"
}

# oracle_check_config fails unless config.h is the default float
# configuration that the port follows.
oracle_check_config() {
	local sym
	for sym in ENABLE_DRED ENABLE_DEEP_PLC ENABLE_OSCE ENABLE_OSCE_BWE ENABLE_QEXT FIXED_POINT CUSTOM_MODES; do
		if grep -q "^#define $sym\b" "$1"; then
			oracle_die "$1 defines $sym; the port follows the default configuration (run build-ref.sh)"
		fi
	done
}

# oracle_go_test <test regexp> <inputs dir> runs a Go test that writes
# golden inputs into the directory. GOWORK=off keeps an unrelated go.work
# in a parent directory (for example /tmp/go.work) from capturing it.
oracle_go_test() {
	mkdir -p "$2"
	(cd "$OPUS_GO_DIR" && GOWORK=off OPUS_GOLDEN_INPUTS=$2 go test -count=1 -run "$1" .)
}

# oracle_out <repo-relative path> registers an output and sets O to its
# path below $OUT. Do not call it in a subshell.
oracle_out() {
	ORACLE_OUTPUTS+=("$1")
	O=$OUT/$1
	mkdir -p "$(dirname "$O")"
}

# oracle_finish compares every registered output with the committed file,
# copies them into the repository with -w, and exits 1 if any differs.
oracle_finish() {
	local rel diff=0
	for rel in "${ORACLE_OUTPUTS[@]}"; do
		local got=$OUT/$rel want=$OPUS_DIR/$rel
		if [ ! -f "$want" ]; then
			echo "new        $rel"
			diff=1
		elif cmp -s "$got" "$want"; then
			echo "identical  $rel"
		elif [ "${rel%.gz}" != "$rel" ] && cmp -s <(gzip -dc "$got") <(gzip -dc "$want"); then
			echo "differs    $rel (same content, gzip bytes differ)"
			diff=1
		else
			echo "DIFFERS    $rel"
			diff=1
		fi
		if [ "$ORACLE_WRITE" = 1 ]; then
			mkdir -p "$(dirname "$want")"
			cp "$got" "$want"
		fi
	done
	if [ "$ORACLE_WRITE" = 1 ]; then
		echo "copied ${#ORACLE_OUTPUTS[@]} files into $OPUS_DIR"
	elif [ $diff = 1 ]; then
		echo "outputs differ from the committed files; inspect $OUT, then rerun with -w to install" >&2
		exit 1
	fi
}

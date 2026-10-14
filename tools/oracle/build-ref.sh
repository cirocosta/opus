#!/usr/bin/env bash
# Downloads, verifies, and builds the libopus 1.6.1 reference that the
# oracle drivers link: the default configuration (no DRED, deep PLC, or
# OSCE), float, scalar (no intrinsics, RTCD, or asm), and compiled with
# -ffp-contract=off.
#
# usage: build-ref.sh <workdir>
#
# Layout below <workdir>:
#
#	dl/                 downloaded tarballs
#	opus-1.6.1/         pristine source tree         (OPUS_SRC)
#	build/opus-1.6.1/   configured and built tree    (OPUS_REF)
#	opus_newvectors/    RFC 8251 test vectors        (OPUS_TESTVECTORS)
#	work/               scratch output for gen.sh    (OPUS_WORK)
#
# The script is idempotent: it skips each step that is already done. It
# prints the environment variables to export at the end.
set -euo pipefail

OPUS_VERSION=1.6.1
OPUS_TARBALL=opus-$OPUS_VERSION.tar.gz
OPUS_URL=https://downloads.xiph.org/releases/opus/$OPUS_TARBALL
OPUS_SHA256=6ffcb593207be92584df15b32466ed64bbec99109f007c82205f0194572411a1

VECTORS_TARBALL=opus_testvectors-rfc8251.tar.gz
VECTORS_URL=https://opus-codec.org/static/testvectors/$VECTORS_TARBALL
VECTORS_SHA256=6b26a22f9ba87b2b836906a9bb7afec5f8e54d49553b1200382520ee6fedfa55

CONFIGURE_FLAGS=(--disable-shared --disable-doc --disable-intrinsics --disable-rtcd --disable-asm)
REF_CFLAGS="-O2 -g -ffp-contract=off"

die() {
	echo "build-ref.sh: $*" >&2
	exit 1
}

[ $# = 1 ] || die "usage: build-ref.sh <workdir>"
for tool in gcc make curl tar sha256sum; do
	command -v $tool >/dev/null || die "missing prerequisite: $tool"
done
mkdir -p "$1"
WORKDIR=$(cd "$1" && pwd)
mkdir -p "$WORKDIR/dl" "$WORKDIR/build" "$WORKDIR/work"

# fetch <url> <file> <sha256> downloads into dl/ unless a file with the
# right checksum is already there.
fetch() {
	local url=$1 file=$WORKDIR/dl/$2 sum=$3
	if [ -f "$file" ] && echo "$sum  $file" | sha256sum -c --status; then
		echo "have $2"
		return
	fi
	echo "downloading $url"
	curl -fsSL --retry 3 -o "$file.part" "$url"
	echo "$sum  $file.part" | sha256sum -c --status ||
		die "checksum mismatch for $url (got $(sha256sum "$file.part" | cut -d' ' -f1), want $sum)"
	mv "$file.part" "$file"
}

# extract <tarball> <destdir> <topdir> extracts the tarball into destdir
# unless destdir/topdir was already extracted completely.
extract() {
	local tarball=$WORKDIR/dl/$1 dest=$2 top=$3
	if [ -f "$dest/.$top.extracted" ]; then
		echo "have $dest/$top"
		return
	fi
	rm -rf "${dest:?}/$top"
	tar -xzf "$tarball" -C "$dest"
	[ -d "$dest/$top" ] || die "$1 did not contain $top/"
	touch "$dest/.$top.extracted"
}

fetch "$OPUS_URL" "$OPUS_TARBALL" "$OPUS_SHA256"
fetch "$VECTORS_URL" "$VECTORS_TARBALL" "$VECTORS_SHA256"

extract "$OPUS_TARBALL" "$WORKDIR" opus-$OPUS_VERSION
extract "$VECTORS_TARBALL" "$WORKDIR" opus_newvectors

REF=$WORKDIR/build/opus-$OPUS_VERSION
stamp="configure ${CONFIGURE_FLAGS[*]} CFLAGS=$REF_CFLAGS"
if [ -f "$REF/.libs/libopus.a" ] && [ -f "$REF/.oracle-build" ] && [ "$(cat "$REF/.oracle-build")" = "$stamp" ]; then
	echo "have $REF"
else
	# Build in a separate copy so that OPUS_SRC stays pristine.
	rm -f "$WORKDIR/build/.opus-$OPUS_VERSION.extracted"
	extract "$OPUS_TARBALL" "$WORKDIR/build" opus-$OPUS_VERSION
	echo "building $REF"
	(
		cd "$REF"
		./configure "${CONFIGURE_FLAGS[@]}" CFLAGS="$REF_CFLAGS" >"$WORKDIR/build/configure.log" 2>&1 ||
			die "configure failed; see $WORKDIR/build/configure.log"
		make -j"$(nproc 2>/dev/null || echo 4)" >"$WORKDIR/build/make.log" 2>&1 ||
			die "make failed; see $WORKDIR/build/make.log"
	)
	echo "$stamp" >"$REF/.oracle-build"
fi

# The port follows the default configuration: refuse a tree with the DNN
# features or other non-default options.
for sym in ENABLE_DRED ENABLE_DEEP_PLC ENABLE_OSCE ENABLE_OSCE_BWE ENABLE_QEXT FIXED_POINT CUSTOM_MODES; do
	if grep -q "^#define $sym\b" "$REF/config.h"; then
		die "$REF/config.h unexpectedly defines $sym"
	fi
done
[ -x "$REF/opus_demo" ] && [ -x "$REF/opus_compare" ] || die "opus_demo or opus_compare missing in $REF"

cat <<EOF

Reference ready. Export:

export OPUS_SRC=$WORKDIR/opus-$OPUS_VERSION
export OPUS_REF=$REF
export OPUS_TESTVECTORS=$WORKDIR/opus_newvectors
export OPUS_WORK=$WORKDIR/work
EOF

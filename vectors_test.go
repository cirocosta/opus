package opus

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vectorPacketLine formats the packet functions applied to one real packet
// (and the previous packet of the stream) like the C driver.
func vectorPacketLine(p, prev []byte) string {
	var sb strings.Builder
	sb.WriteString(fmtPacketInfo(p))

	buf := make([]byte, len(p)+300)
	copy(buf, p)
	ret := opusPacketUnpad(buf[:len(p)])
	_, _ = fmt.Fprintf(&sb, " unpad=%d:%016x", ret, fnvHash(buf[:max(ret, 0)]))
	copy(buf, p)
	pret := opusPacketPad(buf, int32(len(p)), int32(len(p)+300))
	_, _ = fmt.Fprintf(&sb, " pad=%d:%016x", pret, fnvHash(buf))

	rp := NewRepacketizer()
	sb.WriteString(" cat=")
	if prev != nil {
		_, _ = fmt.Fprintf(&sb, "%d.", rp.catImpl(prev, false))
	}
	_, _ = fmt.Fprintf(&sb, "%d", rp.catImpl(p, false))
	out := make([]byte, 3100)
	for i := 0; i < rp.NbFrames(); i++ {
		ret := rp.outRangeImpl(i, rp.NbFrames(), out, false, false, nil)
		_, _ = fmt.Fprintf(&sb, ",%d:%016x", ret, fnvHash(out[:max(ret, 0)]))
	}
	return sb.String()
}

// TestVectors runs the packet functions over the packets of the RFC 8251
// test vectors in $OPUS_TESTVECTORS and compares a digest of the results
// with libopus.
func TestVectors(t *testing.T) {
	dir := os.Getenv("OPUS_TESTVECTORS")
	if dir == "" {
		t.Skip("OPUS_TESTVECTORS not set")
	}
	f, err := os.Open(filepath.Join("testdata", "vectors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var name string
		var count int
		var want uint64
		if _, err := fmt.Sscanf(sc.Text(), "%s %d %x", &name, &count, &want); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, name+".bit"))
		if err != nil {
			t.Fatal(err)
		}
		h := fnv.New64a()
		n := 0
		var prev []byte
		for len(data) >= 8 {
			ln := int(binary.BigEndian.Uint32(data))
			data = data[8:]
			if ln > len(data) {
				break
			}
			p := append([]byte(nil), data[:ln]...)
			data = data[ln:]
			_, _ = fmt.Fprintf(h, "%s\n", vectorPacketLine(p, prev))
			prev = p
			n++
		}
		if n != count || h.Sum64() != want {
			t.Errorf("%s: got %d packets, digest %016x; want %d, %016x",
				name, n, h.Sum64(), count, want)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}

package opus

// Golden tests compare this package with the libopus 1.6.1 reference
// build. The Go test code generates deterministic inputs; a C driver
// (tools/oracle/pkt/golden.c) reads the same inputs, runs libopus, and
// writes one text line per case to testdata/<set>.txt.gz. The tests here
// regenerate the inputs, run the Go port, format the results the same way,
// and compare line by line. tools/oracle/pkt/gen.sh regenerates the
// goldens:
//
//	OPUS_GOLDEN_INPUTS=/some/dir go test -run TestWriteGoldenInputs .
//	./golden <set> /some/dir/<set>.bin | gzip -9n > testdata/<set>.txt.gz
//	./golden vectors $OPUS_TESTVECTORS > testdata/vectors.txt
//
// The C driver links the scalar libopus.a reference build. Input
// generators must not depend on floating-point fusion, so they compute
// float samples from integers.

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenSet describes one golden test set: how to generate its input
// records and how to compute the result line of one record.
type goldenSet struct {
	name string
	gen  func(r *rand.Rand) [][]byte
	run  func(rec []byte) string
}

var goldenSets []goldenSet

func registerGolden(name string, gen func(r *rand.Rand) [][]byte, run func(rec []byte) string) {
	goldenSets = append(goldenSets, goldenSet{name, gen, run})
}

func goldenRand(name string) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(name))
	return rand.New(rand.NewPCG(h.Sum64(), 0x6f707573))
}

// TestWriteGoldenInputs writes the input records of every golden set to
// the directory named by OPUS_GOLDEN_INPUTS.
func TestWriteGoldenInputs(t *testing.T) {
	dir := os.Getenv("OPUS_GOLDEN_INPUTS")
	if dir == "" {
		t.Skip("OPUS_GOLDEN_INPUTS not set")
	}
	for _, gs := range goldenSets {
		recs := gs.gen(goldenRand(gs.name))
		var buf []byte
		for _, rec := range recs {
			buf = binary.LittleEndian.AppendUint32(buf, uint32(len(rec)))
			buf = append(buf, rec...)
		}
		if err := os.WriteFile(filepath.Join(dir, gs.name+".bin"), buf, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readGoldenLines(t *testing.T, name string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name+".txt.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestGolden(t *testing.T) {
	for _, gs := range goldenSets {
		t.Run(gs.name, func(t *testing.T) {
			want := readGoldenLines(t, gs.name)
			recs := gs.gen(goldenRand(gs.name))
			if len(recs) != len(want) {
				t.Fatalf("got %d records, golden has %d lines", len(recs), len(want))
			}
			fails := 0
			for i, rec := range recs {
				got := fmt.Sprintf("%d %s", i, gs.run(rec))
				if got != want[i] {
					in := fmt.Sprintf("%x", rec)
					if len(in) > 200 {
						in = in[:200] + "..."
					}
					t.Errorf("record %d (input %s):\n got: %s\nwant: %s", i, in, got, want[i])
					fails++
					if fails > 5 {
						t.FailNow()
					}
				}
			}
		})
	}
}

// fnvHash is FNV-1a 64, which the C driver also implements.
func fnvHash(b []byte) uint64 {
	h := fnv.New64a()
	h.Write(b)
	return h.Sum64()
}

// recReader decodes the little-endian fields of an input record.
type recReader struct {
	b   []byte
	pos int
}

func (r *recReader) u8() int {
	v := r.b[r.pos]
	r.pos++
	return int(v)
}

func (r *recReader) u16() int {
	v := binary.LittleEndian.Uint16(r.b[r.pos:])
	r.pos += 2
	return int(v)
}

func (r *recReader) i16() int {
	return int(int16(r.u16()))
}

func (r *recReader) bytes(n int) []byte {
	v := r.b[r.pos : r.pos+n]
	r.pos += n
	return v
}

func (r *recReader) blob() []byte { return r.bytes(r.u16()) }

func (r *recReader) rest() []byte { return r.b[r.pos:] }

// recWriter encodes the fields of an input record.
type recWriter struct{ b []byte }

func (w *recWriter) u8(v int)  { w.b = append(w.b, byte(v)) }
func (w *recWriter) u16(v int) { w.b = binary.LittleEndian.AppendUint16(w.b, uint16(v)) }
func (w *recWriter) blob(p []byte) {
	w.u16(len(p))
	w.b = append(w.b, p...)
}

// readExtensions decodes an extension list: u8 count, then u8 id, u8
// frame, and a blob for each.
func (r *recReader) extensions() []ExtensionData {
	n := r.u8()
	exts := make([]ExtensionData, n)
	for i := range exts {
		exts[i].ID = r.u8()
		exts[i].Frame = r.u8()
		exts[i].Data = r.blob()
	}
	return exts
}

func (w *recWriter) extensions(exts []ExtensionData) {
	w.u8(len(exts))
	for _, e := range exts {
		w.u8(e.ID)
		w.u8(e.Frame)
		w.blob(e.Data)
	}
}

func randBytes(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// genFrameSize returns a random frame length, biased toward small values
// and toward the 1 and 2 byte length coding boundary.
func genFrameSize(r *rand.Rand) int {
	switch r.IntN(10) {
	case 0:
		return 0
	case 1:
		return 248 + r.IntN(10)
	case 2:
		return r.IntN(1300)
	default:
		return r.IntN(24)
	}
}

// appendSize appends a frame length with the Opus 1 or 2 byte coding.
// This is independent of encodeSize so that the inputs do not depend on
// the code under test.
func appendSize(b []byte, n int) []byte {
	if n < 252 {
		return append(b, byte(n))
	}
	b0 := 252 + n&3
	return append(b, byte(b0), byte((n-b0)>>2))
}

// genRawExtensions returns padding bytes made of random extension tokens.
// With wellFormed set, it avoids the tokens that can make the padding
// invalid (frame separators, L=0 long extensions, and random bytes).
func genRawExtensions(r *rand.Rand, maxTokens int, wellFormed bool) []byte {
	var b []byte
	n := r.IntN(maxTokens + 1)
	for range n {
		k := r.IntN(12)
		if wellFormed && (k == 1 || k == 2 || k >= 10) {
			k = 4 + r.IntN(6)
		}
		switch k {
		case 0:
			b = append(b, 0x01) // padding
		case 1:
			b = append(b, 0x02) // next frame
		case 2:
			b = append(b, 0x03, byte(r.IntN(4))) // frame increment
		case 3:
			b = append(b, byte(0x04+r.IntN(2))) // repeat
		case 4, 5, 6:
			id := 3 + r.IntN(29)
			l := r.IntN(2)
			b = append(b, byte(id<<1|l))
			if l == 1 {
				b = append(b, byte(r.Uint32()))
			}
		case 7, 8, 9:
			id := 32 + r.IntN(96)
			ln := r.IntN(8)
			if r.IntN(8) == 0 {
				ln = 250 + r.IntN(20)
			}
			b = append(b, byte(id<<1|1))
			b = appendLacing(b, ln)
			b = append(b, randBytes(r, ln)...)
		case 10:
			// Long extension with L=0 consumes the rest.
			id := 32 + r.IntN(96)
			b = append(b, byte(id<<1))
			b = append(b, randBytes(r, r.IntN(6))...)
		default:
			b = append(b, byte(r.Uint32()))
		}
	}
	return b
}

func appendLacing(b []byte, n int) []byte {
	for n >= 255 {
		b = append(b, 255)
		n -= 255
	}
	return append(b, byte(n))
}

// genExtList returns a random extension list for a packet of nbFrames
// frames. With invalid set, it may contain entries that generate rejects.
func genExtList(r *rand.Rand, nbFrames int, invalid bool) []ExtensionData {
	var exts []ExtensionData
	ids := []int{3 + r.IntN(29), 32 + r.IntN(96), 3 + r.IntN(125)}
	n := r.IntN(8)
	mode := r.IntN(3)
	for k := range n {
		var e ExtensionData
		e.ID = ids[r.IntN(len(ids))]
		e.Frame = r.IntN(max(nbFrames, 1))
		if mode == 0 {
			// Same extension in every frame, to exercise repeats.
			e.ID = ids[k%2]
			e.Frame = k % max(nbFrames, 1)
		}
		if e.ID < 32 {
			e.Data = randBytes(r, r.IntN(2))
		} else {
			ln := r.IntN(10)
			if r.IntN(10) == 0 {
				ln = 250 + r.IntN(300)
			}
			e.Data = randBytes(r, ln)
		}
		if invalid && r.IntN(20) == 0 {
			switch r.IntN(3) {
			case 0:
				e.ID = r.IntN(3)
			case 1:
				e.Frame = nbFrames + r.IntN(3)
			default:
				e.ID = 3 + r.IntN(29)
				e.Data = randBytes(r, 2)
			}
		}
		exts = append(exts, e)
	}
	if mode == 1 {
		// Sort by frame, as encoders produce them.
		for i := 1; i < len(exts); i++ {
			for j := i; j > 0 && exts[j].Frame < exts[j-1].Frame; j-- {
				exts[j], exts[j-1] = exts[j-1], exts[j]
			}
		}
	}
	return exts
}

// packetSpec describes a packet built by buildPacket.
type packetSpec struct {
	toc           byte
	frames        [][]byte
	vbr           bool
	padding       []byte
	padFlag       bool
	selfDelimited bool
}

// buildPacket encodes a packet independently of the code under test.
func buildPacket(s packetSpec) []byte {
	b := []byte{s.toc}
	n := len(s.frames)
	switch s.toc & 3 {
	case 0, 1:
		if s.selfDelimited {
			b = appendSize(b, len(s.frames[n-1]))
		}
	case 2:
		b = appendSize(b, len(s.frames[0]))
		if s.selfDelimited {
			b = appendSize(b, len(s.frames[n-1]))
		}
	case 3:
		ch := byte(n & 0x3f)
		if s.vbr {
			ch |= 0x80
		}
		if s.padFlag {
			ch |= 0x40
		}
		b = append(b, ch)
		if s.padFlag {
			p := len(s.padding)
			for p >= 255 {
				b = append(b, 255)
				p -= 254
			}
			b = append(b, byte(p))
		}
		if s.vbr {
			for i := 0; i < n-1; i++ {
				b = appendSize(b, len(s.frames[i]))
			}
		}
		if s.selfDelimited {
			b = appendSize(b, len(s.frames[n-1]))
		}
	}
	for _, f := range s.frames {
		b = append(b, f...)
	}
	if s.padFlag {
		b = append(b, s.padding...)
	}
	return b
}

// genPacketSpec returns a random, mostly valid packet description. If toc
// is non-negative, its top six bits set the configuration.
func genPacketSpec(r *rand.Rand, toc int, maxFrames int) packetSpec {
	var s packetSpec
	if toc < 0 {
		s.toc = byte(r.Uint32())
	} else {
		s.toc = byte(toc&0xfc) | byte(r.IntN(4))
	}
	var n int
	switch s.toc & 3 {
	case 0:
		n = 1
	case 1, 2:
		n = 2
	default:
		n = 1 + r.IntN(maxFrames)
		s.vbr = r.IntN(2) == 0
		s.padFlag = r.IntN(2) == 0
	}
	size := genFrameSize(r)
	if n > 6 {
		size = r.IntN(30)
	}
	for i := 0; i < n; i++ {
		if s.toc&3 == 2 || s.vbr {
			size = genFrameSize(r)
			if n > 6 {
				size = r.IntN(30)
			}
		}
		s.frames = append(s.frames, randBytes(r, size))
	}
	if s.padFlag {
		switch r.IntN(4) {
		case 0:
			s.padding = make([]byte, r.IntN(600))
		case 1:
			s.padding = randBytes(r, r.IntN(20))
		default:
			s.padding = genRawExtensions(r, 6, r.IntN(2) == 0)
		}
	}
	return s
}

// mutate damages a packet in a random way.
func mutate(r *rand.Rand, p []byte) []byte {
	p = append([]byte(nil), p...)
	switch r.IntN(4) {
	case 0:
		if len(p) > 0 {
			p = p[:r.IntN(len(p))]
		}
	case 1:
		if len(p) > 0 {
			p[r.IntN(len(p))] ^= byte(1 << r.IntN(8))
		}
	case 2:
		if len(p) > 1 {
			p[1] = byte(r.Uint32())
		}
	default:
		p = append(p, randBytes(r, 1+r.IntN(4))...)
	}
	return p
}

func joinInts[T ~int | ~int16 | ~int32](v []T) string {
	var sb strings.Builder
	for i, x := range v {
		if i > 0 {
			sb.WriteByte('.')
		}
		fmt.Fprintf(&sb, "%d", x)
	}
	return sb.String()
}

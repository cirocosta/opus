package opus

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

func init() {
	registerGolden("extensions", genExtensionInputs, runExtensions)
}

// Extension records: u8 kind, then
//   - kind 0: u8 frame count, u8 pad flag, u16 buffer length, extension
//     list (opus_packet_extensions_generate, then the parse report of the
//     output);
//   - kind 1: u8 frame count, raw padding bytes (the parse report).
func genExtensionInputs(r *rand.Rand) [][]byte {
	var recs [][]byte
	for range 2500 {
		var w recWriter
		kind := r.IntN(2)
		w.u8(kind)
		nbFrames := 1 + r.IntN(6)
		switch r.IntN(10) {
		case 0:
			nbFrames = r.IntN(49)
		case 1:
			nbFrames = 1
		}
		if kind == 0 {
			if r.IntN(40) == 0 {
				nbFrames = 49
			}
			w.u8(nbFrames)
			w.u8(r.IntN(2))
			exts := genExtList(r, nbFrames, true)
			size := 0
			for _, e := range exts {
				size += 3 + len(e.Data) + len(e.Data)/255
			}
			bufLen := size + r.IntN(20)
			if r.IntN(4) == 0 {
				bufLen = r.IntN(size + 1)
			}
			w.u16(bufLen)
			w.extensions(exts)
		} else {
			w.u8(nbFrames)
			w.b = append(w.b, genRawExtensions(r, 10, r.IntN(3) == 0)...)
		}
		recs = append(recs, w.b)
	}
	return recs
}

func fmtExt(base []byte, e ExtensionData) string {
	return fmt.Sprintf("%d.%d.%d.%d", e.ID, e.Frame, subOffset(base, e.Data), len(e.Data))
}

func fmtIter(base []byte, it *ExtensionIterator) string {
	var parts []string
	for {
		var e ExtensionData
		ret := it.next(&e)
		if ret <= 0 {
			parts = append(parts, fmt.Sprintf("r%d", ret))
			break
		}
		parts = append(parts, fmtExt(base, e))
	}
	return strings.Join(parts, ",")
}

// extReport formats the parse results of extension data like the C driver.
// data must start at the beginning of its backing array.
func extReport(data []byte, nbFrames int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "c=%d", PacketExtensionsCount(data, nbFrames))

	exts := make([]ExtensionData, 64)
	n := int32(64)
	ret := opusPacketExtensionsParse(data, exts, &n, nbFrames)
	fmt.Fprintf(&sb, " p=%d/%d:", ret, n)
	if ret >= 0 {
		for i := 0; i < int(n); i++ {
			sb.WriteString(fmtExt(data, exts[i]) + ",")
		}
	}

	n1 := int32(1)
	ret = opusPacketExtensionsParse(data, exts, &n1, nbFrames)
	fmt.Fprintf(&sb, " p1=%d/%d", ret, n1)

	var nbFrameExts [48]int
	count := PacketExtensionsCountExt(data, nbFrameExts[:], nbFrames)
	fmt.Fprintf(&sb, " ce=%d:%s", count, joinInts(nbFrameExts[:nbFrames]))
	n = 64
	ret = opusPacketExtensionsParseExt(data, exts, &n, nbFrameExts[:], nbFrames)
	fmt.Fprintf(&sb, " pe=%d/%d:", ret, n)
	if ret >= 0 {
		for i := 0; i < int(n); i++ {
			sb.WriteString(fmtExt(data, exts[i]) + ",")
		}
	}

	var it ExtensionIterator
	it.Init(data, nbFrames)
	it.SetFrameMax((nbFrames + 1) / 2)
	sb.WriteString(" i1=" + fmtIter(data, &it))
	it.Reset()
	sb.WriteString(" i2=" + fmtIter(data, &it))
	it.Reset()
	it.SetFrameMax(nbFrames)
	sb.WriteString(" i3=" + fmtIter(data, &it))

	id := 33
	if c := PacketExtensionsCount(data, nbFrames); c > 0 {
		var e ExtensionData
		it.Init(data, nbFrames)
		it.next(&e)
		id = e.ID
	}
	it.Init(data, nbFrames)
	sb.WriteString(" f=")
	for {
		var e ExtensionData
		ret := it.find(&e, id)
		if ret <= 0 {
			fmt.Fprintf(&sb, "r%d", ret)
			break
		}
		sb.WriteString(fmtExt(data, e) + ",")
	}
	return sb.String()
}

func runExtensions(rec []byte) string {
	rd := &recReader{b: rec}
	kind := rd.u8()
	nbFrames := rd.u8()
	if kind == 1 {
		data := append([]byte(nil), rd.rest()...)
		return extReport(data, nbFrames)
	}
	pad := rd.u8() != 0
	bufLen := rd.u16()
	exts := rd.extensions()
	sz := opusPacketExtensionsGenerate(nil, int32(bufLen), exts, nbFrames, pad)
	buf := bytes.Repeat([]byte{0xaa}, bufLen)
	ret := opusPacketExtensionsGenerate(buf, int32(bufLen), exts, nbFrames, pad)
	s := fmt.Sprintf("sz=%d g=%d:%016x", sz, ret, fnvHash(buf))
	if ret >= 0 && nbFrames <= 48 {
		s += " " + extReport(buf[:ret], nbFrames)
	}
	return s
}

func TestExtensionsRoundTrip(t *testing.T) {
	exts := []ExtensionData{
		{ID: 3, Frame: 0, Data: []byte{1}},
		{ID: 40, Frame: 0, Data: []byte("frame0")},
		{ID: 3, Frame: 1, Data: []byte{2}},
		{ID: 40, Frame: 1, Data: []byte("frame1")},
		{ID: 3, Frame: 2, Data: []byte{3}},
		{ID: 40, Frame: 2, Data: []byte("frame2")},
	}
	sz, err := PacketExtensionsSize(exts, 3)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, sz)
	n, err := PacketExtensionsGenerate(buf, exts, 3, false)
	if err != nil || n != sz {
		t.Fatalf("PacketExtensionsGenerate = %d, %v; want %d", n, err, sz)
	}
	var counts [3]int
	if c := PacketExtensionsCountExt(buf, counts[:], 3); c != 6 || counts != [3]int{2, 2, 2} {
		t.Fatalf("PacketExtensionsCountExt = %d %v", c, counts)
	}
	got := make([]ExtensionData, 6)
	n, err = PacketExtensionsParseExt(buf, got, counts[:], 3)
	if err != nil || n != 6 {
		t.Fatalf("PacketExtensionsParseExt = %d, %v", n, err)
	}
	for i := range exts {
		if got[i].ID != exts[i].ID || got[i].Frame != exts[i].Frame ||
			!bytes.Equal(got[i].Data, exts[i].Data) {
			t.Errorf("extension %d = %+v, want %+v", i, got[i], exts[i])
		}
	}
	if _, err := PacketExtensionsParse(buf, got[:2], 3); err != ErrBufferTooSmall {
		t.Errorf("PacketExtensionsParse short err = %v", err)
	}
	it := NewExtensionIterator(buf, 3)
	e, ok, err := it.Find(40)
	if err != nil || !ok || string(e.Data) != "frame0" {
		t.Fatalf("Find(40) = %+v %v %v", e, ok, err)
	}
	padded := make([]byte, sz+5)
	if n, err := PacketExtensionsGenerate(padded, exts, 3, true); err != nil || n != sz+5 {
		t.Fatalf("padded generate = %d, %v", n, err)
	}
	if c := PacketExtensionsCount(padded, 3); c != 6 {
		t.Fatalf("padded count = %d", c)
	}
}

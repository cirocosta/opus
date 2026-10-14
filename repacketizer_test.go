package opus

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

func init() {
	registerGolden("repacketizer", genRepacketizerInputs, runRepacketizer)
	registerGolden("pad", genPadInputs, runPad)
}

// Repacketizer records: u8 packet count, a blob for each packet, u8 op
// count, then for each op: u8 kind (0 out_range, 1 out, 2
// out_range_impl), i16 begin, i16 end, u16 maxlen, u8 flags (1 self
// delimited, 2 pad), and an extension list.
func genRepacketizerInputs(r *rand.Rand) [][]byte {
	var recs [][]byte
	for range 1500 {
		var w recWriter
		toc := int(r.Uint32() & 0xfc)
		np := 1 + r.IntN(6)
		w.u8(np)
		total, nframes := 0, 0
		for range np {
			ptoc := toc
			if r.IntN(15) == 0 {
				ptoc = int(r.Uint32())
			}
			s := genPacketSpec(r, ptoc, 1+r.IntN(8))
			p := buildPacket(s)
			if r.IntN(12) == 0 {
				p = mutate(r, p)
			}
			total += len(p)
			nframes += len(s.frames)
			w.blob(p)
		}
		nops := 1 + r.IntN(6)
		w.u8(nops)
		for range nops {
			kind := r.IntN(3)
			w.u8(kind)
			var begin, end int
			if r.IntN(4) == 0 {
				begin, end = r.IntN(nframes+2)-1, r.IntN(nframes+2)
			} else {
				end = 1 + r.IntN(max(min(nframes, 48), 1))
				begin = r.IntN(end)
				if r.IntN(2) == 0 {
					begin = 0
				}
			}
			w.u16(begin)
			w.u16(end)
			var maxlen int
			switch r.IntN(4) {
			case 0:
				maxlen = r.IntN(total + 20)
			case 1:
				maxlen = total + r.IntN(400)
			default:
				maxlen = total + 64
			}
			w.u16(maxlen)
			w.u8(r.IntN(4))
			var exts []ExtensionData
			if kind == 2 && r.IntN(2) == 0 {
				exts = genExtList(r, max(end-begin, 1), true)
			}
			w.extensions(exts)
		}
		recs = append(recs, w.b)
	}
	return recs
}

func runRepacketizer(rec []byte) string {
	rd := &recReader{b: rec}
	rp := NewRepacketizer()
	var cats []int
	np := rd.u8()
	for range np {
		p := append([]byte(nil), rd.blob()...)
		cats = append(cats, rp.catImpl(p, false))
	}
	var ops []string
	nops := rd.u8()
	for range nops {
		kind := rd.u8()
		begin, end := rd.i16(), rd.i16()
		maxlen := rd.u16()
		flags := rd.u8()
		exts := rd.extensions()
		buf := bytes.Repeat([]byte{0xaa}, maxlen)
		var ret int32
		switch kind {
		case 0:
			n, err := rp.OutRange(begin, end, buf)
			ret = int32(n)
			if err != nil {
				ret = int32(err.(Error))
			}
		case 1:
			n, err := rp.Out(buf)
			ret = int32(n)
			if err != nil {
				ret = int32(err.(Error))
			}
		default:
			ret = rp.outRangeImpl(begin, end, buf, flags&1 != 0, flags&2 != 0, exts)
		}
		ops = append(ops, fmt.Sprintf("%d:%016x", ret, fnvHash(buf)))
	}
	return fmt.Sprintf("cat=%s nf=%d ops=%s", joinInts(cats), rp.NbFrames(),
		strings.Join(ops, ","))
}

// Pad records: u8 kind, then
//   - kind 0: packet blob, u8 delta count, u16 deltas (opus_packet_pad and
//     opus_packet_unpad);
//   - kind 1: packet blob, u16 new length, u8 pad flag, extension list
//     (opus_packet_pad_impl);
//   - kind 2: u8 stream count, packet blob, u8 delta count, u16 deltas
//     (opus_multistream_packet_pad and _unpad).
func genPadInputs(r *rand.Rand) [][]byte {
	var recs [][]byte
	deltas := func(w *recWriter) {
		n := 1 + r.IntN(4)
		w.u8(n)
		for range n {
			switch r.IntN(4) {
			case 0:
				w.u16(r.IntN(3))
			case 1:
				w.u16(250 + r.IntN(20))
			default:
				w.u16(r.IntN(600))
			}
		}
	}
	for range 1500 {
		var w recWriter
		kind := r.IntN(3)
		w.u8(kind)
		switch kind {
		case 0:
			p := buildPacket(genPacketSpec(r, -1, 6))
			if r.IntN(10) == 0 {
				p = mutate(r, p)
			}
			w.blob(p)
			deltas(&w)
		case 1:
			s := genPacketSpec(r, -1, 6)
			p := buildPacket(s)
			if r.IntN(10) == 0 {
				p = mutate(r, p)
			}
			w.blob(p)
			nl := len(p) + r.IntN(200) - 10
			if r.IntN(4) == 0 {
				nl = len(p) + 300 + r.IntN(400)
			}
			w.u16(max(nl, 0))
			w.u8(r.IntN(2))
			w.extensions(genExtList(r, len(s.frames), true))
		default:
			ns := 1 + r.IntN(4)
			if r.IntN(10) == 0 {
				ns = r.IntN(2) // 0 or 1 streams
			}
			w.u8(ns)
			var p []byte
			for i := range max(ns, 1) {
				s := genPacketSpec(r, -1, 6)
				s.selfDelimited = i < ns-1
				p = append(p, buildPacket(s)...)
			}
			if r.IntN(10) == 0 {
				p = mutate(r, p)
			}
			w.blob(p)
			deltas(&w)
		}
		recs = append(recs, w.b)
	}
	return recs
}

func padResult(ret int, buf []byte) string {
	return fmt.Sprintf("%d:%016x", ret, fnvHash(buf))
}

func runPad(rec []byte) string {
	rd := &recReader{b: rec}
	switch rd.u8() {
	case 0:
		p := rd.blob()
		buf := append([]byte(nil), p...)
		ret := opusPacketUnpad(buf)
		parts := []string{"unpad=" + padResult(int(ret), buf)}
		for range rd.u8() {
			d := rd.u16()
			buf := bytes.Repeat([]byte{0xaa}, len(p)+d)
			copy(buf, p)
			ret := opusPacketPad(buf, int32(len(p)), int32(len(p)+d))
			s := "pad=" + padResult(ret, buf)
			ret2 := opusPacketUnpad(buf)
			parts = append(parts, s+",unpad="+padResult(int(ret2), buf))
		}
		return strings.Join(parts, " ")
	case 1:
		p := rd.blob()
		newLen := rd.u16()
		pad := rd.u8() != 0
		exts := rd.extensions()
		buf := bytes.Repeat([]byte{0xaa}, max(len(p), newLen))
		copy(buf, p)
		ret := opusPacketPadImpl(buf, int32(len(p)), int32(newLen), pad, exts)
		return "padimpl=" + padResult(int(ret), buf)
	default:
		ns := rd.u8()
		p := rd.blob()
		buf := append([]byte(nil), p...)
		ret := opusMultistreamPacketUnpad(buf, ns)
		parts := []string{"msunpad=" + padResult(int(ret), buf)}
		for range rd.u8() {
			d := rd.u16()
			buf := bytes.Repeat([]byte{0xaa}, len(p)+d)
			copy(buf, p)
			ret := opusMultistreamPacketPad(buf, int32(len(p)), int32(len(p)+d), ns)
			s := "mspad=" + padResult(ret, buf)
			ret2 := opusMultistreamPacketUnpad(buf, ns)
			parts = append(parts, s+",msunpad="+padResult(int(ret2), buf))
		}
		return strings.Join(parts, " ")
	}
}

func TestRepacketizerAPI(t *testing.T) {
	// Three CELT FB 20 ms mono packets, merged and split again.
	toc := byte(31 << 3)
	pkts := [][]byte{
		{toc, 1, 2, 3},
		{toc | 1, 4, 5, 6, 7},
		{toc, 8},
	}
	rp := NewRepacketizer()
	for _, p := range pkts {
		if err := rp.Cat(p); err != nil {
			t.Fatal(err)
		}
	}
	if rp.NbFrames() != 4 {
		t.Fatalf("NbFrames = %d", rp.NbFrames())
	}
	out := make([]byte, 64)
	n, err := rp.Out(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{toc | 3, 0x84, 3, 2, 2, 1, 2, 3, 4, 5, 6, 7, 8}
	if !bytes.Equal(out[:n], want) {
		t.Fatalf("Out = %x, want %x", out[:n], want)
	}
	n, err = rp.OutRange(1, 3, out)
	if err != nil || !bytes.Equal(out[:n], []byte{toc | 1, 4, 5, 6, 7}) {
		t.Fatalf("OutRange(1, 3) = %x, %v", out[:n], err)
	}
	if _, err := rp.OutRange(0, 4, out[:5]); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("OutRange small buffer err = %v", err)
	}
	if err := rp.Cat([]byte{toc ^ 4, 0}); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("Cat mismatched TOC err = %v", err)
	}

	// Pad to 40 bytes and unpad again.
	buf := make([]byte, 40)
	copy(buf, want)
	if err := PacketPad(buf, len(want)); err != nil {
		t.Fatal(err)
	}
	n, err = PacketUnpad(buf)
	if err != nil || !bytes.Equal(buf[:n], want) {
		t.Fatalf("PacketUnpad = %x, %v", buf[:n], err)
	}
	if err := PacketPad(buf, 41); !errors.Is(err, ErrBadArg) {
		t.Fatalf("PacketPad too long err = %v", err)
	}
}

// FuzzPacket checks that the packet functions do not panic on arbitrary
// input.
func FuzzPacket(f *testing.F) {
	f.Add([]byte{0x03, 0xc2, 0x01, 0xaa, 0xbb}, uint16(10), uint8(2))
	f.Add([]byte{0xfb, 0x43, 0x03, 0x41, 0x05, 0x01}, uint16(300), uint8(1))
	f.Fuzz(func(t *testing.T, p []byte, extra uint16, ns uint8) {
		fmtPacketInfo(p)
		var padding []byte
		var size [48]int16
		var frames [48][]byte
		if n := opusPacketParseImpl(p, false, nil, frames[:], size[:], nil, nil, &padding); n > 0 {
			extReport(append([]byte(nil), padding...), n)
		}
		buf := make([]byte, len(p)+int(extra))
		copy(buf, p)
		PacketPad(buf, len(p))
		PacketUnpad(buf)
		copy(buf, p)
		MultistreamPacketPad(buf, len(p), int(ns%5))
		MultistreamPacketUnpad(buf, int(ns%5))
		rp := NewRepacketizer()
		rp.Cat(p)
		rp.Cat(p)
		rp.Out(buf)
		rp.outRangeImpl(0, rp.NbFrames(), buf, true, true, []ExtensionData{{ID: 40, Data: p}})
	})
}

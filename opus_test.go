package opus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

func init() {
	registerGolden("parse", genParseInputs, runParse)
	registerGolden("softclip", genSoftClipInputs, runSoftClip)
}

// subOffset returns the offset of sub within base. Both must share the
// same backing array, with base starting at its beginning.
func subOffset(base, sub []byte) int {
	return cap(base) - cap(sub)
}

func genParseInputs(r *rand.Rand) [][]byte {
	recs := [][]byte{
		{},
		{0x00},
		{0x01},
		{0x02},
		{0x03},
		{0x01, 0xaa},                   // code 1, odd length
		{0x02, 0xfc},                   // code 2, truncated 2-byte size
		{0x02, 0x05, 1, 2},             // code 2, size larger than data
		{0x03, 0x00},                   // code 3, zero frames
		{0x03, 0x31},                   // code 3, 49 frames
		{0x1b, 0x02, 1, 2},             // 60 ms, 2 frames
		{0x1b, 0x03, 1, 2, 3},          // 60 ms, 3 frames: over 120 ms
		{0xfb, 0x30},                   // 48 frames of 2.5 ms
		{0x03, 0x41},                   // padding flag with no padding byte
		{0x03, 0x41, 0xff},             // chained padding, truncated
		{0x03, 0x41, 0xff, 0x00},       // 254 bytes padding, not present
		{0x03, 0xc2, 0x01, 0xaa, 0xbb}, // VBR, 2 frames
		{0x03, 0x82, 0x05, 0xaa},       // VBR, first size too large
	}
	// Last frame larger than 1275 bytes.
	recs = append(recs, append([]byte{0x00}, make([]byte, 1276)...))
	recs = append(recs, append([]byte{0x00}, make([]byte, 1275)...))
	recs = append(recs, append([]byte{0x01}, make([]byte, 2552)...))
	recs = append(recs, append([]byte{0x03, 0x02}, make([]byte, 2550)...))
	for range 3000 {
		var p []byte
		switch k := r.IntN(10); {
		case k < 6:
			s := genPacketSpec(r, -1, 48)
			s.selfDelimited = r.IntN(3) == 0
			p = buildPacket(s)
		case k < 8:
			s := genPacketSpec(r, -1, 48)
			s.selfDelimited = r.IntN(3) == 0
			p = mutate(r, buildPacket(s))
		default:
			p = randBytes(r, r.IntN(12))
		}
		recs = append(recs, p)
	}
	return recs
}

// fmtParse formats the result of opusPacketParseImpl like the C driver.
func fmtParse(data []byte, selfDelimited bool) string {
	var toc byte
	var frames [48][]byte
	var size [48]int16
	var payloadOffset int
	var packetOffset int32
	var padding []byte
	for i := range size {
		size[i] = -2
	}
	ret := opusPacketParseImpl(data, selfDelimited, &toc, frames[:], size[:],
		&payloadOffset, &packetOffset, &padding)
	if ret < 0 {
		if padding != nil {
			return "padding not nil on error"
		}
		return fmt.Sprintf("E%d", ret)
	}
	offs := make([]int, ret)
	for i := range offs {
		offs[i] = subOffset(data, frames[i])
		if len(frames[i]) != int(size[i]) {
			return "frame length mismatch"
		}
	}
	return fmt.Sprintf("%d,t%d,po%d,ko%d,pd%d/%d,s%s,f%s", ret, toc, payloadOffset,
		packetOffset, subOffset(data, padding), len(padding), joinInts(size[:ret]),
		joinInts(offs))
}

// fmtPacketInfo formats the packet inspection results like the C driver.
func fmtPacketInfo(p []byte) string {
	var sb strings.Builder
	sb.WriteString("sd0:" + fmtParse(p, false) + " sd1:" + fmtParse(p, true))
	if len(p) == 0 {
		_, _ = fmt.Fprintf(&sb, " bw- ch- nf%d ns48%d ns8%d spf- lbrr-",
			opusPacketGetNbFrames(p), opusPacketGetNbSamples(p, 48000),
			opusPacketGetNbSamples(p, 8000))
		return sb.String()
	}
	_, _ = fmt.Fprintf(&sb, " bw%d ch%d nf%d ns48%d ns8%d spf%d.%d lbrr%d",
		opusPacketGetBandwidth(p[0]), opusPacketGetNbChannels(p[0]),
		opusPacketGetNbFrames(p), opusPacketGetNbSamples(p, 48000),
		opusPacketGetNbSamples(p, 8000), opusPacketGetSamplesPerFrame(p[0], 48000),
		opusPacketGetSamplesPerFrame(p[0], 8000), opusPacketHasLBRR(p))
	return sb.String()
}

func runParse(rec []byte) string {
	p := append([]byte(nil), rec...)
	return fmtPacketInfo(p)
}

func genSoftClipInputs(r *rand.Rand) [][]byte {
	var recs [][]byte
	shapes := [][3]int{{0, 1, 1}, {10, 0, 1}, {1, 1, 2}, {2, 2, 2}}
	for range 200 {
		n := 1 + r.IntN(300)
		if r.IntN(4) == 0 {
			n = 960
		}
		shapes = append(shapes, [3]int{n, 1 + r.IntN(3), 1 + r.IntN(4)})
	}
	for _, s := range shapes {
		var w recWriter
		w.u16(s[0])
		w.u8(s[1])
		w.u8(s[2])
		// The samples are computed with integers and converted once, so
		// that the inputs do not depend on FMA fusion (GOARCH, GOAMD64).
		gain := []int64{2, 5, 8, 12}[r.IntN(4)] // in quarters
		phase := r.Int64N(1 << 20)
		freq := 1 + r.Int64N(1<<17)
		for i := range s[0] * s[1] * s[2] {
			// Triangle wave in [-2^19, 2^19], plus noise.
			t := (phase + freq*int64(i/max(s[1], 1))) & (1<<20 - 1)
			tri := 2*max(t-1<<19, 1<<19-t) - 1<<19
			x := tri*gain + (r.Int64N(1<<19)-1<<18)*4/5
			if r.IntN(50) == 0 {
				x *= 2
			}
			v := float32(x) / (1 << 21)
			w.b = binary.LittleEndian.AppendUint32(w.b, math.Float32bits(v))
		}
		recs = append(recs, w.b)
	}
	return recs
}

func runSoftClip(rec []byte) string {
	rd := &recReader{b: rec}
	n, c, calls := rd.u16(), rd.u8(), rd.u8()
	mem := make([]float32, max(c, 1))
	var parts []string
	for range calls {
		x := make([]float32, n*c)
		for i := range x {
			x[i] = math.Float32frombits(binary.LittleEndian.Uint32(rd.bytes(4)))
		}
		var pcm []float32
		if len(x) > 0 {
			pcm = x
		}
		PCMSoftClip(pcm, n, c, mem)
		var b []byte
		for _, v := range x {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
		}
		s := fmt.Sprintf("h%016x,m", fnvHash(b))
		for i := 0; i < c; i++ {
			s += fmt.Sprintf("%08x.", math.Float32bits(mem[i]))
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ";")
}

func TestErrors(t *testing.T) {
	for code := -8; code <= 1; code++ {
		want := map[int]string{
			0: "success", -1: "invalid argument", -2: "buffer too small",
			-3: "internal error", -4: "corrupted stream",
			-5: "request not implemented", -6: "invalid state",
			-7: "memory allocation failed",
		}[code]
		if want == "" {
			want = "unknown error"
		}
		if got := Strerror(code); got != want {
			t.Errorf("Strerror(%d) = %q, want %q", code, got, want)
		}
	}
	if got := ErrInvalidPacket.Error(); got != "opus: corrupted stream" {
		t.Errorf("ErrInvalidPacket.Error() = %q", got)
	}
	if ErrBufferTooSmall.Code() != -2 {
		t.Errorf("ErrBufferTooSmall.Code() = %d", ErrBufferTooSmall.Code())
	}
	_, err := PacketGetNbFrames([]byte{0x03})
	if !errors.Is(err, ErrInvalidPacket) {
		t.Errorf("PacketGetNbFrames code 3 without count: err = %v", err)
	}
	if VersionString() != "libopus 1.6.1" {
		t.Errorf("VersionString() = %q", VersionString())
	}
}

func TestPacketAPI(t *testing.T) {
	// SILK wideband 20 ms stereo, code 3, VBR, 2 frames, 3 bytes padding.
	p := []byte{0x4c | 0x3, 0xc2, 0x03, 0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0, 0, 0}
	toc, frames, off, err := PacketParse(p)
	if err != nil {
		t.Fatal(err)
	}
	if toc != p[0] || off != 4 || len(frames) != 2 ||
		string(frames[0]) != "\xaa\xbb" || string(frames[1]) != "\xcc\xdd" {
		t.Fatalf("PacketParse = %x %d %x", toc, off, frames)
	}
	if bw, _ := PacketGetBandwidth(p); bw != BandwidthWideband {
		t.Errorf("bandwidth = %d", bw)
	}
	if ch, _ := PacketGetNbChannels(p); ch != 2 {
		t.Errorf("channels = %d", ch)
	}
	if n, _ := PacketGetNbSamples(p, 48000); n != 1920 {
		t.Errorf("samples = %d", n)
	}
	if n, _ := PacketGetSamplesPerFrame(p, 16000); n != 320 {
		t.Errorf("samples per frame = %d", n)
	}
	if _, err := PacketGetBandwidth(nil); !errors.Is(err, ErrBadArg) {
		t.Errorf("PacketGetBandwidth(nil) err = %v", err)
	}
	if _, _, _, err := PacketParse([]byte{0x01, 0}); !errors.Is(err, ErrInvalidPacket) {
		t.Errorf("PacketParse odd code 1 err = %v", err)
	}
}

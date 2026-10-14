package opus

import (
	"errors"
	"testing"
)

func TestNewDecoderArgs(t *testing.T) {
	for _, fs := range []int{8000, 12000, 16000, 24000, 48000} {
		for _, ch := range []int{1, 2} {
			d, err := NewDecoder(fs, ch)
			if err != nil {
				t.Fatalf("NewDecoder(%d, %d): %v", fs, ch, err)
			}
			if d.SampleRate() != fs || d.Channels() != ch {
				t.Errorf("got rate %d channels %d", d.SampleRate(), d.Channels())
			}
		}
	}
	for _, c := range [][2]int{{44100, 2}, {48000, 0}, {48000, 3}, {0, 1}} {
		if _, err := NewDecoder(c[0], c[1]); !errors.Is(err, ErrBadArg) {
			t.Errorf("NewDecoder(%d, %d): %v, want ErrBadArg", c[0], c[1], err)
		}
	}
}

func TestDecoderCTLs(t *testing.T) {
	d, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if d.SetComplexity(11) != ErrBadArg || d.SetComplexity(-1) != ErrBadArg {
		t.Error("SetComplexity accepted an out of range value")
	}
	if err := d.SetComplexity(7); err != nil || d.Complexity() != 7 {
		t.Errorf("complexity: %v %d", err, d.Complexity())
	}
	if d.SetGain(32768) != ErrBadArg || d.SetGain(-32769) != ErrBadArg {
		t.Error("SetGain accepted an out of range value")
	}
	if err := d.SetGain(-256); err != nil || d.Gain() != -256 {
		t.Errorf("gain: %v %d", err, d.Gain())
	}
	d.SetIgnoreExtensions(true)
	d.SetPhaseInversionDisabled(true)
	if !d.IgnoreExtensions() || !d.PhaseInversionDisabled() {
		t.Error("boolean controls did not stick")
	}
	if d.Bandwidth() != 0 || d.LastPacketDuration() != 0 || d.FinalRange() != 0 || d.Pitch() != 0 {
		t.Error("fresh decoder reports state")
	}

	// A lost packet before the first packet decodes to silence.
	pcm := make([]int16, 960*2)
	for i := range pcm {
		pcm[i] = 1
	}
	if n, err := d.Decode(nil, pcm, 960, false); n != 960 || err != nil {
		t.Fatalf("PLC: %d %v", n, err)
	}
	for _, v := range pcm {
		if v != 0 {
			t.Fatal("PLC before the first packet is not silent")
		}
	}
	if d.LastPacketDuration() != 960 {
		t.Errorf("LastPacketDuration %d", d.LastPacketDuration())
	}
	// PLC sizes must be multiples of 2.5 ms.
	if _, err := d.Decode(nil, pcm, 100, false); err != ErrBadArg {
		t.Errorf("PLC of 100 samples: %v", err)
	}
	if _, err := d.Decode(nil, pcm, 0, false); err != ErrBadArg {
		t.Errorf("frame size 0: %v", err)
	}
	if _, err := d.Decode(
		[]byte{0xFC, 0xFF, 0xFE},
		pcm[:10],
		960,
		false,
	); err != ErrBufferTooSmall {
		t.Errorf("short pcm: %v", err)
	}
	// A code 3 packet without a frame count byte is invalid.
	if _, err := d.Decode([]byte{0x03}, pcm, 960, false); err != ErrInvalidPacket {
		t.Errorf("invalid packet: %v", err)
	}
	if n, err := d.NbSamples([]byte{0xFC}); n != 960 || err != nil {
		t.Errorf("NbSamples: %d %v", n, err)
	}
	d.Reset()
	if d.Gain() != -256 || d.Complexity() != 7 {
		t.Error("Reset cleared the settings")
	}
}

func TestMSDecoderArgs(t *testing.T) {
	if _, err := NewMSDecoder(48000, 2, 1, 2, []byte{0, 1}); err != ErrBadArg {
		t.Errorf("coupled > streams: %v", err)
	}
	if _, err := NewMSDecoder(48000, 2, 1, 1, []byte{0, 2}); err != ErrBadArg {
		t.Errorf("bad mapping: %v", err)
	}
	if _, err := NewMSDecoder(48000, 3, 1, 1, []byte{0, 1}); err != ErrBadArg {
		t.Errorf("short mapping: %v", err)
	}
	d, err := NewMSDecoder(48000, 3, 2, 1, []byte{0, 255, 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.DecoderState(2); err != ErrBadArg {
		t.Errorf("DecoderState(2): %v", err)
	}
	if s, err := d.DecoderState(0); err != nil || s.Channels() != 2 {
		t.Errorf("DecoderState(0): %v", err)
	}
	pcm := make([]float32, 960*3)
	// One byte cannot hold two streams.
	if _, err := d.DecodeFloat([]byte{0xFC}, pcm, 960, false); err != ErrInvalidPacket {
		t.Errorf("short packet: %v", err)
	}
	if n, err := d.DecodeFloat(nil, pcm, 960, false); n != 960 || err != nil {
		t.Errorf("PLC: %d %v", n, err)
	}
	if _, err := NewProjectionDecoder(48000, 4, 2, 2, make([]byte, 7)); err != ErrBadArg {
		t.Errorf("bad matrix size: %v", err)
	}
	if _, err := NewProjectionDecoder(48000, 4, 0, 0, nil); err != ErrAllocFail {
		t.Errorf("no streams: %v", err)
	}
}

// TestDecoderAllocs checks that decoding does not allocate in steady
// state: normal packets, PLC, FEC and multistream.
func TestDecoderAllocs(t *testing.T) {
	pkts, _, err := readBitPackets("internal/silk/testdata/silk_mix.bit")
	if err != nil {
		t.Fatal(err)
	}
	mix, _, err := readBitPackets("testdata/dec_mix.bit")
	if err != nil {
		t.Fatal(err)
	}
	pkts = append(pkts[:200:200], mix[:300]...)
	d, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetComplexity(7); err != nil {
		t.Fatal(err)
	}
	i16 := make([]int16, 2*5760)
	f32 := make([]float32, 2*5760)
	i24 := make([]int32, 2*5760)
	i := 0
	step := func() {
		p := pkts[i%len(pkts)]
		switch i % 7 {
		case 3:
			_, _ = d.Decode(nil, i16, 960, false)
		case 4:
			_, _ = d.Decode(p, i16, 960, true)
		case 5:
			_, _ = d.Decode(nil, i16, 480, false)
		case 6:
			_, _ = d.Decode24(p, i24, 5760, false)
		default:
			_, _ = d.DecodeFloat(p, f32, 5760, false)
		}
		i++
	}
	for range 50 {
		step()
	}
	if a := testing.AllocsPerRun(len(pkts), step); a != 0 {
		t.Errorf("Decoder: %v allocations per call", a)
	}

	s, err := readMSStream("testdata/proj9.pk")
	if err != nil {
		t.Fatal(err)
	}
	pd, err := NewProjectionDecoder(48000, s.ch, s.streams, s.coupled, s.matrix)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]int16, 5760*s.ch)
	j := 0
	msStep := func() {
		if j%5 == 2 {
			_, _ = pd.Decode(nil, out, 960, false)
		} else {
			_, _ = pd.Decode(s.pkts[j%len(s.pkts)], out, 5760, false)
		}
		j++
	}
	msStep()
	if a := testing.AllocsPerRun(len(s.pkts), msStep); a != 0 {
		t.Errorf("ProjectionDecoder: %v allocations per call", a)
	}
}

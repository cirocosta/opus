package celt

import (
	"path/filepath"
	"testing"

	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
)

func TestEncoderCTL(t *testing.T) {
	if _, ret := NewEncoder(44100, 2); ret != BadArg {
		t.Errorf("44.1 kHz: ret %d, want BadArg", ret)
	}
	if _, ret := NewEncoder(48000, 3); ret != BadArg {
		t.Errorf("3 channels: ret %d, want BadArg", ret)
	}
	e, ret := NewEncoder(16000, 2)
	if ret != OK {
		t.Fatal(ret)
	}
	if e.LSBDepth() != 24 || e.PhaseInversionDisabled() != 0 || e.Mode() != StaticMode() ||
		e.Channels() != 2 || e.StreamChannels() != 2 {
		t.Error("bad defaults")
	}
	for _, c := range []struct {
		name string
		got  int
		want int
	}{
		{"complexity -1", e.SetComplexity(-1), BadArg},
		{"complexity 11", e.SetComplexity(11), BadArg},
		{"complexity 7", e.SetComplexity(7), OK},
		{"start -1", e.SetStartBand(-1), BadArg},
		{"start 21", e.SetStartBand(21), BadArg},
		{"start 0", e.SetStartBand(0), OK},
		{"end 0", e.SetEndBand(0), BadArg},
		{"end 22", e.SetEndBand(22), BadArg},
		{"end 21", e.SetEndBand(21), OK},
		{"prediction 3", e.SetPrediction(3), BadArg},
		{"prediction -1", e.SetPrediction(-1), BadArg},
		{"prediction 2", e.SetPrediction(2), OK},
		{"loss 101", e.SetPacketLossPerc(101), BadArg},
		{"loss -1", e.SetPacketLossPerc(-1), BadArg},
		{"loss 10", e.SetPacketLossPerc(10), OK},
		{"bitrate 500", e.SetBitrate(500), BadArg},
		{"bitrate max", e.SetBitrate(BitrateMax), OK},
		{"bitrate 2M", e.SetBitrate(2000000), OK},
		{"channels 0", e.SetChannels(0), BadArg},
		{"channels 3", e.SetChannels(3), BadArg},
		{"channels 1", e.SetChannels(1), OK},
		{"lsb 7", e.SetLSBDepth(7), BadArg},
		{"lsb 25", e.SetLSBDepth(25), BadArg},
		{"lsb 16", e.SetLSBDepth(16), OK},
		{"inv 2", e.SetPhaseInversionDisabled(2), BadArg},
		{"inv -1", e.SetPhaseInversionDisabled(-1), BadArg},
		{"inv 1", e.SetPhaseInversionDisabled(1), OK},
		{"vbr", e.SetVBR(1), OK},
		{"cvbr", e.SetVBRConstraint(0), OK},
		{"lfe", e.SetLFE(0), OK},
		{"mask", e.SetEnergyMask(nil), OK},
	} {
		if c.got != c.want {
			t.Errorf("%s: %d, want %d", c.name, c.got, c.want)
		}
	}
	if e.bitrate != 1500000 {
		t.Errorf("bitrate not capped: %d", e.bitrate)
	}
	if e.LSBDepth() != 16 || e.PhaseInversionDisabled() != 1 || e.StreamChannels() != 1 {
		t.Error("ctl values not kept")
	}
	e.SetAnalysis(nil)
	e.SetSILKInfo(nil)
	e.SetEnergyMask(make([]float32, 42))
	e.SetAnalysis(&AnalysisInfo{Valid: 1})
	e.SetSILKInfo(&SILKInfo{SignalType: 2, Offset: 7})
	e.Reset()
	if e.st.energyMask != nil || e.st.analysis.Valid != 0 || e.st.silkInfo.Offset != 0 {
		t.Error("reset keeps the analysis, SILK info or energy mask")
	}
	e.SetSignalling(0)
	pcm := make([]float32, 2*320)
	buf := make([]byte, 1275)
	if got := e.EncodeWithEC(pcm, 320, buf, 1, nil); got != BadArg {
		t.Errorf("1-byte packet: %d", got)
	}
	if got := e.EncodeWithEC(nil, 320, buf, 100, nil); got != BadArg {
		t.Errorf("nil pcm: %d", got)
	}
	if got := e.EncodeWithEC(pcm, 100, buf, 100, nil); got != BadArg {
		t.Errorf("bad frame size: %d", got)
	}
	// A silent VBR frame takes 2 bytes.
	if got := e.EncodeWithEC(pcm, 320, buf, 100, nil); got != 2 {
		t.Errorf("silent VBR frame: %d", got)
	}
}

// TestEncoderNoAllocs checks that encoding does not allocate.
func TestEncoderNoAllocs(t *testing.T) {
	src := readPCMGz(t, filepath.Join("testdata", "enc_pcm.raw.gz"))
	pcm := make([]float32, len(src))
	for i, v := range src {
		pcm[i] = dsp.Int16ToRes(v)
	}
	mask := make([]float32, 42)
	for i := range mask {
		mask[i] = float32(i%7-5) * .25
	}
	for _, c := range []struct {
		name       string
		fs         int32
		ch, lm     int
		vbr, start int
		ext        bool
		mask       bool
	}{
		{"stereo 20 ms VBR", 48000, 2, 3, 1, 0, false, true},
		{"mono 2.5 ms CBR", 48000, 1, 0, 0, 0, false, false},
		{"stereo 10 ms 16 kHz", 16000, 2, 2, 1, 0, true, false},
		{"hybrid", 48000, 2, 3, 1, 17, true, false},
	} {
		e, ret := NewEncoder(c.fs, c.ch)
		if ret != OK {
			t.Fatal(ret)
		}
		e.SetSignalling(0)
		e.SetComplexity(10)
		e.SetVBR(c.vbr)
		e.SetBitrate(int32(48000 * c.ch))
		e.SetStartBand(c.start)
		e.SetAnalysis(&AnalysisInfo{Valid: 1, Tonality: .5, Activity: .3, Bandwidth: 20, MaxPitchRatio: 1})
		if c.mask {
			e.SetEnergyMask(mask)
		}
		fsz := (120 << uint(c.lm)) / int(48000/c.fs)
		buf := make([]byte, 1275)
		var enc entcode.Encoder
		pos := 0
		run := func() {
			x := pcm[pos*c.ch : (pos+fsz)*c.ch]
			pos = (pos + fsz) % (len(pcm)/c.ch - fsz)
			if c.ext {
				enc.Init(buf[:400])
				enc.EncBits(0x5a, 8)
				if ret := e.EncodeWithEC(x, fsz, nil, 400, &enc); ret <= 0 {
					t.Fatalf("%s: %d", c.name, ret)
				}
			} else if ret := e.EncodeWithEC(x, fsz, buf, 400, nil); ret <= 0 {
				t.Fatalf("%s: %d", c.name, ret)
			}
		}
		run()
		if n := testing.AllocsPerRun(100, run); n != 0 {
			t.Errorf("%s: %v allocations per frame", c.name, n)
		}
	}
}

func BenchmarkEncoder(b *testing.B) {
	pcm := make([]float32, 2*960)
	for i := range pcm {
		pcm[i] = float32(int32(uint32(i)*2654435761)>>16) * (1.0 / 65536)
	}
	e, _ := NewEncoder(48000, 2)
	e.SetSignalling(0)
	e.SetVBR(1)
	e.SetBitrate(128000)
	buf := make([]byte, 1275)
	b.ReportAllocs()
	for b.Loop() {
		e.EncodeWithEC(pcm, 960, buf, 1275, nil)
	}
}

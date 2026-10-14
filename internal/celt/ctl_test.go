package celt

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestDecoderCTL(t *testing.T) {
	if _, ret := NewDecoder(44100, 2); ret != BadArg {
		t.Errorf("44.1 kHz: ret %d, want BadArg", ret)
	}
	if _, ret := NewDecoder(48000, 3); ret != BadArg {
		t.Errorf("3 channels: ret %d, want BadArg", ret)
	}
	d, ret := NewDecoder(16000, 2)
	if ret != OK {
		t.Fatal(ret)
	}
	if d.Lookahead() != 120/3 {
		t.Errorf("lookahead %d", d.Lookahead())
	}
	if d.PhaseInversionDisabled() != 0 {
		t.Error("stereo decoders allow phase inversion")
	}
	for _, c := range []struct {
		name string
		got  int
		want int
	}{
		{"complexity -1", d.SetComplexity(-1), BadArg},
		{"complexity 11", d.SetComplexity(11), BadArg},
		{"complexity 7", d.SetComplexity(7), OK},
		{"start 21", d.SetStartBand(21), BadArg},
		{"start 17", d.SetStartBand(17), OK},
		{"end 0", d.SetEndBand(0), BadArg},
		{"end 22", d.SetEndBand(22), BadArg},
		{"end 19", d.SetEndBand(19), OK},
		{"channels 3", d.SetChannels(3), BadArg},
		{"channels 1", d.SetChannels(1), OK},
		{"inv 2", d.SetPhaseInversionDisabled(2), BadArg},
		{"inv 1", d.SetPhaseInversionDisabled(1), OK},
	} {
		if c.got != c.want {
			t.Errorf("%s: %d, want %d", c.name, c.got, c.want)
		}
	}
	if d.Complexity() != 7 || d.StartBand() != 17 || d.EndBand() != 19 || d.StreamChannels() != 1 ||
		d.Channels() != 2 || d.PhaseInversionDisabled() != 1 || d.Mode() != StaticMode() {
		t.Error("ctl values not kept")
	}
	pcm := make([]float32, 2*320)
	if got := d.DecodeWithEC(nil, pcm, 100, nil, false); got != BadArg {
		t.Errorf("bad frame size: %d", got)
	}
	if got := d.DecodeWithEC(make([]byte, 1276), pcm, 320, nil, false); got != BadArg {
		t.Errorf("oversized packet: %d", got)
	}
	if got := d.DecodeWithEC(nil, pcm, 320, nil, false); got != 320 {
		t.Errorf("lost frame: %d", got)
	}
	if d.Pitch() < CombFilterMinPeriod {
		t.Errorf("pitch %d", d.Pitch())
	}
	if d.GetAndClearError() != 0 {
		t.Error("error flag set")
	}
	d.Reset()
	if d.FinalRange() != 0 {
		t.Error("reset keeps the range")
	}
	if m, ret := CustomModeCreate(48000, 240); ret != OK || m != StaticMode() {
		t.Error("CustomModeCreate(48000, 240)")
	}
	if _, ret := CustomModeCreate(48000, 100); ret != BadArg {
		t.Error("CustomModeCreate(48000, 100)")
	}
	for _, x := range pcm {
		if math.IsNaN(float64(x)) {
			t.Fatal("NaN output")
		}
	}
}

// TestDecoderAllocs checks that decoding does not allocate.
func TestDecoderAllocs(t *testing.T) {
	f := readPacketFile(t, "full_s240.pk")
	d, _ := NewDecoder(48000, 2)
	d.SetComplexity(5)
	pcm := make([]float32, 2*960)
	i := 0
	allocs := testing.AllocsPerRun(200, func() {
		pkt := f[i%len(f)]
		if i%5 == 3 {
			pkt = nil
		}
		d.DecodeWithEC(pkt, pcm, 240, nil, false)
		i++
	})
	if allocs != 0 {
		t.Errorf("%v allocations per frame", allocs)
	}
}

func readPacketFile(t *testing.T, name string) [][]byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for p := 0; p < len(b); {
		n := int(b[p])<<8 | int(b[p+1])
		out = append(out, b[p+2:p+2+n])
		p += 2 + n
	}
	return out
}

package celt

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// runPacketFile mirrors run_pk of the C driver: it decodes a file of
// packets from the reference CELT encoder (2-byte big-endian length, then
// the packet).
func runPacketFile(t *testing.T, g golden, keep bool) (*fnvHash, int) {
	f, err := os.ReadFile(filepath.Join("testdata", g.params["file"]))
	if err != nil {
		t.Fatal(err)
	}
	CC := g.int(t, "ch")
	fs := g.int(t, "fs")
	d, ret := NewDecoder(int32(fs), CC)
	if ret != OK {
		t.Fatalf("init: %d", ret)
	}
	d.SetSignalling(0)
	if d.SetStartBand(g.int(t, "start")) != OK || d.SetChannels(g.int(t, "sc")) != OK {
		t.Fatal("ctl")
	}
	if d.SetComplexity(g.int(t, "cx")) != OK {
		t.Fatal("complexity")
	}
	pattern := g.int(t, "loss")
	fsz := g.int(t, "fsz") * fs / 48000
	h := newFNV(keep)
	pcm := make([]float32, 2*960)
	idx, frames := 0, 0
	for p := 0; p < len(f); {
		n := int(f[p])<<8 | int(f[p+1])
		pkt := f[p+2 : p+2+n]
		p += 2 + n
		idx++
		if lost(pattern, idx) {
			pkt = nil
		}
		decodeFrame(t, d, h, pcm, pkt, fsz, false)
		frames++
	}
	return h, frames
}

// runRandom mirrors run_rnd of the C driver.
func runRandom(t *testing.T, g golden, keep bool) (*fnvHash, int) {
	seed := uint32(g.int(t, "seed"))
	n := g.int(t, "n")
	CC := g.int(t, "ch")
	fs := g.int(t, "fs")
	d, ret := NewDecoder(int32(fs), CC)
	if ret != OK {
		t.Fatalf("init: %d", ret)
	}
	d.SetSignalling(0)
	h := newFNV(keep)
	pcm := make([]float32, 2*960)
	buf := make([]byte, 1275)
	for i := 0; i < n; i++ {
		seed = lcg(seed)
		length := int((seed >> 8) % 400)
		seed = lcg(seed)
		LM := int((seed >> 8) % 4)
		seed = lcg(seed)
		sc := 1 + int((seed>>8)&1)
		seed = lcg(seed)
		start := 0
		if (seed>>8)%4 == 0 {
			start = 17
		}
		seed = lcg(seed)
		var end int
		if start != 0 {
			end = endBands[2+(seed>>8)&1]
		} else {
			end = endBands[(seed>>8)&3]
		}
		for j := 0; j < length; j++ {
			seed = lcg(seed)
			buf[j] = byte(seed >> 24)
		}
		d.SetStartBand(start)
		d.SetEndBand(end)
		d.SetChannels(sc)
		var data []byte
		if length >= 2 {
			data = buf[:length]
		}
		decodeFrame(t, d, h, pcm, data, (120<<uint(LM))*fs/48000, false)
		h.i32(int32(d.GetAndClearError()))
		if (seed>>8)%50 == 0 {
			d.Reset()
		}
	}
	return h, n
}

// TestSynthetic compares the decoder with the scalar reference build on
// packets from the reference CELT encoder (including start band 17, as in
// hybrid mode) and on random packets (testdata/synth_golden.txt).
func TestSynthetic(t *testing.T) {
	keep := os.Getenv("CELT_DUMP_REF") != ""
	for i, g := range readGoldens(t, "synth_golden.txt") {
		name := fmt.Sprintf("%02d_%s", i, g.kind)
		if g.kind == "pk" {
			name += "_" + g.params["file"]
		}
		t.Run(name, func(t *testing.T) {
			var h *fnvHash
			var frames int
			switch g.kind {
			case "pk":
				h, frames = runPacketFile(t, g, keep)
			case "rnd":
				h, frames = runRandom(t, g, keep)
			default:
				t.Skip("unknown kind")
			}
			if frames != g.int(t, "frames") {
				t.Errorf("frames = %d, want %s", frames, g.params["frames"])
			}
			if got := hexHash(h.h); got != g.params["hash"] {
				t.Errorf("hash = %s, want %s", got, g.params["hash"])
				compareDump(t, h.dump)
			}
		})
	}
}

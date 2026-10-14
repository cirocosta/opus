package celt

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// decodeFrame mirrors decode_frame of the C driver.
func decodeFrame(
	_ *testing.T,
	d *Decoder,
	h *fnvHash,
	pcm []float32,
	data []byte,
	frameSize int,
	accum bool,
) {
	CC := d.Channels()
	if accum {
		for i := 0; i < frameSize*CC; i++ {
			pcm[i] = float32((i*7)%13-6) * (1.0 / 1024)
		}
	}
	ret := d.DecodeWithEC(data, pcm, frameSize, nil, accum)
	h.i32(int32(ret))
	h.u32(d.FinalRange())
	if ret > 0 {
		h.f32s(pcm[:ret*CC])
	}
}

var endBands = [4]int{13, 17, 19, 21}

// runVector mirrors run_vec of the C driver. It returns the hash, the
// number of decoded frames, and the number of packets whose final range
// matched the vector (when there is no loss at 48 kHz).
func runVector(
	t *testing.T,
	path string,
	CC int,
	fs int32,
	pattern int,
	accum bool,
	complexity int,
	keep bool,
) (*fnvHash, int, int, int) {
	f, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, ret := NewDecoder(fs, CC)
	if ret != OK {
		t.Fatalf("init: %d", ret)
	}
	d.SetSignalling(0)
	ds := int(48000 / fs)
	if d.SetComplexity(complexity) != OK {
		t.Fatal("complexity")
	}
	h := newFNV(keep)
	pcm := make([]float32, 2*960*6)
	idx, frames, rngOK, rngTot := 0, 0, 0, 0
	for p := 0; p < len(f); {
		n := int(binary.BigEndian.Uint32(f[p:]))
		frng := binary.BigEndian.Uint32(f[p+4:])
		pkt := f[p+8 : p+8+n]
		p += 8 + n
		idx++
		if n == 0 {
			continue
		}
		toc := pkt[0]
		if toc>>3 < 16 {
			continue
		}
		frs, err := parsePacket(pkt)
		if err != nil {
			continue
		}
		fsz := (120 << (toc >> 3 & 3)) / ds
		d.SetEndBand(endBands[(toc>>5)&3])
		if toc&4 != 0 {
			d.SetChannels(2)
		} else {
			d.SetChannels(1)
		}
		if lost(pattern, idx) {
			for range frs {
				decodeFrame(t, d, h, pcm, nil, fsz, accum)
				frames++
			}
			continue
		}
		for _, fr := range frs {
			decodeFrame(t, d, h, pcm, fr, fsz, accum)
			frames++
		}
		if pattern == 0 && fs == 48000 {
			rngTot++
			if d.FinalRange() == frng {
				rngOK++
			}
		}
	}
	return h, frames, rngOK, rngTot
}

func vectorDir(t *testing.T) string {
	dir := os.Getenv("OPUS_TESTVECTORS")
	if dir == "" {
		t.Skip("OPUS_TESTVECTORS is not set")
	}
	return dir
}

// TestVectors decodes the CELT-only frames of the RFC 8251 test vectors
// and compares a hash of the float output and final ranges with the
// scalar reference build (see testdata/vectors_golden.txt).
func TestVectors(t *testing.T) {
	dir := vectorDir(t)
	keep := os.Getenv("CELT_DUMP_REF") != ""
	only := os.Getenv("CELT_ONLY")
	for _, g := range readGoldens(t, "vectors_golden.txt") {
		if g.kind != "vec" {
			continue
		}
		name := fmt.Sprintf(
			"v%s_ch%s_fs%s_loss%s_acc%s_cx%s",
			g.params["v"],
			g.params["ch"],
			g.params["fs"],
			g.params["loss"],
			g.params["accum"],
			g.params["cx"],
		)
		if only != "" && only != name {
			continue
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "testvector"+g.params["v"]+".bit")
			h, frames, ok, tot := runVector(
				t,
				path,
				g.int(t, "ch"),
				int32(g.int(t, "fs")),
				g.int(t, "loss"),
				g.int(t, "accum") != 0,
				g.int(t, "cx"),
				keep,
			)
			if ok != tot {
				t.Errorf("final range matched %d/%d packets", ok, tot)
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

func TestRotationRecurrence(t *testing.T) {
	rng := rand.New(rand.NewPCG(977, 431))
	coefficients := [][2]float32{{1, 0}, {0, 1}, {.83, .25}, {.25, -.83}, {-.5, .75}}
	for length := 0; length <= maxBandSize; length++ {
		for _, pair := range coefficients {
			got, want := make([]float32, length), make([]float32, length)
			for i := range got {
				got[i] = math.Float32frombits(rng.Uint32())
			}
			copy(want, got)
			expRotation1(got, length, 1, pair[0], pair[1])
			expRotation1Generic(want, length, 1, pair[0], pair[1])
			for i := range got {
				// NaN payload selection can differ when the compiler commutes operands.
				if math.IsNaN(float64(got[i])) && math.IsNaN(float64(want[i])) {
					continue
				}
				if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
					t.Fatalf(
						"length %d, coefficients %v, sample %d: got %08x, want %08x",
						length,
						pair,
						i,
						math.Float32bits(got[i]),
						math.Float32bits(want[i]),
					)
				}
			}
		}
	}
}

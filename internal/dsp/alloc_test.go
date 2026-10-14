package dsp

import "testing"

// TestNoAllocs checks that the hot paths do not allocate for the sizes of
// the static 48 kHz mode.
func TestNoAllocs(t *testing.T) {
	m := &Mode48000_960_120
	in := make([]float32, 2048)
	out := make([]float32, 2048)
	cin := make([]KissFFTCpx, 480)
	cout := make([]KissFFTCpx, 480)
	x := make([]float32, 2100)
	y := make([]float32, 2100)
	ac := make([]float32, 25)
	lpc := make([]float32, 24)
	mem := make([]float32, 24)
	sig := [][]float32{make([]float32, 1984), make([]float32, 1984)}
	xlp := make([]float32, 992)
	r := &rng{s: 1}
	for i := range sig[0] {
		sig[0][i] = float32(1000 * r.frand())
		sig[1][i] = float32(1000 * r.frand())
	}
	for i := range x {
		x[i] = r.frand()
	}
	cases := map[string]func(){
		"OpusFFT":        func() { OpusFFT(m.MDCT.KFFT[0], cin, cout) },
		"OpusIFFT":       func() { OpusIFFT(m.MDCT.KFFT[0], cin, cout) },
		"CltMDCTForward": func() { CltMDCTForward(m.MDCT, in, out, m.Window, 120, 0, 1) },
		"CltMDCTBackward": func() {
			CltMDCTBackward(m.MDCT, in, out, m.Window, 120, 0, 1)
		},
		"CeltAutocorr": func() { CeltAutocorr(x, ac, m.Window, 120, 24, 1024) },
		"CeltLPC":      func() { CeltLPC(lpc, ac, 24) },
		"CeltFIR":      func() { CeltFIR(x, lpc, y, 1024, 24) },
		"CeltIIR":      func() { CeltIIR(x, lpc, y, 1024, 24, mem) },
		"PitchDownsample": func() {
			PitchDownsample(sig, xlp, 992, 2, 2)
		},
		"PitchSearch": func() { PitchSearch(xlp[512:], xlp, 960, 979) },
		"RemoveDoubling": func() {
			t0 := 500
			RemoveDoubling(xlp, 1024, 15, 960, &t0, 100, .5)
		},
	}
	for name, f := range cases {
		if n := testing.AllocsPerRun(10, f); n != 0 {
			t.Errorf("%s: %v allocations per run", name, n)
		}
	}
}

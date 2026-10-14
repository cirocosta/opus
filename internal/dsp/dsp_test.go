package dsp

// The golden files in testdata come from a C driver (tools/oracle/dsp)
// linked against the scalar libopus 1.6.1 build (-O2 -ffp-contract=off).
// Each file is a sequence of little-endian 32-bit words. The tests
// regenerate the same inputs with the same xorshift32 generator.
//
// Set DSP_GOLDEN_DIR to a directory with larger math_*.bin sweeps to run
// the math tests on those instead.

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

type rng struct{ s uint32 }

func (r *rng) next() uint32 {
	x := r.s
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	r.s = x
	return x
}

// frand matches (float)(int32_t)rng() * (1.0f/2147483648.0f).
func (r *rng) frand() float32 {
	return float32(float32(int32(r.next())) * (1.0 / 2147483648.0))
}

type golden struct {
	t    *testing.T
	name string
	w    []uint32
	pos  int
}

func loadGolden(t *testing.T, dir, name string) *golden {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if len(b)%4 != 0 {
		t.Fatalf("%s: size %d is not a multiple of 4", name, len(b))
	}
	w := make([]uint32, len(b)/4)
	for i := range w {
		w[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return &golden{t: t, name: name, w: w}
}

func (g *golden) word() uint32 {
	if g.pos >= len(g.w) {
		g.t.Fatalf("%s: read past end", g.name)
	}
	v := g.w[g.pos]
	g.pos++
	return v
}

func (g *golden) f() float32 { return math.Float32frombits(g.word()) }
func (g *golden) i() int32   { return int32(g.word()) }

func (g *golden) done() {
	if g.pos != len(g.w) {
		g.t.Fatalf("%s: %d words left unread", g.name, len(g.w)-g.pos)
	}
}

// ulpDiff returns the distance in units of least precision between a and
// b. NaNs compare equal to NaNs only.
func ulpDiff(a, b float32) int64 {
	ord := func(x float32) int64 {
		u := math.Float32bits(x)
		if u&0x80000000 != 0 {
			return -int64(u & 0x7fffffff)
		}
		return int64(u)
	}
	if a != a || b != b {
		if a != a && b != b {
			return 0
		}
		return math.MaxInt32
	}
	d := ord(a) - ord(b)
	if d < 0 {
		d = -d
	}
	return d
}

// stats tracks the differences between Go and C outputs.
type stats struct {
	name    string
	n       int
	nDiff   int
	maxULP  int64
	maxAbs  float64
	firstAt int
}

func (s *stats) add(got, want float32) {
	if math.Float32bits(got) != math.Float32bits(want) {
		if s.nDiff == 0 {
			s.firstAt = s.n
		}
		s.nDiff++
		if u := ulpDiff(got, want); u > s.maxULP {
			s.maxULP = u
		}
		if d := math.Abs(float64(got) - float64(want)); d > s.maxAbs {
			s.maxAbs = d
		}
	}
	s.n++
}

// check fails the test when there are more than maxULP differences.
// maxULP 0 asks for bit-exact output.
func (s *stats) check(t *testing.T, maxULP int64) {
	t.Helper()
	if s.nDiff == 0 {
		t.Logf("%s: %d values, bit-exact", s.name, s.n)
		return
	}
	msg := "%s: %d/%d values differ (first at %d), max %d ulp, max abs %g"
	if s.maxULP > maxULP {
		t.Errorf(msg, s.name, s.nDiff, s.n, s.firstAt, s.maxULP, s.maxAbs)
		return
	}
	t.Logf(msg, s.name, s.nDiff, s.n, s.firstAt, s.maxULP, s.maxAbs)
}

func eqI(t *testing.T, what string, got, want int32) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %d, want %d", what, got, want)
	}
}

func TestStaticTables(t *testing.T) {
	g := loadGolden(t, "testdata", "tables.bin")
	m := &Mode48000_960_120
	eqI(t, "Fs", m.Fs, g.i())
	eqI(t, "overlap", int32(m.Overlap), g.i())
	eqI(t, "nbEBands", int32(m.NbEBands), g.i())
	eqI(t, "effEBands", int32(m.EffEBands), g.i())
	s := &stats{name: "preemph+window+twiddles+trig"}
	for i := 0; i < 4; i++ {
		s.add(m.Preemph[i], g.f())
	}
	eqI(t, "maxLM", int32(m.MaxLM), g.i())
	eqI(t, "nbShortMdcts", int32(m.NbShortMdcts), g.i())
	eqI(t, "shortMdctSize", int32(m.ShortMdctSize), g.i())
	eqI(t, "nbAllocVectors", int32(m.NbAllocVectors), g.i())
	if len(m.Window) != 120 {
		t.Fatalf("window length %d", len(m.Window))
	}
	for _, v := range m.Window {
		s.add(v, g.f())
	}
	for i := 0; i < m.NbEBands; i++ {
		eqI(t, "logN", int32(m.LogN[i]), g.i())
	}
	eqI(t, "cache size", int32(m.CacheSize), g.i())
	for i := 0; i < 105; i++ {
		eqI(t, "cache index", int32(m.CacheIndex[i]), g.i())
	}
	for i := 0; i < m.CacheSize; i++ {
		eqI(t, "cache bits", int32(m.CacheBits[i]), g.i())
	}
	for i := 0; i < 168; i++ {
		eqI(t, "cache caps", int32(m.CacheCaps[i]), g.i())
	}
	l := m.MDCT
	eqI(t, "mdct n", int32(l.N), g.i())
	eqI(t, "mdct maxshift", int32(l.MaxShift), g.i())
	for sh := 0; sh < 4; sh++ {
		st := l.KFFT[sh]
		eqI(t, "nfft", int32(st.Nfft), g.i())
		s.add(st.Scale, g.f())
		eqI(t, "shift", int32(st.Shift), g.i())
		for i := 0; i < 16; i++ {
			eqI(t, "factors", int32(st.Factors[i]), g.i())
		}
		if len(st.Bitrev) != st.Nfft {
			t.Fatalf("bitrev length %d", len(st.Bitrev))
		}
		for i := 0; i < st.Nfft; i++ {
			eqI(t, "bitrev", int32(st.Bitrev[i]), g.i())
		}
	}
	tw := l.KFFT[0].Twiddles
	if len(tw) != 480 {
		t.Fatalf("twiddles length %d", len(tw))
	}
	for _, c := range tw {
		s.add(c.R, g.f())
		s.add(c.I, g.f())
	}
	if len(l.Trig) != 1800 {
		t.Fatalf("trig length %d", len(l.Trig))
	}
	for _, v := range l.Trig {
		s.add(v, g.f())
	}
	g.done()
	s.check(t, 0)
}

func TestFFT(t *testing.T) {
	g := loadGolden(t, "testdata", "fft.bin")
	r := &rng{s: 12345}
	for sh := 0; sh < 4; sh++ {
		st := MDCT48000_960.KFFT[sh]
		in := make([]KissFFTCpx, st.Nfft)
		out := make([]KissFFTCpx, st.Nfft)
		for i := range in {
			in[i].R = float32(1000 * r.frand())
			in[i].I = float32(1000 * r.frand())
		}
		s := &stats{name: "opus_fft nfft=" + strconv.Itoa(st.Nfft)}
		OpusFFT(st, in, out)
		for _, c := range out {
			s.add(c.R, g.f())
			s.add(c.I, g.f())
		}
		s.check(t, 0)
		s = &stats{name: "opus_ifft nfft=" + strconv.Itoa(st.Nfft)}
		OpusIFFT(st, in, out)
		for _, c := range out {
			s.add(c.R, g.f())
			s.add(c.I, g.f())
		}
		s.check(t, 0)
	}
	g.done()
}

func TestMDCT(t *testing.T) {
	g := loadGolden(t, "testdata", "mdct.bin")
	r := &rng{s: 777}
	cases := [][2]int{{0, 1}, {1, 1}, {2, 1}, {3, 1}, {3, 8}, {2, 4}, {1, 2}}
	m := &Mode48000_960_120
	ov := m.Overlap
	for _, c := range cases {
		shift, stride := c[0], c[1]
		n := m.MDCT.N >> shift
		n2 := n >> 1
		in := make([]float32, 4000)
		out := make([]float32, 4000)
		for i := 0; i < n2+ov; i++ {
			in[i] = float32(20000 * r.frand())
		}
		CltMDCTForward(m.MDCT, in, out, m.Window, ov, shift, stride)
		s := &stats{name: "clt_mdct_forward shift=" + strconv.Itoa(shift) + " stride=" + strconv.Itoa(stride)}
		for i := 0; i < n2*stride; i++ {
			s.add(out[i], g.f())
		}
		s.check(t, 0)
		for i := 0; i < n2*stride; i++ {
			in[i] = float32(20000 * r.frand())
		}
		for i := 0; i < n2+ov; i++ {
			out[i] = float32(20000 * r.frand())
		}
		CltMDCTBackward(m.MDCT, in, out, m.Window, ov, shift, stride)
		s = &stats{name: "clt_mdct_backward shift=" + strconv.Itoa(shift) + " stride=" + strconv.Itoa(stride)}
		for i := 0; i < n2+ov; i++ {
			s.add(out[i], g.f())
		}
		s.check(t, 0)
	}
	g.done()
}

func TestLPC(t *testing.T) {
	g := loadGolden(t, "testdata", "lpc.bin")
	r := &rng{s: 4242}
	x := make([]float32, 2100)
	ac := make([]float32, 30)
	lpc := make([]float32, 30)
	y := make([]float32, 2100)
	num := make([]float32, 24)
	mem := make([]float32, 24)
	win := Mode48000_960_120.Window
	for tc := 0; tc < 3; tc++ {
		n, lag, ov := 1024, 24, 120
		if tc == 2 {
			n, lag, ov = 992, 4, 0
		}
		var a float32
		for i := 0; i < n; i++ {
			a = float32(.9*a) + r.frand()
			x[i] = float32(5000*a) + float32(100*r.frand())
		}
		sh := CeltAutocorr(x, ac, win, ov, lag, n)
		eqI(t, "autocorr shift", int32(sh), g.i())
		s := &stats{name: "_celt_autocorr n=" + strconv.Itoa(n) + " lag=" + strconv.Itoa(lag) + " overlap=" + strconv.Itoa(ov)}
		for i := 0; i <= lag; i++ {
			s.add(ac[i], g.f())
		}
		s.check(t, 0)
		CeltLPC(lpc, ac, lag)
		s = &stats{name: "_celt_lpc p=" + strconv.Itoa(lag)}
		for i := 0; i < lag; i++ {
			s.add(lpc[i], g.f())
		}
		s.check(t, 0)
	}
	clear(ac)
	CeltLPC(lpc, ac, 24)
	s := &stats{name: "_celt_lpc zero ac"}
	for i := 0; i < 24; i++ {
		s.add(lpc[i], g.f())
	}
	s.check(t, 0)

	for tc := 0; tc < 2; tc++ {
		n, ord := 200, 24
		if tc == 1 {
			n = 203
		}
		for i := 0; i < n+ord; i++ {
			x[i] = float32(1000 * r.frand())
		}
		for i := 0; i < ord; i++ {
			num[i] = float32(.1 * r.frand())
		}
		CeltFIR(x, num, y, n, ord)
		s := &stats{name: "celt_fir n=" + strconv.Itoa(n)}
		for i := 0; i < n; i++ {
			s.add(y[i], g.f())
		}
		s.check(t, 0)
	}

	for tc := 0; tc < 2; tc++ {
		n, ord := 200, 24
		if tc == 1 {
			n = 203
		}
		for i := 0; i < n; i++ {
			x[i] = float32(1000 * r.frand())
		}
		for i := 0; i < ord; i++ {
			num[i] = float32(.05 * r.frand())
		}
		for i := 0; i < ord; i++ {
			mem[i] = float32(100 * r.frand())
		}
		// Run once out of place and once in place.
		mem2 := append([]float32(nil), mem...)
		xin := append([]float32(nil), x[:n]...)
		CeltIIR(x, num, y, n, ord, mem)
		CeltIIR(xin, num, xin, n, ord, mem2)
		s := &stats{name: "celt_iir n=" + strconv.Itoa(n)}
		s2 := &stats{name: "celt_iir in place n=" + strconv.Itoa(n)}
		for i := 0; i < n; i++ {
			w := g.f()
			s.add(y[i], w)
			s2.add(xin[i], w)
		}
		for i := 0; i < ord; i++ {
			w := g.f()
			s.add(mem[i], w)
			s2.add(mem2[i], w)
		}
		s.check(t, 0)
		s2.check(t, 0)
	}

	for i := 0; i < 100; i++ {
		x[i] = r.frand()
	}
	xc := make([]float32, 40)
	s = &stats{name: "celt_pitch_xcorr, dual_inner_prod, celt_inner_prod"}
	for tc := 3; tc <= 8; tc++ {
		CeltPitchXcorr(x, x[50:], xc, 30+tc, 13)
		for i := 0; i < 13; i++ {
			s.add(xc[i], g.f())
		}
	}
	a, b := DualInnerProd(x, x[10:], x[20:], 37)
	s.add(a, g.f())
	s.add(b, g.f())
	s.add(CeltInnerProd(x, x[33:], 41), g.f())
	s.check(t, 0)
	g.done()
}

func TestPitch(t *testing.T) {
	g := loadGolden(t, "testdata", "pitch.bin")
	r := &rng{s: 99}
	var sig [2][]float32
	sig[0] = make([]float32, 1984)
	sig[1] = make([]float32, 1984)
	xlp := make([]float32, 992)
	for tc := 0; tc < 12; tc++ {
		c := 1 + tc&1
		period := 20 + float32(37.3*float32(tc))
		prevPeriod := tc*50 + 15
		prevGain := float32(.1 * float32(tc%8))
		noise := float32(1)
		if tc >= 10 {
			noise = 5
		}
		for ch := 0; ch < c; ch++ {
			for i := 0; i < 1984; i++ {
				p := float32(i+7*ch) / period
				p -= float32(int(p))
				v := float32(p*p) - p + .1666
				sig[ch][i] = float32(48000*v) + float32(float32(3000*r.frand())*noise)
			}
		}
		PitchDownsample(sig[:], xlp, 992, c, 2)
		s := &stats{name: "pitch_downsample C=" + strconv.Itoa(c) + " case " + strconv.Itoa(tc)}
		for i := range xlp {
			s.add(xlp[i], g.f())
		}
		s.check(t, 0)
		pitch := PitchSearch(xlp[512:], xlp, 960, 1024-3*15)
		eqI(t, "pitch_search", int32(pitch), g.i())
		pitch = 1024 - pitch
		gain := RemoveDoubling(xlp, 1024, 15, 960, &pitch, prevPeriod, prevGain)
		eqI(t, "remove_doubling T0", int32(pitch), g.i())
		s = &stats{name: "remove_doubling gain case " + strconv.Itoa(tc)}
		s.add(gain, g.f())
		s.check(t, 0)
	}
	g.done()
}

func goldenDir() string {
	if d := os.Getenv("DSP_GOLDEN_DIR"); d != "" {
		return d
	}
	return "testdata"
}

func TestMathApprox(t *testing.T) {
	testMath(t, "math_approx.bin", CeltLog2Approx, CeltExp2Approx, 0)
}

// TestMathLibm allows 1 ulp: Go's math.Log, math.Exp and math.Cos are not
// glibc's, and they may round differently before the float conversion.
func TestMathLibm(t *testing.T) {
	testMath(t, "math_libm.bin", CeltLog2Libm, CeltExp2Libm, 1)
}

func testMath(t *testing.T, file string, log2, exp2 func(float32) float32, libmULP int64) {
	g := loadGolden(t, goldenDir(), file)
	n := int(g.i())
	ln2 := float32(0.6931471805599453)
	log2e := float32(1.4426950408889634)
	sl, sln := &stats{name: "celt_log2"}, &stats{name: "celt_log"}
	for i := 0; i < n; i++ {
		x := g.f()
		sl.add(log2(x), g.f())
		sln.add(float32(log2(x)*ln2), g.f())
	}
	se, sex := &stats{name: "celt_exp2"}, &stats{name: "celt_exp"}
	for i := 0; i < n; i++ {
		x := g.f()
		se.add(exp2(x), g.f())
		sex.add(exp2(float32(float32(.5*x)*log2e)), g.f())
	}
	sc, sc2, ss := &stats{name: "celt_cos_norm"}, &stats{name: "celt_cos_norm2"}, &stats{name: "celt_sin"}
	for i := 0; i < n; i++ {
		x := g.f()
		sc.add(CeltCosNorm(x), g.f())
		sc2.add(CeltCosNorm2(x), g.f())
		ss.add(CeltSin(x), g.f())
	}
	sa, sap, sf := &stats{name: "celt_atan_norm"}, &stats{name: "celt_atan2p_norm"}, &stats{name: "fast_atan2f"}
	for i := 0; i < n; i++ {
		x, y, a, b := g.f(), g.f(), g.f(), g.f()
		sa.add(CeltAtanNorm(x), g.f())
		sap.add(CeltAtan2pNorm(y, x), g.f())
		sap.add(CeltAtan2pNorm(x, y), g.f())
		sf.add(FastAtan2f(a, b), g.f())
	}
	sq := &stats{name: "celt_sqrt, celt_rsqrt, celt_rcp, frac_div32"}
	for i := 0; i < n; i++ {
		x := g.f()
		sq.add(CeltSqrt(x), g.f())
		sq.add(CeltRsqrt(x), g.f())
		sq.add(CeltRcp(x), g.f())
		sq.add(FracDiv32(1.5, x), g.f())
	}
	for i := 0; i < n; i++ {
		x := g.f()
		eqI(t, "float2int", Float2Int(32768*x), g.i())
		eqI(t, "FLOAT2INT16", int32(Float2Int16(x)), g.i())
		eqI(t, "FLOAT2INT24", Float2Int24(x), g.i())
	}
	for i := 0; i < n; i++ {
		v := uint32(g.i())
		eqI(t, "isqrt32", int32(Isqrt32(v)), g.i())
		a, b := g.i(), g.i()
		eqI(t, "FRAC_MUL16", FracMul16(a, b), g.i())
	}
	sm := &stats{name: "celt_maxabs16"}
	r := &rng{s: 31337}
	v := make([]float32, 37)
	for i := range v {
		v[i] = float32(100 * r.frand())
	}
	sm.add(CeltMaxabs16(v), g.f())
	v[5] = -1000
	sm.add(CeltMaxabs16(v), g.f())
	g.done()
	sl.check(t, libmULP)
	sln.check(t, libmULP)
	se.check(t, libmULP)
	sex.check(t, libmULP)
	sc.check(t, 1)
	sc2.check(t, 0)
	ss.check(t, 0)
	sa.check(t, 0)
	sap.check(t, 0)
	sf.check(t, 0)
	sq.check(t, 0)
	sm.check(t, 0)
}

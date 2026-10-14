package opus

// Golden tests for analysis.go. A C driver (tools/oracle/analysis/drv.c)
// links the scalar libopus 1.6.1 build and runs run_analysis with the
// same configurations over the same PCM (tools/oracle/analysis/gen.sh):
//
//	OPUS_GOLDEN_INPUTS=/some/dir go test -run TestWriteAnalysisSource .
//	./drv synth /some/dir/analysis_src.raw synth.cfg | gzip -9n > testdata/analysis.txt.gz
//	./drv vec $OPUS_TESTVECTORS vec.cfg > testdata/analysis_vectors.txt
//
// The synthetic source is generated from integers only. The golden file
// holds a "# config" line per configuration, followed by one line per
// run_analysis call. The test vector golden holds a hash of these lines
// per vector and configuration.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cirocosta/opus/internal/celt"
)

// analysisCfg is one driver configuration.
type analysisCfg struct {
	fs, c, c1, c2, dm, gain, lsb, fsz, la, nullp, resetp int
	seed                                                 uint32
	maxcalls                                             int
}

func parseAnalysisCfg(s string) (analysisCfg, error) {
	var c analysisCfg
	_, err := fmt.Sscanf(s, "%d %d %d %d %d %d %d %d %d %d %d %d %d", &c.fs, &c.c, &c.c1, &c.c2,
		&c.dm, &c.gain, &c.lsb, &c.fsz, &c.la, &c.nullp, &c.resetp, &c.seed, &c.maxcalls)
	return c, err
}

// isinQ15 is a Bhaskara approximation of 32767*sin(2*pi*phase/2^32) in
// integer arithmetic.
func isinQ15(phase uint32) int64 {
	const h = 65536
	p := int64(phase>>15) & 0xffff
	num := 16 * p * (h - p)
	den := 5*h*h - 4*p*(h-p)
	v := 32767 * num / den
	if phase>>31 != 0 {
		v = -v
	}
	return v
}

func clip16(v int64) int16 {
	return int16(max(-32768, min(32767, v)))
}

// analysisSynthSource returns 5 s of interleaved stereo 48 kHz PCM made of
// ten 0.5 s segments with different content.
func analysisSynthSource() []int16 {
	const seg = 24000
	const hz = 89478 // 2^32/48000
	out := make([]int16, 0, 2*10*seg)
	lcg := uint32(12345)
	noise := func() int64 {
		lcg = lcg*1664525 + 1013904223
		return int64(int32(lcg)) >> 16
	}
	var ph [8]uint32
	var lp [2]int64
	for s := 0; s < 10; s++ {
		for i := 0; i < seg; i++ {
			var l, r int64
			switch s {
			case 0: // digital silence
			case 1: // pure tones
				ph[0] += 440 * hz
				ph[1] += 660 * hz
				l = isinQ15(ph[0]) / 2
				r = isinQ15(ph[1]) / 2
			case 2: // chord with tremolo
				f := [4]uint32{220, 277, 330, 440}
				var sum int64
				for k := range f {
					ph[k] += f[k] * hz
					sum += isinQ15(ph[k]) + isinQ15(2*ph[k])/3
				}
				ph[4] += 3 * hz
				env := 20000 + isinQ15(ph[4])/3
				l = sum * env / (8 * 32768)
				r = l/2 + noise()/64
			case 3: // white noise
				l = noise() / 4
				r = noise() / 4
			case 4: // speech-like: gated harmonic pulse train
				ph[5] += 150 * hz
				ph[6] += 4 * hz
				saw := int64(int32(ph[5])) >> 18
				env := max(0, isinQ15(ph[6]))
				l = saw*env/32768 + noise()/256
				r = l
			case 5: // LSB-level noise
				l = noise() >> 14
				r = noise() >> 14
			case 6: // low-passed noise
				lp[0] += (noise() - lp[0]) >> 4
				lp[1] += (noise() - lp[1]) >> 6
				l = lp[0]
				r = lp[1] * 2
			case 7: // chirp from 100 Hz to about 15 kHz
				ph[0] += uint32((100 + int64(i)*15000/seg) * hz)
				l = isinQ15(ph[0]) / 3
				r = l / 2
			case 8: // near-clipping square wave, R = -L
				ph[1] += 1000 * hz
				l = 32000
				if ph[1]>>31 != 0 {
					l = -32000
				}
				r = -l
			case 9: // 18 kHz tone over a 100 Hz tone
				ph[2] += 18000 * hz
				ph[3] += 100 * hz
				l = isinQ15(ph[2])/4 + isinQ15(ph[3])/4
				r = isinQ15(ph[2]) / 8
			}
			out = append(out, clip16(l), clip16(r))
		}
	}
	return out
}

// analysisRunner holds the state and buffers of one configuration run.
type analysisRunner struct {
	st   tonalityAnalysisState
	b16  []int16
	bf   []float32
	b24  []int32
	n    int
	cfg  analysisCfg
	line []byte
}

func newAnalysisRunner(src []int16, c analysisCfg) *analysisRunner {
	dec := 48000 / c.fs
	n := len(src) / 2 / dec
	r := &analysisRunner{n: n, cfg: c,
		b16: make([]int16, n*c.c), bf: make([]float32, n*c.c), b24: make([]int32, n*c.c)}
	for i := 0; i < n; i++ {
		l, rr := int(src[2*i*dec]), int(src[2*i*dec+1])
		ch := [3]int{l, rr, (l - rr) >> 1}
		if c.c == 1 {
			ch[0] = (l + rr) >> 1
		}
		for k := 0; k < c.c; k++ {
			v := ch[k]
			idx := i*c.c + k
			r.b16[idx] = int16(v)
			r.bf[idx] = float32(v*c.gain) / 32768
			r.b24[idx] = int32(v*256 + int((uint32(idx)*2654435761)>>24&0xff))
		}
	}
	return r
}

func appendAnalysisInfo(b []byte, a *analysisInfo) []byte {
	f := math.Float32bits
	b = fmt.Appendf(b, "%d %08x %08x %08x %08x %08x %08x %08x %d %08x %08x ",
		a.Valid, f(a.Tonality), f(a.TonalitySlope), f(a.Noisiness), f(a.Activity),
		f(a.MusicProb), f(a.MusicProbMin), f(a.MusicProbMax), a.Bandwidth,
		f(a.ActivityProbability), f(a.MaxPitchRatio))
	for _, v := range a.LeakBoost {
		b = fmt.Appendf(b, "%02x", v)
	}
	return b
}

// run runs the configuration and calls emit with each formatted line.
func (r *analysisRunner) run(emit func(line []byte)) int {
	c := r.cfg
	mode := celt.StaticMode()
	tonalityAnalysisInit(&r.st, int32(c.fs))
	rng := c.seed
	sizes := [6]int{1, 2, 4, 8, 16, 24}
	pos, call := 0, 0
	for call < c.maxcalls {
		frame := c.fsz
		if frame <= 0 {
			rng = rng*1664525 + 1013904223
			frame = sizes[(rng>>8)%6] * (c.fs / 400)
		}
		afs := frame + c.la
		if pos+afs > r.n {
			break
		}
		if c.resetp != 0 && call > 0 && call%c.resetp == 0 {
			tonalityAnalysisReset(&r.st)
		}
		var pcm analysisPCM
		var dm downmixFunc
		switch c.dm {
		case 0:
			pcm.i16, dm = r.b16[pos*c.c:], downmixInt
		case 1:
			pcm.f32, dm = r.bf[pos*c.c:], downmixFloat
		default:
			pcm.i32, dm = r.b24[pos*c.c:], downmixInt24
		}
		if c.nullp != 0 && call%c.nullp == c.nullp-1 {
			pcm = analysisPCM{}
		}
		var info analysisInfo
		runAnalysis(&r.st, mode, pcm, afs, frame, c.c1, c.c2, c.c, int32(c.fs), c.lsb, dm, &info)
		if emit != nil {
			r.line = appendAnalysisInfo(r.line[:0], &info)
			emit(r.line)
		}
		pos += frame
		call++
	}
	return call
}

// TestWriteAnalysisSource writes the synthetic source PCM for the C driver
// to the directory named by OPUS_GOLDEN_INPUTS.
func TestWriteAnalysisSource(t *testing.T) {
	dir := os.Getenv("OPUS_GOLDEN_INPUTS")
	if dir == "" {
		t.Skip("OPUS_GOLDEN_INPUTS not set")
	}
	src := analysisSynthSource()
	buf := make([]byte, 0, 2*len(src))
	for _, v := range src {
		buf = binary.LittleEndian.AppendUint16(buf, uint16(v))
	}
	if err := os.WriteFile(filepath.Join(dir, "analysis_src.raw"), buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAnalysisGolden compares every AnalysisInfo returned by runAnalysis
// over the synthetic source with libopus.
func TestAnalysisGolden(t *testing.T) {
	lines := readGoldenLines(t, "analysis")
	src := analysisSynthSource()
	ncfg := 0
	for i := 0; i < len(lines); {
		hdr, ok := strings.CutPrefix(lines[i], "# ")
		if !ok {
			t.Fatalf("line %d: expected config header, got %q", i+1, lines[i])
		}
		i++
		c, err := parseAnalysisCfg(hdr)
		if err != nil {
			t.Fatalf("config %q: %v", hdr, err)
		}
		ncfg++
		var got []string
		newAnalysisRunner(src, c).run(func(l []byte) { got = append(got, string(l)) })
		fails := 0
		for k, g := range got {
			if i+k >= len(lines) || strings.HasPrefix(lines[i+k], "# ") {
				t.Fatalf("config %q: got %d calls, golden has %d", hdr, len(got), k)
			}
			if g != lines[i+k] {
				t.Errorf("config %q call %d:\n got: %s\nwant: %s", hdr, k, g, lines[i+k])
				if fails++; fails > 3 {
					t.FailNow()
				}
			}
		}
		i += len(got)
		if i < len(lines) && !strings.HasPrefix(lines[i], "# ") {
			t.Fatalf("config %q: got %d calls, golden has more", hdr, len(got))
		}
	}
	if ncfg == 0 {
		t.Fatal("empty golden")
	}
}

// TestAnalysisVectors runs the analysis over the decoded RFC 8251 test
// vectors in $OPUS_TESTVECTORS and compares a hash of all the returned
// AnalysisInfo values with libopus.
func TestAnalysisVectors(t *testing.T) {
	dir := os.Getenv("OPUS_TESTVECTORS")
	if dir == "" {
		t.Skip("OPUS_TESTVECTORS not set")
	}
	f, err := os.Open(filepath.Join("testdata", "analysis_vectors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	srcs := map[string][]int16{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 16 {
			t.Fatalf("bad line %q", sc.Text())
		}
		name := fields[0]
		c, err := parseAnalysisCfg(strings.Join(fields[1:14], " "))
		if err != nil {
			t.Fatal(err)
		}
		src, ok := srcs[name]
		if !ok {
			data, err := os.ReadFile(filepath.Join(dir, name+".dec"))
			if err != nil {
				t.Fatal(err)
			}
			src = make([]int16, len(data)/2)
			for i := range src {
				src[i] = int16(binary.LittleEndian.Uint16(data[2*i:]))
			}
			srcs = map[string][]int16{name: src}
		}
		h := fnv.New64a()
		calls := newAnalysisRunner(src, c).run(func(l []byte) {
			h.Write(l)
			h.Write([]byte{'\n'})
		})
		got := fmt.Sprintf("%d %016x", calls, h.Sum64())
		if want := fields[14] + " " + fields[15]; got != want {
			t.Errorf("%s: got %s, want %s", sc.Text(), got, want)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestAnalysisAllocs checks that runAnalysis does not allocate.
func TestAnalysisAllocs(t *testing.T) {
	src := analysisSynthSource()
	for _, dm := range []int{0, 1, 2} {
		for _, fs := range []int{48000, 24000, 16000} {
			c := analysisCfg{fs: fs, c: 2, c1: 0, c2: -2, dm: dm, gain: 1, lsb: 16, fsz: fs / 50, maxcalls: 1}
			r := newAnalysisRunner(src, c)
			mode := celt.StaticMode()
			tonalityAnalysisInit(&r.st, int32(fs))
			pos := 0
			var info analysisInfo
			allocs := testing.AllocsPerRun(100, func() {
				pcm := analysisPCM{i16: r.b16[pos*2:]}
				dmf := downmixInt
				switch dm {
				case 1:
					pcm = analysisPCM{f32: r.bf[pos*2:]}
					dmf = downmixFloat
				case 2:
					pcm = analysisPCM{i32: r.b24[pos*2:]}
					dmf = downmixInt24
				}
				runAnalysis(&r.st, mode, pcm, c.fsz, c.fsz, 0, -2, 2, int32(fs), 16, dmf, &info)
				pos += c.fsz
			})
			if allocs != 0 {
				t.Errorf("dm=%d fs=%d: %v allocs per runAnalysis", dm, fs, allocs)
			}
		}
	}
}

// TestDownmixFloatCap checks the +6 dBFS cap and the NaN handling of
// downmixFloat.
func TestDownmixFloatCap(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	x := []float32{1, 1, 3, -3, nan, 0, inf, -inf, -inf, -inf, 0.5, 0.25}
	y := make([]float32, 6)
	downmixFloat(analysisPCM{f32: x}, y, 6, 0, 0, 1, 2)
	want := []float32{65536, 0, 0, 0, -65536, 24576}
	for i := range want {
		if y[i] != want[i] {
			t.Errorf("y[%d] = %v, want %v", i, y[i], want[i])
		}
	}
}

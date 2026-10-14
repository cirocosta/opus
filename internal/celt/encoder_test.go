package celt

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
)

// The encoder goldens come from tools/oracle/celt/encdrv.c (scalar
// libopus 1.6.1, default configuration). Each line is a case spec, then "=>", then the frame
// count, the total size, an FNV-1a hash of (ret, final range, packet)
// for every frame, and the start of the first packet. This file mirrors
// the driver step by step.

// encSpec is a parsed case spec line.
type encSpec struct {
	line, name, src                                   string
	off                                               int
	ch, fs, lm, n, br, vbr, cvbr, cx, pred, loss      int
	start, end, lsb, lfe, mask, ana, inv, ext, nb, sc int
	rnd, lmvar, seed                                  int
	tone                                              int64
}

func specInt(line, key string, def int) int {
	k := " " + key + "="
	i := strings.Index(line, k)
	if i < 0 {
		return def
	}
	s := line[i+len(k):]
	j := strings.IndexByte(s, ' ')
	if j >= 0 {
		s = s[:j]
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(err)
	}
	return int(v)
}

func parseEncSpec(line string) encSpec {
	s := encSpec{line: line}
	s.name = strings.Fields(line)[0]
	s.src = "pcm"
	if i := strings.Index(line, " src="); i >= 0 {
		s.src = strings.Fields(line[i+5:])[0]
	}
	s.off = specInt(line, "off", 0)
	s.ch = specInt(line, "ch", 2)
	s.fs = specInt(line, "fs", 48000)
	s.lm = specInt(line, "lm", 3)
	s.n = specInt(line, "n", 100)
	s.br = specInt(line, "br", -1)
	s.vbr = specInt(line, "vbr", 0)
	s.cvbr = specInt(line, "cvbr", 1)
	s.cx = specInt(line, "cx", 10)
	s.pred = specInt(line, "pred", 2)
	s.loss = specInt(line, "loss", 0)
	s.start = specInt(line, "start", 0)
	s.end = specInt(line, "end", 21)
	s.lsb = specInt(line, "lsb", 24)
	s.lfe = specInt(line, "lfe", 0)
	s.mask = specInt(line, "mask", 0)
	s.ana = specInt(line, "ana", 0)
	s.inv = specInt(line, "inv", 0)
	s.ext = specInt(line, "ext", 0)
	s.nb = specInt(line, "nb", 1275)
	s.sc = specInt(line, "sc", s.ch)
	s.rnd = specInt(line, "rnd", 0)
	s.lmvar = specInt(line, "lmvar", 0)
	s.seed = specInt(line, "seed", 1)
	s.tone = int64(specInt(line, "tone", 1071961363))
	return s
}

type encParams struct{ br, vbr, cvbr, cx, lm, pred, loss, end, sc, nb int }

var (
	encBrs = [14]int{
		6000,
		12000,
		16000,
		24000,
		32000,
		48000,
		64000,
		96000,
		128000,
		192000,
		256000,
		320000,
		510000,
		-1,
	}
	encEnds = [4]int{13, 17, 19, 21}
	encOffs = [4]int{60, 100, 140, 240}
)

type encOp struct {
	op   int
	a, b uint32
}

// encRunner holds the state of one case run, like the statics of the C
// driver.
type encRunner struct {
	s        encSpec
	pcm      []int16
	r        uint32
	ty0, ty1 int64
	tpos     int
}

func (r *encRunner) rnd() uint32 {
	r.r = r.r*1664525 + 1013904223
	return r.r >> 8
}

func (r *encRunner) perturb(p *encParams) (reset bool) {
	s := &r.s
	hyb := s.start != 0
	if r.rnd()%8 == 0 {
		p.br = encBrs[r.rnd()%14]
	}
	if r.rnd()%16 == 0 {
		p.vbr ^= 1
	}
	if r.rnd()%16 == 0 {
		p.cvbr ^= 1
	}
	if r.rnd()%8 == 0 {
		p.cx = int(r.rnd() % 11)
	}
	if s.lmvar != 0 && r.rnd()%6 == 0 {
		p.lm = int(r.rnd() % 4)
	}
	if r.rnd()%32 == 0 {
		p.pred = int(r.rnd() % 3)
	}
	if r.rnd()%32 == 0 {
		p.loss = int(r.rnd() % 30)
	}
	reset = r.rnd()%97 == 0
	if r.rnd()%16 == 0 {
		if hyb {
			p.end = encEnds[2+r.rnd()%2]
		} else {
			p.end = encEnds[r.rnd()%4]
		}
	}
	if s.ch == 2 && r.rnd()%24 == 0 {
		p.sc = 1 + int(r.rnd()%2)
	}
	if r.rnd()%16 == 0 {
		p.nb = 2 + int(r.rnd()%400)
	}
	return reset
}

func (r *encRunner) srcSample(t, c, frame int) int {
	s := &r.s
	switch s.src {
	case "sil":
		return 0
	case "tone":
		for r.tpos < t {
			y := ((2 * s.tone * r.ty0) >> 30) - r.ty1
			r.ty1 = r.ty0
			r.ty0 = y
			r.tpos++
		}
		v := int(r.ty0)
		if c == 1 {
			v >>= 1
		}
		return min(32767, max(-32768, v))
	case "gap":
		if frame%23 >= 18 {
			return 0
		}
	}
	n := len(r.pcm) / 2
	i := (s.off + t) % n
	return int(r.pcm[2*i+c])
}

type encResult struct {
	hash  uint64
	bytes int
	pk0   string
	lines []string
}

// runEncCase runs one case with the Go encoder, decodes every packet with
// the Go decoder and checks that the final ranges agree.
func runEncCase(t *testing.T, s encSpec, pcm []int16, verbose bool) encResult {
	r := &encRunner{s: s, pcm: pcm, r: uint32(s.seed)}
	r.ty0 = 12000
	r.ty1 = (12000 * s.tone) >> 30
	h := uint64(14695981039346656037)
	hb := func(b []byte) {
		for _, c := range b {
			h ^= uint64(c)
			h *= 1099511628211
		}
	}
	hi := func(v int32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(v))
		hb(b[:])
	}
	var res encResult
	res.pk0 = "-"

	e, ret := NewEncoder(int32(s.fs), s.ch)
	if ret != OK {
		t.Fatalf("%s: init: %d", s.name, ret)
	}
	e.SetSignalling(0)
	d, ret := NewDecoder(int32(s.fs), s.ch)
	if ret != OK {
		t.Fatalf("%s: decoder init: %d", s.name, ret)
	}
	d.SetSignalling(0)
	p := encParams{
		br:   s.br,
		vbr:  s.vbr,
		cvbr: s.cvbr,
		cx:   s.cx,
		lm:   s.lm,
		pred: s.pred,
		loss: s.loss,
		end:  s.end,
		sc:   s.sc,
		nb:   s.nb,
	}
	var pcmf [2 * 960]float32
	var out [2 * 960]float32
	var buf [1500]byte
	var maskv [42]float32
	var ops []encOp
	var enc entcode.Encoder
	var dec entcode.Decoder
	pos := 0
	for f := 0; f < s.n; f++ {
		up := 48000 / s.fs
		reset := false
		if s.rnd != 0 {
			reset = r.perturb(&p)
		}
		if s.start != 0 {
			if p.vbr != 0 {
				p.cvbr = 0
			} else {
				p.br = -1
			}
		}
		if reset {
			e.Reset()
			d.Reset()
		}
		e.SetComplexity(p.cx)
		e.SetStartBand(s.start)
		e.SetEndBand(p.end)
		e.SetPrediction(p.pred)
		e.SetPacketLossPerc(p.loss)
		e.SetVBR(p.vbr)
		e.SetVBRConstraint(p.cvbr)
		e.SetBitrate(int32(p.br))
		e.SetChannels(p.sc)
		e.SetLSBDepth(s.lsb)
		e.SetPhaseInversionDisabled(s.inv)
		e.SetLFE(s.lfe)
		if s.ana != 0 {
			var a AnalysisInfo
			a.Valid = b2i(s.ana == 1)
			a.Tonality = float32(r.rnd()%1001) / 1000
			a.TonalitySlope = float32(int(r.rnd()%2001)-1000) / 1000
			a.Noisiness = float32(r.rnd()%1001) / 1000
			a.Activity = float32(r.rnd()%1001) / 1000
			a.MusicProb = float32(r.rnd()%1001) / 1000
			a.MusicProbMin = float32(r.rnd()%1001) / 1000
			a.MusicProbMax = float32(r.rnd()%1001) / 1000
			a.Bandwidth = int(r.rnd() % 21)
			a.ActivityProbability = float32(r.rnd()%1001) / 1000
			a.MaxPitchRatio = float32(r.rnd()%1001) / 1000
			for i := 0; i < LeakBands; i++ {
				a.LeakBoost[i] = uint8(r.rnd() % 256)
			}
			e.SetAnalysis(&a)
		}
		if s.mask != 0 {
			for i := range maskv {
				maskv[i] = float32(int(r.rnd()%97)-72) / 16
			}
			e.SetEnergyMask(maskv[:])
		}
		if s.start != 0 {
			var si SILKInfo
			si.SignalType = int(r.rnd() % 3)
			si.Offset = encOffs[r.rnd()%4]
			e.SetSILKInfo(&si)
		}
		fsz := (120 << uint(p.lm)) / up
		for i := 0; i < fsz; i++ {
			for c := 0; c < s.ch; c++ {
				var v int
				if s.ch == 1 {
					v = (r.srcSample(pos+i, 0, f) + r.srcSample(pos+i, 1, f)) >> 1
				} else {
					v = r.srcSample(pos+i, c, f)
				}
				switch s.src {
				case "tiny":
					pcmf[s.ch*i+c] = float32(int(r.rnd()%3)-1) * (1.0 / 1048576)
				case "loud":
					pcmf[s.ch*i+c] = float32(dsp.Int16ToRes(int16(v)) * 3)
				case "huge":
					pcmf[s.ch*i+c] = float32(dsp.Int16ToRes(int16(v)) * 262144)
				default:
					pcmf[s.ch*i+c] = dsp.Int16ToRes(int16(v))
				}
			}
		}
		pos += fsz
		nb := p.nb
		ops = ops[:0]
		if s.ext == 0 {
			ret = e.EncodeWithEC(pcmf[:fsz*s.ch], fsz, buf[:], nb, nil)
		} else {
			enc.Init(buf[:nb])
			if s.ext == 2 {
				lim := nb * 4
				if p.br > 0 && p.vbr != 0 && p.br*(120<<uint(p.lm))/48000/2 < lim {
					lim = p.br * (120 << uint(p.lm)) / 48000 / 2
				}
				K := int(r.rnd() % uint32(lim+1))
				for enc.Tell() < K {
					switch r.rnd() % 3 {
					case 0:
						logp := 1 + r.rnd()%15
						v := r.rnd() & 1
						enc.EncBitLogp(int(v), uint(logp))
						ops = append(ops, encOp{0, v, logp})
					case 1:
						ft := 2 + r.rnd()%1000
						v := r.rnd() % ft
						enc.EncUint(v, ft)
						ops = append(ops, encOp{1, v, ft})
					default:
						v := r.rnd() & 0xFF
						enc.EncBits(v, 8)
						ops = append(ops, encOp{2, v, 8})
					}
				}
			}
			ret = e.EncodeWithEC(pcmf[:fsz*s.ch], fsz, nil, nb, &enc)
		}
		rng := e.FinalRange()
		hi(int32(ret))
		hi(int32(rng))
		if ret > 0 {
			hb(buf[:ret])
			res.bytes += ret
		}
		if f == 0 && ret > 0 {
			res.pk0 = fmt.Sprintf("%x", buf[:min(ret, 24)])
		}
		if verbose {
			res.lines = append(
				res.lines,
				fmt.Sprintf(
					"f=%d lm=%d br=%d vbr=%d cvbr=%d cx=%d nb=%d sc=%d end=%d ret=%d rng=%08x h=%016x",
					f,
					p.lm,
					p.br,
					p.vbr,
					p.cvbr,
					p.cx,
					nb,
					p.sc,
					p.end,
					ret,
					rng,
					h,
				),
			)
		}
		// Round trip through the decoder.
		if ret > 0 {
			d.SetStartBand(s.start)
			d.SetEndBand(p.end)
			d.SetChannels(p.sc)
			pkt := buf[:ret]
			var dp *entcode.Decoder
			if s.ext == 2 {
				dec.Init(pkt)
				for _, o := range ops {
					var v uint32
					switch o.op {
					case 0:
						v = uint32(dec.DecBitLogp(uint(o.b)))
					case 1:
						v = dec.DecUint(o.b)
					default:
						v = dec.DecBits(8)
					}
					if v != o.a {
						t.Fatalf("%s: frame %d: prefill symbol %d mismatch", s.name, f, v)
					}
				}
				dp = &dec
			}
			dret := d.DecodeWithEC(pkt, out[:], fsz, dp, false)
			if dret != fsz {
				t.Fatalf("%s: frame %d: decode returned %d, want %d", s.name, f, dret, fsz)
			}
			if d.FinalRange() != rng {
				t.Fatalf(
					"%s: frame %d: decoder range %08x, encoder %08x",
					s.name,
					f,
					d.FinalRange(),
					rng,
				)
			}
		}
	}
	res.hash = h
	return res
}

func readPCMGz(t *testing.T, path string) []int16 {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	return bytesToInt16(b)
}

func bytesToInt16(b []byte) []int16 {
	pcm := make([]int16, len(b)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return pcm
}

type encGolden struct {
	spec   encSpec
	frames int
	bytes  int
	hash   string
	pk0    string
	vec    string
}

func readEncGoldens(t *testing.T, path string) []encGolden {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var gs []encGolden
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		spec, res, ok := strings.Cut(line, " => ")
		if !ok {
			t.Fatalf("bad golden line %q", line)
		}
		g := encGolden{spec: parseEncSpec(spec)}
		for _, kv := range strings.Fields(res) {
			k, v, _ := strings.Cut(kv, "=")
			switch k {
			case "frames":
				g.frames, _ = strconv.Atoi(v)
			case "bytes":
				g.bytes, _ = strconv.Atoi(v)
			case "hash":
				g.hash = v
			case "pk0":
				g.pk0 = v
			}
		}
		if i := strings.Index(spec, " vec="); i >= 0 {
			g.vec = strings.Fields(spec[i+5:])[0]
		}
		gs = append(gs, g)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return gs
}

func checkEncGolden(t *testing.T, g encGolden, pcm []int16) {
	t.Helper()
	verbose := os.Getenv("CELTENC_VERBOSE") == g.spec.name
	res := runEncCase(t, g.spec, pcm, verbose)
	for _, l := range res.lines {
		fmt.Println(l)
	}
	got := fmt.Sprintf("%016x", res.hash)
	if got != g.hash || res.bytes != g.bytes || res.pk0 != g.pk0 {
		t.Errorf("%s: hash=%s bytes=%d pk0=%s, want hash=%s bytes=%d pk0=%s",
			g.spec.name, got, res.bytes, res.pk0, g.hash, g.bytes, g.pk0)
	}
}

// TestEncoderGolden encodes the committed PCM excerpt (testdata
// enc_pcm.raw.gz, 2.4 s from RFC 8251 vectors 01, 11, 02, 05 and 10) and
// synthetic signals with every case of testdata/enc_golden.txt.
func TestEncoderGolden(t *testing.T) {
	pcm := readPCMGz(t, filepath.Join("testdata", "enc_pcm.raw.gz"))
	gs := readEncGoldens(t, filepath.Join("testdata", "enc_golden.txt"))
	if len(gs) == 0 {
		t.Fatal("no goldens")
	}
	for _, g := range gs {
		checkEncGolden(t, g, pcm)
	}
}

// TestEncoderVectors encodes the full decoded RFC 8251 vectors with the
// cases of testdata/enc_vectors_golden.txt. It needs OPUS_TESTVECTORS.
func TestEncoderVectors(t *testing.T) {
	dir := os.Getenv("OPUS_TESTVECTORS")
	if dir == "" {
		t.Skip("OPUS_TESTVECTORS is not set")
	}
	if testing.Short() {
		t.Skip("short")
	}
	gs := readEncGoldens(t, filepath.Join("testdata", "enc_vectors_golden.txt"))
	cache := map[string][]int16{}
	for _, g := range gs {
		pcm, ok := cache[g.vec]
		if !ok {
			b, err := os.ReadFile(filepath.Join(dir, "testvector"+g.vec+".dec"))
			if err != nil {
				t.Fatal(err)
			}
			pcm = bytesToInt16(b)
			cache[g.vec] = pcm
		}
		checkEncGolden(t, g, pcm)
	}
}

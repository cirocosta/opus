package opus

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The encoder goldens come from a C driver (tools/oracle/enc/drv.c) that
// links the libopus 1.6.1 reference build. Each case is a list of tokens (see parseEncCase); the driver
// prints one line per control and per frame, and the golden holds the
// line count and the first 32 hex digits of the SHA-256 of the output.

// encCtl is a control applied before a frame.
type encCtl struct {
	frame int
	key   string
	val   int
}

// encCase is a parsed case.
type encCase struct {
	spec                     string
	typ                      string
	fs, ch, app, fr, n, mb   int
	api                      string
	streams, coupled, family int
	mapping                  []byte
	in                       string
	off                      int
	ctls                     []encCtl
}

func parseEncCase(spec string) (*encCase, error) {
	c := &encCase{spec: spec, typ: "enc", fs: 48000, ch: 1, app: ApplicationAudio, fr: 960, n: 50,
		api: "16", mb: 1500, streams: 1, family: 1, in: "x"}
	for _, tok := range strings.Fields(spec) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			return nil, fmt.Errorf("bad token %q", tok)
		}
		atoi := func() int {
			n, err := strconv.Atoi(v)
			if err != nil {
				panic(fmt.Sprintf("bad value in %q", tok))
			}
			return n
		}
		if strings.HasPrefix(k, "@") {
			f, key, _ := strings.Cut(k[1:], ":")
			fr, _ := strconv.Atoi(f)
			c.ctls = append(c.ctls, encCtl{fr, key, atoi()})
			continue
		}
		switch k {
		case "t":
			c.typ = v
		case "fs":
			c.fs = atoi()
		case "ch":
			c.ch = atoi()
		case "app":
			c.app = atoi()
		case "fr":
			c.fr = atoi()
		case "n":
			c.n = atoi()
		case "api":
			c.api = v
		case "mb":
			c.mb = atoi()
		case "streams":
			c.streams = atoi()
		case "coupled":
			c.coupled = atoi()
		case "family":
			c.family = atoi()
		case "map":
			for _, s := range strings.Split(v, ".") {
				n, _ := strconv.Atoi(s)
				c.mapping = append(c.mapping, byte(n))
			}
		case "in":
			in, off, _ := strings.Cut(v, "+")
			c.in = in
			if off != "" {
				c.off, _ = strconv.Atoi(off)
			}
		default:
			c.ctls = append(c.ctls, encCtl{0, k, atoi()})
		}
	}
	return c, nil
}

var excerptOnce struct {
	sync.Once
	pcm []int16
	err error
}

// encExcerpt returns the committed 2.4 s excerpt of the RFC 8251 vectors
// (48 kHz stereo) of the CELT encoder tests.
func encExcerpt() ([]int16, error) {
	excerptOnce.Do(func() {
		f, err := os.Open(filepath.Join("internal", "celt", "testdata", "enc_pcm.raw.gz"))
		if err != nil {
			excerptOnce.err = err
			return
		}
		defer f.Close()
		z, err := gzip.NewReader(f)
		if err != nil {
			excerptOnce.err = err
			return
		}
		b, err := io.ReadAll(z)
		if err != nil {
			excerptOnce.err = err
			return
		}
		excerptOnce.pcm = bytesToPCM16(b)
	})
	return excerptOnce.pcm, excerptOnce.err
}

func bytesToPCM16(b []byte) []int16 {
	pcm := make([]int16, len(b)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return pcm
}

// encLCG is the noise generator of the synthetic inputs.
type encLCG uint32

func (g *encLCG) next() int32 {
	*g = *g*1103515245 + 12345
	return int32(uint32(*g)>>16) - 32768
}

func sat16(v int32) int16 {
	return int16(max(-32768, min(32767, v)))
}

// synthSource builds a 3 s 48 kHz stereo synthetic signal from integer
// operations only.
func synthSource(kind int) []int16 {
	const frames = 3 * 48000
	out := make([]int16, 2*frames)
	g := encLCG(kind*7919 + 1)
	switch kind {
	case 0: // silence
	case 1: // noise bursts of varying level, including digital silence
		amps := []int32{0, 1, 4, 100, 3000, 20000, 0, 2, 600, 12000, 1, 30000}
		for i := 0; i < frames; i++ {
			a := amps[(i/12000)%len(amps)]
			out[2*i] = sat16(g.next() * a >> 15)
			out[2*i+1] = sat16(g.next() * a >> 15)
		}
	case 2: // triangle sweeps, different per channel
		var p0, p1 uint32
		for i := 0; i < frames; i++ {
			p0 += 2000000 + uint32(i)*500
			p1 += 9000000 + uint32(i%48000)*900
			t0 := int32(p0 >> 16)
			if t0 >= 32768 {
				t0 = 65535 - t0
			}
			t1 := int32(p1 >> 16)
			if t1 >= 32768 {
				t1 = 65535 - t1
			}
			amp := int32(2000 + (i/4800)%10*2500)
			out[2*i] = sat16((t0 - 16384) * amp >> 14)
			out[2*i+1] = sat16((t1 - 16384) * amp >> 15)
		}
	case 3: // excerpt with gaps of silence and near-silence (DTX)
		x, _ := encExcerpt()
		n := len(x) / 2
		for i := 0; i < frames; i++ {
			seg := i / 9600
			if seg%3 == 0 {
				out[2*i] = x[2*(i%n)]
				out[2*i+1] = x[2*(i%n)+1]
			} else if seg%6 == 2 {
				out[2*i] = sat16(g.next() >> 15)
				out[2*i+1] = out[2*i]
			}
		}
	case 4: // full-scale square waves and impulses
		for i := 0; i < frames; i++ {
			v := int16(32767)
			if (i/50)%2 == 1 {
				v = -32768
			}
			if (i/24000)%2 == 1 {
				v /= 64
			}
			out[2*i] = v
			out[2*i+1] = -v
			if i%4801 == 0 {
				out[2*i] = 32767
				out[2*i+1] = 32767
			}
		}
	case 5: // excerpt switching between dual mono, inverted and stereo
		x, _ := encExcerpt()
		n := len(x) / 2
		for i := 0; i < frames; i++ {
			l := x[2*(i%n)]
			switch (i / 14400) % 3 {
			case 0:
				out[2*i] = l
				out[2*i+1] = l
			case 1:
				out[2*i] = l
				out[2*i+1] = sat16(-int32(l))
			default:
				out[2*i] = l
				out[2*i+1] = x[2*(i%n)+1]
			}
		}
	}
	return out
}

// encSource returns the 48 kHz stereo source of a case input.
func encSource(in string) ([]int16, error) {
	switch {
	case in == "x":
		return encExcerpt()
	case strings.HasPrefix(in, "s"):
		k, err := strconv.Atoi(in[1:])
		if err != nil {
			return nil, err
		}
		return synthSource(k), nil
	case strings.HasPrefix(in, "v"):
		dir := os.Getenv("OPUS_TESTVECTORS")
		if dir == "" {
			return nil, errNoVectors
		}
		b, err := os.ReadFile(filepath.Join(dir, "testvector"+in[1:]+".dec"))
		if err != nil {
			return nil, err
		}
		return bytesToPCM16(b), nil
	}
	return nil, fmt.Errorf("bad input %q", in)
}

var errNoVectors = fmt.Errorf("OPUS_TESTVECTORS not set")

// encInput derives the case input: n*fr frames of ch channels at fs,
// decimated from the 48 kHz stereo source, starting at source frame off.
func encInput(c *encCase) ([]int16, error) {
	src, err := encSource(c.in)
	if err != nil {
		return nil, err
	}
	srcFrames := len(src) / 2
	factor := 48000 / c.fs
	out := make([]int16, c.n*c.fr*c.ch)
	for i := 0; i < c.n*c.fr; i++ {
		j := (c.off + i*factor) % srcFrames
		l, r := int32(src[2*j]), int32(src[2*j+1])
		switch c.ch {
		case 1:
			out[i] = int16((l + r) >> 1)
		case 2:
			out[2*i] = int16(l)
			out[2*i+1] = int16(r)
		default:
			for k := 0; k < c.ch; k++ {
				jk := (j + srcFrames - k*37) % srcFrames
				wa := int32((k*5 + 3) % 17)
				out[i*c.ch+k] = int16((int32(src[2*jk])*wa + int32(src[2*jk+1])*(16-wa)) >> 4)
			}
		}
	}
	return out, nil
}

// encRunner abstracts the three encoder types.
type encRunner struct {
	enc  *Encoder
	ms   *MSEncoder
	proj *ProjectionEncoder

	dec     *Decoder
	msDec   *MSDecoder
	projDec *ProjectionDecoder
}

func errCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(Error); ok {
		return int(e)
	}
	return -999
}

func (r *encRunner) ctl(key string, v int) int {
	if r.enc != nil {
		e := r.enc
		switch key {
		case "br":
			return errCode(e.SetBitrate(v))
		case "cx":
			return errCode(e.SetComplexity(v))
		case "fec":
			return errCode(e.SetInbandFEC(v))
		case "loss":
			return errCode(e.SetPacketLossPerc(v))
		case "dtx":
			e.SetDTX(v != 0)
		case "vbr":
			e.SetVBR(v != 0)
		case "cvbr":
			e.SetVBRConstraint(v != 0)
		case "sig":
			return errCode(e.SetSignal(v))
		case "bw":
			return errCode(e.SetBandwidth(v))
		case "maxbw":
			return errCode(e.SetMaxBandwidth(v))
		case "fc":
			return errCode(e.SetForceChannels(v))
		case "lsb":
			return errCode(e.SetLSBDepth(v))
		case "fd":
			return errCode(e.SetExpertFrameDuration(v))
		case "pd":
			e.SetPredictionDisabled(v != 0)
		case "pi":
			e.SetPhaseInversionDisabled(v != 0)
		case "mode":
			return errCode(e.SetForceMode(v))
		case "reset":
			e.Reset()
		case "setapp":
			return errCode(e.SetApplication(v))
		case "vr":
			return errCode(e.SetVoiceRatio(v))
		default:
			panic("bad ctl " + key)
		}
		return 0
	}
	var m *MSEncoder
	if r.ms != nil {
		m = r.ms
	} else {
		p := r.proj
		switch key {
		case "br":
			return errCode(p.SetBitrate(v))
		case "cx":
			return errCode(p.SetComplexity(v))
		case "vbr":
			p.SetVBR(v != 0)
		case "reset":
			p.Reset()
		default:
			panic("bad proj ctl " + key)
		}
		return 0
	}
	switch key {
	case "br":
		return errCode(m.SetBitrate(v))
	case "cx":
		return errCode(m.SetComplexity(v))
	case "fec":
		return errCode(m.SetInbandFEC(v))
	case "loss":
		return errCode(m.SetPacketLossPerc(v))
	case "dtx":
		m.SetDTX(v != 0)
	case "vbr":
		m.SetVBR(v != 0)
	case "cvbr":
		m.SetVBRConstraint(v != 0)
	case "sig":
		return errCode(m.SetSignal(v))
	case "bw":
		return errCode(m.SetBandwidth(v))
	case "maxbw":
		return errCode(m.SetMaxBandwidth(v))
	case "fc":
		return errCode(m.SetForceChannels(v))
	case "lsb":
		return errCode(m.SetLSBDepth(v))
	case "fd":
		m.SetExpertFrameDuration(v)
	case "pd":
		m.SetPredictionDisabled(v != 0)
	case "pi":
		m.SetPhaseInversionDisabled(v != 0)
	case "mode":
		return errCode(m.SetForceMode(v))
	case "reset":
		m.Reset()
	case "setapp":
		return errCode(m.SetApplication(v))
	default:
		panic("bad ms ctl " + key)
	}
	return 0
}

// runEncCase runs a case like the C driver and returns its output. With
// checkDecode, every packet is also decoded with the Go decoder, whose
// final range must match the encoder's.
func runEncCase(t testing.TB, c *encCase, pcm []int16, checkDecode bool) string {
	var sb strings.Builder
	var r encRunner
	var err error
	switch c.typ {
	case "enc":
		r.enc, err = NewEncoder(c.fs, c.ch, c.app)
		if err == nil && checkDecode {
			r.dec, _ = NewDecoder(c.fs, c.ch)
		}
	case "ms":
		r.ms, err = NewMSEncoder(c.fs, c.ch, c.streams, c.coupled, c.mapping, c.app)
		if err == nil && checkDecode {
			r.msDec, err = NewMSDecoder(c.fs, c.ch, c.streams, c.coupled, c.mapping)
		}
	case "sur":
		r.ms, err = NewMSSurroundEncoder(c.fs, c.ch, c.family, c.app)
		if err == nil {
			fmt.Fprintf(&sb, "layout %d %d", r.ms.Streams(), r.ms.CoupledStreams())
			for _, m := range r.ms.Mapping() {
				fmt.Fprintf(&sb, " %d", m)
			}
			sb.WriteString("\n")
			if checkDecode {
				r.msDec, err = NewMSDecoder(c.fs, c.ch, r.ms.Streams(), r.ms.CoupledStreams(), r.ms.Mapping())
			}
		}
	case "proj":
		r.proj, err = NewProjectionAmbisonicsEncoder(c.fs, c.ch, c.family, c.app)
		if err == nil {
			m := make([]byte, r.proj.DemixingMatrixSize())
			if e := r.proj.DemixingMatrix(m); e != nil {
				t.Fatal(e)
			}
			fmt.Fprintf(&sb, "layout %d %d %d %d %s\n", r.proj.Streams(), r.proj.CoupledStreams(),
				len(m), r.proj.DemixingMatrixGain(), hex.EncodeToString(m))
			if checkDecode {
				r.projDec, err = NewProjectionDecoder(c.fs, c.ch, r.proj.Streams(), r.proj.CoupledStreams(), m)
			}
		}
	default:
		t.Fatalf("bad type %q", c.typ)
	}
	fmt.Fprintf(&sb, "create %d\n", errCode(err))
	if err != nil {
		return sb.String()
	}
	if r.enc != nil {
		fmt.Fprintf(&sb, "lookahead %d\n", r.enc.Lookahead())
	}
	frameLen := c.fr * c.ch
	p24 := make([]int32, frameLen)
	pf := make([]float32, frameLen)
	buf := make([]byte, max(c.mb, 1))
	out := make([]int16, 5760*c.ch)
	for fi := 0; fi < c.n; fi++ {
		for _, ct := range c.ctls {
			if ct.frame == fi {
				fmt.Fprintf(&sb, "c %s %d %d\n", ct.key, ct.val, r.ctl(ct.key, ct.val))
			}
		}
		in := pcm[fi*frameLen : (fi+1)*frameLen]
		var n int
		var err error
		data := buf[:c.mb]
		switch c.api {
		case "24":
			for i := range p24 {
				p24[i] = int32(in[i])*256 + int32((i*31+fi*7)&255) - 128
			}
			switch {
			case r.enc != nil:
				n, err = r.enc.Encode24(p24, c.fr, data)
			case r.ms != nil:
				n, err = r.ms.Encode24(p24, c.fr, data)
			default:
				n, err = r.proj.Encode24(p24, c.fr, data)
			}
		case "f", "f2", "f3", "f4":
			sc := map[string]float32{"f": 1.0 / 32768, "f2": 1.0 / 16384, "f3": 1.0 / 32, "f4": 1}[c.api]
			for i := range pf {
				pf[i] = float32(in[i]) * sc
			}
			switch {
			case r.enc != nil:
				n, err = r.enc.EncodeFloat(pf, c.fr, data)
			case r.ms != nil:
				n, err = r.ms.EncodeFloat(pf, c.fr, data)
			default:
				n, err = r.proj.EncodeFloat(pf, c.fr, data)
			}
		default:
			switch {
			case r.enc != nil:
				n, err = r.enc.Encode(in, c.fr, data)
			case r.ms != nil:
				n, err = r.ms.Encode(in, c.fr, data)
			default:
				n, err = r.proj.Encode(in, c.fr, data)
			}
		}
		ret := n
		if err != nil {
			ret = errCode(err)
		}
		var rng uint32
		switch {
		case r.enc != nil:
			rng = r.enc.FinalRange()
		case r.ms != nil:
			rng = r.ms.FinalRange()
		default:
			rng = r.proj.FinalRange()
		}
		fmt.Fprintf(&sb, "%d %d", ret, rng)
		if r.enc != nil {
			fmt.Fprintf(&sb, " %d %d %d", r.enc.Bandwidth(), b2i(r.enc.InDTX()), r.enc.Bitrate())
		}
		sb.WriteString(" ")
		if ret > 0 {
			sb.WriteString(hex.EncodeToString(data[:ret]))
		}
		sb.WriteString("\n")
		if checkDecode && ret > 0 {
			checkEncDecode(t, c, fi, &r, data[:ret], out, rng)
		}
	}
	sb.WriteString(encGetters(&r))
	return sb.String()
}

// encGetters formats the final values of the getters like the driver.
func encGetters(r *encRunner) string {
	if e := r.enc; e != nil {
		return fmt.Sprintf("g %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d\n",
			e.Application(), e.Bitrate(), e.ForceChannels(), e.MaxBandwidth(), e.Bandwidth(),
			b2i(e.DTX()), e.Complexity(), e.InbandFEC(), e.PacketLossPerc(), b2i(e.VBR()),
			e.VoiceRatio(), b2i(e.VBRConstraint()), e.Signal(), e.Lookahead(), e.SampleRate(),
			e.LSBDepth(), e.ExpertFrameDuration(), b2i(e.PredictionDisabled()),
			b2i(e.PhaseInversionDisabled()), b2i(e.InDTX()), e.FinalRange())
	}
	m := r.ms
	if m == nil {
		m = r.proj.MSEncoder()
		p := r.proj
		// The projection encoder methods forward to the multistream
		// encoder; check that they agree.
		if p.Bitrate() != m.Bitrate() || p.Lookahead() != m.Lookahead() ||
			p.SampleRate() != m.SampleRate() || p.FinalRange() != m.FinalRange() || p.Channels() != m.Channels() {
			return "projection getters differ\n"
		}
	}
	return fmt.Sprintf("g %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d\n",
		m.Bitrate(), m.LSBDepth(), b2i(m.VBR()), m.Application(), m.Bandwidth(), m.Complexity(),
		m.PacketLossPerc(), b2i(m.DTX()), b2i(m.VBRConstraint()), m.Signal(), m.Lookahead(),
		m.SampleRate(), m.InbandFEC(), m.ForceChannels(), b2i(m.PredictionDisabled()),
		b2i(m.PhaseInversionDisabled()), m.ExpertFrameDuration(), m.VoiceRatio(), m.FinalRange())
}

// checkEncDecode decodes a packet and compares the decoder final range
// with the encoder's.
func checkEncDecode(t testing.TB, c *encCase, fi int, r *encRunner, pkt []byte, out []int16, rng uint32) {
	var err error
	var drng uint32
	switch {
	case r.dec != nil:
		_, err = r.dec.Decode(pkt, out, 5760, false)
		drng = r.dec.FinalRange()
	case r.msDec != nil:
		_, err = r.msDec.Decode(pkt, make([]int16, 5760*c.ch), 5760, false)
		drng = r.msDec.FinalRange()
	case r.projDec != nil:
		_, err = r.projDec.Decode(pkt, make([]int16, 5760*c.ch), 5760, false)
		drng = r.projDec.FinalRange()
	default:
		return
	}
	if err != nil {
		t.Errorf("%s: frame %d: decode: %v", c.spec, fi, err)
		return
	}
	if drng != rng {
		t.Errorf("%s: frame %d: decoder final range %d, encoder %d", c.spec, fi, drng, rng)
	}
}

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:32]
}

type encGolden struct {
	lines int
	hash  string
	spec  string
}

func readEncGoldens(t *testing.T, path string) []encGolden {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var gs []encGolden
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 {
			t.Fatalf("bad golden line %q", line)
		}
		n, _ := strconv.Atoi(fields[0])
		gs = append(gs, encGolden{n, fields[1], fields[2]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return gs
}

// checkEncGoldens runs every golden case and compares its output. With
// OPUS_ENC_DUMP set, it writes the output of failing cases there.
func checkEncGoldens(t *testing.T, path string) {
	gs := readEncGoldens(t, path)
	if len(gs) == 0 {
		t.Fatal("no goldens")
	}
	dump := os.Getenv("OPUS_ENC_DUMP")
	for i, g := range gs {
		c, err := parseEncCase(g.spec)
		if err != nil {
			t.Fatal(err)
		}
		// In short mode, run every fourth case and skip long ones.
		if testing.Short() && (i%4 != 0 || c.n*c.fr*c.ch > 48000*2*4) {
			continue
		}
		pcm, err := encInput(c)
		if err == errNoVectors {
			t.Skip("OPUS_TESTVECTORS not set")
		}
		if err != nil {
			t.Fatal(err)
		}
		out := runEncCase(t, c, pcm, true)
		lines := strings.Count(out, "\n")
		if lines != g.lines || digest(out) != g.hash {
			t.Errorf("case %d %q: got %d lines %s, want %d lines %s", i+1, g.spec, lines, digest(out), g.lines, g.hash)
			if dump != "" {
				os.WriteFile(filepath.Join(dump, fmt.Sprintf("%04d.txt", i+1)), []byte(out), 0o644)
			}
		}
	}
}

// TestEncoderGolden checks the encoder, multistream and projection
// encoder output against libopus on the committed excerpt and synthetic
// signals.
func TestEncoderGolden(t *testing.T) {
	checkEncGoldens(t, filepath.Join("testdata", "encoder_golden.txt"))
}

// TestEncoderVectors checks the encoder on the RFC 8251 test vectors in
// $OPUS_TESTVECTORS.
func TestEncoderVectors(t *testing.T) {
	if os.Getenv("OPUS_TESTVECTORS") == "" {
		t.Skip("OPUS_TESTVECTORS not set")
	}
	if testing.Short() {
		t.Skip("slow; skipped in short mode")
	}
	checkEncGoldens(t, filepath.Join("testdata", "encoder_vectors.txt"))
}

// TestWriteEncInputs writes the input of every case of the file
// $OPUS_ENC_CASES (one spec per line) to $OPUS_ENC_INPUTS/NNNN.pcm for
// the C driver.
func TestWriteEncInputs(t *testing.T) {
	dir := os.Getenv("OPUS_ENC_INPUTS")
	cases := os.Getenv("OPUS_ENC_CASES")
	if dir == "" || cases == "" {
		t.Skip("OPUS_ENC_INPUTS or OPUS_ENC_CASES not set")
	}
	b, err := os.ReadFile(cases)
	if err != nil {
		t.Fatal(err)
	}
	i := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" || line[0] == '#' {
			continue
		}
		i++
		c, err := parseEncCase(line)
		if err != nil {
			t.Fatal(err)
		}
		pcm, err := encInput(c)
		if err != nil {
			t.Fatal(err)
		}
		raw := make([]byte, 2*len(pcm))
		for j, s := range pcm {
			binary.LittleEndian.PutUint16(raw[2*j:], uint16(s))
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%04d.pcm", i)), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestEncodeOutput writes the Go output of the case $OPUS_ENC_SPEC to
// stdout, for comparison with the C driver.
func TestEncodeOutput(t *testing.T) {
	spec := os.Getenv("OPUS_ENC_SPEC")
	if spec == "" {
		t.Skip("OPUS_ENC_SPEC not set")
	}
	c, err := parseEncCase(spec)
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := encInput(c)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Print(runEncCase(t, c, pcm, true))
}

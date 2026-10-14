package silk

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cirocosta/opus/internal/entcode"
)

// The encoder goldens come from the C driver tools/oracle/silk/enc.c,
// which links the scalar reference libopus.a and calls silk_InitEncoder
// and silk_Encode directly, the way opus_encoder.c does: one call per
// packet with a fresh ec_enc over a maxBytes buffer, then ec_enc_done.
// It then decodes the packets with silk_Decode. Each scenario prints one
// line per packet and a final decoder line (see encPacketLine); the
// golden file stores the line count and the SHA-256 of the output, plus
// the first lines of each scenario for diagnosis.

// encPacketCtl is one line of the per-packet control script.
type encPacketCtl struct {
	prefill  int
	nSamples int // per channel, at the API rate
	ctl      EncControl
	maxBytes int
	activity int
}

// scriptFields lists the script columns.
func (p *encPacketCtl) scriptLine() string {
	c := &p.ctl
	return fmt.Sprintf("%d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d %d",
		p.prefill, p.nSamples, c.NChannelsAPI, c.NChannelsInternal, c.APISampleRate,
		c.MaxInternalSampleRate, c.MinInternalSampleRate, c.DesiredInternalSampleRate,
		c.PayloadSizeMs, c.BitRate, c.PacketLossPercentage, c.Complexity, c.UseInBandFEC,
		c.LBRRCoded, c.UseDTX, c.UseCBR, p.maxBytes, c.ToMono, c.OpusCanSwitch,
		c.ReducedDependency, p.activity)
}

// encScenario describes one encoder test stream.
type encScenario struct {
	name    string
	seed    uint32
	apiRate int
	ch      int // API channels (and encoder channels)
	ms      int // total duration
	// Fixed configuration (used when vary is false).
	internalCh, payloadMs, bitRate, complexity int
	maxRate, minRate, desRate                  int
	fec, loss, dtx, cbr, maxBytes              int
	vary                                       bool
	src                                        string // test vector name; empty for synthetic input
}

var internalRates = [3]int{8000, 12000, 16000}

// schedule builds the per-packet control script of a scenario.
func (sc *encScenario) schedule() []encPacketCtl {
	rng := lcg(sc.seed ^ 0x9e3779b9)
	var out []encPacketCtl
	base := EncControl{
		NChannelsAPI:              int32(sc.ch),
		NChannelsInternal:         int32(sc.internalCh),
		APISampleRate:             int32(sc.apiRate),
		MaxInternalSampleRate:     int32(sc.maxRate),
		MinInternalSampleRate:     int32(sc.minRate),
		DesiredInternalSampleRate: int32(sc.desRate),
		PayloadSizeMs:             sc.payloadMs,
		BitRate:                   int32(sc.bitRate),
		PacketLossPercentage:      sc.loss,
		Complexity:                sc.complexity,
		UseInBandFEC:              sc.fec,
		LBRRCoded:                 sc.fec,
		UseDTX:                    sc.dtx,
		UseCBR:                    sc.cbr,
	}
	maxBytes := sc.maxBytes
	pos := 0
	total := sc.ms * sc.apiRate / 1000
	prevInternal := base.NChannelsInternal
	lastToMono := false
	for pos < total {
		p := encPacketCtl{ctl: base, maxBytes: maxBytes, activity: vadNoDecision}
		c := &p.ctl
		if sc.vary {
			if rng.intn(12) == 0 {
				base.PayloadSizeMs = [4]int{10, 20, 40, 60}[rng.intn(4)]
			}
			if rng.intn(6) == 0 {
				base.BitRate = int32(6000 + rng.intn(34001))
			}
			if rng.intn(10) == 0 {
				base.Complexity = rng.intn(11)
			}
			if rng.intn(15) == 0 {
				base.UseInBandFEC ^= 1
				base.PacketLossPercentage = rng.intn(31)
			}
			if rng.intn(20) == 0 {
				base.UseDTX ^= 1
			}
			if rng.intn(20) == 0 {
				base.UseCBR ^= 1
			}
			if rng.intn(10) == 0 {
				switch rng.intn(3) {
				case 0:
					maxBytes = 1275
				case 1:
					maxBytes = 10 + rng.intn(60)
				default:
					maxBytes = int(base.BitRate)*base.PayloadSizeMs/8000 + rng.intn(40)
				}
			}
			if rng.intn(12) == 0 {
				a, b, d := internalRates[rng.intn(3)], internalRates[rng.intn(3)], internalRates[rng.intn(3)]
				lo, hi := min(a, b), max(a, b)
				d = max(lo, min(hi, d))
				base.MinInternalSampleRate, base.MaxInternalSampleRate, base.DesiredInternalSampleRate = int32(
					lo,
				), int32(
					hi,
				), int32(
					d,
				)
			}
			if sc.ch == 2 && rng.intn(15) == 0 {
				base.NChannelsInternal = int32(3 - int(base.NChannelsInternal))
			}
			*c = base
			c.OpusCanSwitch = btoi(rng.intn(8) == 0)
			c.ReducedDependency = btoi(rng.intn(25) == 0)
			c.LBRRCoded = base.UseInBandFEC * btoi(rng.intn(4) != 0)
			p.activity = rng.intn(3) - 1
			p.maxBytes = maxBytes
			if c.NChannelsInternal == 1 && prevInternal == 2 && !lastToMono {
				// Opus sends one toMono frame before switching to mono.
				c.NChannelsInternal = 2
				c.ToMono = 1
			}
			lastToMono = c.ToMono != 0
			if rng.intn(40) == 0 {
				p.prefill = 1 + rng.intn(2)
			}
		}
		prevInternal = c.NChannelsInternal
		if p.maxBytes > 1275 {
			p.maxBytes = 1275
		}
		if p.maxBytes < 2 {
			p.maxBytes = 2
		}
		c.MaxBits = p.maxBytes * 8
		if p.prefill != 0 {
			p.nSamples = sc.apiRate / 100
		} else {
			p.nSamples = c.PayloadSizeMs * sc.apiRate / 1000
		}
		if pos+p.nSamples > total {
			break
		}
		pos += p.nSamples
		out = append(out, p)
	}
	return out
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// fnvBytes is FNV-1a 64.
func fnvBytes(h uint64, b []byte) uint64 {
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}

const fnvOffset = 14695981039346656037

// encPacketLine formats one output line, as the C driver prints it.
func encPacketLine(
	idx int,
	p *encPacketCtl,
	ret int,
	nBytes int32,
	tell int,
	rng uint32,
	c *EncControl,
	h uint64,
) string {
	return fmt.Sprintf("%d %d %d %d %d %d %d %d %d %d %d %d %d %d %016x",
		idx, p.prefill, ret, nBytes, tell, rng, c.SwitchReady, c.InternalSampleRate,
		c.InWBmodeWithoutVariableLP, c.StereoWidthQ14, c.SignalType, c.Offset,
		c.AllowBandwidthSwitch, c.MaxBits, h)
}

// encRunner holds the encoder and decoder of one stream.
type encRunner struct {
	enc     Encoder
	dec     Decoder
	encCtl  EncControl
	decCtl  DecControl
	pcm     []float32
	buf     [1275]byte
	out16   [2 * 48 * 20]int16
	decHash uint64
	decN    int
}

// run encodes the stream and returns the output lines.
func (r *encRunner) run(pcm []int16, sched []encPacketCtl, ch int) []string {
	var lines []string
	r.enc.Init(ch, &r.encCtl)
	r.dec.Init()
	r.decHash = fnvOffset
	r.decN = 0
	pos := 0
	for i := range sched {
		p := &sched[i]
		lines = append(lines, r.encodePacket(i, p, pcm[pos*ch:(pos+p.nSamples)*ch], ch))
		pos += p.nSamples
	}
	lines = append(lines, fmt.Sprintf("dec %016x %d", r.decHash, r.decN))
	return lines
}

func (r *encRunner) encodePacket(idx int, p *encPacketCtl, in []int16, ch int) string {
	c := &r.encCtl
	// Copy the inputs; outputs (and fields the encoder does not read)
	// persist like in the C driver's control struct.
	in0 := p.ctl
	c.NChannelsAPI, c.NChannelsInternal, c.APISampleRate = in0.NChannelsAPI, in0.NChannelsInternal, in0.APISampleRate
	c.MaxInternalSampleRate, c.MinInternalSampleRate, c.DesiredInternalSampleRate = in0.MaxInternalSampleRate, in0.MinInternalSampleRate, in0.DesiredInternalSampleRate
	c.PayloadSizeMs, c.BitRate, c.PacketLossPercentage, c.Complexity = in0.PayloadSizeMs, in0.BitRate, in0.PacketLossPercentage, in0.Complexity
	c.UseInBandFEC, c.LBRRCoded, c.UseDTX, c.UseCBR = in0.UseInBandFEC, in0.LBRRCoded, in0.UseDTX, in0.UseCBR
	c.ToMono, c.OpusCanSwitch, c.ReducedDependency = in0.ToMono, in0.OpusCanSwitch, in0.ReducedDependency
	c.MaxBits = p.maxBytes * 8

	if cap(r.pcm) < len(in) {
		r.pcm = make([]float32, len(in))
	}
	pcmf := r.pcm[:len(in)]
	for i, v := range in {
		pcmf[i] = float32(v) * (1.0 / 32768.0)
	}
	for i := range r.buf {
		r.buf[i] = 0
	}
	var enc entcode.Encoder
	enc.Init(r.buf[:p.maxBytes])
	nBytes := int32(p.maxBytes)
	ret := r.enc.Encode(c, pcmf, p.nSamples, &enc, &nBytes, p.prefill, p.activity)
	tell := enc.Tell()
	if p.prefill != 0 {
		return encPacketLine(idx, p, ret, nBytes, tell, enc.Rng, c, 0)
	}
	enc.Done()
	h := fnvBytes(fnvOffset, r.buf[:p.maxBytes])
	line := encPacketLine(idx, p, ret, nBytes, tell, enc.Rng, c, h)

	// Decode.
	d := &r.decCtl
	d.NChannelsAPI = int32(ch)
	d.NChannelsInternal = c.NChannelsInternal
	d.APISampleRate = c.APISampleRate
	d.InternalSampleRate = c.InternalSampleRate
	d.PayloadSizeMs = c.PayloadSizeMs
	nFrames := 1
	if c.PayloadSizeMs > 20 {
		nFrames = c.PayloadSizeMs / 20
	}
	lost := FlagDecodeNormal
	if nBytes <= 0 {
		lost = FlagPacketLost
	}
	var dec entcode.Decoder
	dec.Init(r.buf[:max(nBytes, 0)])
	var b [2]byte
	for f := 0; f < nFrames; f++ {
		n, dret := r.dec.DecodeInt16(d, lost, f == 0, &dec, r.out16[:])
		if dret != 0 {
			line += fmt.Sprintf(" decret %d", dret)
		}
		for i := 0; i < int(n)*ch; i++ {
			binary.LittleEndian.PutUint16(b[:], uint16(r.out16[i]))
			r.decHash = fnvBytes(r.decHash, b[:])
		}
		r.decN += int(n)
	}
	return line
}

// encScenarios returns the scenarios with synthetic input.
func encScenarios() []encScenario {
	var list []encScenario
	seed := uint32(1)
	// Fixed configurations across API rates, channels, frame sizes and
	// complexities.
	for _, rate := range []int{8000, 12000, 16000, 24000, 48000} {
		for _, ch := range []int{1, 2} {
			for ci, cx := range []int{0, 1, 2, 3, 4, 6, 8, 10} {
				pay := [4]int{10, 20, 40, 60}[(ci+ch+rate/8000)%4]
				br := 6000 + ((ci*7919+rate/1000*31+ch*977)%35)*1000
				maxR := internalRates[(ci+ch)%3]
				seed++
				list = append(list, encScenario{
					name: fmt.Sprintf("fix_%d_%d_c%d_p%d_b%d", rate, ch, cx, pay, br), seed: seed,
					apiRate: rate, ch: ch, ms: 1200, internalCh: ch, payloadMs: pay, bitRate: br,
					complexity: cx, maxRate: maxR, minRate: 8000, desRate: maxR, maxBytes: 1275,
					fec: ci & 1, loss: (ci * 4) % 31, dtx: btoi(ci%3 == 2), cbr: btoi(ci%4 == 3),
				})
			}
		}
	}
	// Long enough for the VAD counter to reach its limit (20 s).
	list = append(list, encScenario{
		name: "long_16000_2", seed: 999, apiRate: 16000, ch: 2, ms: 22000,
		internalCh: 2, payloadMs: 20, bitRate: 14000, complexity: 3, maxRate: 16000, minRate: 8000,
		desRate: 16000, maxBytes: 1275, dtx: 1,
	})
	// Randomly varying controls.
	for i := 0; i < 24; i++ {
		rate := []int{8000, 12000, 16000, 24000, 48000}[i%5]
		ch := 1 + (i/5)%2
		seed++
		list = append(list, encScenario{
			name: fmt.Sprintf(
				"vary_%d_%d_%d",
				i,
				rate,
				ch,
			),
			seed:       seed,
			apiRate:    rate,
			ch:         ch,
			ms:         6000,
			internalCh: ch,
			payloadMs:  20,
			bitRate:    20000,
			complexity: 5,
			maxRate:    16000,
			minRate:    8000,
			desRate:    16000,
			maxBytes:   1275,
			vary:       true,
		})
	}
	return list
}

// encScenarioPCM returns the API-rate input of a scenario. For test
// vector scenarios it reads $OPUS_TESTVECTORS/<src>.dec (48 kHz stereo
// int16) and sets sc.ms to its duration.
func encScenarioPCM(t *testing.T, sc *encScenario) []int16 {
	if sc.src == "" {
		n48 := sc.ms*48 + 48*100
		return decimatePCM(synthPCM48(sc.seed*7919+17, n48), sc.apiRate, sc.ch)
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("OPUS_TESTVECTORS"), sc.src+".dec"))
	if err != nil {
		t.Fatal(err)
	}
	pcm48 := make([]int16, len(raw)/2)
	for i := range pcm48 {
		pcm48[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	sc.ms = len(pcm48) / 2 / 48
	return decimatePCM(pcm48, sc.apiRate, sc.ch)
}

// encVectorScenarios returns the scenarios that encode the RFC 8251
// test vector outputs (testvectorNN.dec).
func encVectorScenarios() []encScenario {
	var list []encScenario
	rates := []int{8000, 12000, 16000, 24000, 48000}
	for v := 1; v <= 12; v++ {
		src := fmt.Sprintf("testvector%02d", v)
		seed := uint32(1000 + 10*v)
		list = append(list,
			encScenario{
				name: src + "_vary", src: src, seed: seed, apiRate: rates[v%5], ch: 1 + v%2,
				internalCh: 1 + v%2, payloadMs: 20, bitRate: 20000, complexity: 5, maxRate: 16000,
				minRate: 8000, desRate: 16000, maxBytes: 1275, vary: true,
			},
			encScenario{
				name:       src + "_c10",
				src:        src,
				seed:       seed + 1,
				apiRate:    48000,
				ch:         2,
				internalCh: 2,
				payloadMs:  20,
				bitRate:    24000 + 1000*v,
				complexity: 10,
				maxRate:    16000,
				minRate:    8000,
				desRate:    16000,
				maxBytes:   1275,
			},
			encScenario{
				name:       src + "_c0dtx",
				src:        src,
				seed:       seed + 2,
				apiRate:    16000,
				ch:         1,
				internalCh: 1,
				payloadMs:  60,
				bitRate:    6000 + 500*v,
				complexity: 0,
				maxRate:    12000 + 4000*(v%2),
				minRate:    8000,
				desRate:    12000,
				maxBytes:   1275,
				dtx:        1,
			},
			encScenario{
				name:       src + "_fec",
				src:        src,
				seed:       seed + 3,
				apiRate:    rates[(v+2)%5],
				ch:         2,
				internalCh: 2,
				payloadMs:  [4]int{10, 20, 40, 60}[v%4],
				bitRate:    16000 + 2000*v,
				complexity: v % 11,
				maxRate:    16000,
				minRate:    8000,
				desRate:    internalRates[v%3],
				maxBytes:   1275,
				fec:        1,
				loss:       5 * (v % 7),
				cbr:        v % 2,
			},
		)
	}
	return list
}

// TestWriteEncGoldenInputs writes the PCM and control script of every
// scenario to $OPUS_GOLDEN_INPUTS for the C driver.
func TestWriteEncGoldenInputs(t *testing.T) {
	dir := os.Getenv("OPUS_GOLDEN_INPUTS")
	if dir == "" {
		t.Skip("OPUS_GOLDEN_INPUTS not set")
	}
	write := func(scs []encScenario, listName string) {
		var list strings.Builder
		for _, sc := range scs {
			pcm := encScenarioPCM(t, &sc)
			writeEncInputs(t, dir, &sc, pcm)
			_, _ = fmt.Fprintf(&list, "%s %d\n", sc.name, sc.ch)
		}
		if err := os.WriteFile(
			filepath.Join(dir, listName),
			[]byte(list.String()),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	write(encScenarios(), "list.txt")
	if os.Getenv("OPUS_TESTVECTORS") != "" {
		write(encVectorScenarios(), "list_vec.txt")
	}
}

func writeEncInputs(t *testing.T, dir string, sc *encScenario, pcm []int16) {
	raw := make([]byte, 2*len(pcm))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(raw[2*i:], uint16(v))
	}
	if err := os.WriteFile(filepath.Join(dir, sc.name+".pcm"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, p := range sc.schedule() {
		sb.WriteString(p.scriptLine())
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(
		filepath.Join(dir, sc.name+".txt"),
		[]byte(sb.String()),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

// encGolden is one scenario of testdata/enc_golden.txt.
type encGolden struct {
	lines   int
	hash    string
	samples []string
}

func readEncGoldens(t *testing.T, path string) map[string]*encGolden {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	g := map[string]*encGolden{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		switch fields[0] {
		case "s":
			var n int
			var h string
			if _, err := fmt.Sscanf(fields[2], "%d %s", &n, &h); err != nil {
				t.Fatal(err)
			}
			g[fields[1]] = &encGolden{lines: n, hash: h}
		case "l":
			g[fields[1]].samples = append(g[fields[1]].samples, fields[2])
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return g
}

func hashLines(lines []string) string {
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func checkEncScenario(t *testing.T, name string, lines []string, g *encGolden) {
	t.Helper()
	if g == nil {
		t.Fatalf("%s: no golden", name)
	}
	for i, want := range g.samples {
		if i >= len(lines) || lines[i] != want {
			got := ""
			if i < len(lines) {
				got = lines[i]
			}
			t.Fatalf("%s: line %d:\n got %s\nwant %s", name, i, got, want)
		}
	}
	if len(lines) != g.lines || hashLines(lines) != g.hash {
		t.Fatalf(
			"%s: got %d lines hash %s, want %d lines hash %s",
			name,
			len(lines),
			hashLines(lines),
			g.lines,
			g.hash,
		)
	}
}

// TestEncodeGoldens checks the encoder against the C reference on the
// synthetic scenarios, bit for bit.
func TestEncodeGoldens(t *testing.T) {
	g := readEncGoldens(t, "testdata/enc_golden.txt")
	dump := os.Getenv("SILK_ENC_DUMP")
	var r encRunner
	for _, sc := range encScenarios() {
		if testing.Short() && sc.vary && sc.seed%4 != 0 {
			continue
		}
		pcm := encScenarioPCM(t, &sc)
		lines := r.run(pcm, sc.schedule(), sc.ch)
		if dump != "" {
			if err := os.WriteFile(
				filepath.Join(dump, sc.name+".go.out"),
				[]byte(strings.Join(lines, "\n")+"\n"),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
		}
		checkEncScenario(t, sc.name, lines, g[sc.name])
	}
}

// TestEncodeVectors checks the encoder against the C reference on the
// decoded RFC 8251 test vectors. Set OPUS_TESTVECTORS to the directory
// with testvectorNN.dec to run it.
func TestEncodeVectors(t *testing.T) {
	if os.Getenv("OPUS_TESTVECTORS") == "" {
		t.Skip("OPUS_TESTVECTORS not set")
	}
	g := readEncGoldens(t, "testdata/enc_vectors_golden.txt")
	dump := os.Getenv("SILK_ENC_DUMP")
	var r encRunner
	for _, sc := range encVectorScenarios() {
		pcm := encScenarioPCM(t, &sc)
		lines := r.run(pcm, sc.schedule(), sc.ch)
		if dump != "" {
			if err := os.WriteFile(
				filepath.Join(dump, sc.name+".go.out"),
				[]byte(strings.Join(lines, "\n")+"\n"),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
		}
		checkEncScenario(t, sc.name, lines, g[sc.name])
	}
}

// TestEncodeAllocs checks that Encode does not allocate.
func TestEncodeAllocs(t *testing.T) {
	for _, sc := range []encScenario{
		{
			name: "mono", seed: 5, apiRate: 48000, ch: 1, ms: 4000, internalCh: 1, payloadMs: 20, bitRate: 20000,
			complexity: 10, maxRate: 16000, minRate: 8000, desRate: 16000, maxBytes: 1275, fec: 1, loss: 10,
		},
		{
			name: "stereo", seed: 6, apiRate: 24000, ch: 2, ms: 4000, internalCh: 2, payloadMs: 60, bitRate: 30000,
			complexity: 2, maxRate: 12000, minRate: 8000, desRate: 12000, maxBytes: 1275, dtx: 1, cbr: 1,
		},
		{
			name: "vary", seed: 7, apiRate: 16000, ch: 2, ms: 8000, internalCh: 2, payloadMs: 20, bitRate: 20000,
			complexity: 5, maxRate: 16000, minRate: 8000, desRate: 16000, maxBytes: 1275, vary: true,
		},
	} {
		pcm := encScenarioPCM(t, &sc)
		sched := sc.schedule()
		r := new(encRunner)
		r.enc.Init(sc.ch, &r.encCtl)
		r.dec.Init()
		r.pcm = make([]float32, 2*48*60)
		// Warm up, then measure the remaining packets.
		pos, i := 0, 0
		for ; i < len(sched)/4; i++ {
			r.encodePacket(i, &sched[i], pcm[pos*sc.ch:(pos+sched[i].nSamples)*sc.ch], sc.ch)
			pos += sched[i].nSamples
		}
		var enc entcode.Encoder
		allocs := testing.AllocsPerRun(len(sched)-i-1, func() {
			p := &sched[i]
			in := pcm[pos*sc.ch : (pos+p.nSamples)*sc.ch]
			pcmf := r.pcm[:len(in)]
			for k, v := range in {
				pcmf[k] = float32(v) * (1.0 / 32768.0)
			}
			c := &r.encCtl
			*c = p.ctl
			enc.Init(r.buf[:p.maxBytes])
			nBytes := int32(p.maxBytes)
			r.enc.Encode(c, pcmf, p.nSamples, &enc, &nBytes, p.prefill, p.activity)
			pos += p.nSamples
			i++
		})
		if allocs != 0 {
			t.Errorf("%s: %v allocs per Encode", sc.name, allocs)
		}
	}
}

package opus

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cirocosta/opus/internal/opuscompare"
)

// decOpts are the options of the reference driver
// (tools/oracle/dec/drv.c).
type decOpts struct {
	fs, ch, fmt, cx, gain, loss, reset, plc int
	seed                                    uint32
	fec, ignext, phinv                      bool
}

func parseDecOpts(args []string) (decOpts, error) {
	o := decOpts{fs: 48000, ch: 2, fmt: 16, reset: -1, seed: 1}
	for i := 0; i < len(args); i++ {
		next := func() int {
			i++
			v, _ := strconv.Atoi(args[i])
			return v
		}
		switch args[i] {
		case "-fs":
			o.fs = next()
		case "-ch":
			o.ch = next()
		case "-fmt":
			i++
			if args[i] == "f" {
				o.fmt = 0
			} else {
				o.fmt, _ = strconv.Atoi(args[i])
			}
		case "-cx":
			o.cx = next()
		case "-gain":
			o.gain = next()
		case "-loss":
			o.loss = next()
		case "-seed":
			o.seed = uint32(next())
		case "-reset":
			o.reset = next()
		case "-plc":
			o.plc = next()
		case "-fec":
			o.fec = true
		case "-ignext":
			o.ignext = true
		case "-phinv":
			o.phinv = true
		default:
			return o, fmt.Errorf("bad option %q", args[i])
		}
	}
	return o, nil
}

// lossGen is the LCG loss generator of the driver.
type lossGen uint32

func (g *lossGen) lost(pct int) bool {
	*g = *g*1103515245 + 12345
	return pct > 0 && int((uint32(*g)>>16)%100) < pct
}

// pcmBufs holds the output buffers and hashes the samples like the driver
// (FNV-1a 64 of the little-endian sample bytes).
type pcmBufs struct {
	fmt int
	i16 []int16
	i24 []int32
	f32 []float32
	b   []byte
}

func newPCMBufs(fmt, n int) *pcmBufs {
	return &pcmBufs{
		fmt: fmt, i16: make([]int16, n), i24: make([]int32, n),
		f32: make([]float32, n), b: make([]byte, 4*n),
	}
}

func (p *pcmBufs) hash(n int) uint64 {
	if n <= 0 {
		return 0
	}
	var b []byte
	switch p.fmt {
	case 16:
		b = p.b[:2*n]
		for i := 0; i < n; i++ {
			binary.LittleEndian.PutUint16(b[2*i:], uint16(p.i16[i]))
		}
	case 24:
		b = p.b[:4*n]
		for i := 0; i < n; i++ {
			binary.LittleEndian.PutUint32(b[4*i:], uint32(p.i24[i]))
		}
	default:
		b = p.b[:4*n]
		for i := 0; i < n; i++ {
			binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(p.f32[i]))
		}
	}
	h := fnv.New64a()
	h.Write(b)
	return h.Sum64()
}

func retCode(n int, err error) int {
	if err != nil {
		var e Error
		if ok := asError(err, &e); ok {
			return int(e)
		}
		return -99
	}
	return n
}

func asError(err error, e *Error) bool {
	v, ok := err.(Error)
	if ok {
		*e = v
	}
	return ok
}

// readBitPackets reads a stream in the opus_demo format (4-byte BE
// length, 4-byte BE final range, payload).
func readBitPackets(path string) (pkts [][]byte, rngs []uint32, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	for len(data) >= 8 {
		n := int(binary.BigEndian.Uint32(data))
		r := binary.BigEndian.Uint32(data[4:])
		data = data[8:]
		if n > len(data) || n > 1500 {
			break
		}
		pkts = append(pkts, data[:n])
		rngs = append(rngs, r)
		data = data[n:]
	}
	return pkts, rngs, nil
}

// runDecCase is mode_dec of the driver: an opus_demo style decode loop
// with packet loss and FEC.
func runDecCase(w io.Writer, path string, o decOpts) error {
	pkts, _, err := readBitPackets(path)
	if err != nil {
		return err
	}
	dec, err := NewDecoder(o.fs, o.ch)
	if err != nil {
		return err
	}
	if err := dec.SetComplexity(o.cx); err != nil {
		return err
	}
	if o.gain != 0 {
		if err := dec.SetGain(o.gain); err != nil {
			return err
		}
	}
	if o.ignext {
		dec.SetIgnoreExtensions(true)
	}
	if o.phinv {
		dec.SetPhaseInversionDisabled(true)
	}
	maxf := maxFrameSamples48k * o.fs / 48000
	p := newPCMBufs(o.fmt, maxFrameSamples48k*2)
	decode := func(data []byte, fs int, fec bool) int {
		switch o.fmt {
		case 16:
			return retCode(dec.Decode(data, p.i16, fs, fec))
		case 24:
			return retCode(dec.Decode24(data, p.i24, fs, fec))
		}
		return retCode(dec.DecodeFloat(data, p.f32, fs, fec))
	}
	g := lossGen(o.seed)
	lostCount := 0
	for idx, data := range pkts {
		lost := len(data) == 0 || g.lost(o.loss)
		run := 0
		if lost {
			lostCount++
		} else {
			run = 1 + lostCount
		}
		for fr := 0; fr < run; fr++ {
			var ret, kind int
			if fr == lostCount-1 && o.fec && opusPacketHasLBRR(data) > 0 {
				os := dec.LastPacketDuration()
				if o.plc != 0 {
					os = o.plc * o.fs / 400
				}
				ret = decode(data, os, true)
				kind = 1
			} else if fr < lostCount {
				os := dec.LastPacketDuration()
				if o.plc != 0 {
					os = o.plc * o.fs / 400
				}
				ret = decode(nil, os, false)
				kind = 3
			} else {
				ret = decode(data, maxf, false)
			}
			if _, err := fmt.Fprintf(
				w,
				"%d %d %d %08x %d %d %d %016x\n",
				idx,
				kind,
				ret,
				dec.FinalRange(),
				dec.LastPacketDuration(),
				dec.Bandwidth(),
				dec.Pitch(),
				p.hash(ret*o.ch),
			); err != nil {
				return err
			}
		}
		if !lost {
			lostCount = 0
		}
		if idx == o.reset {
			dec.Reset()
		}
	}
	return nil
}

// msStream is a multistream packet file written by the driver (msenc).
type msStream struct {
	projection               bool
	fs, ch, streams, coupled int
	mapping, matrix          []byte
	pkts                     [][]byte
}

func readMSStream(path string) (*msStream, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(d) < 12 || string(d[:4]) != "OPMS" {
		return nil, fmt.Errorf("%s: bad header", path)
	}
	s := &msStream{
		projection: d[4] == 1, fs: int(binary.BigEndian.Uint32(d[5:])),
		ch: int(d[9]), streams: int(d[10]), coupled: int(d[11]),
	}
	d = d[12:]
	if s.projection {
		n := int(binary.BigEndian.Uint32(d))
		s.matrix = d[4 : 4+n]
		d = d[4+n:]
	} else {
		s.mapping = d[:s.ch]
		d = d[s.ch:]
	}
	for len(d) >= 4 {
		n := int(binary.BigEndian.Uint32(d))
		s.pkts = append(s.pkts, d[4:4+n])
		d = d[4+n:]
	}
	return s, nil
}

// msDecoderAPI is the decoding interface shared by MSDecoder and
// ProjectionDecoder.
type msDecoderAPI interface {
	Decode(data []byte, pcm []int16, frameSize int, decodeFEC bool) (int, error)
	Decode24(data []byte, pcm []int32, frameSize int, decodeFEC bool) (int, error)
	DecodeFloat(data []byte, pcm []float32, frameSize int, decodeFEC bool) (int, error)
	FinalRange() uint32
	LastPacketDuration() int
	Bandwidth() int
	Reset()
	SetComplexity(int) error
	SetGain(int) error
	SetPhaseInversionDisabled(bool)
}

// runMSDecCase is mode_msdec of the driver.
func runMSDecCase(w io.Writer, path string, o decOpts) error {
	s, err := readMSStream(path)
	if err != nil {
		return err
	}
	var dec msDecoderAPI
	if s.projection {
		dec, err = NewProjectionDecoder(o.fs, s.ch, s.streams, s.coupled, s.matrix)
	} else {
		dec, err = NewMSDecoder(o.fs, s.ch, s.streams, s.coupled, s.mapping)
	}
	if err != nil {
		return err
	}
	if err := dec.SetComplexity(o.cx); err != nil {
		return err
	}
	if o.gain != 0 {
		if err := dec.SetGain(o.gain); err != nil {
			return err
		}
	}
	if o.phinv {
		dec.SetPhaseInversionDisabled(true)
	}
	p := newPCMBufs(o.fmt, maxFrameSamples48k*s.ch)
	g := lossGen(o.seed)
	for idx, data := range s.pkts {
		lost := g.lost(o.loss)
		fsz := maxFrameSamples48k * o.fs / 48000
		if lost {
			fsz = o.fs / 50
			data = nil
		}
		var ret int
		switch o.fmt {
		case 16:
			ret = retCode(dec.Decode(data, p.i16, fsz, false))
		case 24:
			ret = retCode(dec.Decode24(data, p.i24, fsz, false))
		default:
			ret = retCode(dec.DecodeFloat(data, p.f32, fsz, false))
		}
		if _, err := fmt.Fprintf(w, "%d %d %08x %d %d %016x\n", idx, ret, dec.FinalRange(),
			dec.LastPacketDuration(), dec.Bandwidth(), p.hash(ret*s.ch)); err != nil {
			return err
		}
		if idx == o.reset {
			dec.Reset()
		}
	}
	return nil
}

// TestDecoderGolden replays the cases of testdata/decoder_golden.txt and
// compares a digest of every decoder call (return value, final range,
// controls and an FNV hash of the output) with the libopus reference
// build (tools/oracle/dec). Cases on the RFC 8251 vectors ($V) need
// OPUS_TESTVECTORS. Set OPUS_DEC_DUMP to a directory to write the lines
// of failing cases.
func TestDecoderGolden(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "decoder_golden.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	vdir := os.Getenv("OPUS_TESTVECTORS")
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		n++
		fields := strings.Fields(line)
		wantLines, _ := strconv.Atoi(fields[0])
		wantHash, mode, stream := fields[1], fields[2], fields[3]
		name := fmt.Sprintf("%02d_%s_%s", n, filepath.Base(stream), strings.Join(fields[4:], ""))
		caseNum := n
		t.Run(name, func(t *testing.T) {
			if strings.HasPrefix(stream, "$V/") {
				if vdir == "" {
					t.Skip("OPUS_TESTVECTORS not set")
				}
				stream = filepath.Join(vdir, stream[3:])
			}
			if testing.Short() && strings.Contains(stream, "testvector") {
				t.Skip("short")
			}
			if testing.Short() && caseNum%3 != 1 {
				// Short mode runs every third case (race builds are slow).
				t.Skip("short")
			}
			o, err := parseDecOpts(fields[4:])
			if err != nil {
				t.Fatal(err)
			}
			var sb strings.Builder
			if mode == "dec" {
				err = runDecCase(&sb, stream, o)
			} else {
				err = runMSDecCase(&sb, stream, o)
			}
			if err != nil {
				t.Fatal(err)
			}
			out := sb.String()
			sum := sha256.Sum256([]byte(out))
			got := hex.EncodeToString(sum[:])[:32]
			gotLines := strings.Count(out, "\n")
			if got != wantHash || gotLines != wantLines {
				t.Errorf("got %d lines %s, want %d lines %s", gotLines, got, wantLines, wantHash)
				if dir := os.Getenv("OPUS_DEC_DUMP"); dir != "" {
					if err := os.WriteFile(
						filepath.Join(dir, fmt.Sprintf("c%d.go.txt", n)),
						[]byte(out),
						0o644,
					); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}

// demoDecode is the decode-only loop of opus_demo ("opus_demo -d fs ch
// in.bit out"): opus_decode24 output converted to 16-bit PCM. It checks
// the final range of every packet against the one recorded in the
// stream, as opus_demo does, and returns the output bytes.
func demoDecode(t *testing.T, pkts [][]byte, rngs []uint32, fs, ch int, h hash.Hash) []int16 {
	dec, err := NewDecoder(fs, ch)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]int32, 2*48000*ch)
	var pcm []int16
	var b [2]byte
	lostPrev := true
	lostCount := 0
	for i, data := range pkts {
		lost := len(data) == 0
		run := 0
		if lost {
			lostCount++
		} else {
			run = 1 + lostCount
		}
		for fr := 0; fr < run; fr++ {
			var n int
			if fr < lostCount {
				n = retCode(dec.Decode24(nil, out, dec.LastPacketDuration(), false))
			} else {
				n = retCode(dec.Decode24(data, out, 2*48000, false))
			}
			if n <= 0 {
				t.Errorf("packet %d: decode returned %d", i, n)
				continue
			}
			for _, s := range out[:n*ch] {
				s = min(max(s, -0x007fff00), 0x007fff00)
				v := int16((s + 128) >> 8)
				pcm = append(pcm, v)
				binary.LittleEndian.PutUint16(b[:], uint16(v))
				h.Write(b[:])
			}
		}
		if rngs[i] != 0 && !lost && !lostPrev && dec.FinalRange() != rngs[i] {
			t.Fatalf("packet %d: final range %08x, want %08x", i, dec.FinalRange(), rngs[i])
		}
		lostPrev = lost
		if !lost {
			lostCount = 0
		}
	}
	return pcm
}

func readPCM16(path string) ([]int16, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	x := make([]int16, len(d)/2)
	for i := range x {
		x[i] = int16(binary.LittleEndian.Uint16(d[2*i:]))
	}
	return x, nil
}

// TestRFC8251 decodes the RFC 8251 test vectors in $OPUS_TESTVECTORS
// like opus_demo at every decoder rate in mono and stereo. It checks the
// final range of every packet, compares the output bit-exactly with the
// scalar libopus opus_demo (testdata/decoder_vectors.txt), and at 48 kHz
// runs opus_compare against the reference decodings (.dec and m.dec;
// either may pass, as in run_vectors.sh).
func TestRFC8251(t *testing.T) {
	dir := os.Getenv("OPUS_TESTVECTORS")
	if dir == "" {
		t.Skip("OPUS_TESTVECTORS not set")
	}
	if testing.Short() {
		t.Skip("slow; skipped in short mode")
	}
	f, err := os.Open(filepath.Join("testdata", "decoder_vectors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		var name, want string
		var rate, ch, nbytes int
		if _, err := fmt.Sscanf(
			line,
			"%s %d %d %d %s",
			&name,
			&rate,
			&ch,
			&nbytes,
			&want,
		); err != nil {
			t.Fatal(err)
		}
		t.Run(fmt.Sprintf("%s_%d_%d", name, rate, ch), func(t *testing.T) {
			if testing.Short() && rate != 48000 {
				t.Skip("short")
			}
			pkts, rngs, err := readBitPackets(filepath.Join(dir, name+".bit"))
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.New()
			pcm := demoDecode(t, pkts, rngs, rate, ch, h)
			got := hex.EncodeToString(h.Sum(nil))[:32]
			if got != want || 2*len(pcm) != nbytes {
				t.Errorf(
					"output %d bytes %s, want %d bytes %s (scalar opus_demo)",
					2*len(pcm),
					got,
					nbytes,
					want,
				)
			}
			if rate != 48000 {
				return
			}
			best := opuscompare.Result{Quality: math.Inf(-1)}
			for _, suffix := range []string{".dec", "m.dec"} {
				ref, err := readPCM16(filepath.Join(dir, name+suffix))
				if err != nil {
					t.Fatal(err)
				}
				r, err := opuscompare.Compare(ref, pcm, ch == 2, rate)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s%s: %v", name, suffix, r)
				if r.Quality > best.Quality {
					best = r
				}
			}
			if !best.Pass {
				t.Errorf("opus_compare: %v", best)
			}
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMSDecoderOmittedChannels(t *testing.T) {
	const rate, frame = 48000, 960
	encoder, err := NewMSEncoder(rate, 2, 2, 0, []byte{0, 1}, ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	full, err := NewMSDecoder(rate, 2, 2, 0, []byte{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	omitted, err := NewMSDecoder(rate, 1, 2, 0, []byte{0})
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]int16, 2*frame)
	for i := 0; i < frame; i++ {
		pcm[2*i] = int16((i%337 - 168) * 80)
		pcm[2*i+1] = int16((i%193 - 96) * 90)
	}
	packet := make([]byte, 3000)
	n, err := encoder.Encode(pcm, frame, packet)
	if err != nil {
		t.Fatal(err)
	}
	all, one := make([]int16, 2*frame), make([]int16, frame)
	if _, err := full.Decode(packet[:n], all, frame, false); err != nil {
		t.Fatal(err)
	}
	if _, err := omitted.Decode(packet[:n], one, frame, false); err != nil {
		t.Fatal(err)
	}
	for i, sample := range one {
		if sample != all[2*i] {
			t.Fatalf("sample %d: got %d, want %d", i, sample, all[2*i])
		}
	}
	if omitted.FinalRange() != full.FinalRange() {
		t.Fatal("omitted channels changed the entropy range")
	}
}

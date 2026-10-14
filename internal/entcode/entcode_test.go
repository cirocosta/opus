package entcode

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"math/bits"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Operation kinds. They match the C driver that generated
// testdata/golden.txt.
const (
	opEncode = iota
	opEncodeBin
	opBitLogp
	opIcdf
	opIcdf16
	opUint
	opBits
	opLaplace
	opLaplaceP0
	opShrink
	opPatch
	numCodingOps = opLaplaceP0 + 1
)

type op struct {
	kind int
	p    []int64
	// Expected results: encoder return value, encoder tell and tell_frac
	// after the operation, then the same for the decoder.
	want [6]int64
}

type goldenCase struct {
	name     string
	flags    int // 1: round trip expected, 2: decode-only input
	storage  int
	input    []byte
	ops      []op
	enc      []int64 // error offs end_offs storage rng val tell frac
	bytes    []byte
	dec      []int64 // error rng tell frac
	lineBase int
}

func parseInts(t *testing.T, fields []string) []int64 {
	t.Helper()
	out := make([]int64, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		out[i] = v
	}
	return out
}

func loadGolden(t *testing.T) []*goldenCase {
	t.Helper()
	f, err := os.Open("testdata/golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cases []*goldenCase
	var c *goldenCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "case":
			v := parseInts(t, fields[2:])
			c = &goldenCase{name: fields[1], flags: int(v[0]), storage: int(v[1]), lineBase: line}
		case "input":
			c.input, err = hex.DecodeString(fields[1])
			if err != nil {
				t.Fatal(err)
			}
		case "op":
			eq := len(fields) - 7
			if fields[eq] != "=" {
				t.Fatalf("line %d: malformed op", line)
			}
			v := parseInts(t, fields[1:eq])
			o := op{kind: int(v[0]), p: v[1:]}
			copy(o.want[:], parseInts(t, fields[eq+1:]))
			c.ops = append(c.ops, o)
		case "enc":
			c.enc = parseInts(t, fields[1:])
		case "bytes":
			c.bytes, err = hex.DecodeString(strings.TrimSuffix(fields[1], "-"))
			if err != nil {
				t.Fatal(err)
			}
		case "dec":
			c.dec = parseInts(t, fields[1:])
		case "end":
			cases = append(cases, c)
			c = nil
		default:
			t.Fatalf("line %d: unknown record %q", line, fields[0])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

func icdf8(p []int64) []uint8 {
	n := int(p[2])
	t := make([]uint8, n)
	for i := range t {
		t[i] = uint8(p[3+i])
	}
	return t
}

func icdf16(p []int64) []uint16 {
	n := int(p[2])
	t := make([]uint16, n)
	for i := range t {
		t[i] = uint16(p[3+i])
	}
	return t
}

// encodeOp applies o to e and returns the encoder result (the coded
// value for Laplace, 0 otherwise).
func encodeOp(e *Encoder, o *op) int64 {
	p := o.p
	switch o.kind {
	case opEncode:
		e.Encode(uint32(p[0]), uint32(p[1]), uint32(p[2]))
	case opEncodeBin:
		e.EncodeBin(uint32(p[0]), uint32(p[1]), uint(p[2]))
	case opBitLogp:
		e.EncBitLogp(int(p[0]), uint(p[1]))
	case opIcdf:
		e.EncIcdf(int(p[0]), icdf8(p), uint(p[1]))
	case opIcdf16:
		e.EncIcdf16(int(p[0]), icdf16(p), uint(p[1]))
	case opUint:
		e.EncUint(uint32(p[0]), uint32(p[1]))
	case opBits:
		e.EncBits(uint32(p[0]), uint(p[1]))
	case opLaplace:
		return int64(e.LaplaceEncode(int(p[0]), uint32(p[1]), int(p[2])))
	case opLaplaceP0:
		e.LaplaceEncodeP0(int(p[0]), uint16(p[1]), uint16(p[2]))
	case opShrink:
		e.Shrink(uint32(p[0]))
	case opPatch:
		e.PatchInitialBits(uint32(p[0]), uint32(p[1]))
	default:
		panic(fmt.Sprintf("unknown op kind %d", o.kind))
	}
	return 0
}

// decodeOp decodes the counterpart of o from d and returns the decoded
// value.
func decodeOp(d *Decoder, o *op) int64 {
	p := o.p
	switch o.kind {
	case opEncode:
		v := d.Decode(uint32(p[2]))
		d.Update(uint32(p[0]), uint32(p[1]), uint32(p[2]))
		return int64(v)
	case opEncodeBin:
		v := d.DecodeBin(uint(p[2]))
		d.Update(uint32(p[0]), uint32(p[1]), 1<<uint(p[2]))
		return int64(v)
	case opBitLogp:
		return int64(d.DecBitLogp(uint(p[1])))
	case opIcdf:
		return int64(d.DecIcdf(icdf8(p), uint(p[1])))
	case opIcdf16:
		return int64(d.DecIcdf16(icdf16(p), uint(p[1])))
	case opUint:
		return int64(d.DecUint(uint32(p[1])))
	case opBits:
		return int64(d.DecBits(uint(p[1])))
	case opLaplace:
		return int64(d.LaplaceDecode(uint32(p[1]), int(p[2])))
	case opLaplaceP0:
		return int64(d.LaplaceDecodeP0(uint16(p[1]), uint16(p[2])))
	}
	return 0
}

// checkRoundTrip reports whether the decoded value v matches the value
// that o encoded. eret is the encoder result of o.
func checkRoundTrip(o *op, v, eret int64) bool {
	p := o.p
	switch o.kind {
	case opEncode, opEncodeBin:
		return p[0] <= v && v < p[1]
	case opBitLogp:
		return (p[0] != 0) == (v != 0)
	case opIcdf, opIcdf16, opUint, opBits:
		return v == p[0]
	case opLaplace:
		return v == eret
	case opLaplaceP0:
		return v == p[0]
	}
	return true
}

// TestGolden replays testdata/golden.txt. A C driver linked against the
// libopus 1.6.1 celt/entcode.c, entenc.c, entdec.c, and laplace.c sources
// wrote it (tools/oracle/entcode/driver.c). Each case lists
// the initial storage, the operations with the encoder and decoder
// results and tell values after each one, the encoder state after
// ec_enc_done, the output bytes, and the final decoder state. The buffer
// starts filled with 0xA5 so that the clearing in Done is checked too.
func TestGolden(t *testing.T) {
	for _, c := range loadGolden(t) {
		t.Run(c.name, func(t *testing.T) {
			var buf []byte
			if c.storage > 0 {
				buf = bytes.Repeat([]byte{0xA5}, c.storage)
			}
			storage := c.storage
			eret := make([]int64, len(c.ops))
			if c.flags&2 != 0 {
				copy(buf, c.input)
			} else {
				var e Encoder
				e.Init(buf)
				for i := range c.ops {
					o := &c.ops[i]
					eret[i] = encodeOp(&e, o)
					got := [3]int64{eret[i], int64(e.Tell()), int64(e.TellFrac())}
					if got != [3]int64(o.want[:3]) {
						t.Fatalf("line %d: op %d kind %d: encoder (ret, tell, frac) = %v, want %v",
							c.lineBase+i+1, i, o.kind, got, o.want[:3])
					}
				}
				e.Done()
				got := []int64{int64(e.Error), int64(e.Offs), int64(e.EndOffs), int64(e.Storage),
					int64(e.Rng), int64(e.Val), int64(e.Tell()), int64(e.TellFrac())}
				if fmt.Sprint(got) != fmt.Sprint(c.enc) {
					t.Fatalf("after Done (error offs end_offs storage rng val tell frac) = %v, want %v", got, c.enc)
				}
				if e.RangeBytes() != e.Offs {
					t.Fatalf("RangeBytes = %d, want %d", e.RangeBytes(), e.Offs)
				}
				storage = int(e.Storage)
				if !bytes.Equal(buf[:storage], c.bytes) {
					t.Fatalf("bytes mismatch\n got %x\nwant %x", buf[:storage], c.bytes)
				}
			}
			var d Decoder
			d.Init(buf[:storage])
			for i := range c.ops {
				o := &c.ops[i]
				v := decodeOp(&d, o)
				got := [3]int64{v, int64(d.Tell()), int64(d.TellFrac())}
				if got != [3]int64(o.want[3:]) {
					t.Fatalf("line %d: op %d kind %d: decoder (ret, tell, frac) = %v, want %v",
						c.lineBase+i+1, i, o.kind, got, o.want[3:])
				}
				if c.flags&1 != 0 {
					if !checkRoundTrip(o, v, eret[i]) {
						t.Fatalf("op %d kind %d %v: decoded %d", i, o.kind, o.p, v)
					}
					if o.want[1] != o.want[4] || o.want[2] != o.want[5] {
						t.Fatalf("op %d: encoder and decoder tell differ: %v", i, o.want)
					}
				}
			}
			got := []int64{int64(d.Error), int64(d.Rng), int64(d.Tell()), int64(d.TellFrac())}
			if fmt.Sprint(got) != fmt.Sprint(c.dec) {
				t.Fatalf("final decoder (error rng tell frac) = %v, want %v", got, c.dec)
			}
		})
	}
}

// genOp builds a random valid coding operation of the given kind. It
// follows the parameter ranges of the C golden driver.
func genOp(kind int, rnd func() uint32) op {
	o := op{kind: kind}
	add := func(v ...int64) { o.p = append(o.p, v...) }
	table := func(ftb uint32) {
		t := uint32(1) << ftb
		n := 2 + rnd()%7
		n = min(n, t)
		add(int64(rnd()%n), int64(ftb), int64(n))
		for i := range n - 1 {
			mx, mn := t-1, n-1-i
			v := mn + rnd()%(mx-mn+1)
			add(int64(v))
			t = v
		}
		add(0)
	}
	switch kind {
	case opEncode:
		ft := min(2+rnd()%((1<<(1+rnd()%16))-1), 65536)
		fl := rnd() % ft
		add(int64(fl), int64(fl+1+rnd()%(ft-fl)), int64(ft))
	case opEncodeBin:
		b := 1 + rnd()%16
		ft := uint32(1) << b
		fl := rnd() % ft
		add(int64(fl), int64(fl+1+rnd()%(ft-fl)), int64(b))
	case opBitLogp:
		add(int64(rnd()%2), int64(1+rnd()%15))
	case opIcdf:
		table(1 + rnd()%8)
	case opIcdf16:
		table(1 + rnd()%15)
	case opUint:
		b := 1 + rnd()%32
		ft := rnd() >> (32 - b)
		if rnd()%8 == 0 {
			ft = 0xFFFFFFFF
		}
		ft = max(ft, 2)
		add(int64(rnd()%ft), int64(ft))
	case opBits:
		nb := 1 + rnd()%25
		add(int64(rnd()&(1<<nb-1)), int64(nb))
	case opLaplace:
		v := int64(rnd()%41) - 20
		if rnd()%8 == 0 {
			v = int64(100 + rnd()%2000)
			if rnd()%2 == 0 {
				v = -v
			}
		}
		add(v, int64((1+rnd()%200)<<7), int64((1+rnd()%179)<<6))
	case opLaplaceP0:
		add(int64(rnd()%121)-60, int64(1+rnd()%32766), int64(rnd()%32768))
	}
	return o
}

// roundTrip encodes ops into a buffer of the given size, decodes them,
// and checks the decoded values and the tell values when the encoder
// reports no error. It must never panic.
func roundTrip(t *testing.T, ops []op, storage int) {
	t.Helper()
	buf := make([]byte, storage)
	var e Encoder
	e.Init(buf)
	eret := make([]int64, len(ops))
	tells := make([][2]int64, len(ops))
	for i := range ops {
		eret[i] = encodeOp(&e, &ops[i])
		tells[i] = [2]int64{int64(e.Tell()), int64(e.TellFrac())}
		if i > 0 && tells[i][1] < tells[i-1][1] {
			t.Fatalf("op %d: TellFrac decreased", i)
		}
	}
	tell := e.Tell()
	e.Done()
	if e.Tell() != tell {
		t.Fatalf("Tell changed after Done: %d, want %d", e.Tell(), tell)
	}
	var d Decoder
	d.Init(buf[:e.Storage])
	for i := range ops {
		v := decodeOp(&d, &ops[i])
		if e.Error != 0 {
			continue
		}
		if !checkRoundTrip(&ops[i], v, eret[i]) {
			t.Fatalf("op %d kind %d %v: decoded %d", i, ops[i].kind, ops[i].p, v)
		}
		got := [2]int64{int64(d.Tell()), int64(d.TellFrac())}
		if got != tells[i] {
			t.Fatalf("op %d: decoder tell %v, encoder tell %v", i, got, tells[i])
		}
	}
	if e.Error == 0 && d.Error != 0 {
		t.Fatalf("decoder error %d on a valid stream", d.Error)
	}
}

func TestRandomRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for iter := range 2000 {
		n := 1 + r.IntN(300)
		ops := make([]op, n)
		for i := range ops {
			ops[i] = genOp(r.IntN(numCodingOps), r.Uint32)
		}
		storage := 4 * 1024
		if iter%4 == 0 {
			storage = r.IntN(64)
		}
		roundTrip(t, ops, storage)
	}
}

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte{0}, uint16(100))
	f.Add([]byte("entcode fuzz seed with a few more bytes"), uint16(3))
	f.Add(bytes.Repeat([]byte{0xff, 0x00, 0x55}, 50), uint16(1275))
	f.Fuzz(func(t *testing.T, data []byte, storage uint16) {
		pos := 0
		rnd := func() uint32 {
			var w [4]byte
			pos += copy(w[:], data[min(pos, len(data)):])
			return uint32(w[0]) | uint32(w[1])<<8 | uint32(w[2])<<16 | uint32(w[3])<<24
		}
		var ops []op
		for pos < len(data) && len(ops) < 512 {
			ops = append(ops, genOp(int(rnd()%numCodingOps), rnd))
		}
		roundTrip(t, ops, int(storage%4096))
	})
}

func FuzzDecodeGarbage(f *testing.F) {
	f.Add([]byte{0x12, 0x34, 0x56}, uint64(1))
	f.Fuzz(func(t *testing.T, data []byte, seed uint64) {
		r := rand.New(rand.NewPCG(seed, 0))
		var d Decoder
		d.Init(data)
		for range 200 {
			o := genOp(r.IntN(numCodingOps), r.Uint32)
			decodeOp(&d, &o)
			if d.Tell() < 0 {
				t.Fatal("negative Tell")
			}
		}
	})
}

// tellFracExact is the bit-exact reference ec_tell_frac from the #else
// branch of celt/entcode.c.
func tellFracExact(c *Ctx) uint32 {
	nbits := uint32(c.NbitsTotal) << BitRes
	l := Ilog(c.Rng)
	r := c.Rng >> uint(l-16)
	for range BitRes {
		r = r * r >> 15
		b := int(r >> 16)
		l = l<<1 | b
		r >>= uint(b)
	}
	return nbits - uint32(l)
}

func TestTellFracMatchesExact(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	check := func(rng uint32) {
		c := Ctx{Rng: rng, NbitsTotal: 100}
		if got, want := c.TellFrac(), tellFracExact(&c); got != want {
			t.Fatalf("rng %#x: TellFrac = %d, want %d", rng, got, want)
		}
	}
	for rng := uint32(codeBot + 1); rng < codeBot+1<<16; rng++ {
		check(rng)
	}
	for range 1 << 20 {
		check(codeBot + 1 + r.Uint32()%(codeTop-codeBot))
	}
	check(codeTop)
}

// ilogFallback is the portable ec_ilog from celt/entcode.c.
func ilogFallback(v uint32) int {
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	ret := b2i(v != 0)
	m := b2i(v&0xFFFF0000 != 0) << 4
	v >>= uint(m)
	ret |= m
	m = b2i(v&0xFF00 != 0) << 3
	v >>= uint(m)
	ret |= m
	m = b2i(v&0xF0 != 0) << 2
	v >>= uint(m)
	ret |= m
	m = b2i(v&0xC != 0) << 1
	v >>= uint(m)
	ret |= m
	ret += b2i(v&0x2 != 0)
	return ret
}

func TestIlog(t *testing.T) {
	for i := range 33 {
		var v uint32
		if i > 0 {
			v = 1 << (i - 1)
		}
		for _, x := range []uint32{v, v | v>>1, v - 1, bits.Reverse32(v)} {
			if Ilog(x) != ilogFallback(x) {
				t.Fatalf("Ilog(%#x) = %d, want %d", x, Ilog(x), ilogFallback(x))
			}
		}
	}
}

// TestEncoderCopy checks that a struct copy snapshots the state, which
// silk relies on when it retries a frame encode.
func TestEncoderCopy(t *testing.T) {
	buf := make([]byte, 64)
	var e Encoder
	e.Init(buf)
	e.EncUint(1234, 5000)
	snap := e
	e.EncBits(0x155, 9)
	e = snap
	e.EncBits(0x0aa, 9)
	e.Done()
	var d Decoder
	d.Init(buf)
	if v := d.DecUint(5000); v != 1234 {
		t.Fatalf("DecUint = %d", v)
	}
	if v := d.DecBits(9); v != 0x0aa {
		t.Fatalf("DecBits = %#x", v)
	}
}

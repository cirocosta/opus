package celt

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fnvHash is the FNV-1a 64 hash that the C drivers of the goldens
// (tools/oracle/celt) use. When dump is set, it also records the hashed bytes, so a
// test can locate the first difference against a C dump.
type fnvHash struct {
	h    uint64
	dump []byte
	keep bool
}

func newFNV(keep bool) *fnvHash { return &fnvHash{h: 1469598103934665603, keep: keep} }

func (f *fnvHash) bytes(b []byte) {
	for _, c := range b {
		f.h ^= uint64(c)
		f.h *= 1099511628211
	}
	if f.keep {
		f.dump = append(f.dump, b...)
	}
}

func (f *fnvHash) i32(v int32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	f.bytes(b[:])
}

func (f *fnvHash) u32(v uint32) { f.i32(int32(v)) }

func (f *fnvHash) f32s(v []float32) {
	var b [4]byte
	for _, x := range v {
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(x))
		f.bytes(b[:])
	}
}

func lcg(x uint32) uint32 { return x*1664525 + 1013904223 }

// lost is the loss pattern of the C driver.
func lost(pattern, p int) bool {
	switch pattern {
	case 0:
		return false
	case 1:
		r := lcg(uint32(p) * 2654435761)
		if (r>>16)%100 < 15 {
			return true
		}
		return p%97 >= 50 && p%97 < 62
	}
	return p%211 >= 100 && p%211 < 160
}

func parseSize(data []byte) (int, int) {
	if len(data) < 1 {
		return -1, -1
	}
	if data[0] < 252 {
		return 1, int(data[0])
	}
	if len(data) < 2 {
		return -1, -1
	}
	return 2, 4*int(data[1]) + int(data[0])
}

// parsePacket is a minimal opus_packet_parse for the test vectors.
func parsePacket(data []byte) ([][]byte, error) {
	if len(data) < 1 {
		return nil, errors.New("empty")
	}
	toc := data[0]
	data = data[1:]
	switch toc & 3 {
	case 0:
		return [][]byte{data}, nil
	case 1:
		if len(data)&1 != 0 {
			return nil, errors.New("odd")
		}
		h := len(data) / 2
		return [][]byte{data[:h], data[h:]}, nil
	case 2:
		n, sz := parseSize(data)
		if n < 0 || sz > len(data)-n {
			return nil, errors.New("bad size")
		}
		data = data[n:]
		return [][]byte{data[:sz], data[sz:]}, nil
	}
	if len(data) < 1 {
		return nil, errors.New("short")
	}
	ch := data[0]
	data = data[1:]
	count := int(ch & 0x3f)
	if count == 0 {
		return nil, errors.New("count")
	}
	pad := 0
	if ch&0x40 != 0 {
		for {
			if len(data) < 1 {
				return nil, errors.New("pad")
			}
			p := int(data[0])
			data = data[1:]
			if p == 255 {
				pad += 254
			} else {
				pad += p
				break
			}
		}
	}
	if pad > len(data) {
		return nil, errors.New("pad")
	}
	data = data[:len(data)-pad]
	frames := make([][]byte, count)
	if ch&0x80 != 0 {
		sizes := make([]int, count)
		for i := 0; i < count-1; i++ {
			n, sz := parseSize(data)
			if n < 0 {
				return nil, errors.New("vbr size")
			}
			data = data[n:]
			sizes[i] = sz
		}
		for i := 0; i < count-1; i++ {
			if sizes[i] > len(data) {
				return nil, errors.New("vbr")
			}
			frames[i] = data[:sizes[i]]
			data = data[sizes[i]:]
		}
		frames[count-1] = data
		return frames, nil
	}
	if len(data)%count != 0 {
		return nil, errors.New("cbr")
	}
	sz := len(data) / count
	for i := range frames {
		frames[i] = data[i*sz : (i+1)*sz]
	}
	return frames, nil
}

// golden is one line of a golden file: a scenario name, key=value
// parameters and the expected hash.
type golden struct {
	line   string
	kind   string
	params map[string]string
}

func (g golden) int(t *testing.T, k string) int {
	t.Helper()
	v, err := strconv.Atoi(g.params[k])
	if err != nil {
		t.Fatalf("%s: bad %s", g.line, k)
	}
	return v
}

func readGoldens(t *testing.T, name string) []golden {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []golden
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		g := golden{line: line, kind: fields[0], params: map[string]string{}}
		for _, fld := range fields[1:] {
			k, v, ok := strings.Cut(fld, "=")
			if !ok {
				t.Fatalf("bad field %q", fld)
			}
			g.params[k] = v
		}
		out = append(out, g)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// compareDump reports the first difference between the bytes hashed by
// the Go test and a C dump file named by CELT_DUMP_REF, for debugging.
func compareDump(t *testing.T, got []byte) {
	ref := os.Getenv("CELT_DUMP_REF")
	if ref == "" {
		return
	}
	want, err := os.ReadFile(ref)
	if err != nil {
		t.Logf("dump: %v", err)
		return
	}
	n := min(len(got), len(want))
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			t.Logf("first dump difference at byte %d (word %d)", i, i/4)
			lo := max(0, i/4*4-16)
			hi := min(n, i/4*4+16)
			for j := lo; j+4 <= hi; j += 4 {
				g := binary.LittleEndian.Uint32(got[j:])
				w := binary.LittleEndian.Uint32(want[j:])
				t.Logf("  word %d: got %08x (%g) want %08x (%g)", j/4, g, math.Float32frombits(g), w, math.Float32frombits(w))
			}
			return
		}
	}
	if len(got) != len(want) {
		t.Logf("dump lengths differ: got %d want %d", len(got), len(want))
		return
	}
	t.Logf("dumps are identical")
}

func hexHash(h uint64) string { return fmt.Sprintf("%016x", h) }

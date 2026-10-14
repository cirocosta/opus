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
)

// testdata/silk_mix.bit concatenates short streams from the reference
// opus_demo encoder (see testdata/README): NB/MB/WB
// SILK and SWB hybrid, mono and stereo, 10/20/40/60 ms frames, in-band
// FEC (LBRR), and DTX. Concatenation adds bandwidth, channel and frame
// size switches.

func readMixGolden(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "mix_golden.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 6 {
			t.Fatalf("bad golden line %q", line)
		}
		m[strings.Join(fields[:4], " ")] = fields[4] + " " + fields[5]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestMix compares every silk_Decode call on testdata/silk_mix.bit with
// the C reference, at all API rates, mono and stereo output, with no
// loss, PLC and FEC.
func TestMix(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "silk_mix.bit"))
	if err != nil {
		t.Fatal(err)
	}
	frames := splitStream(data)
	golden := readMixGolden(t)
	dump := os.Getenv("SILK_DUMP_DIR")
	for _, rate := range vectorRates {
		for _, ch := range vectorChans {
			for _, loss := range vectorLosses {
				key := fmt.Sprintf("mix %d %d %d", rate, ch, loss)
				h := sha256.New()
				var sb strings.Builder
				n := 0
				runSILK(frames, rate, ch, loss, func(line string, _ []float32) {
					h.Write([]byte(line))
					if dump != "" {
						sb.WriteString(line)
					}
					n++
				})
				if dump != "" {
					name := fmt.Sprintf("mix_%d_%d_%d.txt", rate, ch, loss)
					if err := os.WriteFile(filepath.Join(dump, name), []byte(sb.String()), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				got := hex.EncodeToString(h.Sum(nil))[:32] + " " + fmt.Sprint(n)
				if want := golden[key]; got != want {
					t.Errorf("%s: got %s, want %s", key, got, want)
				}
			}
		}
	}
}

// TestMixSamples compares the first 4096 output samples of
// testdata/silk_mix.bit (16 kHz mono output, with PLC) with the C
// reference samples.
func TestMixSamples(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "silk_mix.bit"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "mix_samples_16k_mono_plc.raw"))
	if err != nil {
		t.Fatal(err)
	}
	want := make([]int16, len(raw)/2)
	for i := range want {
		want[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	var got []int16
	runSILK(splitStream(data), 16000, 1, 1, func(_ string, out []float32) {
		for _, s := range out {
			got = append(got, int16(s*32768))
		}
	})
	if len(got) < len(want) {
		t.Fatalf("got %d samples, want at least %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

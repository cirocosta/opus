package silk

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vectorConfigs lists the decoder configurations of the golden file.
var (
	vectorNames  = []string{"02", "03", "04", "05", "06", "08", "09", "10", "12"}
	vectorRates  = []int{8000, 12000, 16000, 24000, 48000}
	vectorChans  = []int{1, 2}
	vectorLosses = []int{0, 1, 2}
)

func goldenKey(vec string, rate, ch, loss int) string {
	return fmt.Sprintf("testvector%s %d %d %d", vec, rate, ch, loss)
}

func readGolden(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "vectors_golden.txt"))
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

// TestVectors decodes the SILK frames of the RFC 8251 test vectors at
// all API rates, mono and stereo output, with no loss, PLC and FEC, and
// compares a hash of every silk_Decode call's samples, final range and
// pitch lag with goldens from the C reference. Set OPUS_TESTVECTORS to
// the directory with testvectorNN.bit. Set SILK_DUMP_DIR to write the
// per-call lines for diffing against the C driver output.
func TestVectors(t *testing.T) {
	dir := os.Getenv("OPUS_TESTVECTORS")
	if dir == "" {
		t.Skip("OPUS_TESTVECTORS not set")
	}
	golden := readGolden(t)
	dump := os.Getenv("SILK_DUMP_DIR")
	for _, vec := range vectorNames {
		frames, err := readVector(filepath.Join(dir, "testvector"+vec+".bit"))
		if err != nil {
			t.Fatal(err)
		}
		for _, rate := range vectorRates {
			for _, ch := range vectorChans {
				for _, loss := range vectorLosses {
					if testing.Short() && (rate != 48000 && rate != 16000) {
						continue
					}
					key := goldenKey(vec, rate, ch, loss)
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
					got := hex.EncodeToString(h.Sum(nil))[:32] + " " + fmt.Sprint(n)
					if dump != "" {
						name := fmt.Sprintf("tv%s_%d_%d_%d.txt", vec, rate, ch, loss)
						if err := os.WriteFile(filepath.Join(dump, name), []byte(sb.String()), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					want, ok := golden[key]
					if !ok {
						t.Errorf("%s: no golden", key)
						continue
					}
					if got != want {
						t.Errorf("%s: got %s, want %s", key, got, want)
					}
				}
			}
		}
	}
}

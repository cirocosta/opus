package silk

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

// TestEncUnitGoldens compares encoder helpers that the encoder streams
// rarely or never reach (A2NLSF bandwidth expansion, the unused
// silk_LPC_inverse_pred_gain_FLP and silk_residual_energy_covar_FLP, the
// silk_schur_FLP and silk_autocorrelation_FLP clamps) with the C
// reference on random inputs. The inputs avoid NaN results: the sign of a
// NaN can differ, because Go may swap the operands of commutative
// operations. The goldens come from tools/oracle/silk/unit.c,
// which uses the same generator.
func TestEncUnitGoldens(t *testing.T) {
	raw, err := os.ReadFile("testdata/enc_unit_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := strings.Fields(l)
		want[f[0]] = f[1]
	}
	var b [4]byte
	f32 := func(h uint64, v float32) uint64 {
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		return fnvBytes(h, b[:])
	}
	i32 := func(h uint64, v int32) uint64 {
		binary.LittleEndian.PutUint32(b[:], uint32(v))
		return fnvBytes(h, b[:])
	}
	i16 := func(h uint64, v int16) uint64 {
		binary.LittleEndian.PutUint16(b[:], uint16(v))
		return fnvBytes(h, b[:2])
	}
	got := map[string]string{}
	var rng lcg
	rf := func(scale int) float32 {
		return float32(int(rng.next()>>16)-32768) / float32(scale)
	}

	h := uint64(fnvOffset)
	rng = 1
	for c := 0; c < 20000; c++ {
		d := 10
		if c&1 != 0 {
			d = 16
		}
		var a [16]int32
		var nlsf [16]int16
		sh := 10 + rng.intn(10)
		for i := 0; i < d; i++ {
			a[i] = int32(rng.next()) >> uint(sh)
		}
		a2nlsf(nlsf[:], a[:], d)
		for i := 0; i < d; i++ {
			h = i16(h, nlsf[i])
		}
		for i := 0; i < d; i++ {
			h = i32(h, a[i])
		}
	}
	got["a2nlsf"] = fmt.Sprintf("%016x", h)

	h = fnvOffset
	rng = 2
	for c := 0; c < 20000; c++ {
		order := 1 + rng.intn(16)
		var a [16]float32
		for i := 0; i < order; i++ {
			a[i] = rf(8192 * (1 + c%8))
		}
		h = f32(h, lpcInversePredGainFLP(a[:], order))
	}
	got["invpredgain"] = fmt.Sprintf("%016x", h)

	h = fnvOffset
	rng = 3
	for c := 0; c < 20000; c++ {
		d := 1 + rng.intn(16)
		var wXX [256]float32
		var wXx, cc [16]float32
		for i := 0; i < d; i++ {
			for j := i; j < d; j++ {
				v := rf(256)
				wXX[i+j*d], wXX[j+i*d] = v, v
			}
		}
		for i := 0; i < d; i++ {
			wXx[i] = rf(512)
			cc[i] = rf(16384)
		}
		wxx := rf(64)
		nrg := residualEnergyCovarFLP(cc[:], wXX[:], wXx[:], wxx, d)
		h = f32(h, nrg)
		for i := 0; i < d*d; i++ {
			h = f32(h, wXX[i])
		}
	}
	got["rescovar"] = fmt.Sprintf("%016x", h)

	h = fnvOffset
	rng = 4
	for c := 0; c < 20000; c++ {
		order := 1 + rng.intn(24)
		var ac [25]float32
		var rc [24]float32
		var sig [64]float32
		for i := range sig {
			if c%3 != 0 {
				sig[i] = rf(64)
			}
		}
		autocorrelationFLP(ac[:], sig[:], 64, order+1)
		e := schurFLP(rc[:], ac[:], order)
		h = f32(h, e)
		for i := 0; i < order; i++ {
			h = f32(h, rc[i])
		}
	}
	got["schur"] = fmt.Sprintf("%016x", h)

	h = fnvOffset
	rng = 5
	for c := 0; c < 20000; c++ {
		n, cnt := 1+rng.intn(30), 1+rng.intn(40)
		var in [30]float32
		var res [40]float32
		for i := 0; i < n; i++ {
			in[i] = rf(128)
		}
		autocorrelationFLP(res[:], in[:], n, cnt)
		for i := range res {
			h = f32(h, res[i])
		}
	}
	got["autocorr"] = fmt.Sprintf("%016x", h)

	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %s, want %s", k, got[k], w)
		}
	}
}

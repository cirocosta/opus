package kernels

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

func samples(n int, seed uint64) []float32 {
	r := rand.New(rand.NewPCG(seed, 7))
	x := make([]float32, n)
	for i := range x {
		x[i] = float32(int32(r.Uint32())) / (1 << 17)
	}
	return x
}

func equalBits(got, want []float32) error {
	for i, v := range want {
		if math.Float32bits(got[i]) != math.Float32bits(v) {
			return fmt.Errorf(
				"sample %d: got %08x, want %08x",
				i,
				math.Float32bits(got[i]),
				math.Float32bits(v),
			)
		}
	}
	return nil
}

func TestXcorr4(t *testing.T) {
	for n := 0; n <= 1024; n++ {
		for offset := 0; offset < 4; offset++ {
			x := samples(n+offset, uint64(n)+1)[offset:]
			y := samples(n+3+offset, uint64(n)+2)[offset:]
			got := [4]float32{1.25, -17.5, 93, -0.25}
			want := got
			xcorr4Generic(x, y, &want, n)
			Xcorr4(x, y, &got, n)
			if err := equalBits(got[:], want[:]); err != nil {
				t.Fatalf("Xcorr4(n=%d, offset=%d): %v", n, offset, err)
			}
		}
	}
}

func TestCrossCorrelation(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 7, 15, 16, 33, 120, 240, 480, 960} {
		for lags := 0; lags <= 65; lags++ {
			x := samples(n, 1)
			y := samples(n+lags+3, 2)
			got, want := make([]float32, lags+2), make([]float32, lags+2)
			got[0], got[lags+1] = 123, -456
			want[0], want[lags+1] = 123, -456
			crossCorrelationGeneric(x, y, want[1:lags+1], n, lags)
			CrossCorrelation(x, y, got[1:lags+1], n, lags)
			if err := equalBits(got, want); err != nil {
				t.Fatalf("CrossCorrelation(n=%d, lags=%d): %v", n, lags, err)
			}
		}
	}
}

func TestDenseAccum(t *testing.T) {
	for rows := 0; rows <= 33; rows++ {
		for _, cols := range []int{0, 1, 3, 4, 8, 25, 32} {
			for _, padding := range []int{0, 7, 64} {
				stride := rows + padding
				weights := make([]int8, cols*stride)
				for i := range weights {
					weights[i] = int8(i*79 + 11)
				}
				x := samples(cols, 3)
				got := samples(rows, 4)
				want := append([]float32(nil), got...)
				for i := range want {
					for j, v := range x {
						want[i] += float32(float32(weights[j*stride+i]) * v)
					}
				}
				DenseAccum(got, weights, x, rows, cols, stride)
				if err := equalBits(got, want); err != nil {
					t.Fatalf("DenseAccum(rows=%d, cols=%d, stride=%d): %v", rows, cols, stride, err)
				}
			}
		}
	}
}

func TestLPC(t *testing.T) {
	for _, order := range []int{1, 6, 8, 10, 12, 16, 24} {
		coeff := samples(order, 3)
		for n := order; n <= order+65; n++ {
			for offset := 0; offset < 4; offset++ {
				src := samples(n+offset, 4)[offset:]
				got, want := make([]float32, n+1), make([]float32, n+1)
				got[n], want[n] = 91, 91
				lpcGeneric(want, src, coeff, order, n, order)
				LPC(got[:n], src, coeff, n)
				if err := equalBits(got, want); err != nil {
					t.Fatalf("LPC(n=%d, order=%d, offset=%d): %v", n, order, offset, err)
				}
			}
		}
	}
}

func TestCorrelationRounding(t *testing.T) {
	values := []float32{
		0, math.Float32frombits(1 << 31), math.SmallestNonzeroFloat32,
		-math.SmallestNonzeroFloat32, 1, -1, 1e-18, -1e-18, 1e18, -1e18,
	}
	x, y := make([]float32, 320), make([]float32, 323)
	for i := range x {
		x[i] = values[i%len(values)]
	}
	for i := range y {
		y[i] = values[(i*7+3)%len(values)]
	}
	got := [4]float32{1, -1, math.SmallestNonzeroFloat32, -math.SmallestNonzeroFloat32}
	want := got
	xcorr4Generic(x, y, &want, len(x))
	Xcorr4(x, y, &got, len(x))
	if err := equalBits(got[:], want[:]); err != nil {
		t.Fatal(err)
	}
}

func TestPredict(t *testing.T) {
	rng := rand.New(rand.NewPCG(123, 456))
	for _, order := range []int{10, 16} {
		for trial := 0; trial < 10000; trial++ {
			history, coeff := make([]int32, order), make([]int16, order)
			for i := range history {
				history[i], coeff[i] = int32(rng.Uint32()), int16(rng.Uint32())
			}
			got, want := Predict(history, coeff, order), predictGeneric(history, coeff, order)
			if got != want {
				t.Fatalf("Predict(order=%d, trial=%d): got %d, want %d", order, trial, got, want)
			}
		}
	}
}

func TestPredictLimits(t *testing.T) {
	values := []int32{
		math.MinInt32,
		math.MaxInt32,
		-65537,
		-65536,
		-32768,
		-1,
		0,
		1,
		32767,
		32768,
		65535,
		65536,
	}
	for _, order := range []int{10, 16} {
		for _, value := range values {
			for _, c := range []int16{math.MinInt16, math.MaxInt16, -1, 0, 1} {
				history, coeff := make([]int32, order), make([]int16, order)
				for i := range history {
					history[i], coeff[i] = value, c
				}
				got, want := Predict(history, coeff, order), predictGeneric(history, coeff, order)
				if got != want {
					t.Errorf(
						"Predict(order=%d, sample=%d, coeff=%d): got %d, want %d",
						order,
						value,
						c,
						got,
						want,
					)
				}
			}
		}
	}
}

func TestSynthesize(t *testing.T) {
	rng := rand.New(rand.NewPCG(123, 456))
	for _, order := range []int{10, 16} {
		for _, n := range []int{0, 1, 3, 40, 80, 160} {
			for trial := 0; trial < 100; trial++ {
				history, residual, coeff := make(
					[]int32,
					16+n,
				), make(
					[]int32,
					n,
				), make(
					[]int16,
					order,
				)
				for i := range history {
					history[i] = int32(rng.Uint32())
				}
				for i := range residual {
					residual[i] = int32(rng.Uint32())
				}
				for i := range coeff {
					coeff[i] = int16(rng.Uint32())
				}
				gain := int32(rng.Uint32())
				got, want := make([]int16, n+1), make([]int16, n+1)
				got[n], want[n] = 123, 123
				wantHistory := append([]int32(nil), history...)
				synthesizeGeneric(wantHistory, residual, coeff, gain, want[:n], order)
				Synthesize(history, residual, coeff, gain, got[:n], order)
				for i := range got {
					if got[i] != want[i] {
						t.Fatalf(
							"Synthesize(order=%d, n=%d, trial=%d): sample %d: got %d, want %d",
							order,
							n,
							trial,
							i,
							got[i],
							want[i],
						)
					}
				}
				for i := range history {
					if history[i] != wantHistory[i] {
						t.Fatalf(
							"Synthesize(order=%d, n=%d, trial=%d): history %d: got %d, want %d",
							order,
							n,
							trial,
							i,
							history[i],
							wantHistory[i],
						)
					}
				}
			}
		}
	}
}

func TestWarpedFeedback(t *testing.T) {
	rng := rand.New(rand.NewPCG(123, 456))
	for _, order := range []int{2, 12, 14, 16, 20, 24} {
		for trial := 0; trial < 10000; trial++ {
			state, coeff := make([]int32, order), make([]int16, order)
			for i := range state {
				state[i], coeff[i] = int32(rng.Uint32()), int16(rng.Uint32())
			}
			wantState := append([]int32(nil), state...)
			diff, warp := int32(rng.Uint32()), int32(rng.Uint32())
			got, want := WarpedFeedback(
				state,
				coeff,
				diff,
				warp,
			), warpedFeedbackGeneric(
				wantState,
				coeff,
				diff,
				warp,
			)
			if got != want {
				t.Fatalf(
					"WarpedFeedback(order=%d, trial=%d): got %d, want %d",
					order,
					trial,
					got,
					want,
				)
			}
			for i := range state {
				if state[i] != wantState[i] {
					t.Fatalf(
						"WarpedFeedback(order=%d, trial=%d): state %d: got %d, want %d",
						order,
						trial,
						i,
						state[i],
						wantState[i],
					)
				}
			}
		}
	}
}

func BenchmarkCrossCorrelation(b *testing.B) {
	for _, n := range []int{24, 120, 480} {
		b.Run(fmt.Sprintf("n%d", n), func(b *testing.B) {
			x, y, dst := samples(n, 1), samples(n+255, 2), make([]float32, 256)
			b.ReportAllocs()
			for b.Loop() {
				CrossCorrelation(x, y, dst, n, len(dst))
			}
		})
	}
}

func BenchmarkDenseAccum(b *testing.B) {
	for _, c := range []struct{ rows, cols, stride int }{
		{rows: 24, cols: 32, stride: 72},
		{rows: 24, cols: 24, stride: 72},
		{rows: 32, cols: 25, stride: 32},
		{rows: 32, cols: 32, stride: 32},
	} {
		b.Run(fmt.Sprintf("r%dc%ds%d", c.rows, c.cols, c.stride), func(b *testing.B) {
			weights := make([]int8, c.cols*c.stride)
			for i := range weights {
				weights[i] = int8(i*79 + 11)
			}
			x, out := samples(c.cols, 1), make([]float32, c.rows)
			b.ReportAllocs()
			for b.Loop() {
				clear(out)
				DenseAccum(out, weights, x, c.rows, c.cols, c.stride)
			}
		})
	}
}

func BenchmarkLPC(b *testing.B) {
	x, coeff, out := samples(960, 1), samples(16, 2), make([]float32, 960)
	b.ReportAllocs()
	for b.Loop() {
		LPC(out, x, coeff, len(x))
	}
}

func BenchmarkPredict(b *testing.B) {
	history, coeff := make([]int32, 16), make([]int16, 16)
	for i := range history {
		history[i], coeff[i] = int32(i*1735917), int16(i*1319)
	}
	b.ReportAllocs()
	for b.Loop() {
		Predict(history, coeff, 16)
	}
}

func BenchmarkSynthesize(b *testing.B) {
	history, residual, coeff, out := make(
		[]int32,
		16+80,
	), make(
		[]int32,
		80,
	), make(
		[]int16,
		16,
	), make(
		[]int16,
		80,
	)
	for i := range coeff {
		coeff[i] = int16(i * 1319)
	}
	b.ReportAllocs()
	for b.Loop() {
		Synthesize(history, residual, coeff, 1024, out, 16)
	}
}

func TestInterpolate(t *testing.T) {
	rng := rand.New(rand.NewPCG(123, 456))
	var phases [12][8]int16
	for i := range phases {
		for j := range phases[i] {
			phases[i][j] = int16(rng.Uint32())
		}
	}
	for _, limit := range []int32{0, 1, 65535, 65536, 33 * 65536, 127 * 65536} {
		for _, step := range []int32{12345, 16384, 21845, 32768, 43691, 49152, 65536, 98304, 131072, 196608} {
			n := int((int64(limit) + int64(step) - 1) / int64(step))
			src := make([]int16, int(limit>>16)+8)
			for i := range src {
				src[i] = int16(rng.Uint32())
			}
			got, want := make([]int16, n+1), make([]int16, n+1)
			got[n], want[n] = 123, 123
			interpolateGeneric(want[:n], src, &phases, step)
			written := Interpolate(got[:n], src, &phases, limit, step)
			if written != n {
				t.Errorf(
					"Interpolate(limit=%d, step=%d): got %d samples, want %d",
					limit,
					step,
					written,
					n,
				)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf(
						"Interpolate(limit=%d, step=%d): sample %d: got %d, want %d",
						limit,
						step,
						i,
						got[i],
						want[i],
					)
				}
			}
		}
	}
}

func TestInterpolateExtremes(t *testing.T) {
	for _, input := range []int16{-32768, -32767, -1, 0, 1, 32767} {
		for _, weight := range []int16{-32768, -32767, -1, 0, 1, 32767} {
			var phases [12][8]int16
			for i := range phases {
				for j := range phases[i] {
					phases[i][j] = weight
				}
			}
			src := make([]int16, 20)
			for i := range src {
				src[i] = input
			}
			const limit, step = 12 * 65536, 5461
			dst := make([]int16, (limit+step-1)/step+1)
			dst[len(dst)-1] = 123
			written := Interpolate(dst, src, &phases, limit, step)
			// accumulate in int64, then apply the reference's int32 wrap.
			sum := int32(int64(input) * int64(weight) * 8)
			want := int16(max(-32768, min(32767, (int64(sum)+16384)>>15)))
			for i, got := range dst[:written] {
				if got != want {
					t.Fatalf(
						"input %d, weight %d, sample %d: got %d, want %d",
						input,
						weight,
						i,
						got,
						want,
					)
				}
			}
			if dst[written] != 123 {
				t.Fatal("output exceeded the requested sample count")
			}
		}
	}
}

package silk

import (
	"math/rand/v2"
	"testing"
)

// TestPackedInterpolation compares the kernel against the original split-table
// resampler, including every phase and the mirrored tap ordering.
func TestPackedInterpolation(t *testing.T) {
	for phase := range interpolationCoefficients {
		for tap := 0; tap < 4; tap++ {
			if interpolationCoefficients[phase][tap] != resamplerFracFIR12[phase][tap] ||
				interpolationCoefficients[phase][tap+4] != resamplerFracFIR12[11-phase][3-tap] {
				t.Fatalf("phase %d, tap %d: packed coefficient differs", phase, tap)
			}
		}
	}
	rng := rand.New(rand.NewPCG(372, 184))
	const limit = 240 * 65536
	buf := make([]int16, 248)
	for i := range buf {
		buf[i] = int16(rng.Uint32())
	}
	for _, step := range []int32{5461, 21845, 32768, 43691, 49152, 65536, 98304, 131072, 196608} {
		out := make([]int16, (limit+int(step)-1)/int(step))
		if written := resamplerPrivateIIRFIRInterpol(out, buf, limit, step); written != len(out) {
			t.Fatalf("step %d: wrote %d samples, want %d", step, written, len(out))
		}
		var position int32
		for i, got := range out {
			phase := smulwb(position&0xffff, 12)
			input := buf[position>>16:]
			var sum int32
			for tap := 0; tap < 4; tap++ {
				sum = smlabb(sum, int32(input[tap]), int32(resamplerFracFIR12[phase][tap]))
				sum = smlabb(sum, int32(input[tap+4]), int32(resamplerFracFIR12[11-phase][3-tap]))
			}
			want := int16(sat16(rshiftRound(sum, 15)))
			if got != want {
				t.Fatalf("step %d, sample %d, phase %d: got %d, want %d", step, i, phase, got, want)
			}
			position += step
		}
	}
}

package kernels

// Interpolate applies an eight-tap Q15 FIR phase to resampled int16 input.
// limit and step are Q16 positions. limit is nonnegative and step is positive.
// src and dst must not overlap. phases contains 12 read-only coefficient rows.
// dst needs ceil(limit/step) elements. For nonzero limit, src needs eight taps
// after the last output's integer input position. Neither buffer may alias phases.
func Interpolate(dst, src []int16, phases *[12][8]int16, limit, step int32) int {
	if limit == 0 {
		return 0
	}
	n := int((int64(limit) + int64(step) - 1) / int64(step))
	dst = dst[:n]
	last := int(int64(n-1)*int64(step)>>16) + 8
	src = src[:last]
	interpolate(dst, src, phases, step)
	return n
}

func interpolateGeneric(dst, src []int16, phases *[12][8]int16, step int32) {
	var position int32
	for i := range dst {
		phase := (position & 0xffff) * 12 >> 16
		coeff := &phases[phase]
		start := int(position >> 16)
		window := src[start : start+8]
		var sum int32
		for j, v := range window {
			sum += int32(v) * int32(coeff[j])
		}
		rounded := ((sum >> 14) + 1) >> 1
		dst[i] = int16(max(-32768, min(32767, rounded)))
		position += step
	}
}

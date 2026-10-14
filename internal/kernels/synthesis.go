package kernels

// Synthesize reconstructs a SILK subframe from Q14 residuals and Q12 LPC taps.
// history starts with 16 previous Q14 samples and has space for len(dst) new
// samples. The new samples are retained in history[16:]. gain is Q10.
// order must be 10 or 16. The buffers must not overlap.
func Synthesize(history, residual []int32, coeff []int16, gain int32, dst []int16, order int) {
	n := len(dst)
	history = history[:16+n]
	residual = residual[:n]
	coeff = coeff[:order]
	if n == 0 {
		return
	}
	synthesize(history, residual, coeff, gain, dst, order)
}

func synthesizeGeneric(
	history, residual []int32,
	coeff []int16,
	gain int32,
	dst []int16,
	order int,
) {
	for i := range dst {
		prediction := int64(predictGeneric(history[16+i-order:16+i], coeff, order))
		prediction = max(-1<<27, min(1<<27-1, prediction)) << 4
		sample := int64(residual[i]) + prediction
		sample = max(-1<<31, min(1<<31-1, sample))
		history[16+i] = int32(sample)
		scaled := int32(sample * int64(gain) >> 16)
		rounded := ((scaled >> 7) + 1) >> 1
		dst[i] = int16(max(-32768, min(32767, rounded)))
	}
}

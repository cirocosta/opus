package kernels

// Predict returns the fixed-point LPC prediction over the last order samples.
// order must be 10 or 16; coeff contains order Q12 coefficients.
// Products are rounded down individually before int32 addition wraps.
func Predict(history []int32, coeff []int16, order int) int32 {
	history = history[len(history)-order:]
	coeff = coeff[:order]
	return predict(history, coeff, order)
}

func predictGeneric(history []int32, coeff []int16, order int) int32 {
	out := int32(order >> 1)
	for i, v := range coeff[:order] {
		out += int32((int64(history[order-1-i]) * int64(v)) >> 16)
	}
	return out
}

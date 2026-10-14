package kernels

// LPC writes the prediction residual of src using the filter coefficients.
// The first len(coeff) outputs are zero. dst and src must not overlap.
// n must be at least len(coeff), and coeff must contain at least one tap.
func LPC(dst, src, coeff []float32, n int) {
	dst = dst[:n]
	src = src[:n]
	order := len(coeff)
	clear(dst[:order])
	if n == order {
		return
	}
	lpc(dst, src, coeff, n, order)
}

func lpcGeneric(dst, src, coeff []float32, start, n, order int) {
	for i := start; i < n; i++ {
		window := src[i-order : i]
		sum := float32(window[order-1] * coeff[0])
		for j := 1; j < order; j++ {
			sum += float32(window[order-1-j] * coeff[j])
		}
		dst[i] = src[i] - sum
	}
}

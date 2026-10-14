//go:build !amd64 || purego

package kernels

func xcorr4(x, y []float32, sum *[4]float32, n int) {
	xcorr4Generic(x, y, sum, n)
}

func crossCorrelation(x, y, dst []float32, n, lags int) {
	crossCorrelationGeneric(x, y, dst, n, lags)
}

func denseAccum(out []float32, weights []int8, x []float32, rows, cols, stride int) {
	denseAccumGeneric(out, weights, x, rows, cols, stride)
}

func lpc(dst, src, coeff []float32, n, order int) {
	lpcGeneric(dst, src, coeff, order, n, order)
}

func predict(history []int32, coeff []int16, order int) int32 {
	return predictGeneric(history, coeff, order)
}

func synthesize(history, residual []int32, coeff []int16, gain int32, dst []int16, order int) {
	synthesizeGeneric(history, residual, coeff, gain, dst, order)
}

func warpedFeedback(state []int32, coeff []int16, diff, warp int32) int32 {
	return warpedFeedbackGeneric(state, coeff, diff, warp)
}

//go:build amd64 && !purego

package kernels

func xcorr4(x, y []float32, sum *[4]float32, n int) {
	xcorr4SSE2(&x[0], &y[0], sum, n)
}

func crossCorrelation(x, y, dst []float32, n, lags int) {
	blocks := lags &^ 7
	if blocks != 0 && n != 0 {
		crossCorrelationSSE2(&x[0], &y[0], &dst[0], n, blocks)
	}
	crossCorrelationGeneric(x, y[blocks:], dst[blocks:], n, lags-blocks)
}

func denseAccum(out []float32, weights []int8, x []float32, rows, cols, stride int) {
	blocks := rows &^ 7
	if blocks != 0 {
		denseAccumSSE2(&out[0], &weights[0], &x[0], blocks, cols, stride)
	}
	denseAccumGeneric(out[blocks:], weights[blocks:], x, rows-blocks, cols, stride)
}

func lpc(dst, src, coeff []float32, n, order int) {
	blocks := (n - order) &^ 3
	if blocks != 0 {
		lpcSSE2(&dst[order], &src[order], &coeff[0], blocks, order)
	}
	lpcGeneric(dst, src, coeff, order+blocks, n, order)
}

func predict(history []int32, coeff []int16, order int) int32 {
	return predictSSE2(&history[order-1], &coeff[0], order)
}

func synthesize(history, residual []int32, coeff []int16, gain int32, dst []int16, order int) {
	synthesizeSSE2(&history[15], &residual[0], &coeff[0], gain, &dst[0], len(dst), order)
}

func warpedFeedback(state []int32, coeff []int16, diff, warp int32) int32 {
	return warpedFeedbackAMD64(&state[0], &coeff[0], diff, warp, len(coeff))
}

//go:noescape
func xcorr4SSE2(x, y *float32, sum *[4]float32, n int)

//go:noescape
func crossCorrelationSSE2(x, y, dst *float32, n, lags int)

//go:noescape
func denseAccumSSE2(out *float32, weights *int8, x *float32, rows, cols, stride int)

//go:noescape
func lpcSSE2(dst, src, coeff *float32, n, order int)

//go:noescape
func predictSSE2(history *int32, coeff *int16, order int) int32

//go:noescape
func synthesizeSSE2(history, residual *int32, coeff *int16, gain int32, dst *int16, n, order int)

//go:noescape
func warpedFeedbackAMD64(state *int32, coeff *int16, diff, warp int32, order int) int32

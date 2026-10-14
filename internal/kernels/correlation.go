package kernels

// Xcorr4 adds the correlations of x with four adjacent windows of y to sum.
// It reads n elements of x and n+3 elements of y. n must be nonnegative.
func Xcorr4(x, y []float32, sum *[4]float32, n int) {
	x = x[:n]
	y = y[:n+3]
	if n == 0 {
		return
	}
	xcorr4(x, y, sum, n)
}

func xcorr4Generic(x, y []float32, sum *[4]float32, n int) {
	s0, s1, s2, s3 := sum[0], sum[1], sum[2], sum[3]
	for i, v := range x[:n] {
		w := y[i : i+4]
		s0 += float32(v * w[0])
		s1 += float32(v * w[1])
		s2 += float32(v * w[2])
		s3 += float32(v * w[3])
	}
	*sum = [4]float32{s0, s1, s2, s3}
}

// CrossCorrelation writes lags correlations, each over n samples, to dst.
// x must contain n samples and y must contain n+lags-1 samples.
// The output must not overlap either input. n and lags must be nonnegative.
func CrossCorrelation(x, y, dst []float32, n, lags int) {
	x = x[:n]
	dst = dst[:lags]
	if lags == 0 {
		return
	}
	if n == 0 {
		clear(dst)
		return
	}
	y = y[:n+lags-1]
	crossCorrelation(x, y, dst, n, lags)
}

func crossCorrelationGeneric(x, y, dst []float32, n, lags int) {
	i := 0
	for ; i+4 <= lags; i += 4 {
		var sum [4]float32
		xcorr4Generic(x, y[i:], &sum, n)
		copy(dst[i:i+4], sum[:])
	}
	for ; i < lags; i++ {
		var sum float32
		w := y[i : i+n]
		for j, v := range x[:n] {
			sum += float32(v * w[j])
		}
		dst[i] = sum
	}
}

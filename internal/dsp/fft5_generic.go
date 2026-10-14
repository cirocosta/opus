//go:build !amd64 || purego

package dsp

func fft5Accelerated(_ []KissFFTCpx, _ int, _ *KissFFTState, _, _ int) bool {
	return false
}

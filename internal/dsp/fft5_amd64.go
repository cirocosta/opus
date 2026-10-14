//go:build amd64 && !purego

package dsp

import "golang.org/x/sys/cpu"

func init() {
	if !cpu.X86.HasAVX2 {
		return
	}
	for _, st := range []*KissFFTState{
		&FFTState48000_960_0, &FFTState48000_960_1,
		&FFTState48000_960_2, &FFTState48000_960_3,
	} {
		st.fft5 = packFFT5(st)
	}
}

func fft5Accelerated(fout []KissFFTCpx, stride int, st *KissFFTState, m, n int) bool {
	if !cpu.X86.HasAVX2 || n != 1 || m != st.Nfft/5 || stride != 1<<max(st.Shift, 0) ||
		len(st.fft5) == 0 {
		return false
	}
	fout = fout[:5*m]
	coeff := st.fft5[:m/4]
	ya, yb := st.Twiddles[stride*m], st.Twiddles[2*stride*m]
	fft5AVX2(&fout[0], &coeff[0], m, ya.R, ya.I, yb.R, yb.I)
	return true
}

//go:noescape
func fft5AVX2(fout *KissFFTCpx, tw *fft5Coefficients, m int, yaR, yaI, ybR, ybI float32)

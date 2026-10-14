package dsp

// fft5Coefficients packs four consecutive complex twiddles for each input leg.
// Static plans treat these coefficients and their original Twiddles as read-only.
type fft5Coefficients [4][8]float32

func packFFT5(st *KissFFTState) []fft5Coefficients {
	m := st.Nfft / 5
	stride := 1 << max(st.Shift, 0)
	coeff := make([]fft5Coefficients, m/4)
	for block := range coeff {
		for leg := range coeff[block] {
			for lane := 0; lane < 4; lane++ {
				w := st.Twiddles[(4*block+lane)*(leg+1)*stride]
				coeff[block][leg][2*lane] = w.R
				coeff[block][leg][2*lane+1] = w.I
			}
		}
	}
	return coeff
}

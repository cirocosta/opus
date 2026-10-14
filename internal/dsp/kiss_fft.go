// Ported from libopus 1.6.1 celt/kiss_fft.h, celt/kiss_fft.c and
// celt/_kiss_fft_guts.h (float build, no CUSTOM_MODES).

package dsp

// MaxFactors is MAXFACTORS.
const MaxFactors = 8

// KissFFTCpx is kiss_fft_cpx.
type KissFFTCpx struct {
	R, I float32
}

// KissTwiddleCpx is kiss_twiddle_cpx.
type KissTwiddleCpx struct {
	R, I float32
}

// KissFFTState is kiss_fft_state. The states of the static modes are
// precomputed (see static_modes.go). A state with Shift > 0 uses every
// (1<<Shift)-th twiddle of a larger FFT.
type KissFFTState struct {
	Nfft     int
	Scale    float32
	Shift    int
	Factors  [2 * MaxFactors]int16
	Bitrev   []int16
	Twiddles []KissTwiddleCpx
}

// cMul is C_MUL.
func cMul(a KissFFTCpx, b KissTwiddleCpx) KissFFTCpx {
	return KissFFTCpx{
		R: float32(a.R*b.R) - float32(a.I*b.I),
		I: float32(a.R*b.I) + float32(a.I*b.R),
	}
}

// kfBfly2 is kf_bfly2. Without CUSTOM_MODES, m is always 4.
func kfBfly2(fout []KissFFTCpx, m, n int) {
	_ = m
	tw := float32(0.7071067812)
	for i := 0; i < n; i++ {
		f := fout[8*i : 8*i+8 : 8*i+8]
		t := f[4]
		f[4].R = f[0].R - t.R
		f[4].I = f[0].I - t.I
		f[0].R += t.R
		f[0].I += t.I

		t.R = float32((f[5].R + f[5].I) * tw)
		t.I = float32((f[5].I - f[5].R) * tw)
		f[5].R = f[1].R - t.R
		f[5].I = f[1].I - t.I
		f[1].R += t.R
		f[1].I += t.I

		t.R = f[6].I
		t.I = -f[6].R
		f[6].R = f[2].R - t.R
		f[6].I = f[2].I - t.I
		f[2].R += t.R
		f[2].I += t.I

		t.R = float32((f[7].I - f[7].R) * tw)
		t.I = float32(-(f[7].I + f[7].R) * tw)
		f[7].R = f[3].R - t.R
		f[7].I = f[3].I - t.I
		f[3].R += t.R
		f[3].I += t.I
	}
}

// kfBfly4 is kf_bfly4.
func kfBfly4(fout []KissFFTCpx, fstride int, st *KissFFTState, m, n, mm int) {
	if m == 1 {
		// Degenerate case where all the twiddles are 1.
		for i := 0; i < n; i++ {
			f := fout[4*i : 4*i+4 : 4*i+4]
			var s0, s1 KissFFTCpx
			s0.R = f[0].R - f[2].R
			s0.I = f[0].I - f[2].I
			f[0].R += f[2].R
			f[0].I += f[2].I
			s1.R = f[1].R + f[3].R
			s1.I = f[1].I + f[3].I
			f[2].R = f[0].R - s1.R
			f[2].I = f[0].I - s1.I
			f[0].R += s1.R
			f[0].I += s1.I
			s1.R = f[1].R - f[3].R
			s1.I = f[1].I - f[3].I

			f[1].R = s0.R + s1.I
			f[1].I = s0.I - s1.R
			f[3].R = s0.R - s1.I
			f[3].I = s0.I + s1.R
		}
		return
	}
	tw := st.Twiddles
	m2 := 2 * m
	m3 := 3 * m
	for i := 0; i < n; i++ {
		f := fout[i*mm:]
		t1, t2, t3 := 0, 0, 0
		// m is guaranteed to be a multiple of 4.
		for j := 0; j < m; j++ {
			var s0, s1, s2, s3, s4, s5 KissFFTCpx
			s0 = cMul(f[j+m], tw[t1])
			s1 = cMul(f[j+m2], tw[t2])
			s2 = cMul(f[j+m3], tw[t3])

			s5.R = f[j].R - s1.R
			s5.I = f[j].I - s1.I
			f[j].R += s1.R
			f[j].I += s1.I
			s3.R = s0.R + s2.R
			s3.I = s0.I + s2.I
			s4.R = s0.R - s2.R
			s4.I = s0.I - s2.I
			f[j+m2].R = f[j].R - s3.R
			f[j+m2].I = f[j].I - s3.I
			t1 += fstride
			t2 += fstride * 2
			t3 += fstride * 3
			f[j].R += s3.R
			f[j].I += s3.I

			f[j+m].R = s5.R + s4.I
			f[j+m].I = s5.I - s4.R
			f[j+m3].R = s5.R - s4.I
			f[j+m3].I = s5.I + s4.R
		}
	}
}

// kfBfly3 is kf_bfly3.
func kfBfly3(fout []KissFFTCpx, fstride int, st *KissFFTState, m, n, mm int) {
	m2 := 2 * m
	tw := st.Twiddles
	epi3 := tw[fstride*m]
	for i := 0; i < n; i++ {
		f := fout[i*mm:]
		t1, t2 := 0, 0
		// For non-custom modes, m is guaranteed to be a multiple of 4.
		for k := 0; k < m; k++ {
			var s0, s1, s2, s3 KissFFTCpx
			s1 = cMul(f[k+m], tw[t1])
			s2 = cMul(f[k+m2], tw[t2])

			s3.R = s1.R + s2.R
			s3.I = s1.I + s2.I
			s0.R = s1.R - s2.R
			s0.I = s1.I - s2.I
			t1 += fstride
			t2 += fstride * 2

			f[k+m].R = f[k].R - float32(s3.R*.5)
			f[k+m].I = f[k].I - float32(s3.I*.5)

			s0.R = float32(s0.R * epi3.I)
			s0.I = float32(s0.I * epi3.I)

			f[k].R += s3.R
			f[k].I += s3.I

			f[k+m2].R = f[k+m].R + s0.I
			f[k+m2].I = f[k+m].I - s0.R

			f[k+m].R = f[k+m].R - s0.I
			f[k+m].I = f[k+m].I + s0.R
		}
	}
}

// kfBfly5 is kf_bfly5.
func kfBfly5(fout []KissFFTCpx, fstride int, st *KissFFTState, m, n, mm int) {
	tw := st.Twiddles
	ya := tw[fstride*m]
	yb := tw[fstride*2*m]
	for i := 0; i < n; i++ {
		f := fout[i*mm:]
		f0 := f[0:m:m]
		f1 := f[m : 2*m : 2*m]
		f2 := f[2*m : 3*m : 3*m]
		f3 := f[3*m : 4*m : 4*m]
		f4 := f[4*m : 5*m : 5*m]
		// For non-custom modes, m is guaranteed to be a multiple of 4.
		for u := 0; u < m; u++ {
			var s [13]KissFFTCpx
			s[0] = f0[u]

			s[1] = cMul(f1[u], tw[u*fstride])
			s[2] = cMul(f2[u], tw[2*u*fstride])
			s[3] = cMul(f3[u], tw[3*u*fstride])
			s[4] = cMul(f4[u], tw[4*u*fstride])

			s[7].R = s[1].R + s[4].R
			s[7].I = s[1].I + s[4].I
			s[10].R = s[1].R - s[4].R
			s[10].I = s[1].I - s[4].I
			s[8].R = s[2].R + s[3].R
			s[8].I = s[2].I + s[3].I
			s[9].R = s[2].R - s[3].R
			s[9].I = s[2].I - s[3].I

			f0[u].R = f0[u].R + (s[7].R + s[8].R)
			f0[u].I = f0[u].I + (s[7].I + s[8].I)

			s[5].R = s[0].R + (float32(s[7].R*ya.R) + float32(s[8].R*yb.R))
			s[5].I = s[0].I + (float32(s[7].I*ya.R) + float32(s[8].I*yb.R))

			s[6].R = float32(s[10].I*ya.I) + float32(s[9].I*yb.I)
			s[6].I = -(float32(s[10].R*ya.I) + float32(s[9].R*yb.I))

			f1[u].R = s[5].R - s[6].R
			f1[u].I = s[5].I - s[6].I
			f4[u].R = s[5].R + s[6].R
			f4[u].I = s[5].I + s[6].I

			s[11].R = s[0].R + (float32(s[7].R*yb.R) + float32(s[8].R*ya.R))
			s[11].I = s[0].I + (float32(s[7].I*yb.R) + float32(s[8].I*ya.R))
			s[12].R = float32(s[9].I*ya.I) - float32(s[10].I*yb.I)
			s[12].I = float32(s[10].R*yb.I) - float32(s[9].R*ya.I)

			f2[u].R = s[11].R + s[12].R
			f2[u].I = s[11].I + s[12].I
			f3[u].R = s[11].R - s[12].R
			f3[u].I = s[11].I - s[12].I
		}
	}
}

// OpusFFTImpl is opus_fft_impl: an in-place FFT of fout, which must
// already be in bit-reversed order. It does not scale.
func OpusFFTImpl(st *KissFFTState, fout []KissFFTCpx) {
	var fstride [MaxFactors]int
	// st.Shift can be -1.
	shift := 0
	if st.Shift > 0 {
		shift = st.Shift
	}
	fout = fout[:st.Nfft]

	fstride[0] = 1
	l := 0
	var m int
	for {
		p := int(st.Factors[2*l])
		m = int(st.Factors[2*l+1])
		fstride[l+1] = fstride[l] * p
		l++
		if m == 1 {
			break
		}
	}
	m = int(st.Factors[2*l-1])
	for i := l - 1; i >= 0; i-- {
		m2 := 1
		if i != 0 {
			m2 = int(st.Factors[2*i-1])
		}
		switch st.Factors[2*i] {
		case 2:
			kfBfly2(fout, m, fstride[i])
		case 4:
			kfBfly4(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		case 3:
			kfBfly3(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		case 5:
			kfBfly5(fout, fstride[i]<<shift, st, m, fstride[i], m2)
		}
		m = m2
	}
}

// OpusFFT is opus_fft_c: a forward FFT scaled by 1/nfft. fin and fout
// must not overlap.
func OpusFFT(st *KissFFTState, fin, fout []KissFFTCpx) {
	scale := st.Scale
	fin = fin[:st.Nfft]
	fout = fout[:st.Nfft]
	// Bit-reverse the input.
	for i, x := range fin {
		b := st.Bitrev[i]
		fout[b].R = float32(x.R * scale)
		fout[b].I = float32(x.I * scale)
	}
	OpusFFTImpl(st, fout)
}

// OpusIFFT is opus_ifft_c: an unscaled inverse FFT. fin and fout must not
// overlap.
func OpusIFFT(st *KissFFTState, fin, fout []KissFFTCpx) {
	fin = fin[:st.Nfft]
	fout = fout[:st.Nfft]
	// Bit-reverse the input.
	for i, x := range fin {
		fout[st.Bitrev[i]] = x
	}
	for i := range fout {
		fout[i].I = -fout[i].I
	}
	OpusFFTImpl(st, fout)
	for i := range fout {
		fout[i].I = -fout[i].I
	}
}

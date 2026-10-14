// Ported from libopus 1.6.1 celt/mdct.h and celt/mdct.c (float build, no
// CUSTOM_MODES).

package dsp

// MDCTLookup is mdct_lookup. Trig holds the twiddles of all the shifts
// back to back: N/2 values for shift 0, then N/4 for shift 1, and so on.
type MDCTLookup struct {
	N        int
	MaxShift int
	KFFT     [4]*KissFFTState
	Trig     []float32
}

// mdctMaxN4 bounds the stack scratch buffers. It covers the static 48 kHz
// mode (N = 1920). Larger sizes fall back to the heap.
const mdctMaxN4 = 480

// CltMDCTForward is clt_mdct_forward_c: a forward MDCT of N = l.N>>shift
// inputs scaled by 4/N. It reads N/2+overlap samples of in (the C function
// documents that it trashes the input, but the float path does not write
// it) and writes N/2 coefficients to out with the given stride.
func CltMDCTForward(
	l *MDCTLookup,
	in, out []float32,
	window []float32,
	overlap, shift, stride int,
) {
	st := l.KFFT[shift]
	scale := st.Scale

	n := l.N
	trig := l.Trig
	for i := 0; i < shift; i++ {
		n >>= 1
		trig = trig[n:]
	}
	n2 := n >> 1
	n4 := n >> 2

	var fBuf [2 * mdctMaxN4]float32
	var f2Buf [mdctMaxN4]KissFFTCpx
	var f []float32
	var f2 []KissFFTCpx
	if n4 <= mdctMaxN4 {
		f = fBuf[:n2]
		f2 = f2Buf[:n4]
	} else {
		f = make([]float32, n2)
		f2 = make([]KissFFTCpx, n4)
	}

	// Consider the input to be composed of four blocks: [a, b, c, d].
	// Window, shuffle, fold.
	{
		xp1 := overlap >> 1
		xp2 := n2 - 1 + (overlap >> 1)
		yp := 0
		wp1 := overlap >> 1
		wp2 := (overlap >> 1) - 1
		i := 0
		for ; i < ((overlap + 3) >> 2); i++ {
			// Real part arranged as -d-cR, Imag part arranged as -b+aR.
			f[yp] = float32(in[xp1+n2]*window[wp2]) + float32(in[xp2]*window[wp1])
			f[yp+1] = float32(in[xp1]*window[wp1]) - float32(in[xp2-n2]*window[wp2])
			yp += 2
			xp1 += 2
			xp2 -= 2
			wp1 += 2
			wp2 -= 2
		}
		wp1 = 0
		wp2 = overlap - 1
		for ; i < n4-((overlap+3)>>2); i++ {
			// Real part arranged as a-bR, Imag part arranged as -c-dR.
			f[yp] = in[xp2]
			f[yp+1] = in[xp1]
			yp += 2
			xp1 += 2
			xp2 -= 2
		}
		for ; i < n4; i++ {
			// Real part arranged as a-bR, Imag part arranged as -c-dR.
			f[yp] = -float32(in[xp1-n2]*window[wp1]) + float32(in[xp2]*window[wp2])
			f[yp+1] = float32(in[xp1]*window[wp2]) + float32(in[xp2+n2]*window[wp1])
			yp += 2
			xp1 += 2
			xp2 -= 2
			wp1 += 2
			wp2 -= 2
		}
	}
	// Pre-rotation.
	{
		t := trig[:n2]
		bitrev := st.Bitrev
		for i := 0; i < n4; i++ {
			t0 := t[i]
			t1 := t[n4+i]
			re := f[2*i]
			im := f[2*i+1]
			yr := float32(re*t0) - float32(im*t1)
			yi := float32(im*t0) + float32(re*t1)
			f2[bitrev[i]] = KissFFTCpx{R: float32(yr * scale), I: float32(yi * scale)}
		}
	}

	// N/4 complex FFT, does not downscale anymore.
	OpusFFTImpl(st, f2)

	// Post-rotate.
	{
		t := trig[:n2]
		yp1 := 0
		yp2 := stride * (n2 - 1)
		for i := 0; i < n4; i++ {
			fp := f2[i]
			t0 := t[i]
			t1 := t[n4+i]
			yr := float32(fp.I*t1) - float32(fp.R*t0)
			yi := float32(fp.R*t1) + float32(fp.I*t0)
			out[yp1] = yr
			out[yp2] = yi
			yp1 += 2 * stride
			yp2 -= 2 * stride
		}
	}
}

// CltMDCTBackward is clt_mdct_backward_c: an inverse MDCT (no scaling)
// with weighted overlap-add (scales implicitly by 1/2). It reads N/2
// coefficients of in with the given stride and writes N/2+overlap samples
// to out, where N = l.N>>shift.
func CltMDCTBackward(
	l *MDCTLookup,
	in, out []float32,
	window []float32,
	overlap, shift, stride int,
) {
	n := l.N
	trig := l.Trig
	for i := 0; i < shift; i++ {
		n >>= 1
		trig = trig[n:]
	}
	n2 := n >> 1
	n4 := n >> 2
	st := l.KFFT[shift]

	// The C code runs the FFT in place on out+(overlap>>1), viewed as
	// kiss_fft_cpx. Go cannot alias []float32 as []KissFFTCpx without
	// unsafe, so the FFT runs on a scratch buffer that is copied back to
	// out before the post-rotation. The arithmetic is unchanged.
	var fBuf [mdctMaxN4]KissFFTCpx
	var f []KissFFTCpx
	if n4 <= mdctMaxN4 {
		f = fBuf[:n4]
	} else {
		f = make([]KissFFTCpx, n4)
	}

	// Pre-rotate.
	{
		xp1 := 0
		xp2 := stride * (n2 - 1)
		t := trig[:n2]
		bitrev := st.Bitrev
		for i := 0; i < n4; i++ {
			rev := bitrev[i]
			x1 := in[xp1]
			x2 := in[xp2]
			yr := float32(x2*t[i]) + float32(x1*t[n4+i])
			yi := float32(x1*t[i]) - float32(x2*t[n4+i])
			// We swap real and imag because we use an FFT instead of an
			// IFFT.
			f[rev].I = yr
			f[rev].R = yi
			// Storing the pre-rotation directly in the bitrev order.
			xp1 += 2 * stride
			xp2 -= 2 * stride
		}
	}

	OpusFFTImpl(st, f)

	y := out[overlap>>1 : (overlap>>1)+n2]
	for i, c := range f {
		y[2*i] = c.R
		y[2*i+1] = c.I
	}

	// Post-rotate and de-shuffle from both ends of the buffer at once to
	// make it in-place.
	{
		yp0 := 0
		yp1 := n2 - 2
		t := trig[:n2]
		// Loop to (N4+1)>>1 to handle odd N4. When N4 is odd, the middle
		// pair will be computed twice.
		for i := 0; i < (n4+1)>>1; i++ {
			// We swap real and imag because we're using an FFT instead of
			// an IFFT.
			re := y[yp0+1]
			im := y[yp0]
			t0 := t[i]
			t1 := t[n4+i]
			// We'd scale up by 2 here, but instead it's done when mixing
			// the windows.
			yr := float32(re*t0) + float32(im*t1)
			yi := float32(re*t1) - float32(im*t0)
			// We swap real and imag because we're using an FFT instead of
			// an IFFT.
			re = y[yp1+1]
			im = y[yp1]
			y[yp0] = yr
			y[yp1+1] = yi

			t0 = t[n4-i-1]
			t1 = t[n2-i-1]
			// We'd scale up by 2 here, but instead it's done when mixing
			// the windows.
			yr = float32(re*t0) + float32(im*t1)
			yi = float32(re*t1) - float32(im*t0)
			y[yp1] = yr
			y[yp0+1] = yi
			yp0 += 2
			yp1 -= 2
		}
	}

	// Mirror on both sides for TDAC.
	{
		xp1 := overlap - 1
		yp1 := 0
		wp1 := 0
		wp2 := overlap - 1
		for i := 0; i < overlap/2; i++ {
			x1 := out[xp1]
			x2 := out[yp1]
			out[yp1] = float32(x2*window[wp2]) - float32(x1*window[wp1])
			out[xp1] = float32(x2*window[wp1]) + float32(x1*window[wp2])
			yp1++
			xp1--
			wp1++
			wp2--
		}
	}
}

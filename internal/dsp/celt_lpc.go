// Ported from libopus 1.6.1 celt/celt_lpc.h and celt/celt_lpc.c (float
// build).

package dsp

// CeltLPCOrder is CELT_LPC_ORDER.
const CeltLPCOrder = 24

// lpcStackMax bounds the stack scratch buffers of the filters. Larger
// sizes fall back to the heap.
const lpcStackMax = 2048

// CeltLPC is _celt_lpc: it computes p LPC coefficients from the p+1
// autocorrelation values in ac using the Levinson-Durbin recursion.
// preserve reference harmonic-search and numerical stopping decisions
//
//revive:disable-next-line:cognitive-complexity
func CeltLPC(lpc []float32, ac []float32, p int) {
	lpc = lpc[:p]
	ac = ac[:p+1]
	errv := ac[0]

	clear(lpc)
	if ac[0] > 1e-10 {
		for i := 0; i < p; i++ {
			// Sum up this iteration's reflection coefficient.
			var rr float32
			for j := 0; j < i; j++ {
				rr += float32(lpc[j] * ac[i-j])
			}
			rr += ac[i+1]
			r := -FracDiv32(rr, errv)
			// Update LPC coefficients and total error.
			lpc[i] = r
			for j := 0; j < (i+1)>>1; j++ {
				tmp1 := lpc[j]
				tmp2 := lpc[i-1-j]
				lpc[j] = tmp1 + float32(r*tmp2)
				lpc[i-1-j] = tmp2 + float32(r*tmp1)
			}

			errv = errv - float32(float32(r*r)*errv)
			// Bail out once we get 30 dB gain.
			if errv <= float32(.001*ac[0]) {
				break
			}
		}
	}
}

// CeltFIR is celt_fir_c: y[i] = x[i] + sum(num[j]*x[i-j-1]) for i in
// [0, n). x is indexed from -ord, so the caller passes the input with ord
// samples of history before it: xHist[0:ord] is the history and
// xHist[ord:ord+n] is x. x and y must not overlap.
func CeltFIR(xHist []float32, num []float32, y []float32, n, ord int) {
	var rnumBuf [CeltLPCOrder]float32
	var rnum []float32
	if ord <= CeltLPCOrder {
		rnum = rnumBuf[:ord]
	} else {
		rnum = make([]float32, ord)
	}
	for i := 0; i < ord; i++ {
		rnum[i] = num[ord-i-1]
	}
	x := xHist
	i := 0
	for ; i < n-3; i += 4 {
		var sum [4]float32
		sum[0] = x[ord+i]
		sum[1] = x[ord+i+1]
		sum[2] = x[ord+i+2]
		sum[3] = x[ord+i+3]
		XcorrKernel(rnum, x[i:], &sum, ord)
		y[i] = sum[0]
		y[i+1] = sum[1]
		y[i+2] = sum[2]
		y[i+3] = sum[3]
	}
	for ; i < n; i++ {
		sum := x[ord+i]
		for j := 0; j < ord; j++ {
			sum = sum + float32(rnum[j]*x[i+j])
		}
		y[i] = sum
	}
}

// CeltIIR is celt_iir (the non-SMALL_FOOTPRINT version): an all-pole
// filter with ord coefficients den and ord samples of state mem. ord must
// be a multiple of 4. x and y may be the same slice.
func CeltIIR(x []float32, den []float32, y []float32, n, ord int, mem []float32) {
	var rdenBuf [CeltLPCOrder]float32
	var yyBuf [lpcStackMax]float32
	var rden, yy []float32
	if ord <= CeltLPCOrder {
		rden = rdenBuf[:ord]
	} else {
		rden = make([]float32, ord)
	}
	if n+ord <= lpcStackMax {
		yy = yyBuf[:n+ord]
	} else {
		yy = make([]float32, n+ord)
	}
	for i := 0; i < ord; i++ {
		rden[i] = den[ord-i-1]
	}
	i := 0
	for ; i < ord; i++ {
		yy[i] = -mem[ord-i-1]
	}
	for ; i < n+ord; i++ {
		yy[i] = 0
	}
	for i = 0; i < n-3; i += 4 {
		// Unroll by 4 as if it were an FIR filter.
		var sum [4]float32
		sum[0] = x[i]
		sum[1] = x[i+1]
		sum[2] = x[i+2]
		sum[3] = x[i+3]
		XcorrKernel(rden, yy[i:], &sum, ord)

		// Patch up the result to compensate for the fact that this is an
		// IIR.
		yy[i+ord] = -sum[0]
		y[i] = sum[0]
		sum[1] = sum[1] + float32(yy[i+ord]*den[0])
		yy[i+ord+1] = -sum[1]
		y[i+1] = sum[1]
		sum[2] = sum[2] + float32(yy[i+ord+1]*den[0])
		sum[2] = sum[2] + float32(yy[i+ord]*den[1])
		yy[i+ord+2] = -sum[2]
		y[i+2] = sum[2]

		sum[3] = sum[3] + float32(yy[i+ord+2]*den[0])
		sum[3] = sum[3] + float32(yy[i+ord+1]*den[1])
		sum[3] = sum[3] + float32(yy[i+ord]*den[2])
		yy[i+ord+3] = -sum[3]
		y[i+3] = sum[3]
	}
	for ; i < n; i++ {
		sum := x[i]
		for j := 0; j < ord; j++ {
			sum -= float32(rden[j] * yy[i+j])
		}
		yy[i+ord] = sum
		y[i] = sum
	}
	for i = 0; i < ord; i++ {
		mem[i] = y[n-i-1]
	}
}

// CeltAutocorr is _celt_autocorr: ac[k] for k in [0, lag] is the
// autocorrelation of the n samples of x, with x windowed at both ends by
// the first overlap values of window (no windowing when overlap is 0). It
// returns the shift, which is always 0 in the float build.
func CeltAutocorr(x []float32, ac []float32, window []float32, overlap, lag, n int) int {
	fastN := n - lag
	var xxBuf [lpcStackMax]float32
	var xptr []float32
	if overlap == 0 {
		xptr = x[:n]
	} else {
		var xx []float32
		if n <= lpcStackMax {
			xx = xxBuf[:n]
		} else {
			xx = make([]float32, n)
		}
		copy(xx, x[:n])
		for i := 0; i < overlap; i++ {
			w := window[i]
			xx[i] = float32(x[i] * w)
			xx[n-i-1] = float32(x[n-i-1] * w)
		}
		xptr = xx
	}
	shift := 0
	CeltPitchXcorr(xptr, xptr, ac, fastN, lag+1)
	for k := 0; k <= lag; k++ {
		var d float32
		for i := k + fastN; i < n; i++ {
			d = d + float32(xptr[i]*xptr[i-k])
		}
		ac[k] += d
	}
	return shift
}

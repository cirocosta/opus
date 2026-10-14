// Ported from libopus 1.6.1 celt/pitch.h and celt/pitch.c (float build,
// generic C paths).
//
// comb_filter_const_c, which pitch.h declares, lives in celt/celt.c and
// belongs to the celt package.

package dsp

import "github.com/cirocosta/opus/internal/kernels"

// pitchStackMax bounds the stack scratch buffers of the pitch search.
// Larger sizes fall back to the heap.
const pitchStackMax = 1032

// XcorrKernel is xcorr_kernel_c: sum[k] += x[j]*y[j+k] for k in [0, 4)
// and j in [0, n). n must be at least 3; y must hold n+3 values.
func XcorrKernel(x, y []float32, sum *[4]float32, n int) {
	kernels.Xcorr4(x, y, sum, n)
}

// DualInnerProd is dual_inner_prod_c: it returns the inner products of the
// first n values of x with y01 and with y02.
func DualInnerProd(x, y01, y02 []float32, n int) (xy1, xy2 float32) {
	x = x[:n]
	y01 = y01[:n]
	y02 = y02[:n]
	var xy01, xy02 float32
	for i := range x {
		xy01 = xy01 + float32(x[i]*y01[i])
		xy02 = xy02 + float32(x[i]*y02[i])
	}
	return xy01, xy02
}

// CeltInnerProd is celt_inner_prod_c: the inner product of the first n
// values of x and y.
func CeltInnerProd(x, y []float32, n int) float32 {
	x = x[:n]
	y = y[:n]
	var xy float32
	for i := range x {
		xy = xy + float32(x[i]*y[i])
	}
	return xy
}

// CeltPitchXcorr is celt_pitch_xcorr_c (the unrolled version):
// xcorr[i] = sum(x[j]*y[i+j]) for i in [0, maxPitch) and j in [0, n).
func CeltPitchXcorr(x, y []float32, xcorr []float32, n, maxPitch int) {
	kernels.CrossCorrelation(x, y, xcorr, n, maxPitch)
}

// findBestPitch is find_best_pitch.
// preserve reference harmonic-search and numerical stopping decisions
//
//revive:disable-next-line:cognitive-complexity
func findBestPitch(xcorr []float32, y []float32, n, maxPitch int, bestPitch *[2]int) {
	syy := float32(1)
	var bestNum [2]float32
	var bestDen [2]float32
	bestNum[0] = -1
	bestNum[1] = -1
	bestDen[0] = 0
	bestDen[1] = 0
	bestPitch[0] = 0
	bestPitch[1] = 1
	for j := 0; j < n; j++ {
		syy = syy + float32(y[j]*y[j])
	}
	for i := 0; i < maxPitch; i++ {
		if xcorr[i] > 0 {
			xcorr16 := xcorr[i]
			// Considering the range of xcorr16, this should avoid both
			// underflows and overflows (inf) when squaring xcorr16.
			xcorr16 = float32(xcorr16 * 1e-12)
			num := float32(xcorr16 * xcorr16)
			if float32(num*bestDen[1]) > float32(bestNum[1]*syy) {
				if float32(num*bestDen[0]) > float32(bestNum[0]*syy) {
					bestNum[1] = bestNum[0]
					bestDen[1] = bestDen[0]
					bestPitch[1] = bestPitch[0]
					bestNum[0] = num
					bestDen[0] = syy
					bestPitch[0] = i
				} else {
					bestNum[1] = num
					bestDen[1] = syy
					bestPitch[1] = i
				}
			}
		}
		syy += float32(y[i+n]*y[i+n]) - float32(y[i]*y[i])
		syy = Max32(1, syy)
	}
}

// celtFir5 is celt_fir5: an in-place 5-tap FIR filter.
func celtFir5(x []float32, num []float32, n int) {
	num0 := num[0]
	num1 := num[1]
	num2 := num[2]
	num3 := num[3]
	num4 := num[4]
	var mem0, mem1, mem2, mem3, mem4 float32
	x = x[:n]
	for i := range x {
		sum := x[i]
		sum = sum + float32(num0*mem0)
		sum = sum + float32(num1*mem1)
		sum = sum + float32(num2*mem2)
		sum = sum + float32(num3*mem3)
		sum = sum + float32(num4*mem4)
		mem4 = mem3
		mem3 = mem2
		mem2 = mem1
		mem1 = mem0
		mem0 = x[i]
		x[i] = sum
	}
}

// PitchDownsample is pitch_downsample: it low-passes and decimates the c
// channels of x by factor into the n values of xLp, then whitens xLp with
// a 4th-order LPC.
func PitchDownsample(x [][]float32, xLp []float32, n, c, factor int) {
	var ac [5]float32
	tmp := Q15One
	var lpc [4]float32
	var lpc2 [5]float32
	c1 := float32(.8)
	offset := factor / 2
	x0 := x[0]
	for i := 1; i < n; i++ {
		xLp[i] = float32(
			.25*x0[factor*i-offset],
		) + float32(
			.25*x0[factor*i+offset],
		) + float32(
			.5*x0[factor*i],
		)
	}
	xLp[0] = float32(.25*x0[offset]) + float32(.5*x0[0])
	if c == 2 {
		x1 := x[1]
		for i := 1; i < n; i++ {
			xLp[i] += float32(
				.25*x1[factor*i-offset],
			) + float32(
				.25*x1[factor*i+offset],
			) + float32(
				.5*x1[factor*i],
			)
		}
		xLp[0] += float32(.25*x1[offset]) + float32(.5*x1[0])
	}
	CeltAutocorr(xLp, ac[:], nil, 0, 4, n)

	// Noise floor -40 dB.
	ac[0] = float32(ac[0] * 1.0001)
	// Lag windowing.
	for i := 1; i <= 4; i++ {
		// ac[i] *= exp(-.5*(2*M_PI*.002*i)*(2*M_PI*.002*i));
		fi := float32(i)
		ac[i] -= float32(float32(ac[i]*float32(.008*fi)) * float32(.008*fi))
	}

	CeltLPC(lpc[:], ac[:], 4)
	for i := 0; i < 4; i++ {
		tmp = float32(.9 * tmp)
		lpc[i] = float32(lpc[i] * tmp)
	}
	// Add a zero.
	lpc2[0] = lpc[0] + .8
	lpc2[1] = lpc[1] + float32(c1*lpc[0])
	lpc2[2] = lpc[2] + float32(c1*lpc[1])
	lpc2[3] = lpc[3] + float32(c1*lpc[2])
	lpc2[4] = float32(c1 * lpc[3])
	celtFir5(xLp, lpc2[:], n)
}

// PitchSearch is pitch_search: it returns the pitch period (in samples of
// xLp) that best matches xLp (n values) within y (n+maxPitch values).
// preserve reference harmonic-search and numerical stopping decisions
//
//revive:disable-next-line:cognitive-complexity
func PitchSearch(xLp []float32, y []float32, n, maxPitch int) int {
	bestPitch := [2]int{0, 0}
	lag := n + maxPitch

	var xLp4Buf, yLp4Buf, xcorrBuf [pitchStackMax]float32
	var xLp4, yLp4, xcorr []float32
	if n>>2 <= pitchStackMax {
		xLp4 = xLp4Buf[:n>>2]
	} else {
		xLp4 = make([]float32, n>>2)
	}
	if lag>>2 <= pitchStackMax {
		yLp4 = yLp4Buf[:lag>>2]
	} else {
		yLp4 = make([]float32, lag>>2)
	}
	if maxPitch>>1 <= pitchStackMax {
		xcorr = xcorrBuf[:maxPitch>>1]
	} else {
		xcorr = make([]float32, maxPitch>>1)
	}

	// Downsample by 2 again.
	for j := range xLp4 {
		xLp4[j] = xLp[2*j]
	}
	for j := range yLp4 {
		yLp4[j] = y[2*j]
	}

	// Coarse search with 4x decimation.
	CeltPitchXcorr(xLp4, yLp4, xcorr, n>>2, maxPitch>>2)

	findBestPitch(xcorr, yLp4, n>>2, maxPitch>>2, &bestPitch)

	// Finer search with 2x decimation.
	for i := 0; i < maxPitch>>1; i++ {
		xcorr[i] = 0
		if absInt(i-2*bestPitch[0]) > 2 && absInt(i-2*bestPitch[1]) > 2 {
			continue
		}
		sum := CeltInnerProd(xLp, y[i:], n>>1)
		xcorr[i] = Max32(-1, sum)
	}
	findBestPitch(xcorr, y, n>>1, maxPitch>>1, &bestPitch)

	// Refine by pseudo-interpolation.
	var offset int
	if bestPitch[0] > 0 && bestPitch[0] < (maxPitch>>1)-1 {
		a := xcorr[bestPitch[0]-1]
		b := xcorr[bestPitch[0]]
		c := xcorr[bestPitch[0]+1]
		if (c - a) > float32(.7*(b-a)) {
			offset = 1
		} else if (a - c) > float32(.7*(b-c)) {
			offset = -1
		} else {
			offset = 0
		}
	} else {
		offset = 0
	}
	return 2*bestPitch[0] - offset
}

// computePitchGain is compute_pitch_gain (float version).
func computePitchGain(xy, xx, yy float32) float32 {
	return xy / CeltSqrt(1+float32(xx*yy))
}

var secondCheck = [16]int{0, 0, 3, 2, 3, 2, 5, 2, 3, 2, 3, 2, 5, 2, 3, 2}

// RemoveDoubling is remove_doubling: it checks the pitch *t0 for period
// doubling and returns the pitch gain. x holds maxperiod history samples
// followed by n samples. *t0 is updated in place.
// preserve reference harmonic-search and numerical stopping decisions
//
//revive:disable-next-line:cognitive-complexity
func RemoveDoubling(
	x []float32,
	maxperiod, minperiod, n int,
	t0 *int,
	prevPeriod int,
	prevGain float32,
) float32 {
	minperiod0 := minperiod
	maxperiod /= 2
	minperiod /= 2
	*t0 /= 2
	prevPeriod /= 2
	n /= 2
	// x += maxperiod: xb is the index of x[0] in the C code.
	xb := maxperiod
	if *t0 >= maxperiod {
		*t0 = maxperiod - 1
	}
	T := *t0
	T0 := *t0

	var yyLookupBuf [pitchStackMax]float32
	var yyLookup []float32
	if maxperiod+1 <= pitchStackMax {
		yyLookup = yyLookupBuf[:maxperiod+1]
	} else {
		yyLookup = make([]float32, maxperiod+1)
	}

	xx, xy := DualInnerProd(x[xb:], x[xb:], x[xb-T0:], n)
	yyLookup[0] = xx
	yy := xx
	for i := 1; i <= maxperiod; i++ {
		yy = yy + float32(x[xb-i]*x[xb-i]) - float32(x[xb+n-i]*x[xb+n-i])
		yyLookup[i] = Max32(0, yy)
	}
	yy = yyLookup[T0]
	bestXy := xy
	bestYy := yy
	g0 := computePitchGain(xy, xx, yy)
	g := g0
	// Look for any pitch at T/k.
	for k := 2; k <= 15; k++ {
		var T1b int
		var cont float32
		T1 := int(uint32(2*T0+k) / uint32(2*k))
		if T1 < minperiod {
			break
		}
		// Look for another strong correlation at T1b.
		if k == 2 {
			if T1+T0 > maxperiod {
				T1b = T0
			} else {
				T1b = T0 + T1
			}
		} else {
			T1b = int(uint32(2*secondCheck[k]*T0+k) / uint32(2*k))
		}
		var xy2 float32
		xy, xy2 = DualInnerProd(x[xb:], x[xb-T1:], x[xb-T1b:], n)
		xy = float32(.5 * (xy + xy2))
		yy = float32(.5 * (yyLookup[T1] + yyLookup[T1b]))
		g1 := computePitchGain(xy, xx, yy)
		if absInt(T1-prevPeriod) <= 1 {
			cont = prevGain
		} else if absInt(T1-prevPeriod) <= 2 && 5*k*k < T0 {
			cont = float32(.5 * prevGain)
		} else {
			cont = 0
		}
		thresh := Max16(.3, float32(.7*g0)-cont)
		// Bias against very high pitch (very short period) to avoid
		// false-positives due to short-term correlation.
		if T1 < 3*minperiod {
			thresh = Max16(.4, float32(.85*g0)-cont)
		} else if T1 < 2*minperiod {
			thresh = Max16(.5, float32(.9*g0)-cont)
		}
		if g1 > thresh {
			bestXy = xy
			bestYy = yy
			T = T1
			g = g1
		}
	}
	bestXy = Max32(0, bestXy)
	var pg float32
	if bestYy <= bestXy {
		pg = Q15One
	} else {
		pg = FracDiv32(bestXy, bestYy+1)
	}

	var xcorr [3]float32
	for k := 0; k < 3; k++ {
		xcorr[k] = CeltInnerProd(x[xb:], x[xb-(T+k-1):], n)
	}
	var offset int
	if (xcorr[2] - xcorr[0]) > float32(.7*(xcorr[1]-xcorr[0])) {
		offset = 1
	} else if (xcorr[0] - xcorr[2]) > float32(.7*(xcorr[1]-xcorr[2])) {
		offset = -1
	} else {
		offset = 0
	}
	if pg > g {
		pg = g
	}
	*t0 = 2*T + offset
	if *t0 < minperiod0 {
		*t0 = minperiod0
	}
	return pg
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

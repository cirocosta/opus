// Ported from libopus 1.6.1 celt/bands.h and celt/bands.c (float build,
// no ENABLE_QEXT, no FUZZING, no MEASURE_NORM_MSE).

package celt

import (
	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
)

// hysteresisDecision is hysteresis_decision.
func hysteresisDecision(val float32, thresholds, hysteresis []float32, N, prev int) int {
	i := 0
	for ; i < N; i++ {
		if val < thresholds[i] {
			break
		}
	}
	if i > prev && val < thresholds[prev]+hysteresis[prev] {
		i = prev
	}
	if i < prev && val > thresholds[prev-1]-hysteresis[prev-1] {
		i = prev
	}
	return i
}

// celtLCGRand is celt_lcg_rand.
func celtLCGRand(seed uint32) uint32 {
	return 1664525*seed + 1013904223
}

// bitexactCos is bitexact_cos: a cos() approximation designed to be
// bit-exact on any platform. Bit exactness with this approximation is
// important because it has an impact on the bit allocation.
func bitexactCos(x int16) int16 {
	tmp := (4096 + int32(x)*int32(x)) >> 13
	x2 := int16(tmp)
	x2 = int16(
		int32(
			32767-x2,
		) + dsp.FracMul16(
			int32(x2),
			-7651+dsp.FracMul16(int32(x2), 8277+dsp.FracMul16(-626, int32(x2))),
		),
	)
	return int16(1 + int32(x2))
}

// bitexactLog2tan is bitexact_log2tan.
func bitexactLog2tan(isin, icos int) int {
	lc := entcode.Ilog(uint32(icos))
	ls := entcode.Ilog(uint32(isin))
	icos <<= uint(15 - lc)
	isin <<= uint(15 - ls)
	return (ls-lc)*(1<<11) +
		int(dsp.FracMul16(int32(isin), dsp.FracMul16(int32(isin), -2597)+7932)) -
		int(dsp.FracMul16(int32(icos), dsp.FracMul16(int32(icos), -2597)+7932))
}

// computeBandEnergies is compute_band_energies: the amplitude (sqrt
// energy) in each of the bands.
func computeBandEnergies(m *Mode, X []float32, bandE []float32, end, C, LM int) {
	eBands := m.EBands
	N := m.ShortMdctSize << uint(LM)
	for c := 0; c < C; c++ {
		for i := 0; i < end; i++ {
			off := c*N + int(eBands[i])<<uint(LM)
			n := int(eBands[i+1]-eBands[i]) << uint(LM)
			sum := 1e-27 + dsp.CeltInnerProd(X[off:], X[off:], n)
			bandE[i+c*m.NbEBands] = dsp.CeltSqrt(sum)
		}
	}
}

// normaliseBands is normalise_bands: it normalises each band such that the
// energy is one.
func normaliseBands(m *Mode, freq []float32, X []float32, bandE []float32, end, C, M int) {
	eBands := m.EBands
	N := M * m.ShortMdctSize
	for c := 0; c < C; c++ {
		for i := 0; i < end; i++ {
			g := 1 / (1e-27 + bandE[i+c*m.NbEBands])
			for j := M * int(eBands[i]); j < M*int(eBands[i+1]); j++ {
				X[j+c*N] = float32(freq[j+c*N] * g)
			}
		}
	}
}

// denormaliseBands is denormalise_bands: it de-normalises the energy to
// produce the synthesis from the unit-energy bands.
func denormaliseBands(
	m *Mode,
	X []float32,
	freq []float32,
	bandLogE []float32,
	start, end, M, downsample int,
	silence bool,
) {
	eBands := m.EBands
	N := M * m.ShortMdctSize
	bound := M * int(eBands[end])
	if downsample != 1 {
		bound = min(bound, N/downsample)
	}
	if silence {
		bound = 0
		start = 0
		end = 0
	}
	f := M * int(eBands[start])
	x := f
	clear(freq[:f])
	for i := start; i < end; i++ {
		j := M * int(eBands[i])
		bandEnd := M * int(eBands[i+1])
		lg := bandLogE[i] + eMeans[i]
		g := dsp.CeltExp2Db(dsp.Min32(32, lg))
		for {
			freq[f] = float32(X[x] * g)
			f++
			x++
			j++
			if j >= bandEnd {
				break
			}
		}
	}
	clear(freq[bound:N])
}

// antiCollapse is anti_collapse: it prevents energy collapse for
// transients with multiple short MDCTs.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func antiCollapse(m *Mode, inputX []float32, collapseMasks []uint8, LM, C, size, start, end int,
	logE, prev1logE, prev2logE []float32, pulses []int, seed uint32, encode bool,
) {
	for i := start; i < end; i++ {
		N0 := int(m.EBands[i+1] - m.EBands[i])
		// depth in 1/8 bits
		depth := int(entcode.Udiv(uint32(1+pulses[i]), uint32(N0))) >> uint(LM)

		thresh := float32(.5 * dsp.CeltExp2(float32(-.125*float32(depth))))
		sqrt1 := dsp.CeltRsqrt(float32(N0 << uint(LM)))

		for c := 0; c < C; c++ {
			renormalize := false
			prev1 := prev1logE[c*m.NbEBands+i]
			prev2 := prev2logE[c*m.NbEBands+i]
			if !encode && C == 1 {
				prev1 = dsp.Max32(prev1, prev1logE[m.NbEBands+i])
				prev2 = dsp.Max32(prev2, prev2logE[m.NbEBands+i])
			}
			Ediff := logE[c*m.NbEBands+i] - dsp.Min32(prev1, prev2)
			Ediff = dsp.Max32(0, Ediff)

			// r needs to be multiplied by 2 or 2*sqrt(2) depending on LM
			// because short blocks don't have the same energy as long.
			r := float32(2 * dsp.CeltExp2Db(-Ediff))
			if LM == 3 {
				r = float32(r * 1.41421356)
			}
			r = dsp.Min16(thresh, r)
			r = float32(r * sqrt1)
			X := inputX[c*size+int(m.EBands[i])<<uint(LM):]
			for k := 0; k < 1<<uint(LM); k++ {
				// Detect collapse.
				if collapseMasks[i*C+c]&(1<<uint(k)) == 0 {
					// Fill with noise.
					for j := 0; j < N0; j++ {
						seed = celtLCGRand(seed)
						if seed&0x8000 != 0 {
							X[(j<<uint(LM))+k] = r
						} else {
							X[(j<<uint(LM))+k] = -r
						}
					}
					renormalize = true
				}
			}
			// We just added some energy, so we need to renormalise.
			if renormalize {
				renormaliseVector(X, N0<<uint(LM), 1)
			}
		}
	}
}

// computeChannelWeights is compute_channel_weights.
func computeChannelWeights(Ex, Ey float32, w *[2]float32) {
	minE := dsp.Min32(Ex, Ey)
	// Adjustment to make the weights a bit more conservative.
	Ex = Ex + minE/3
	Ey = Ey + minE/3
	w[0] = Ex
	w[1] = Ey
}

// intensityStereo is intensity_stereo.
func intensityStereo(m *Mode, X, Y []float32, bandE []float32, bandID, N int) {
	i := bandID
	left := bandE[i]
	right := bandE[i+m.NbEBands]
	norm := dsp.Epsilon + dsp.CeltSqrt(dsp.Epsilon+float32(left*left)+float32(right*right))
	a1 := left / norm
	a2 := right / norm
	for j := 0; j < N; j++ {
		X[j] = float32(a1*X[j]) + float32(a2*Y[j])
		// Side is not encoded, no need to calculate.
	}
}

// stereoSplit is stereo_split.
func stereoSplit(X, Y []float32, N int) {
	for j := 0; j < N; j++ {
		l := float32(.70710678 * X[j])
		r := float32(.70710678 * Y[j])
		X[j] = l + r
		Y[j] = r - l
	}
}

// stereoMerge is stereo_merge.
func stereoMerge(X, Y []float32, mid float32, N int) {
	// Compute the norm of X+Y and X-Y as |X|^2 + |Y|^2 +/- sum(xy).
	xp := dsp.CeltInnerProd(Y, X, N)
	side := dsp.CeltInnerProd(Y, Y, N)
	// Compensating for the mid normalization.
	xp = float32(mid * xp)
	// mid and side are in Q15, not Q14 like X and Y.
	El := float32(mid*mid) + side - float32(2*xp)
	Er := float32(mid*mid) + side + float32(2*xp)
	if Er < 6e-4 || El < 6e-4 {
		copy(Y[:N], X[:N])
		return
	}

	t := El
	lgain := dsp.CeltRsqrtNorm32(t)
	t = Er
	rgain := dsp.CeltRsqrtNorm32(t)

	for j := 0; j < N; j++ {
		// Apply mid scaling (side is already scaled).
		l := float32(mid * X[j])
		r := Y[j]
		X[j] = float32(lgain * (l - r))
		Y[j] = float32(rgain * (l + r))
	}
}

// spreadingDecision is spreading_decision: it decides whether we should
// spread the pulses in the current frame.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func spreadingDecision(m *Mode, X []float32, average *int, lastDecision int, hfAverage *int,
	tapsetDecision *int, updateHF bool, end, C, M int, spreadWeight []int,
) int {
	sum := 0
	nbBands := 0
	eBands := m.EBands
	hfSum := 0

	N0 := M * m.ShortMdctSize

	if M*int(eBands[end]-eBands[end-1]) <= 8 {
		return spreadNone
	}
	for c := 0; c < C; c++ {
		for i := 0; i < end; i++ {
			var tcount [3]int
			x := X[M*int(eBands[i])+c*N0:]
			N := M * int(eBands[i+1]-eBands[i])
			if N <= 8 {
				continue
			}
			// Compute rough CDF of |x[j]|.
			for j := 0; j < N; j++ {
				x2N := float32(float32(x[j]*x[j]) * float32(N))
				if x2N < 0.25 {
					tcount[0]++
				}
				if x2N < 0.0625 {
					tcount[1]++
				}
				if x2N < 0.015625 {
					tcount[2]++
				}
			}

			// Only include four last bands (8 kHz and up).
			if i > m.NbEBands-4 {
				hfSum += int(entcode.Udiv(uint32(32*(tcount[1]+tcount[0])), uint32(N)))
			}
			tmp := b2i(2*tcount[2] >= N) + b2i(2*tcount[1] >= N) + b2i(2*tcount[0] >= N)
			sum += tmp * spreadWeight[i]
			nbBands += spreadWeight[i]
		}
	}

	if updateHF {
		if hfSum != 0 {
			hfSum = int(entcode.Udiv(uint32(hfSum), uint32(C*(4-m.NbEBands+end))))
		}
		*hfAverage = (*hfAverage + hfSum) >> 1
		hfSum = *hfAverage
		switch *tapsetDecision {
		case 2:
			hfSum += 4
		case 0:
			hfSum -= 4
		}
		if hfSum > 22 {
			*tapsetDecision = 2
		} else if hfSum > 18 {
			*tapsetDecision = 1
		} else {
			*tapsetDecision = 0
		}
	}
	sum = int(entcode.Udiv(uint32(int32(sum)<<8), uint32(nbBands)))
	// Recursive averaging.
	sum = (sum + *average) >> 1
	*average = sum
	// Hysteresis.
	sum = (3*sum + (((3 - lastDecision) << 7) + 64) + 2) >> 2
	switch {
	case sum < 80:
		return spreadAggressive
	case sum < 256:
		return spreadNormal
	case sum < 384:
		return spreadLight
	}
	return spreadNone
}

// orderyTable is ordery_table: the indexing table for converting from
// natural Hadamard to ordery Hadamard. This is essentially a bit-reversed
// Gray, on top of which we've added an inversion of the order because we
// want the DC at the end rather than the beginning. The lines are for
// N=2, 4, 8, 16.
var orderyTable = [...]int{
	1, 0,
	3, 0, 2, 1,
	7, 0, 4, 3, 6, 1, 5, 2,
	15, 0, 8, 7, 12, 3, 11, 4, 14, 1, 9, 6, 13, 2, 10, 5,
}

// deinterleaveHadamard is deinterleave_hadamard.
func deinterleaveHadamard(X []float32, N0, stride int, hadamard bool) {
	var tmpBuf [maxBandSize]float32
	N := N0 * stride
	tmp := tmpBuf[:N]
	if hadamard {
		ordery := orderyTable[stride-2:]
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[ordery[i]*N0+j] = X[j*stride+i]
			}
		}
		copy(X[:N], tmp)
		return
	}
	for i := 0; i < stride; i++ {
		for j := 0; j < N0; j++ {
			tmp[i*N0+j] = X[j*stride+i]
		}
	}
	copy(X[:N], tmp)
}

// interleaveHadamard is interleave_hadamard.
func interleaveHadamard(X []float32, N0, stride int, hadamard bool) {
	var tmpBuf [maxBandSize]float32
	N := N0 * stride
	tmp := tmpBuf[:N]
	if hadamard {
		ordery := orderyTable[stride-2:]
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[j*stride+i] = X[ordery[i]*N0+j]
			}
		}
		copy(X[:N], tmp)
		return
	}
	for i := 0; i < stride; i++ {
		for j := 0; j < N0; j++ {
			tmp[j*stride+i] = X[i*N0+j]
		}
	}
	copy(X[:N], tmp)
}

// haar1 is haar1.
func haar1(X []float32, N0, stride int) {
	N0 >>= 1
	for i := 0; i < stride; i++ {
		for j := 0; j < N0; j++ {
			tmp1 := float32(.70710678 * X[stride*2*j+i])
			tmp2 := float32(.70710678 * X[stride*(2*j+1)+i])
			X[stride*2*j+i] = tmp1 + tmp2
			X[stride*(2*j+1)+i] = tmp1 - tmp2
		}
	}
}

var exp2Table8 = [8]int16{16384, 17866, 19483, 21247, 23170, 25267, 27554, 30048}

// computeQn is compute_qn.
func computeQn(N, b, offset, pulseCap int, stereo bool) int {
	N2 := 2*N - 1
	if stereo && N == 2 {
		N2--
	}
	// The upper limit ensures that in a stereo split with itheta==16384,
	// we'll always have enough bits left over to code at least one pulse
	// in the side; otherwise it would collapse, since it doesn't get
	// folded.
	qb := int(entcode.Sudiv(int32(b+N2*offset), int32(N2)))
	qb = min(b-pulseCap-(4<<bitRes), qb)

	qb = min(8<<bitRes, qb)

	var qn int
	if qb < (1 << bitRes >> 1) {
		qn = 1
	} else {
		qn = int(exp2Table8[qb&0x7]) >> uint(14-(qb>>bitRes))
		qn = (qn + 1) >> 1 << 1
	}
	return qn
}

// bandCtx is struct band_ctx.
type bandCtx struct {
	encode          bool
	resynth         bool
	m               *Mode
	i               int
	intensity       int
	spread          int
	tfChange        int
	ec              rangeCoder
	remainingBits   int32
	bandE           []float32
	seed            uint32
	thetaRound      int
	disableInv      bool
	avoidSplitNoise bool
}

// splitCtx is struct split_ctx.
type splitCtx struct {
	inv    bool
	imid   int
	iside  int
	delta  int
	itheta int
	qalloc int
}

// computeTheta is compute_theta.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func computeTheta(
	ctx *bandCtx,
	sctx *splitCtx,
	X, Y []float32,
	N int,
	b *int,
	B, B0, LM int,
	stereo bool,
	fill *int,
) {
	itheta := 0
	var delta, imid, iside int
	inv := false
	encode := ctx.encode
	m := ctx.m
	i := ctx.i
	intensity := ctx.intensity
	ec := ctx.ec
	bandE := ctx.bandE

	// Decide on the resolution to give to the split parameter theta.
	pulseCap := int(m.LogN[i]) + LM*(1<<bitRes)
	offset := pulseCap >> 1
	if stereo && N == 2 {
		offset -= qthetaOffsetTwophase
	} else {
		offset -= qthetaOffset
	}
	qn := computeQn(N, *b, offset, pulseCap, stereo)
	if stereo && i >= intensity {
		qn = 1
	}
	if encode {
		// theta is the atan() of the ratio between the (normalized) side
		// and mid. With just that parameter, we can re-scale both mid and
		// side because we know that 1) they have unit norm and 2) they are
		// orthogonal.
		ithetaQ30 := stereoItheta(X, Y, stereo, N)
		itheta = int(ithetaQ30 >> 16)
	}
	tell := ec.tellFrac()
	if qn != 1 {
		if encode {
			if !stereo || ctx.thetaRound == 0 {
				itheta = int((int32(itheta)*int32(qn) + 8192) >> 14)
				if !stereo && ctx.avoidSplitNoise && itheta > 0 && itheta < qn {
					// Check if the selected value of theta will cause the
					// bit allocation to inject noise on one side. If so,
					// make sure the energy of that side is zero.
					unquantized := int(entcode.Udiv(uint32(int32(itheta)*16384), uint32(qn)))
					imid = int(bitexactCos(int16(unquantized)))
					iside = int(bitexactCos(int16(16384 - unquantized)))
					delta = int(dsp.FracMul16(int32((N-1)<<7), int32(bitexactLog2tan(iside, imid))))
					if delta > *b {
						itheta = qn
					} else if delta < -*b {
						itheta = 0
					}
				}
			} else {
				// Bias quantization towards itheta=0 and itheta=16384.
				bias := -32767 / qn
				if itheta > 8192 {
					bias = 32767 / qn
				}
				down := min(qn-1, max(0, int((int32(itheta)*int32(qn)+int32(bias))>>14)))
				if ctx.thetaRound < 0 {
					itheta = down
				} else {
					itheta = down + 1
				}
			}
		}
		// Entropy coding of the angle. We use a uniform pdf for the time
		// split, a step for stereo, and a triangular one for the rest.
		if stereo && N > 2 {
			p0 := 3
			x := itheta
			x0 := qn / 2
			ft := p0*(x0+1) + x0
			// Use a probability of p0 up to itheta=8192 and then use 1
			// after.
			if encode {
				var fl, fh int
				if x <= x0 {
					fl = p0 * x
					fh = p0 * (x + 1)
				} else {
					fl = (x - 1 - x0) + (x0+1)*p0
					fh = (x - x0) + (x0+1)*p0
				}
				ec.enc.Encode(uint32(fl), uint32(fh), uint32(ft))
			} else {
				fs := int(ec.dec.Decode(uint32(ft)))
				if fs < (x0+1)*p0 {
					x = fs / p0
				} else {
					x = x0 + 1 + (fs - (x0+1)*p0)
				}
				var fl, fh int
				if x <= x0 {
					fl = p0 * x
					fh = p0 * (x + 1)
				} else {
					fl = (x - 1 - x0) + (x0+1)*p0
					fh = (x - x0) + (x0+1)*p0
				}
				ec.dec.Update(uint32(fl), uint32(fh), uint32(ft))
				itheta = x
			}
		} else if B0 > 1 || stereo {
			// Uniform pdf.
			if encode {
				ec.enc.EncUint(uint32(itheta), uint32(qn+1))
			} else {
				itheta = int(ec.dec.DecUint(uint32(qn + 1)))
			}
		} else {
			var fs int
			ft := ((qn >> 1) + 1) * ((qn >> 1) + 1)
			if encode {
				var fl int
				if itheta <= (qn >> 1) {
					fs = itheta + 1
					fl = itheta * (itheta + 1) >> 1
				} else {
					fs = qn + 1 - itheta
					fl = ft - ((qn + 1 - itheta) * (qn + 2 - itheta) >> 1)
				}
				ec.enc.Encode(uint32(fl), uint32(fl+fs), uint32(ft))
			} else {
				// Triangular pdf.
				fl := 0
				fm := int(ec.dec.Decode(uint32(ft)))

				if fm < ((qn >> 1) * ((qn >> 1) + 1) >> 1) {
					itheta = int(dsp.Isqrt32(8*uint32(fm)+1)-1) >> 1
					fs = itheta + 1
					fl = itheta * (itheta + 1) >> 1
				} else {
					itheta = (2*(qn+1) - int(dsp.Isqrt32(8*uint32(ft-fm-1)+1))) >> 1
					fs = qn + 1 - itheta
					fl = ft - ((qn + 1 - itheta) * (qn + 2 - itheta) >> 1)
				}

				ec.dec.Update(uint32(fl), uint32(fl+fs), uint32(ft))
			}
		}
		itheta = int(entcode.Udiv(uint32(int32(itheta)*16384), uint32(qn)))
		if encode && stereo {
			if itheta == 0 {
				intensityStereo(m, X, Y, bandE, i, N)
			} else {
				stereoSplit(X, Y, N)
			}
		}
		// NOTE: Renormalising X and Y *may* help fixed-point a bit at very
		// high rate. Let's do that at higher complexity.
	} else if stereo {
		if encode {
			inv = itheta > 8192 && !ctx.disableInv
			if inv {
				for j := 0; j < N; j++ {
					Y[j] = -Y[j]
				}
			}
			intensityStereo(m, X, Y, bandE, i, N)
		}
		if *b > 2<<bitRes && ctx.remainingBits > 2<<bitRes {
			if encode {
				ec.enc.EncBitLogp(b2i(inv), 2)
			} else {
				inv = ec.dec.DecBitLogp(2) != 0
			}
		} else {
			inv = false
		}
		// inv flag override to avoid problems with downmixing.
		if ctx.disableInv {
			inv = false
		}
		itheta = 0
	}
	qalloc := ec.tellFrac() - tell
	*b -= qalloc

	switch itheta {
	case 0:
		imid = 32767
		iside = 0
		*fill &= (1 << uint(B)) - 1
		delta = -16384
	case 16384:
		imid = 0
		iside = 32767
		*fill &= ((1 << uint(B)) - 1) << uint(B)
		delta = 16384
	default:
		imid = int(bitexactCos(int16(itheta)))
		iside = int(bitexactCos(int16(16384 - itheta)))
		// This is the mid vs side allocation that minimizes squared error
		// in that band.
		delta = int(dsp.FracMul16(int32((N-1)<<7), int32(bitexactLog2tan(iside, imid))))
	}

	sctx.inv = inv
	sctx.imid = imid
	sctx.iside = iside
	sctx.delta = delta
	sctx.itheta = itheta
	sctx.qalloc = qalloc
}

// quantBandN1 is quant_band_n1. Y is nil for mono.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantBandN1(ctx *bandCtx, X, Y []float32, lowbandOut []float32) uint {
	encode := ctx.encode
	ec := ctx.ec

	stereo := Y != nil
	x := X
	for c := 0; c < 1+b2i(stereo); c++ {
		sign := 0
		if ctx.remainingBits >= 1<<bitRes {
			if encode {
				sign = b2i(x[0] < 0)
				ec.enc.EncBits(uint32(sign), 1)
			} else {
				sign = int(ec.dec.DecBits(1))
			}
			ctx.remainingBits -= 1 << bitRes
		}
		if ctx.resynth {
			if sign != 0 {
				x[0] = -1
			} else {
				x[0] = 1
			}
		}
		x = Y
	}
	if lowbandOut != nil {
		lowbandOut[0] = X[0]
	}
	return 1
}

// quantPartition is quant_partition: it encodes or decodes a mono
// partition. It can split the band in two and transmit the energy
// difference with the two half-bands. It can be called recursively so
// bands can end up being split in 8 parts. lowband may be nil.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantPartition(
	ctx *bandCtx,
	X []float32,
	N, b, B int,
	lowband []float32,
	LM int,
	gain float32,
	fill int,
) uint {
	B0 := B
	var cm uint
	encode := ctx.encode
	m := ctx.m
	i := ctx.i
	spread := ctx.spread
	ec := ctx.ec

	// If we need 1.5 more bit than we can produce, split the band in two.
	cache := m.Cache.Bits[m.Cache.Index[(LM+1)*m.NbEBands+i]:]
	if LM != -1 && b > int(cache[cache[0]])+12 && N > 2 {
		var sctx splitCtx
		var nextLowband2 []float32

		N >>= 1
		Y := X[N:]
		LM--
		if B == 1 {
			fill = (fill & 1) | (fill << 1)
		}
		B = (B + 1) >> 1

		computeTheta(ctx, &sctx, X, Y, N, &b, B, B0, LM, false, &fill)
		imid := sctx.imid
		iside := sctx.iside
		delta := sctx.delta
		itheta := sctx.itheta
		qalloc := sctx.qalloc
		mid := float32((1.0 / 32768) * float32(imid))
		side := float32((1.0 / 32768) * float32(iside))

		// Give more bits to low-energy MDCTs than they would otherwise
		// deserve.
		if B0 > 1 && (itheta&0x3fff) != 0 {
			if itheta > 8192 {
				// Rough approximation for pre-echo masking.
				delta -= delta >> uint(4-LM)
			} else {
				// Corresponds to a forward-masking slope of 1.5 dB per 10
				// ms.
				delta = min(0, delta+(N<<bitRes>>uint(5-LM)))
			}
		}
		mbits := max(0, min(b, (b-delta)/2))
		sbits := b - mbits
		ctx.remainingBits -= int32(qalloc)

		if lowband != nil {
			nextLowband2 = lowband[N:] // >32-bit split case
		}

		rebalance := ctx.remainingBits
		if mbits >= sbits {
			cm = quantPartition(ctx, X, N, mbits, B, lowband, LM, float32(gain*mid), fill)
			rebalance = int32(mbits) - (rebalance - ctx.remainingBits)
			if rebalance > 3<<bitRes && itheta != 0 {
				sbits += int(rebalance) - (3 << bitRes)
			}
			cm |= quantPartition(
				ctx,
				Y,
				N,
				sbits,
				B,
				nextLowband2,
				LM,
				float32(gain*side),
				fill>>uint(B),
			) << uint(
				B0>>1,
			)
		} else {
			cm = quantPartition(
				ctx,
				Y,
				N,
				sbits,
				B,
				nextLowband2,
				LM,
				float32(gain*side),
				fill>>uint(B),
			) << uint(
				B0>>1,
			)
			rebalance = int32(sbits) - (rebalance - ctx.remainingBits)
			if rebalance > 3<<bitRes && itheta != 16384 {
				mbits += int(rebalance) - (3 << bitRes)
			}
			cm |= quantPartition(ctx, X, N, mbits, B, lowband, LM, float32(gain*mid), fill)
		}
	} else {
		// This is the basic no-split case.
		q := bits2pulses(m, i, LM, b)
		currBits := pulses2bits(m, i, LM, q)
		ctx.remainingBits -= int32(currBits)

		// Ensures we can never bust the budget.
		for ctx.remainingBits < 0 && q > 0 {
			ctx.remainingBits += int32(currBits)
			q--
			currBits = pulses2bits(m, i, LM, q)
			ctx.remainingBits -= int32(currBits)
		}

		if q != 0 {
			K := getPulses(q)

			// Finally do the actual quantization.
			if encode {
				cm = algQuant(X, N, K, spread, B, ec.enc, gain, ctx.resynth)
			} else {
				cm = algUnquant(X, N, K, spread, B, ec.dec, gain)
			}
		} else if ctx.resynth {
			// If there's no pulse, fill the band anyway.
			// B can be as large as 16, so this shift might overflow an int
			// on a 16-bit platform; use a long to get defined behavior.
			cmMask := uint(1)<<uint(B) - 1
			fill &= int(cmMask)
			if fill == 0 {
				clear(X[:N])
			} else {
				if lowband == nil {
					// Noise.
					for j := 0; j < N; j++ {
						ctx.seed = celtLCGRand(ctx.seed)
						X[j] = float32(int32(ctx.seed) >> 20)
					}
					cm = cmMask
				} else {
					// Folded spectrum.
					for j := 0; j < N; j++ {
						ctx.seed = celtLCGRand(ctx.seed)
						// About 48 dB below the "normal" folding level.
						tmp := float32(1.0 / 256)
						if ctx.seed&0x8000 == 0 {
							tmp = -tmp
						}
						X[j] = lowband[j] + tmp
					}
					cm = uint(fill)
				}
				renormaliseVector(X, N, gain)
			}
		}
	}

	return cm
}

var bitInterleaveTable = [16]uint8{
	0, 1, 1, 1, 2, 3, 3, 3, 2, 3, 3, 3, 2, 3, 3, 3,
}

var bitDeinterleaveTable = [16]uint8{
	0x00, 0x03, 0x0C, 0x0F, 0x30, 0x33, 0x3C, 0x3F,
	0xC0, 0xC3, 0xCC, 0xCF, 0xF0, 0xF3, 0xFC, 0xFF,
}

// quantBand is quant_band: it encodes or decodes a band for the mono
// case. lowband, lowbandOut and lowbandScratch may be nil.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantBand(ctx *bandCtx, X []float32, N, b, B int, lowband []float32, LM int,
	lowbandOut []float32, gain float32, lowbandScratch []float32, fill int,
) uint {
	N0 := N
	NB := N
	B0 := B
	timeDivide := 0
	recombine := 0
	var cm uint
	encode := ctx.encode
	tfChange := ctx.tfChange

	longBlocks := B0 == 1

	NB = int(entcode.Udiv(uint32(NB), uint32(B)))

	// Special case for one sample.
	if N == 1 {
		return quantBandN1(ctx, X, nil, lowbandOut)
	}

	if tfChange > 0 {
		recombine = tfChange
	}
	// Band recombining to increase frequency resolution.

	if lowbandScratch != nil && lowband != nil &&
		(recombine != 0 || ((NB&1) == 0 && tfChange < 0) || B0 > 1) {
		copy(lowbandScratch[:N], lowband[:N])
		lowband = lowbandScratch
	}

	for k := 0; k < recombine; k++ {
		if encode {
			haar1(X, N>>uint(k), 1<<uint(k))
		}
		if lowband != nil {
			haar1(lowband, N>>uint(k), 1<<uint(k))
		}
		fill = int(bitInterleaveTable[fill&0xF]) | int(bitInterleaveTable[fill>>4])<<2
	}
	B >>= uint(recombine)
	NB <<= uint(recombine)

	// Increasing the time resolution.
	for (NB&1) == 0 && tfChange < 0 {
		if encode {
			haar1(X, NB, B)
		}
		if lowband != nil {
			haar1(lowband, NB, B)
		}
		fill |= fill << uint(B)
		B <<= 1
		NB >>= 1
		timeDivide++
		tfChange++
	}
	B0 = B
	NB0 := NB

	// Reorganize the samples in time order instead of frequency order.
	if B0 > 1 {
		if encode {
			deinterleaveHadamard(X, NB>>uint(recombine), B0<<uint(recombine), longBlocks)
		}
		if lowband != nil {
			deinterleaveHadamard(lowband, NB>>uint(recombine), B0<<uint(recombine), longBlocks)
		}
	}

	cm = quantPartition(ctx, X, N, b, B, lowband, LM, gain, fill)

	// This code is used by the decoder and by the resynthesis-enabled
	// encoder.
	if ctx.resynth {
		// Undo the sample reorganization going from time order to
		// frequency order.
		if B0 > 1 {
			interleaveHadamard(X, NB>>uint(recombine), B0<<uint(recombine), longBlocks)
		}

		// Undo time-freq changes that we did earlier.
		NB = NB0
		B = B0
		for k := 0; k < timeDivide; k++ {
			B >>= 1
			NB <<= 1
			cm |= cm >> uint(B)
			haar1(X, NB, B)
		}

		for k := 0; k < recombine; k++ {
			cm = uint(bitDeinterleaveTable[cm])
			haar1(X, N0>>uint(k), 1<<uint(k))
		}
		B <<= uint(recombine)

		// Scale output for later folding.
		if lowbandOut != nil {
			n := dsp.CeltSqrt(float32(N0))
			for j := 0; j < N0; j++ {
				lowbandOut[j] = float32(n * X[j])
			}
		}
		cm &= (1 << uint(B)) - 1
	}
	return cm
}

const minStereoEnergy = 1e-10

// quantBandStereo is quant_band_stereo: it encodes or decodes a band for
// the stereo case.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantBandStereo(ctx *bandCtx, X, Y []float32, N, b, B int, lowband []float32, LM int,
	lowbandOut []float32, lowbandScratch []float32, fill int,
) uint {
	var cm uint
	var sctx splitCtx
	encode := ctx.encode
	ec := ctx.ec

	// Special case for one sample.
	if N == 1 {
		return quantBandN1(ctx, X, Y, lowbandOut)
	}

	origFill := fill

	if encode {
		if ctx.bandE[ctx.i] < minStereoEnergy || ctx.bandE[ctx.m.NbEBands+ctx.i] < minStereoEnergy {
			if ctx.bandE[ctx.i] > ctx.bandE[ctx.m.NbEBands+ctx.i] {
				copy(Y[:N], X[:N])
			} else {
				copy(X[:N], Y[:N])
			}
		}
	}
	computeTheta(ctx, &sctx, X, Y, N, &b, B, B, LM, true, &fill)
	inv := sctx.inv
	imid := sctx.imid
	iside := sctx.iside
	delta := sctx.delta
	itheta := sctx.itheta
	qalloc := sctx.qalloc
	mid := float32((1.0 / 32768) * float32(imid))
	side := float32((1.0 / 32768) * float32(iside))

	// This is a special case for N=2 that only works for stereo and takes
	// advantage of the fact that mid and side are orthogonal to encode the
	// side with just one bit.
	if N == 2 {
		sign := 0
		mbits := b
		sbits := 0
		// Only need one bit for the side.
		if itheta != 0 && itheta != 16384 {
			sbits = 1 << bitRes
		}
		mbits -= sbits
		c := itheta > 8192
		ctx.remainingBits -= int32(qalloc + sbits)

		x2, y2 := X, Y
		if c {
			x2, y2 = Y, X
		}
		if sbits != 0 {
			if encode {
				// Here we only need to encode a sign for the side.
				sign = b2i(float32(x2[0]*y2[1])-float32(x2[1]*y2[0]) < 0)
				ec.enc.EncBits(uint32(sign), 1)
			} else {
				sign = int(ec.dec.DecBits(1))
			}
		}
		sign = 1 - 2*sign
		// We use orig_fill here because we want to fold the side, but if
		// itheta==16384, we'll have cleared the low bits of fill.
		cm = quantBand(ctx, x2, N, mbits, B, lowband, LM, lowbandOut, 1, lowbandScratch, origFill)
		// We don't split N=2 bands, so cm is either 1 or 0 (for a
		// fold-collapse), and there's no need to worry about mixing with
		// the other channel.
		y2[0] = float32(float32(-sign) * x2[1])
		y2[1] = float32(float32(sign) * x2[0])
		if ctx.resynth {
			X[0] = float32(mid * X[0])
			X[1] = float32(mid * X[1])
			Y[0] = float32(side * Y[0])
			Y[1] = float32(side * Y[1])
			tmp := X[0]
			X[0] = tmp - Y[0]
			Y[0] = tmp + Y[0]
			tmp = X[1]
			X[1] = tmp - Y[1]
			Y[1] = tmp + Y[1]
		}
	} else {
		// "Normal" split code.
		mbits := max(0, min(b, (b-delta)/2))
		sbits := b - mbits
		ctx.remainingBits -= int32(qalloc)

		rebalance := ctx.remainingBits
		if mbits >= sbits {
			// In stereo mode, we do not apply a scaling to the mid because
			// we need the normalized mid for folding later.
			cm = quantBand(ctx, X, N, mbits, B, lowband, LM, lowbandOut, 1, lowbandScratch, fill)
			rebalance = int32(mbits) - (rebalance - ctx.remainingBits)
			if rebalance > 3<<bitRes && itheta != 0 {
				sbits += int(rebalance) - (3 << bitRes)
			}
			// For a stereo split, the high bits of fill are always zero,
			// so no folding will be done to the side.
			cm |= quantBand(ctx, Y, N, sbits, B, nil, LM, nil, side, nil, fill>>uint(B))
		} else {
			// For a stereo split, the high bits of fill are always zero,
			// so no folding will be done to the side.
			cm = quantBand(ctx, Y, N, sbits, B, nil, LM, nil, side, nil, fill>>uint(B))
			rebalance = int32(sbits) - (rebalance - ctx.remainingBits)
			if rebalance > 3<<bitRes && itheta != 16384 {
				mbits += int(rebalance) - (3 << bitRes)
			}
			// In stereo mode, we do not apply a scaling to the mid because
			// we need the normalized mid for folding later.
			cm |= quantBand(ctx, X, N, mbits, B, lowband, LM, lowbandOut, 1, lowbandScratch, fill)
		}
	}

	// This code is used by the decoder and by the resynthesis-enabled
	// encoder.
	if ctx.resynth {
		if N != 2 {
			stereoMerge(X, Y, mid, N)
		}
		if inv {
			for j := 0; j < N; j++ {
				Y[j] = -Y[j]
			}
		}
	}
	return cm
}

// specialHybridFolding is special_hybrid_folding. norm2 may be nil when
// dualStereo is false.
func specialHybridFolding(m *Mode, norm, norm2 []float32, start, M int, dualStereo bool) {
	eBands := m.EBands
	n1 := M * int(eBands[start+1]-eBands[start])
	n2 := M * int(eBands[start+2]-eBands[start+1])
	// Duplicate enough of the first band folding data to be able to fold
	// the second band. Copies no data for CELT-only mode.
	if n2 > n1 {
		copy(norm[n1:n2], norm[2*n1-n2:n1])
		if dualStereo {
			copy(norm2[n1:n2], norm2[2*n1-n2:n1])
		}
	}
}

// quantAllBandsMaxNorm bounds the folding buffer of quantAllBands:
// C*M*eBands[nbEBands-1] for C=2 and M=8.
const quantAllBandsMaxNorm = 2 * 8 * 78

// quantAllBands is quant_all_bands: quantisation (encode) or
// unquantisation (decode) of the residual spectrum. Y is nil for mono.
// When encoding, ec.enc is set and bandE holds the band amplitudes; when
// decoding, ec.dec is set and bandE may be nil.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantAllBands(
	encode bool,
	m *Mode,
	start, end int,
	inputX, inputY []float32,
	collapseMasks []uint8,
	bandE []float32,
	pulses []int,
	shortBlocks bool,
	spread int,
	dualStereo bool,
	intensity int,
	tfRes []int,
	totalBits, balance int32,
	ec rangeCoder,
	LM, codedBands int,
	seed *uint32,
	complexity int,
	disableInv bool,
) {
	var normBuf [quantAllBandsMaxNorm]float32
	var lowbandScratchBuf, XSave, YSave, XSave2, YSave2, normSave2 [maxBandSize]float32
	var bytesSave [1275]byte

	eBands := m.EBands
	updateLowband := true
	C := 1
	if inputY != nil {
		C = 2
	}
	thetaRDO := encode && inputY != nil && !dualStereo && complexity >= 8
	resynth := !encode || thetaRDO
	var ctx bandCtx

	M := 1 << uint(LM)
	B := 1
	if shortBlocks {
		B = M
	}
	normOffset := M * int(eBands[start])
	// No need to allocate norm for the last band because we don't need an
	// output in that band.
	normLen := M*int(eBands[m.NbEBands-1]) - normOffset
	norm := normBuf[:normLen]
	norm2 := normBuf[normLen : 2*normLen]
	if C == 1 {
		norm2 = normBuf[normLen:normLen]
	}

	// For decoding, we can use the last band as scratch space because we
	// don't need that scratch space for the last band and we don't care
	// about the data there until we're decoding the last band.
	var lowbandScratch []float32
	if encode && resynth {
		lowbandScratch = lowbandScratchBuf[:M*int(eBands[m.NbEBands]-eBands[m.NbEBands-1])]
	} else {
		lowbandScratch = inputX[M*int(eBands[m.EffEBands-1]):]
	}

	lowbandOffset := 0
	ctx.bandE = bandE
	ctx.ec = ec
	ctx.encode = encode
	ctx.intensity = intensity
	ctx.m = m
	ctx.seed = *seed
	ctx.spread = spread
	ctx.disableInv = disableInv
	ctx.resynth = resynth
	ctx.thetaRound = 0

	// Avoid injecting noise in the first band on transients.
	ctx.avoidSplitNoise = B > 1
	for i := start; i < end; i++ {
		var b int
		effectiveLowband := -1
		var xCM, yCM uint

		ctx.i = i
		last := i == end-1

		X := inputX[M*int(eBands[i]):]
		var Y []float32
		if inputY != nil {
			Y = inputY[M*int(eBands[i]):]
		}
		N := M*int(eBands[i+1]) - M*int(eBands[i])
		tell := int32(ec.tellFrac())

		// Compute how many bits we want to allocate to this band.
		if i != start {
			balance -= tell
		}
		remainingBits := totalBits - tell - 1
		ctx.remainingBits = remainingBits
		if i <= codedBands-1 {
			currBalance := entcode.Sudiv(balance, int32(min(3, codedBands-i)))
			b = int(max(0, min(16383, min(remainingBits+1, int32(pulses[i])+currBalance))))
		} else {
			b = 0
		}

		if resynth && (M*int(eBands[i])-N >= M*int(eBands[start]) || i == start+1) &&
			(updateLowband || lowbandOffset == 0) {
			lowbandOffset = i
		}
		if i == start+1 {
			specialHybridFolding(m, norm, norm2, start, M, dualStereo)
		}

		tfChange := tfRes[i]
		ctx.tfChange = tfChange
		if i >= m.EffEBands {
			X = norm
			if inputY != nil {
				Y = norm
			}
			lowbandScratch = nil
		}
		if last && !thetaRDO {
			lowbandScratch = nil
		}

		// Get a conservative estimate of the collapse_mask's for the bands
		// we're going to be folding from.
		if lowbandOffset != 0 && (spread != spreadAggressive || B > 1 || tfChange < 0) {
			// This ensures we never repeat spectral content within one
			// band.
			effectiveLowband = max(0, M*int(eBands[lowbandOffset])-normOffset-N)
			foldStart := lowbandOffset
			for {
				foldStart--
				if M*int(eBands[foldStart]) <= effectiveLowband+normOffset {
					break
				}
			}
			foldEnd := lowbandOffset - 1
			for {
				foldEnd++
				if foldEnd >= i || M*int(eBands[foldEnd]) >= effectiveLowband+normOffset+N {
					break
				}
			}
			xCM, yCM = 0, 0
			for foldI := foldStart; ; {
				xCM |= uint(collapseMasks[foldI*C+0])
				yCM |= uint(collapseMasks[foldI*C+C-1])
				foldI++
				if foldI >= foldEnd {
					break
				}
			}
		} else {
			// Otherwise, we'll be using the LCG to fold, so all blocks
			// will (almost always) be non-zero.
			xCM = (1 << uint(B)) - 1
			yCM = xCM
		}

		if dualStereo && i == intensity {
			// Switch off dual stereo to do intensity.
			dualStereo = false
			if resynth {
				for j := 0; j < M*int(eBands[i])-normOffset; j++ {
					norm[j] = float32(.5 * (norm[j] + norm2[j]))
				}
			}
		}
		var lowband, lowband2, lowbandOut, lowbandOut2 []float32
		if effectiveLowband != -1 {
			lowband = norm[effectiveLowband:]
			if len(norm2) > 0 {
				lowband2 = norm2[effectiveLowband:]
			}
		}
		if !last {
			lowbandOut = norm[M*int(eBands[i])-normOffset:]
			if len(norm2) > 0 {
				lowbandOut2 = norm2[M*int(eBands[i])-normOffset:]
			}
		}
		if dualStereo {
			xCM = quantBand(
				&ctx,
				X,
				N,
				b/2,
				B,
				lowband,
				LM,
				lowbandOut,
				1,
				lowbandScratch,
				int(xCM),
			)
			yCM = quantBand(
				&ctx,
				Y,
				N,
				b/2,
				B,
				lowband2,
				LM,
				lowbandOut2,
				1,
				lowbandScratch,
				int(yCM),
			)
		} else {
			if Y != nil {
				if thetaRDO && i < intensity {
					var w [2]float32
					computeChannelWeights(bandE[i], bandE[i+m.NbEBands], &w)
					// Make a copy.
					cm := xCM | yCM
					ecSave := *ec.enc
					ctxSave := ctx
					copy(XSave[:N], X[:N])
					copy(YSave[:N], Y[:N])
					// Encode and round down.
					ctx.thetaRound = -1
					xCM = quantBandStereo(
						&ctx,
						X,
						Y,
						N,
						b,
						B,
						lowband,
						LM,
						lowbandOut,
						lowbandScratch,
						int(cm),
					)
					dist0 := float32(
						w[0]*dsp.CeltInnerProd(XSave[:], X, N),
					) + float32(
						w[1]*dsp.CeltInnerProd(YSave[:], Y, N),
					)

					// Save first result.
					cm2 := xCM
					ecSave2 := *ec.enc
					ctxSave2 := ctx
					copy(XSave2[:N], X[:N])
					copy(YSave2[:N], Y[:N])
					if !last {
						copy(normSave2[:N], lowbandOut[:N])
					}
					nstartBytes := int(ecSave.Offs)
					nendBytes := int(ecSave.Storage)
					bytesBuf := ecSave.Buf[nstartBytes:nendBytes]
					saveBytes := nendBytes - nstartBytes
					copy(bytesSave[:saveBytes], bytesBuf)
					// Restore.
					*ec.enc = ecSave
					ctx = ctxSave
					copy(X[:N], XSave[:N])
					copy(Y[:N], YSave[:N])
					if i == start+1 {
						specialHybridFolding(m, norm, norm2, start, M, dualStereo)
					}
					// Encode and round up.
					ctx.thetaRound = 1
					xCM = quantBandStereo(
						&ctx,
						X,
						Y,
						N,
						b,
						B,
						lowband,
						LM,
						lowbandOut,
						lowbandScratch,
						int(cm),
					)
					dist1 := float32(
						w[0]*dsp.CeltInnerProd(XSave[:], X, N),
					) + float32(
						w[1]*dsp.CeltInnerProd(YSave[:], Y, N),
					)
					if dist0 >= dist1 {
						xCM = cm2
						*ec.enc = ecSave2
						ctx = ctxSave2
						copy(X[:N], XSave2[:N])
						copy(Y[:N], YSave2[:N])
						if !last {
							copy(lowbandOut[:N], normSave2[:N])
						}
						copy(bytesBuf, bytesSave[:saveBytes])
					}
				} else {
					ctx.thetaRound = 0
					xCM = quantBandStereo(
						&ctx,
						X,
						Y,
						N,
						b,
						B,
						lowband,
						LM,
						lowbandOut,
						lowbandScratch,
						int(xCM|yCM),
					)
				}
			} else {
				xCM = quantBand(
					&ctx,
					X,
					N,
					b,
					B,
					lowband,
					LM,
					lowbandOut,
					1,
					lowbandScratch,
					int(xCM|yCM),
				)
			}
			yCM = xCM
		}
		collapseMasks[i*C+0] = uint8(xCM)
		collapseMasks[i*C+C-1] = uint8(yCM)
		balance += int32(pulses[i]) + tell

		// Update the folding position only as long as we have 1 bit/sample
		// depth.
		updateLowband = b > (N << bitRes)
		// We only need to avoid noise on a split for the first band. After
		// that, we have folding.
		ctx.avoidSplitNoise = false
	}
	*seed = ctx.seed
}

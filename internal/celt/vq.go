// Ported from libopus 1.6.1 celt/vq.h and celt/vq.c (float build, generic
// C path, no ENABLE_QEXT).

package celt

import (
	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
)

// maxBandSize is the largest band of the static mode: (100-78)<<3.
const maxBandSize = 176

// expRotation1 is exp_rotation1.
func expRotation1(X []float32, length, stride int, c, s float32) {
	X = X[:length]
	if stride != 1 {
		expRotation1Generic(X, length, stride, c, s)
		return
	}
	if length < 2 {
		return
	}
	ms := -s
	carry := X[0]
	for i := 0; i < length-1; i++ {
		next := X[i+1]
		X[i] = float32(c*carry) + float32(ms*next)
		carry = float32(c*next) + float32(s*carry)
	}
	X[length-1] = carry
	if length < 3 {
		return
	}
	carry = X[length-2]
	for i := length - 3; i >= 0; i-- {
		next := X[i]
		X[i+1] = float32(c*carry) + float32(s*next)
		carry = float32(c*next) + float32(ms*carry)
	}
	X[0] = carry
}

func expRotation1Generic(X []float32, length, stride int, c, s float32) {
	ms := -s
	for i := 0; i < length-stride; i++ {
		x1 := X[i]
		x2 := X[i+stride]
		X[i+stride] = float32(c*x2) + float32(s*x1)
		X[i] = float32(c*x1) + float32(ms*x2)
	}
	for i := length - 2*stride - 1; i >= 0; i-- {
		x1 := X[i]
		x2 := X[i+stride]
		X[i+stride] = float32(c*x2) + float32(s*x1)
		X[i] = float32(c*x1) + float32(ms*x2)
	}
}

var spreadFactor = [3]int{15, 10, 5}

// expRotation is exp_rotation.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func expRotation(X []float32, length, dir, stride, K, spread int) {
	if 2*K >= length || spread == spreadNone {
		return
	}
	factor := spreadFactor[spread-1]

	gain := dsp.CeltDiv(float32(1.0*float32(length)), float32(length+factor*K))
	theta := float32(.5 * float32(gain*gain))

	c := dsp.CeltCosNorm(theta)
	s := dsp.CeltCosNorm(1 - theta) // sin(theta)

	stride2 := 0
	if length >= 8*stride {
		stride2 = 1
		// This is just a simple (equivalent) way of computing
		// sqrt(len/stride) with rounding. It's basically incrementing
		// long as (stride2+0.5)^2 < len/stride.
		for (stride2*stride2+stride2)*stride+(stride>>2) < length {
			stride2++
		}
	}
	length = int(entcode.Udiv(uint32(length), uint32(stride)))
	for i := 0; i < stride; i++ {
		x := X[i*length:]
		if dir < 0 {
			if stride2 != 0 {
				expRotation1(x, length, stride2, s, c)
			}
			expRotation1(x, length, 1, c, s)
		} else {
			expRotation1(x, length, 1, c, -s)
			if stride2 != 0 {
				expRotation1(x, length, stride2, s, -c)
			}
		}
	}
}

// normaliseResidual is normalise_residual: it normalizes the decoded
// integer pvq codeword to unit norm.
func normaliseResidual(iy []int, X []float32, N int, Ryy, gain float32) {
	t := Ryy
	g := float32(dsp.CeltRsqrtNorm32(t) * gain)
	for i := 0; i < N; i++ {
		X[i] = float32(float32(iy[i]) * g)
	}
}

// extractCollapseMask is extract_collapse_mask.
func extractCollapseMask(iy []int, N, B int) uint {
	if B <= 1 {
		return 1
	}
	// NOTE: As a minor optimization, we could be passing around log2(B),
	// not B, for both this and for exp_rotation().
	N0 := int(entcode.Udiv(uint32(N), uint32(B)))
	var collapseMask uint
	for i := 0; i < B; i++ {
		tmp := 0
		for j := 0; j < N0; j++ {
			tmp |= iy[i*N0+j]
		}
		if tmp != 0 {
			collapseMask |= 1 << uint(i)
		}
	}
	return collapseMask
}

// opPVQSearch is op_pvq_search_c. It overwrites X with its absolute value.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func opPVQSearch(X []float32, iy []int, K, N int) float32 {
	var yBuf [maxBandSize]float32
	var signxBuf [maxBandSize]int
	y := yBuf[:N]
	signx := signxBuf[:N]
	X = X[:N]
	iy = iy[:N]

	// Get rid of the sign.
	var sum float32
	for j := 0; j < N; j++ {
		signx[j] = b2i(X[j] < 0)
		X[j] = dsp.Abs16(X[j])
		iy[j] = 0
		y[j] = 0
	}

	var xy, yy float32
	pulsesLeft := K

	// Do a pre-search by projecting on the pyramid.
	if K > (N >> 1) {
		for j := 0; j < N; j++ {
			sum += X[j]
		}

		// If X is too small, just replace it with a pulse at 0. This
		// prevents infinities and NaNs from causing too many pulses to be
		// allocated. 64 is an approximation of infinity here.
		if !(sum > dsp.Epsilon && sum < 64) {
			X[0] = 1
			for j := 1; j < N; j++ {
				X[j] = 0
			}
			sum = 1
		}
		// Using K+e with e < 1 guarantees we cannot get more than K pulses.
		rcp := float32((float32(K) + 0.8) * dsp.CeltRcp(sum))
		for j := 0; j < N; j++ {
			iy[j] = floorInt(float32(rcp * X[j]))
			y[j] = float32(iy[j])
			yy = yy + float32(y[j]*y[j])
			xy = xy + float32(X[j]*y[j])
			y[j] = float32(y[j] * 2)
			pulsesLeft -= iy[j]
		}
	}

	// This should never happen, but just in case it does (e.g. on silence)
	// we fill the first bin with pulses.
	if pulsesLeft > N+3 {
		tmp := float32(pulsesLeft)
		yy = yy + float32(tmp*tmp)
		yy = yy + float32(tmp*y[0])
		iy[0] += pulsesLeft
		pulsesLeft = 0
	}

	for i := 0; i < pulsesLeft; i++ {
		bestID := 0
		// The squared magnitude term gets added anyway, so we might as well
		// add it outside the loop.
		yy = yy + 1

		// Calculations for position 0 are out of the loop, in part to
		// reduce mispredicted branches (since the if condition is usually
		// false) in the loop.
		// Temporary sums of the new pulse(s).
		Rxy := xy + X[0]
		// We're multiplying y[j] by two so we don't have to do it here.
		Ryy := yy + y[0]

		// Approximate score: we maximise Rxy/sqrt(Ryy) (we're guaranteed
		// that Rxy is positive because the sign is pre-computed).
		Rxy = float32(Rxy * Rxy)
		bestDen := Ryy
		bestNum := Rxy
		for j := 1; j < N; j++ {
			Rxy = xy + X[j]
			Ryy = yy + y[j]
			Rxy = float32(Rxy * Rxy)
			// The idea is to check for num/den >= best_num/best_den, but
			// that way we can do it without any division.
			if float32(bestDen*Rxy) > float32(Ryy*bestNum) {
				bestDen = Ryy
				bestNum = Rxy
				bestID = j
			}
		}

		// Updating the sums of the new pulse(s).
		xy = xy + X[bestID]
		// We're multiplying y[j] by two so we don't have to do it here.
		yy = yy + y[bestID]

		// Only now that we've made the final choice, update y/iy.
		// Multiplying y[j] by 2 so we don't have to do it everywhere else.
		y[bestID] += 2
		iy[bestID]++
	}

	// Put the original sign back.
	for j := 0; j < N; j++ {
		iy[j] = (iy[j] ^ -signx[j]) + signx[j]
	}
	return yy
}

// algQuant is alg_quant: the algebraic pulse-vector quantiser. X is
// replaced by the quantised vector when resynth is set. It returns a mask
// of the blocks in the band that received pulses.
func algQuant(
	X []float32,
	N, K, spread, B int,
	enc *entcode.Encoder,
	gain float32,
	resynth bool,
) uint {
	var iyBuf [maxBandSize + 3]int
	iy := iyBuf[:N+3]

	expRotation(X, N, 1, B, K, spread)

	yy := opPVQSearch(X, iy, K, N)
	collapseMask := extractCollapseMask(iy, N, B)
	encodePulses(iy, N, K, enc)
	if resynth {
		normaliseResidual(iy, X, N, yy, gain)
		expRotation(X, N, -1, B, K, spread)
	}
	return collapseMask
}

// algUnquant is alg_unquant: it decodes the pulse vector and combines it
// with the pitch vector to produce the final normalised signal in the
// current band.
func algUnquant(X []float32, N, K, spread, B int, dec *entcode.Decoder, gain float32) uint {
	var iyBuf [maxBandSize]int
	iy := iyBuf[:N]
	Ryy := decodePulses(iy, N, K, dec)
	normaliseResidual(iy, X, N, Ryy, gain)
	expRotation(X, N, -1, B, K, spread)
	return extractCollapseMask(iy, N, B)
}

// renormaliseVector is renormalise_vector.
func renormaliseVector(X []float32, N int, gain float32) {
	E := dsp.Epsilon + dsp.CeltInnerProd(X, X, N)
	t := E
	g := float32(dsp.CeltRsqrtNorm(t) * gain)
	for i := 0; i < N; i++ {
		X[i] = float32(g * X[i])
	}
}

// stereoItheta is stereo_itheta.
func stereoItheta(X, Y []float32, stereo bool, N int) int32 {
	var Emid, Eside float32
	if stereo {
		for i := 0; i < N; i++ {
			m := X[i] + Y[i]
			s := X[i] - Y[i]
			Emid = Emid + float32(m*m)
			Eside = Eside + float32(s*s)
		}
	} else {
		Emid += dsp.CeltInnerProd(X, X, N)
		Eside += dsp.CeltInnerProd(Y, Y, N)
	}
	mid := dsp.CeltSqrt32(Emid)
	side := dsp.CeltSqrt32(Eside)
	return int32(floorInt(.5 + float32(1073741824*dsp.CeltAtan2pNorm(side, mid))))
}

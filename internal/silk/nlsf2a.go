// Ported from libopus 1.6.1 silk/NLSF2A.c.

package silk

// Conversion between prediction filter coefficients and LSFs. The order
// should be even. A piecewise linear approximation maps LSF <-> cos(LSF),
// therefore the result is not accurate LSFs, but the two functions are
// accurate inverses of each other.

const nlsf2aQA = 16 // QA

// nlsf2aFindPoly is silk_NLSF2A_find_poly, a helper function for
// NLSF2A. out gets the intermediate polynomial, QA [dd+1]; cLSF is the
// vector of interleaved 2*cos(LSFs), QA [d], read with stride 2.
func nlsf2aFindPoly(out []int32, cLSF []int32, dd int) {
	out[0] = lshift(1, nlsf2aQA)
	out[1] = -cLSF[0]
	for k := 1; k < dd; k++ {
		ftmp := cLSF[2*k] // QA
		out[k+1] = lshift(out[k-1], 1) - int32(rshiftRound64(smull(ftmp, out[k]), nlsf2aQA))
		for n := k; n > 1; n-- {
			out[n] += out[n-2] - int32(rshiftRound64(smull(ftmp, out[n-1]), nlsf2aQA))
		}
		out[1] -= ftmp
	}
}

// This ordering was found to maximize quality. It improves numerical
// accuracy of silk_NLSF2A_find_poly() compared to "standard" ordering.
var (
	nlsf2aOrdering16 = [16]uint8{0, 15, 8, 7, 4, 11, 12, 3, 2, 13, 10, 5, 6, 9, 14, 1}
	nlsf2aOrdering10 = [10]uint8{0, 9, 6, 3, 4, 5, 8, 1, 2, 7}
)

// nlsf2a is silk_NLSF2A: compute whitening filter coefficients (monic,
// Q12, [d]) from normalized line spectral frequencies (Q15, [d]).
func nlsf2a(aQ12 []int16, nlsf []int16, d int) {
	var cosLSFQA [silkMaxOrderLPC]int32
	var p, q [silkMaxOrderLPC/2 + 1]int32
	var a32QA1 [silkMaxOrderLPC]int32

	// Convert LSFs to 2*cos(LSF), using piecewise linear curve from table.
	var ordering []uint8
	if d == 16 {
		ordering = nlsf2aOrdering16[:]
	} else {
		ordering = nlsf2aOrdering10[:]
	}
	for k := 0; k < d; k++ {
		// fInt on a scale 0-127 (rounded down).
		fInt := int32(nlsf[k]) >> (15 - 7)

		// fFrac, range: 0..255.
		fFrac := int32(nlsf[k]) - lshift(fInt, 15-7)

		// Read start and end value from table.
		cosVal := int32(lsfCosTabFIXQ12[fInt])           // Q12
		delta := int32(lsfCosTabFIXQ12[fInt+1]) - cosVal // Q12, with a range of 0..200

		// Linear interpolation.
		cosLSFQA[ordering[k]] = rshiftRound(lshift(cosVal, 8)+delta*fFrac, 20-nlsf2aQA) // QA
	}

	dd := d >> 1

	// Generate even and odd polynomials using convolution.
	nlsf2aFindPoly(p[:], cosLSFQA[0:], dd)
	nlsf2aFindPoly(q[:], cosLSFQA[1:], dd)

	// Convert even and odd polynomials to opus_int32 Q12 filter coefs.
	for k := 0; k < dd; k++ {
		ptmp := p[k+1] + p[k]
		qtmp := q[k+1] - q[k]

		// The Ptmp and Qtmp values at this stage need to fit in int32.
		a32QA1[k] = -qtmp - ptmp    // QA+1
		a32QA1[d-k-1] = qtmp - ptmp // QA+1
	}

	// Convert int32 coefficients to Q12 int16 coefs.
	lpcFit(aQ12, a32QA1[:], 12, nlsf2aQA+1, d)

	for i := 0; lpcInversePredGain(aQ12, d) == 0 && i < maxLPCStabilizeIterations; i++ {
		// Prediction coefficients are (too close to) unstable; apply
		// bandwidth expansion on the unscaled coefficients, convert to
		// Q12 and measure again.
		bwexpander32(a32QA1[:], d, 65536-lshift(2, i))
		for k := 0; k < d; k++ {
			aQ12[k] = int16(rshiftRound(a32QA1[k], nlsf2aQA+1-12)) // QA+1 -> Q12
		}
	}
}

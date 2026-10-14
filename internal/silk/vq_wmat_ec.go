// Ported from libopus 1.6.1 silk/VQ_WMat_EC.c.

package silk

// vq1001Q15 is SILK_FIX_CONST( 1.001, 15 ).
var vq1001Q15 = fixConst(1.001, 15)

// vqWMatEC is silk_VQ_WMat_EC (silk_VQ_WMat_EC_c): entropy constrained
// matrix-weighted VQ, hard-coded to 5-element vectors, for a single
// input data vector. It returns the index of the best codebook vector,
// the best residual energy, the best total bitrate and the sum of
// absolute LTP coefficients.
func vqWMatEC(xxQ17 []int32, xXQ17 []int32, cbQ7 [][ltpOrder]int8, cbGainQ7 []uint8, clQ5 []uint8,
	subfrLen int, maxGainQ7 int32, l int,
) (ind int8, resNrgQ15, rateDistQ8 int32, gainQ7 int) {
	var negxXQ24 [5]int32

	// Negate and convert to new Q domain.
	negxXQ24[0] = -lshift(xXQ17[0], 7)
	negxXQ24[1] = -lshift(xXQ17[1], 7)
	negxXQ24[2] = -lshift(xXQ17[2], 7)
	negxXQ24[3] = -lshift(xXQ17[3], 7)
	negxXQ24[4] = -lshift(xXQ17[4], 7)

	// Loop over codebook.
	rateDistQ8 = int32MAX
	resNrgQ15 = int32MAX
	// If things go really bad, at least ind is set to something safe.
	ind = 0
	for k := 0; k < l; k++ {
		cbRowQ7 := &cbQ7[k]
		gainTmpQ7 := int32(cbGainQ7[k])
		// Weighted rate.
		// Quantization error: 1 - 2 * xX * cb + cb' * XX * cb.
		sum1Q15 := vq1001Q15

		// Penalty for too large gain.
		penalty := lshift(max32(gainTmpQ7-maxGainQ7, 0), 11)

		// First row of XX_Q17.
		sum2Q24 := mla(negxXQ24[0], xxQ17[1], int32(cbRowQ7[1]))
		sum2Q24 = mla(sum2Q24, xxQ17[2], int32(cbRowQ7[2]))
		sum2Q24 = mla(sum2Q24, xxQ17[3], int32(cbRowQ7[3]))
		sum2Q24 = mla(sum2Q24, xxQ17[4], int32(cbRowQ7[4]))
		sum2Q24 = lshift(sum2Q24, 1)
		sum2Q24 = mla(sum2Q24, xxQ17[0], int32(cbRowQ7[0]))
		sum1Q15 = smlawb(sum1Q15, sum2Q24, int32(cbRowQ7[0]))

		// Second row of XX_Q17.
		sum2Q24 = mla(negxXQ24[1], xxQ17[7], int32(cbRowQ7[2]))
		sum2Q24 = mla(sum2Q24, xxQ17[8], int32(cbRowQ7[3]))
		sum2Q24 = mla(sum2Q24, xxQ17[9], int32(cbRowQ7[4]))
		sum2Q24 = lshift(sum2Q24, 1)
		sum2Q24 = mla(sum2Q24, xxQ17[6], int32(cbRowQ7[1]))
		sum1Q15 = smlawb(sum1Q15, sum2Q24, int32(cbRowQ7[1]))

		// Third row of XX_Q17.
		sum2Q24 = mla(negxXQ24[2], xxQ17[13], int32(cbRowQ7[3]))
		sum2Q24 = mla(sum2Q24, xxQ17[14], int32(cbRowQ7[4]))
		sum2Q24 = lshift(sum2Q24, 1)
		sum2Q24 = mla(sum2Q24, xxQ17[12], int32(cbRowQ7[2]))
		sum1Q15 = smlawb(sum1Q15, sum2Q24, int32(cbRowQ7[2]))

		// Fourth row of XX_Q17.
		sum2Q24 = mla(negxXQ24[3], xxQ17[19], int32(cbRowQ7[4]))
		sum2Q24 = lshift(sum2Q24, 1)
		sum2Q24 = mla(sum2Q24, xxQ17[18], int32(cbRowQ7[3]))
		sum1Q15 = smlawb(sum1Q15, sum2Q24, int32(cbRowQ7[3]))

		// Last row of XX_Q17.
		sum2Q24 = lshift(negxXQ24[4], 1)
		sum2Q24 = mla(sum2Q24, xxQ17[24], int32(cbRowQ7[4]))
		sum1Q15 = smlawb(sum1Q15, sum2Q24, int32(cbRowQ7[4]))

		// Find best.
		if sum1Q15 >= 0 {
			// Translate residual energy to bits using high-rate assumption
			// (6 dB ==> 1 bit/sample).
			bitsResQ8 := smulbb(int32(subfrLen), lin2log(sum1Q15+penalty)-(15<<7))
			// In the following line we reduce the codelength component by
			// half ("-1"); seems to slightly improve quality.
			bitsTotQ8 := addLshift32(bitsResQ8, int32(clQ5[k]), 3-1)
			if bitsTotQ8 <= rateDistQ8 {
				rateDistQ8 = bitsTotQ8
				resNrgQ15 = sum1Q15 + penalty
				ind = int8(k)
				gainQ7 = int(gainTmpQ7)
			}
		}
	}
	return ind, resNrgQ15, rateDistQ8, gainQ7
}

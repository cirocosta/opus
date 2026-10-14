// Ported from libopus 1.6.1 silk/quant_LTP_gains.c.

package silk

// Constants of silk_quant_LTP_gains.
var (
	ltpGainSafetyQ7    = fixConst(0.4, 7)                          // SILK_FIX_CONST( 0.4, 7 )
	ltpMaxSumLogGainQ7 = fixConst(float64(maxSumLogGainDB)/6.0, 7) // SILK_FIX_CONST( MAX_SUM_LOG_GAIN_DB / 6.0, 7 )
)

// quantLTPGains is silk_quant_LTP_gains. It returns the LTP prediction
// gain in dB, Q7.
func quantLTPGains(bQ14 []int16, cbkIndex []int8, periodicityIndex *int8, sumLogGainQ7 *int32,
	xxQ17 []int32, xXQ17 []int32, subfrLen, nbSubfr int) (predGainDBQ7 int) {
	var tempIdx [maxNbSubfr]int8
	var resNrgQ15 int32

	// Iterate over different codebooks with different rates/distortions,
	// and choose best.
	minRateDistQ7 := int32MAX
	bestSumLogGainQ7 := int32(0)
	for k := 0; k < 3; k++ {
		// Safety margin for pitch gain control, to take into account
		// factors such as state rescaling/rewhitening.
		gainSafety := ltpGainSafetyQ7

		clPtrQ5 := ltpGainBITSQ5Ptrs[k]
		cbkPtrQ7 := ltpVqPtrsQ7[k]
		cbkGainPtrQ7 := ltpVqGainPtrsQ7[k]
		cbkSize := int(ltpVqSizes[k])

		// Set up pointers to first subframe.
		xxOff := 0
		xXOff := 0

		resNrgQ15 = 0
		rateDistQ7 := int32(0)
		sumLogGainTmpQ7 := *sumLogGainQ7
		for j := 0; j < nbSubfr; j++ {
			maxGainQ7 := log2lin((ltpMaxSumLogGainQ7-sumLogGainTmpQ7)+(7<<7)) - gainSafety
			idx, resNrgQ15Subfr, rateDistQ7Subfr, gainQ7 := vqWMatEC(
				xxQ17[xxOff:], // I    correlation matrix
				xXQ17[xXOff:], // I    correlation vector
				cbkPtrQ7,      // I    codebook
				cbkGainPtrQ7,  // I    codebook effective gains
				clPtrQ5,       // I    code length for each codebook vector
				subfrLen,      // I    number of samples per subframe
				maxGainQ7,     // I    maximum sum of absolute LTP coefficients
				cbkSize,       // I    number of vectors in codebook
			)
			tempIdx[j] = idx

			resNrgQ15 = addPosSat32(resNrgQ15, resNrgQ15Subfr)
			rateDistQ7 = addPosSat32(rateDistQ7, rateDistQ7Subfr)
			sumLogGainTmpQ7 = max32(0, sumLogGainTmpQ7+lin2log(gainSafety+int32(gainQ7))-(7<<7))

			xxOff += ltpOrder * ltpOrder
			xXOff += ltpOrder
		}

		if rateDistQ7 <= minRateDistQ7 {
			minRateDistQ7 = rateDistQ7
			*periodicityIndex = int8(k)
			copy(cbkIndex[:nbSubfr], tempIdx[:nbSubfr])
			bestSumLogGainQ7 = sumLogGainTmpQ7
		}
	}

	cbkPtrQ7 := ltpVqPtrsQ7[*periodicityIndex]
	for j := 0; j < nbSubfr; j++ {
		for k := 0; k < ltpOrder; k++ {
			bQ14[j*ltpOrder+k] = int16(lshift(int32(cbkPtrQ7[cbkIndex[j]][k]), 7))
		}
	}

	if nbSubfr == 2 {
		resNrgQ15 = resNrgQ15 >> 1
	} else {
		resNrgQ15 = resNrgQ15 >> 2
	}

	*sumLogGainQ7 = bestSumLogGainQ7
	predGainDBQ7 = int(smulbb(-3, lin2log(resNrgQ15)-(15<<7)))
	return predGainDBQ7
}

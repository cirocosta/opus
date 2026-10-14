// Ported from libopus 1.6.1 silk/NLSF_decode.c.

package silk

// nlsfResidualDequant is silk_NLSF_residual_dequant: predictive
// dequantizer for NLSF residuals.
func nlsfResidualDequant(
	xQ10 []int16,
	indices []int8,
	predCoefQ8 []uint8,
	quantStepSizeQ16 int32,
	order int,
) {
	outQ10 := int32(0)
	for i := order - 1; i >= 0; i-- {
		predQ10 := smulbb(outQ10, int32(int16(predCoefQ8[i]))) >> 8
		outQ10 = lshift(int32(indices[i]), 10)
		if outQ10 > 0 {
			outQ10 = outQ10 - 102 // SILK_FIX_CONST( NLSF_QUANT_LEVEL_ADJ, 10 )
		} else if outQ10 < 0 {
			outQ10 = outQ10 + 102
		}
		outQ10 = smlawb(predQ10, outQ10, quantStepSizeQ16)
		xQ10[i] = int16(outQ10)
	}
}

// nlsfDecode is silk_NLSF_decode: NLSF vector decoder. pNLSFQ15 gets
// the quantized NLSF vector [LPC_ORDER]; nlsfIndices is the codebook
// path vector [LPC_ORDER + 1].
func nlsfDecode(pNLSFQ15 []int16, nlsfIndices []int8, psNLSFCB *nlsfCBStruct) {
	var predQ8 [maxLPCOrder]uint8
	var ecIx [maxLPCOrder]int16
	var resQ10 [maxLPCOrder]int16

	order := int(psNLSFCB.order)

	// Unpack entropy table indices and predictor for current CB1 index.
	nlsfUnpack(ecIx[:], predQ8[:], psNLSFCB, int(nlsfIndices[0]))

	// Predictive residual dequantizer.
	nlsfResidualDequant(
		resQ10[:],
		nlsfIndices[1:],
		predQ8[:],
		int32(psNLSFCB.quantStepSizeQ16),
		order,
	)

	// Apply inverse square-rooted weights to first stage and add to output.
	pCBElement := psNLSFCB.cb1NLSFQ8[int(nlsfIndices[0])*order:]
	pCBWghtQ9 := psNLSFCB.cb1WghtQ9[int(nlsfIndices[0])*order:]
	for i := 0; i < order; i++ {
		nlsfQ15Tmp := addLshift32(
			div32_16(lshift(int32(resQ10[i]), 14), int32(pCBWghtQ9[i])),
			int32(int16(pCBElement[i])),
			7,
		)
		pNLSFQ15[i] = int16(limit32(nlsfQ15Tmp, 0, 32767))
	}

	// NLSF stabilization.
	nlsfStabilize(pNLSFQ15, psNLSFCB.deltaMinQ15, order)
}

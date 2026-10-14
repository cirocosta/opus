// Ported from libopus 1.6.1 silk/NLSF_encode.c.

package silk

// nlsfEncode is silk_NLSF_encode: NLSF vector encoder. It returns the RD
// value in Q25.
func nlsfEncode(nlsfIndices []int8, pNLSFQ15 []int16, psNLSFCB *nlsfCBStruct, pWQ2 []int16,
	nlsfMuQ20, nSurvivors, signalType int,
) int32 {
	var (
		errQ24       [nlsfVQMaxVectors]int32
		rdQ25        [nlsfVQMaxVectors]int32
		tempIndices1 [nlsfVQMaxVectors]int
		tempIndices2 [nlsfVQMaxVectors * maxLPCOrder]int8
		resQ10       [maxLPCOrder]int16
		nlsfTmpQ15   [maxLPCOrder]int16
		wAdjQ5       [maxLPCOrder]int16
		predQ8       [maxLPCOrder]uint8
		ecIx         [maxLPCOrder]int16
		bestIndex    [1]int
	)
	order := int(psNLSFCB.order)

	// NLSF stabilization.
	nlsfStabilize(pNLSFQ15, psNLSFCB.deltaMinQ15, order)

	// First stage: VQ.
	nlsfVQ(
		errQ24[:],
		pNLSFQ15,
		psNLSFCB.cb1NLSFQ8,
		psNLSFCB.cb1WghtQ9,
		int(psNLSFCB.nVectors),
		order,
	)

	// Sort the quantization errors.
	insertionSortIncreasing(errQ24[:], tempIndices1[:], int(psNLSFCB.nVectors), nSurvivors)

	// Loop over survivors.
	for s := 0; s < nSurvivors; s++ {
		ind1 := tempIndices1[s]

		// Residual after first stage.
		pCBElement := psNLSFCB.cb1NLSFQ8[ind1*order:]
		pCBWghtQ9 := psNLSFCB.cb1WghtQ9[ind1*order:]
		for i := 0; i < order; i++ {
			nlsfTmpQ15[i] = int16(uint16(int16(pCBElement[i])) << 7)
			wTmpQ9 := int32(pCBWghtQ9[i])
			resQ10[i] = int16(smulbb(int32(pNLSFQ15[i])-int32(nlsfTmpQ15[i]), wTmpQ9) >> 14)
			wAdjQ5[i] = int16(div32VarQ(int32(pWQ2[i]), smulbb(wTmpQ9, wTmpQ9), 21))
		}

		// Unpack entropy table indices and predictor for current CB1
		// index.
		nlsfUnpack(ecIx[:], predQ8[:], psNLSFCB, ind1)

		// Trellis quantizer.
		rdQ25[s] = nlsfDelDecQuant(
			tempIndices2[s*maxLPCOrder:],
			resQ10[:],
			wAdjQ5[:],
			predQ8[:],
			ecIx[:],
			psNLSFCB.ecRatesQ5,
			int(psNLSFCB.quantStepSizeQ16),
			psNLSFCB.invQuantStepSizeQ6,
			int32(nlsfMuQ20),
			psNLSFCB.order,
		)

		// Add rate for first stage.
		iCDFPtr := psNLSFCB.cb1ICDF[(signalType>>1)*int(psNLSFCB.nVectors):]
		var probQ8 int
		if ind1 == 0 {
			probQ8 = 256 - int(iCDFPtr[ind1])
		} else {
			probQ8 = int(iCDFPtr[ind1-1]) - int(iCDFPtr[ind1])
		}
		bitsQ7 := (8 << 7) - lin2log(int32(probQ8))
		rdQ25[s] = smlabb(rdQ25[s], bitsQ7, int32(nlsfMuQ20>>2))
	}

	// Find the lowest rate-distortion error.
	insertionSortIncreasing(rdQ25[:], bestIndex[:], nSurvivors, 1)

	nlsfIndices[0] = int8(tempIndices1[bestIndex[0]])
	copy(
		nlsfIndices[1:1+order],
		tempIndices2[bestIndex[0]*maxLPCOrder:bestIndex[0]*maxLPCOrder+order],
	)

	// Decode.
	nlsfDecode(pNLSFQ15, nlsfIndices, psNLSFCB)

	return rdQ25[0]
}

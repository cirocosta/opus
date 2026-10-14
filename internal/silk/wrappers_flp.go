// Ported from libopus 1.6.1 silk/float/wrappers_FLP.c.

package silk

// Wrappers. Calls flp / fix code.

// a2nlsfFLP is silk_A2NLSF_FLP: convert AR filter coefficients to NLSF
// parameters.
func a2nlsfFLP(nlsfQ15 []int16, pAR []float32, lpcOrder int) {
	var aFixQ16 [maxLPCOrder]int32

	for i := 0; i < lpcOrder; i++ {
		aFixQ16[i] = float2intFLP(float32(pAR[i] * 65536.0))
	}

	a2nlsf(nlsfQ15, aFixQ16[:], lpcOrder)
}

// nlsf2aFLP is silk_NLSF2A_FLP: convert LSF parameters to AR prediction
// filter coefficients.
func nlsf2aFLP(pAR []float32, nlsfQ15 []int16, lpcOrder int) {
	var aFixQ12 [maxLPCOrder]int16

	nlsf2a(aFixQ12[:], nlsfQ15, lpcOrder)

	for i := 0; i < lpcOrder; i++ {
		pAR[i] = float32(float32(aFixQ12[i]) * (1.0 / 4096.0))
	}
}

// processNLSFsFLP is silk_process_NLSFs_FLP: floating-point NLSF
// processing wrapper.
func (psEncC *encoderState) processNLSFsFLP(
	predCoef *[2][maxLPCOrder]float32,
	nlsfQ15 []int16,
	prevNLSFQ15 []int16,
) {
	var predCoefQ12 [2][maxLPCOrder]int16

	psEncC.processNLSFs(&predCoefQ12, nlsfQ15, prevNLSFQ15)

	for j := 0; j < 2; j++ {
		for i := 0; i < psEncC.predictLPCOrder; i++ {
			predCoef[j][i] = float32(float32(predCoefQ12[j][i]) * (1.0 / 4096.0))
		}
	}
}

// nsqWrapperFLP is silk_NSQ_wrapper_FLP: floating-point SILK NSQ
// wrapper. x[xOff] is the C x pointer.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psEnc *encoderStateFLP) nsqWrapperFLP(
	psEncCtrl *encoderControlFLP,
	psIndices *sideInfoIndices,
	psNSQ *nsqState,
	pulses []int8,
	x []float32,
	xOff int,
) {
	p := &psEnc.scratch.nsqParams
	x16 := psEnc.scratch.x16[:]

	// Convert control struct to fix control struct.
	// Noise shape parameters.
	for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
		for j := 0; j < psEnc.sCmn.shapingLPCOrder; j++ {
			p.arQ13[i*maxShapeLPCOrder+j] = int16(
				float2intFLP(float32(psEncCtrl.ar[i*maxShapeLPCOrder+j] * 8192.0)),
			)
		}
	}

	for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
		p.lfShpQ14[i] = lshift(float2intFLP(float32(psEncCtrl.lfARShp[i]*16384.0)), 16) |
			int32(uint16(float2intFLP(float32(psEncCtrl.lfMAShp[i]*16384.0))))
		p.tiltQ14[i] = int(float2intFLP(float32(psEncCtrl.tilt[i] * 16384.0)))
		p.harmShapeGainQ14[i] = int(float2intFLP(float32(psEncCtrl.harmShapeGain[i] * 16384.0)))
	}
	p.lambdaQ10 = int(float2intFLP(float32(psEncCtrl.lambda * 1024.0)))

	// Prediction and coding parameters.
	for i := 0; i < psEnc.sCmn.nbSubfr*ltpOrder; i++ {
		p.ltpCoefQ14[i] = int16(float2intFLP(float32(psEncCtrl.ltpCoef[i] * 16384.0)))
	}

	for j := 0; j < 2; j++ {
		for i := 0; i < psEnc.sCmn.predictLPCOrder; i++ {
			p.predCoefQ12[j][i] = int16(float2intFLP(float32(psEncCtrl.predCoef[j][i] * 4096.0)))
		}
	}

	for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
		p.gainsQ16[i] = float2intFLP(float32(psEncCtrl.gains[i] * 65536.0))
	}

	if psIndices.signalType == typeVoiced {
		p.ltpScaleQ14 = int(ltpScalesTableQ14[psIndices.ltpScaleIndex])
	} else {
		p.ltpScaleQ14 = 0
	}
	p.pitchL = psEncCtrl.pitchL

	// Convert input to fix.
	for i := 0; i < psEnc.sCmn.frameLength; i++ {
		x16[i] = int16(float2intFLP(x[xOff+i]))
	}

	// Call NSQ.
	if psEnc.sCmn.nStatesDelayedDecision > 1 || psEnc.sCmn.warpingQ16 > 0 {
		psEnc.sCmn.nsqDelDec(psNSQ, psIndices, x16, pulses, p, &psEnc.scratch.nsq)
	} else {
		psEnc.sCmn.nsq(psNSQ, psIndices, x16, pulses, p, &psEnc.scratch.nsq)
	}
}

// quantLTPGainsFLP is silk_quant_LTP_gains_FLP: floating-point SILK LTP
// quantization wrapper.
func quantLTPGainsFLP(
	b *[maxNbSubfr * ltpOrder]float32,
	cbkIndex []int8,
	periodicityIndex *int8,
	sumLogGainQ7 *int32,
	predGainDB *float32,
	xx *[maxNbSubfr * ltpOrder * ltpOrder]float32,
	xX *[maxNbSubfr * ltpOrder]float32,
	subfrLen, nbSubfr int,
) {
	var (
		bQ14  [maxNbSubfr * ltpOrder]int16
		xxQ17 [maxNbSubfr * ltpOrder * ltpOrder]int32
		xXQ17 [maxNbSubfr * ltpOrder]int32
	)

	for i := 0; i < nbSubfr*ltpOrder*ltpOrder; i++ {
		xxQ17[i] = float2intFLP(float32(xx[i] * 131072.0))
	}
	for i := 0; i < nbSubfr*ltpOrder; i++ {
		xXQ17[i] = float2intFLP(float32(xX[i] * 131072.0))
	}

	predGainDBQ7 := quantLTPGains(
		bQ14[:],
		cbkIndex,
		periodicityIndex,
		sumLogGainQ7,
		xxQ17[:],
		xXQ17[:],
		subfrLen,
		nbSubfr,
	)

	for i := 0; i < nbSubfr*ltpOrder; i++ {
		b[i] = float32(float32(bQ14[i]) * (1.0 / 16384.0))
	}

	*predGainDB = float32(float32(predGainDBQ7) * (1.0 / 128.0))
}

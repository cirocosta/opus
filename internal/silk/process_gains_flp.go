// Ported from libopus 1.6.1 silk/float/process_gains_FLP.c.

package silk

import "math"

// processGainsFLP is silk_process_gains_FLP: processing of gains.
func (psEnc *encoderStateFLP) processGainsFLP(psEncCtrl *encoderControlFLP, condCoding int) {
	var pGainsQ16 [maxNbSubfr]int32
	psShapeSt := &psEnc.sShape

	// Gain reduction when LTP coding gain is high.
	if psEnc.sCmn.indices.signalType == typeVoiced {
		s := 1.0 - float32(0.5*sigmoid(float32(0.25*(psEncCtrl.ltpredCodGain-12.0))))
		for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
			psEncCtrl.gains[k] = float32(psEncCtrl.gains[k] * s)
		}
	}

	// Limit the quantized signal.
	invMaxSqrVal := float32(math.Pow(2.0, float64(float32(0.33*(21.0-float32(float32(psEnc.sCmn.snrDBQ7)*(1/128.0)))))) /
		float64(psEnc.sCmn.subfrLength))

	for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
		// Soft limit on ratio residual energy and squared gains.
		gain := psEncCtrl.gains[k]
		gain = float32(math.Sqrt(float64(float32(gain*gain) + float32(psEncCtrl.resNrg[k]*invMaxSqrVal))))
		psEncCtrl.gains[k] = minFloat(gain, 32767.0)
	}

	// Prepare gains for noise shaping quantization.
	for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
		pGainsQ16[k] = int32(float32(psEncCtrl.gains[k] * 65536.0))
	}

	// Save unquantized gains and gain Index.
	copy(psEncCtrl.gainsUnqQ16[:psEnc.sCmn.nbSubfr], pGainsQ16[:psEnc.sCmn.nbSubfr])
	psEncCtrl.lastGainIndexPrev = psShapeSt.lastGainIndex

	// Quantize gains.
	gainsQuant(&psEnc.sCmn.indices.gainsIndices, &pGainsQ16,
		&psShapeSt.lastGainIndex, condCoding == codeConditionally, psEnc.sCmn.nbSubfr)

	// Overwrite unquantized gains with quantized gains and convert back to
	// Q0 from Q16.
	for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
		psEncCtrl.gains[k] = float32(pGainsQ16[k]) / 65536.0
	}

	// Set quantizer offset for voiced signals. Larger offset when LTP
	// coding gain is low or tilt is high (ie low-pass).
	if psEnc.sCmn.indices.signalType == typeVoiced {
		if psEncCtrl.ltpredCodGain+float32(float32(psEnc.sCmn.inputTiltQ15)*(1.0/32768.0)) > 1.0 {
			psEnc.sCmn.indices.quantOffsetType = 0
		} else {
			psEnc.sCmn.indices.quantOffsetType = 1
		}
	}

	// Quantizer boundary adjustment.
	quantOffset := float32(quantizationOffsetsQ10[psEnc.sCmn.indices.signalType>>1][psEnc.sCmn.indices.quantOffsetType]) / 1024.0
	psEncCtrl.lambda = lambdaOffset +
		float32(lambdaDelayedDecisions*float32(psEnc.sCmn.nStatesDelayedDecision)) +
		float32(float32(lambdaSpeechAct*float32(psEnc.sCmn.speechActivityQ8))*(1.0/256.0)) +
		float32(lambdaInputQuality*psEncCtrl.inputQuality) +
		float32(lambdaCodingQuality*psEncCtrl.codingQuality) +
		float32(lambdaQuantOffset*quantOffset)
}

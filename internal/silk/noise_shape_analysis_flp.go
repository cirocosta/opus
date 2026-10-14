// Ported from libopus 1.6.1 silk/float/noise_shape_analysis_FLP.c.

package silk

import "math"

// warpedGain is warped_gain: compute gain to make warped filter
// coefficients have a zero mean log frequency response on a non-warped
// frequency scale (so that it can be implemented with a minimum-phase
// monic filter).
func warpedGain(coefs []float32, lambda float32, order int) float32 {
	lambda = -lambda
	gain := coefs[order-1]
	for i := order - 2; i >= 0; i-- {
		gain = float32(lambda*gain) + coefs[i]
	}
	return 1.0 / (1.0 - float32(lambda*gain))
}

// warpedTrue2MonicCoefs is warped_true2monic_coefs: convert warped
// filter coefficients to monic pseudo-warped coefficients and limit the
// maximum amplitude of monic warped coefficients by using bandwidth
// expansion on the true coefficients.
func warpedTrue2MonicCoefs(coefs []float32, lambda, limit float32, order int) {
	ind := 0

	// Convert to monic coefficients.
	for i := order - 1; i > 0; i-- {
		coefs[i-1] -= float32(lambda * coefs[i])
	}
	gain := (1.0 - float32(lambda*lambda)) / (1.0 + float32(lambda*coefs[0]))
	for i := 0; i < order; i++ {
		coefs[i] = float32(coefs[i] * gain)
	}

	// Limit.
	for iter := 0; iter < 10; iter++ {
		// Find maximum absolute value.
		maxabs := float32(-1.0)
		for i := 0; i < order; i++ {
			tmp := absFloat(coefs[i])
			if tmp > maxabs {
				maxabs = tmp
				ind = i
			}
		}
		if maxabs <= limit {
			// Coefficients are within range - done.
			return
		}

		// Convert back to true warped coefficients.
		for i := 1; i < order; i++ {
			coefs[i-1] += float32(lambda * coefs[i])
		}
		gain = 1.0 / gain
		for i := 0; i < order; i++ {
			coefs[i] = float32(coefs[i] * gain)
		}

		// Apply bandwidth expansion.
		chirp := 0.99 - float32(float32((0.8+float32(0.1*float32(iter)))*(maxabs-limit))/float32(maxabs*float32(ind+1)))
		bwexpanderFLP(coefs, order, chirp)

		// Convert to monic warped coefficients.
		for i := order - 1; i > 0; i-- {
			coefs[i-1] -= float32(lambda * coefs[i])
		}
		gain = (1.0 - float32(lambda*lambda)) / (1.0 + float32(lambda*coefs[0]))
		for i := 0; i < order; i++ {
			coefs[i] = float32(coefs[i] * gain)
		}
	}
}

// limitCoefs is limit_coefs.
func limitCoefs(coefs []float32, limit float32, order int) {
	ind := 0

	for iter := 0; iter < 10; iter++ {
		// Find maximum absolute value.
		maxabs := float32(-1.0)
		for i := 0; i < order; i++ {
			tmp := absFloat(coefs[i])
			if tmp > maxabs {
				maxabs = tmp
				ind = i
			}
		}
		if maxabs <= limit {
			// Coefficients are within range - done.
			return
		}

		// Apply bandwidth expansion.
		chirp := 0.99 - float32(float32((0.8+float32(0.1*float32(iter)))*(maxabs-limit))/float32(maxabs*float32(ind+1)))
		bwexpanderFLP(coefs, order, chirp)
	}
}

// noiseShapeAnalysisFLP is silk_noise_shape_analysis_FLP: compute noise
// shaping coefficients and initial gain values. pitchRes[resOff] and
// x[xOff] are the C pitch_res and x pointers.
func (psEnc *encoderStateFLP) noiseShapeAnalysisFLP(psEncCtrl *encoderControlFLP, pitchRes []float32, resOff int,
	x []float32, xOff int) {
	var (
		xWindowed [shapeLPCWinMax]float32
		autoCorr  [maxShapeLPCOrder + 1]float32
		rc        [maxShapeLPCOrder + 1]float32
	)
	psShapeSt := &psEnc.sShape

	// Point to start of first LPC analysis block.
	xPtr := xOff - psEnc.sCmn.laShape

	// GAIN CONTROL.
	snrAdjDB := float32(float32(psEnc.sCmn.snrDBQ7) * (1 / 128.0))

	// Input quality is the average of the quality in the lowest two VAD
	// bands.
	psEncCtrl.inputQuality = float32(float32(0.5*float32(psEnc.sCmn.inputQualityBandsQ15[0]+psEnc.sCmn.inputQualityBandsQ15[1])) * (1.0 / 32768.0))

	// Coding quality level, between 0.0 and 1.0.
	psEncCtrl.codingQuality = sigmoid(float32(0.25 * (snrAdjDB - 20.0)))

	if psEnc.sCmn.useCBR == 0 {
		// Reduce coding SNR during low speech activity.
		b := 1.0 - float32(float32(psEnc.sCmn.speechActivityQ8)*(1.0/256.0))
		snrAdjDB -= float32(float32(float32(float32(bgSNRDecrDB*psEncCtrl.codingQuality)*(0.5+float32(0.5*psEncCtrl.inputQuality)))*b) * b)
	}

	if psEnc.sCmn.indices.signalType == typeVoiced {
		// Reduce gains for periodic signals.
		snrAdjDB += float32(harmSNRIncrDB * psEnc.ltpCorr)
	} else {
		// For unvoiced signals and low-quality input, adjust the quality
		// slower than SNR_dB setting.
		snrAdjDB += float32((float32(float32(-0.4*float32(psEnc.sCmn.snrDBQ7))*(1/128.0)) + 6.0) * (1.0 - psEncCtrl.inputQuality))
	}

	// SPARSENESS PROCESSING.
	// Set quantizer offset.
	if psEnc.sCmn.indices.signalType == typeVoiced {
		// Initially set to 0; may be overruled in process_gains(..).
		psEnc.sCmn.indices.quantOffsetType = 0
	} else {
		// Sparseness measure, based on relative fluctuations of energy per
		// 2 milliseconds.
		nSamples := 2 * psEnc.sCmn.fsKHz
		energyVariation := float32(0.0)
		logEnergyPrev := float32(0.0)
		pitchResPtr := resOff
		nSegs := int(smulbb(subFrameLengthMs, int32(psEnc.sCmn.nbSubfr))) / 2
		for k := 0; k < nSegs; k++ {
			nrg := float32(nSamples) + float32(energyFLP(pitchRes[pitchResPtr:], nSamples))
			logEnergy := silkLog2(float64(nrg))
			if k > 0 {
				energyVariation += absFloat(logEnergy - logEnergyPrev)
			}
			logEnergyPrev = logEnergy
			pitchResPtr += nSamples
		}

		// Set quantization offset depending on sparseness measure.
		if energyVariation > float32(energyVariationThresholdQntOffset*float32(nSegs-1)) {
			psEnc.sCmn.indices.quantOffsetType = 0
		} else {
			psEnc.sCmn.indices.quantOffsetType = 1
		}
	}

	// Control bandwidth expansion.
	// More BWE for signals with high prediction gain.
	strength := float32(findPitchWhiteNoiseFraction * psEncCtrl.predGain) // between 0.0 and 1.0
	bwExp := bandwidthExpansion / (1.0 + float32(strength*strength))

	// Slightly more warping in analysis will move quantization noise up in
	// frequency, where it's better masked.
	warping := float32(float32(psEnc.sCmn.warpingQ16)/65536.0) + float32(0.01*psEncCtrl.codingQuality)

	// Compute noise shaping AR coefs and gains.
	for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
		// Apply window: sine slope followed by flat part followed by
		// cosine slope.
		flatPart := psEnc.sCmn.fsKHz * 3
		slopePart := (psEnc.sCmn.shapeWinLength - flatPart) / 2

		applySineWindowFLP(xWindowed[:], x[xPtr:], 1, slopePart)
		shift := slopePart
		copy(xWindowed[shift:shift+flatPart], x[xPtr+shift:xPtr+shift+flatPart])
		shift += flatPart
		applySineWindowFLP(xWindowed[shift:], x[xPtr+shift:], 2, slopePart)

		// Update pointer: next LPC analysis block.
		xPtr += psEnc.sCmn.subfrLength

		if psEnc.sCmn.warpingQ16 > 0 {
			// Calculate warped auto correlation.
			warpedAutocorrelationFLP(autoCorr[:], xWindowed[:], warping,
				psEnc.sCmn.shapeWinLength, psEnc.sCmn.shapingLPCOrder)
		} else {
			// Calculate regular auto correlation.
			autocorrelationFLP(autoCorr[:], xWindowed[:], psEnc.sCmn.shapeWinLength, psEnc.sCmn.shapingLPCOrder+1)
		}

		// Add white noise, as a fraction of energy.
		autoCorr[0] += float32(autoCorr[0]*shapeWhiteNoiseFraction) + 1.0

		// Convert correlations to prediction coefficients, and compute
		// residual energy.
		nrg := schurFLP(rc[:], autoCorr[:], psEnc.sCmn.shapingLPCOrder)
		ar := psEncCtrl.ar[k*maxShapeLPCOrder : (k+1)*maxShapeLPCOrder]
		k2aFLP(ar, rc[:], psEnc.sCmn.shapingLPCOrder)
		psEncCtrl.gains[k] = float32(math.Sqrt(float64(nrg)))

		if psEnc.sCmn.warpingQ16 > 0 {
			// Adjust gain for warping.
			psEncCtrl.gains[k] = float32(psEncCtrl.gains[k] * warpedGain(ar, warping, psEnc.sCmn.shapingLPCOrder))
		}

		// Bandwidth expansion for synthesis filter shaping.
		bwexpanderFLP(ar, psEnc.sCmn.shapingLPCOrder, bwExp)

		if psEnc.sCmn.warpingQ16 > 0 {
			// Convert to monic warped prediction coefficients and limit
			// absolute values.
			warpedTrue2MonicCoefs(ar, warping, 3.999, psEnc.sCmn.shapingLPCOrder)
		} else {
			// Limit absolute values.
			limitCoefs(ar, 3.999, psEnc.sCmn.shapingLPCOrder)
		}
	}

	// Gain tweaking.
	// Increase gains during low speech activity.
	gainMult := float32(math.Pow(2.0, float64(float32(-0.16*snrAdjDB))))
	gainAdd := float32(math.Pow(2.0, float64(float32(0.16)*minQGainDb)))
	for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
		psEncCtrl.gains[k] = float32(psEncCtrl.gains[k] * gainMult)
		psEncCtrl.gains[k] += gainAdd
	}

	// Control low-frequency shaping and noise tilt.
	// Less low frequency shaping for noisy inputs.
	strength = float32(lowFreqShaping * (1.0 + float32(lowQualityLowFreqShapingDecr*(float32(float32(psEnc.sCmn.inputQualityBandsQ15[0])*(1.0/32768.0))-1.0))))
	strength = float32(strength * float32(float32(psEnc.sCmn.speechActivityQ8)*(1.0/256.0)))
	var tilt float32
	if psEnc.sCmn.indices.signalType == typeVoiced {
		// Reduce low frequencies quantization noise for periodic signals,
		// depending on pitch lag.
		// f = 400; freqz([1, -0.98 + 2e-4 * f], [1, -0.97 + 7e-4 * f], 2^12, Fs); axis([0, 1000, -10, 1])
		for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
			b := 0.2/float32(psEnc.sCmn.fsKHz) + 3.0/float32(psEncCtrl.pitchL[k])
			psEncCtrl.lfMAShp[k] = -1.0 + b
			psEncCtrl.lfARShp[k] = 1.0 - b - float32(b*strength)
		}
		tilt = -hpNoiseCoef -
			float32(float32(float32((1-hpNoiseCoef)*harmHPNoiseCoef)*float32(psEnc.sCmn.speechActivityQ8))*(1.0/256.0))
	} else {
		b := 1.3 / float32(psEnc.sCmn.fsKHz)
		psEncCtrl.lfMAShp[0] = -1.0 + b
		psEncCtrl.lfARShp[0] = 1.0 - b - float32(float32(b*strength)*0.6)
		for k := 1; k < psEnc.sCmn.nbSubfr; k++ {
			psEncCtrl.lfMAShp[k] = psEncCtrl.lfMAShp[0]
			psEncCtrl.lfARShp[k] = psEncCtrl.lfARShp[0]
		}
		tilt = -hpNoiseCoef
	}

	// HARMONIC SHAPING CONTROL.
	var harmShapeGain float32
	if useHarmShaping != 0 && psEnc.sCmn.indices.signalType == typeVoiced {
		// Harmonic noise shaping.
		harmShapeGain = harmonicShaping

		// More harmonic noise shaping for high bitrates or noisy input.
		harmShapeGain += float32(highRateOrLowQualityHarmonicShaping *
			(1.0 - float32((1.0-psEncCtrl.codingQuality)*psEncCtrl.inputQuality)))

		// Less harmonic noise shaping for less periodic signals.
		harmShapeGain = float32(harmShapeGain * float32(math.Sqrt(float64(psEnc.ltpCorr))))
	} else {
		harmShapeGain = 0.0
	}

	// Smooth over subframes.
	for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
		psShapeSt.harmShapeGainSmth += float32(subfrSmthCoef * (harmShapeGain - psShapeSt.harmShapeGainSmth))
		psEncCtrl.harmShapeGain[k] = psShapeSt.harmShapeGainSmth
		psShapeSt.tiltSmth += float32(subfrSmthCoef * (tilt - psShapeSt.tiltSmth))
		psEncCtrl.tilt[k] = psShapeSt.tiltSmth
	}
}

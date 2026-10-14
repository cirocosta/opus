// Ported from libopus 1.6.1 silk/VAD.c.

package silk

// vadInit is silk_VAD_Init: initialization of the Silk VAD.
func vadInit(psSilkVAD *vadState) int {
	ret := 0

	// Reset state memory.
	*psSilkVAD = vadState{}

	// Init noise levels. Initialize array with approx pink noise levels
	// (psd proportional to inverse of frequency).
	for b := 0; b < vadNBands; b++ {
		psSilkVAD.noiseLevelBias[b] = max32(div32_16(vadNoiseLevelsBias, int32(b+1)), 1)
	}

	// Initialize state.
	for b := 0; b < vadNBands; b++ {
		psSilkVAD.nl[b] = mul(100, psSilkVAD.noiseLevelBias[b])
		psSilkVAD.invNL[b] = div32(int32MAX, psSilkVAD.nl[b])
	}
	psSilkVAD.counter = 15

	// Init smoothed energy-to-noise ratio.
	for b := 0; b < vadNBands; b++ {
		psSilkVAD.nrgRatioSmthQ8[b] = 100 * 256 // 100 * 256 --> 20 dB SNR
	}

	return ret
}

// Weighting factors for tilt measure.
var tiltWeights = [vadNBands]int32{30000, 6000, -12000, -12000}

// vadGetSAQ8 is silk_VAD_GetSA_Q8 (silk_VAD_GetSA_Q8_c): get the speech
// activity level in Q8. pIn is the PCM input.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psEncC *encoderState) vadGetSAQ8(pIn []int16) int {
	var (
		x                 [maxFrameLength + maxFrameLength/4]int16
		xnrg              [vadNBands]int32
		nrgToNoiseRatioQ8 [vadNBands]int32
		xOffset           [vadNBands]int
	)
	ret := 0
	psSilkVAD := &psEncC.sVAD

	// Filter and Decimate.
	decimatedFramelength1 := psEncC.frameLength >> 1
	decimatedFramelength2 := psEncC.frameLength >> 2
	decimatedFramelength := psEncC.frameLength >> 3
	// Decimate into 4 bands:
	//  0       L      3L       L              3L                             5L
	//          -      --       -              --                             --
	//          8       8       2               4                              4
	//
	//  [0-1 kHz| temp. |1-2 kHz|    2-4 kHz    |            4-8 kHz           |
	//
	// They're arranged to allow the minimal ( frame_length / 4 ) extra
	// scratch space during the downsampling process.
	xOffset[0] = 0
	xOffset[1] = decimatedFramelength + decimatedFramelength2
	xOffset[2] = xOffset[1] + decimatedFramelength
	xOffset[3] = xOffset[2] + decimatedFramelength2

	// 0-8 kHz to 0-4 kHz and 4-8 kHz.
	anaFiltBank1(pIn, &psSilkVAD.anaState, x[:], x[xOffset[3]:], psEncC.frameLength)

	// 0-4 kHz to 0-2 kHz and 2-4 kHz.
	anaFiltBank1(x[:], &psSilkVAD.anaState1, x[:], x[xOffset[2]:], decimatedFramelength1)

	// 0-2 kHz to 0-1 kHz and 1-2 kHz.
	anaFiltBank1(x[:], &psSilkVAD.anaState2, x[:], x[xOffset[1]:], decimatedFramelength2)

	// HP filter on lowest band (differentiator).
	x[decimatedFramelength-1] = x[decimatedFramelength-1] >> 1
	hpStateTmp := x[decimatedFramelength-1]
	for i := decimatedFramelength - 1; i > 0; i-- {
		x[i-1] = x[i-1] >> 1
		x[i] -= x[i-1]
	}
	x[0] -= psSilkVAD.hpState
	psSilkVAD.hpState = hpStateTmp

	// Calculate the energy in each band.
	var sumSquared int32
	for b := 0; b < vadNBands; b++ {
		// Find the decimated framelength in the non-uniformly divided
		// bands.
		decimatedFramelength = psEncC.frameLength >> uint(minInt(vadNBands-b, vadNBands-1))

		// Split length into subframe lengths.
		decSubframeLength := decimatedFramelength >> vadInternalSubframesLog2
		decSubframeOffset := 0

		// Compute energy per sub-frame; initialize with summed energy of
		// last subframe.
		xnrg[b] = psSilkVAD.xnrgSubfr[b]
		for s := 0; s < vadInternalSubframes; s++ {
			sumSquared = 0
			for i := 0; i < decSubframeLength; i++ {
				// The energy will be less than dec_subframe_length *
				// ( silk_int16_MIN / 8 ) ^ 2. Therefore we can accumulate
				// with no risk of overflow (unless dec_subframe_length >
				// 128).
				xTmp := int32(x[xOffset[b]+i+decSubframeOffset]) >> 3
				sumSquared = smlabb(sumSquared, xTmp, xTmp)
			}

			// Add/saturate summed energy of current subframe.
			if s < vadInternalSubframes-1 {
				xnrg[b] = addPosSat32(xnrg[b], sumSquared)
			} else {
				// Look-ahead subframe.
				xnrg[b] = addPosSat32(xnrg[b], sumSquared>>1)
			}

			decSubframeOffset += decSubframeLength
		}
		psSilkVAD.xnrgSubfr[b] = sumSquared
	}

	// Noise estimation.
	vadGetNoiseLevels(&xnrg, psSilkVAD)

	// Signal-plus-noise to noise ratio estimation.
	sumSquared = 0
	inputTilt := int32(0)
	for b := 0; b < vadNBands; b++ {
		speechNrg := xnrg[b] - psSilkVAD.nl[b]
		if speechNrg > 0 {
			// Divide, with sufficient resolution.
			if uint32(xnrg[b])&0xFF800000 == 0 {
				nrgToNoiseRatioQ8[b] = div32(lshift(xnrg[b], 8), psSilkVAD.nl[b]+1)
			} else {
				nrgToNoiseRatioQ8[b] = div32(xnrg[b], (psSilkVAD.nl[b]>>8)+1)
			}

			// Convert to log domain.
			snrQ7 := lin2log(nrgToNoiseRatioQ8[b]) - 8*128

			// Sum-of-squares.
			sumSquared = smlabb(sumSquared, snrQ7, snrQ7) // Q14

			// Tilt measure.
			if speechNrg < (int32(1) << 20) {
				// Scale down SNR value for small subband speech energies.
				snrQ7 = smulwb(lshift(sqrtApprox(speechNrg), 6), snrQ7)
			}
			inputTilt = smlawb(inputTilt, tiltWeights[b], snrQ7)
		} else {
			nrgToNoiseRatioQ8[b] = 256
		}
	}

	// Mean-of-squares.
	sumSquared = div32_16(sumSquared, vadNBands) // Q14

	// Root-mean-square approximation, scale to dBs, and write to output
	// pointer.
	pSNRdBQ7 := int32(int16(3 * sqrtApprox(sumSquared))) // Q7

	// Speech Probability Estimation.
	saQ15 := int32(sigmQ15(int(smulwb(vadSNRFactorQ16, pSNRdBQ7) - vadNegativeOffsetQ5)))

	// Frequency Tilt Measure.
	psEncC.inputTiltQ15 = int(lshift(int32(sigmQ15(int(inputTilt)))-16384, 1))

	// Scale the sigmoid output based on power levels.
	speechNrg := int32(0)
	for b := 0; b < vadNBands; b++ {
		// Accumulate signal-without-noise energies, higher frequency
		// bands have more weight.
		speechNrg += int32(b+1) * ((xnrg[b] - psSilkVAD.nl[b]) >> 4)
	}

	if psEncC.frameLength == 20*psEncC.fsKHz {
		speechNrg = speechNrg >> 1
	}
	// Power scaling.
	if speechNrg <= 0 {
		saQ15 = saQ15 >> 1
	} else if speechNrg < 16384 {
		speechNrg = lshift(speechNrg, 16)

		// Square-root.
		speechNrg = sqrtApprox(speechNrg)
		saQ15 = smulwb(32768+speechNrg, saQ15)
	}

	// Copy the resulting speech activity in Q8.
	psEncC.speechActivityQ8 = minInt(int(saQ15>>7), uint8MAX)

	// Energy Level and SNR estimation.
	// Smoothing coefficient.
	smoothCoefQ16 := smulwb(vadSNRSmoothCoefQ18, smulwb(saQ15, saQ15))

	if psEncC.frameLength == 10*psEncC.fsKHz {
		smoothCoefQ16 >>= 1
	}

	for b := 0; b < vadNBands; b++ {
		// Compute smoothed energy-to-noise ratio per band.
		psSilkVAD.nrgRatioSmthQ8[b] = smlawb(psSilkVAD.nrgRatioSmthQ8[b],
			nrgToNoiseRatioQ8[b]-psSilkVAD.nrgRatioSmthQ8[b], smoothCoefQ16)

		// Signal to noise ratio in dB per band.
		snrQ7 := 3 * (lin2log(psSilkVAD.nrgRatioSmthQ8[b]) - 8*128)
		// quality = sigmoid( 0.25 * ( SNR_dB - 16 ) ).
		psEncC.inputQualityBandsQ15[b] = sigmQ15(int((snrQ7 - 16*128) >> 4))
	}

	return ret
}

// vadGetNoiseLevels is silk_VAD_GetNoiseLevels: noise level estimation.
func vadGetNoiseLevels(pX *[vadNBands]int32, psSilkVAD *vadState) {
	var minCoef int32

	// Initially faster smoothing.
	if psSilkVAD.counter < 1000 { // 1000 = 20 sec
		minCoef = div32_16(int16MAX, (psSilkVAD.counter>>4)+1)
		// Increment frame counter.
		psSilkVAD.counter++
	} else {
		minCoef = 0
	}

	for k := 0; k < vadNBands; k++ {
		// Get old noise level estimate for current band.
		nl := psSilkVAD.nl[k]

		// Add bias.
		nrg := addPosSat32(pX[k], psSilkVAD.noiseLevelBias[k])

		// Invert energies.
		invNrg := div32(int32MAX, nrg)

		// Less update when subband energy is high.
		var coef int32
		if nrg > lshift(nl, 3) {
			coef = vadNoiseLevelSmoothCoefQ16 >> 3
		} else if nrg < nl {
			coef = vadNoiseLevelSmoothCoefQ16
		} else {
			coef = smulwb(smulww(invNrg, nl), vadNoiseLevelSmoothCoefQ16<<1)
		}

		// Initially faster smoothing.
		coef = max32(coef, minCoef)

		// Smooth inverse energies.
		psSilkVAD.invNL[k] = smlawb(psSilkVAD.invNL[k], invNrg-psSilkVAD.invNL[k], coef)

		// Compute noise level by inverting again.
		nl = div32(int32MAX, psSilkVAD.invNL[k])

		// Limit noise levels (guarantee 7 bits of head room).
		nl = min32(nl, 0x00FFFFFF)

		// Store as part of state.
		psSilkVAD.nl[k] = nl
	}
}

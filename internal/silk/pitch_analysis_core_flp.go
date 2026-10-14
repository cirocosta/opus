// Ported from libopus 1.6.1 silk/float/pitch_analysis_core_FLP.c.

package silk

import "github.com/cirocosta/opus/internal/dsp"

// pitchScratchSize is SCRATCH_SIZE.
const pitchScratchSize = 22

// pitchLagTables holds the lag codebook and lag range tables that
// silk_pitch_analysis_core_FLP selects (Lag_CB_ptr, Lag_range_ptr), so
// that matrix_ptr( Lag_CB_ptr, i, j, cbk_size ) is cb(i, j).
type pitchLagTables struct {
	nbSubfr    int
	complexity int
}

// cbStage2 is matrix_ptr( Lag_CB_ptr, i, j, cbk_size ) for the stage 2
// codebook.
func (t pitchLagTables) cbStage2(i, j int) int {
	if t.nbSubfr == peMaxNbSubfr {
		return int(cbLagsStage2[i][j])
	}
	return int(cbLagsStage2_10Ms[i][j])
}

// cbStage3 is matrix_ptr( Lag_CB_ptr, i, j, cbk_size ) for the stage 3
// codebook.
func (t pitchLagTables) cbStage3(i, j int) int {
	if t.nbSubfr == peMaxNbSubfr {
		return int(cbLagsStage3[i][j])
	}
	return int(cbLagsStage3_10Ms[i][j])
}

// lagRange is matrix_ptr( Lag_range_ptr, k, j, 2 ).
func (t pitchLagTables) lagRange(k, j int) int {
	if t.nbSubfr == peMaxNbSubfr {
		return int(lagRangeStage3[t.complexity][k][j])
	}
	return int(lagRangeStage3_10Ms[k][j])
}

// nbCbkSearchStage3 is the number of stage 3 codebook vectors to search.
func (t pitchLagTables) nbCbkSearchStage3() int {
	if t.nbSubfr == peMaxNbSubfr {
		return int(nbCbkSearchsStage3[t.complexity])
	}
	return peNbCbksStage3_10ms
}

// pitchStage3Array is the 3 dimensional [PE_MAX_NB_SUBFR][
// PE_NB_CBKS_STAGE3_MAX ][ PE_NB_STAGE3_LAGS ] array of the stage 3
// correlations and energies.
type pitchStage3Array [peMaxNbSubfr][peNbCbksStage3Max][peNbStage3Lags]float32

// pitchAnalysisCoreFLP is silk_pitch_analysis_core_FLP: core pitch
// analysis function. frame is the signal of length PE_FRAME_LENGTH_MS *
// Fs_kHz. It returns the voicing estimate: 0 voiced, 1 unvoiced.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func pitchAnalysisCoreFLP(
	frame []float32,
	pitchOut *[maxNbSubfr]int,
	lagIndex *int16,
	contourIndex *int8,
	ltpCorr *float32,
	prevLag int,
	searchThres1, searchThres2 float32,
	fsKHz, complexity, nbSubfr int,
) int {
	var (
		frame8kHz    [peMaxFrameLengthMs * 8]float32
		frame4kHz    [peMaxFrameLengthMs * 4]float32
		frame8FIX    [peMaxFrameLengthMs * 8]int16
		frame4FIX    [peMaxFrameLengthMs * 4]int16
		filtState    [6]int32
		c            [peMaxNbSubfr][(peMaxLag >> 1) + 5]float32
		xcorr        [peMaxLagMs*4 - peMinLagMs*4 + 1]float32
		cc           [peNbCbksStage2Ext]float32
		dSrch        [peDSrchLength]int
		dComp        [(peMaxLag >> 1) + 5]int16
		energiesSt3  pitchStage3Array
		crossCorrSt3 pitchStage3Array
	)
	tables := pitchLagTables{nbSubfr: nbSubfr, complexity: complexity}

	// Set up frame lengths max / min lag for the sampling frequency.
	frameLength := (peLTPMemLengthMs + nbSubfr*peSubfrLengthMs) * fsKHz
	frameLength4kHz := (peLTPMemLengthMs + nbSubfr*peSubfrLengthMs) * 4
	frameLength8kHz := (peLTPMemLengthMs + nbSubfr*peSubfrLengthMs) * 8
	sfLength := peSubfrLengthMs * fsKHz
	sfLength4kHz := peSubfrLengthMs * 4
	sfLength8kHz := peSubfrLengthMs * 8
	minLag := peMinLagMs * fsKHz
	minLag4kHz := peMinLagMs * 4
	minLag8kHz := peMinLagMs * 8
	maxLag := peMaxLagMs*fsKHz - 1
	maxLag4kHz := peMaxLagMs * 4
	maxLag8kHz := peMaxLagMs*8 - 1

	// Resample from input sampled at Fs_kHz to 8 kHz.
	switch fsKHz {
	case 16:
		// Resample to 16 -> 8 khz.
		var frame16FIX [16 * peMaxFrameLengthMs]int16
		float2shortArray(frame16FIX[:], frame, frameLength)
		filtState[0], filtState[1] = 0, 0
		resamplerDown2((*[2]int32)(filtState[:2]), frame8FIX[:], frame16FIX[:], frameLength)
		short2floatArray(frame8kHz[:], frame8FIX[:], frameLength8kHz)
	case 12:
		// Resample to 12 -> 8 khz.
		var frame12FIX [12 * peMaxFrameLengthMs]int16
		float2shortArray(frame12FIX[:], frame, frameLength)
		filtState = [6]int32{}
		resamplerDown2_3(&filtState, frame8FIX[:], frame12FIX[:], frameLength)
		short2floatArray(frame8kHz[:], frame8FIX[:], frameLength8kHz)
	default:
		float2shortArray(frame8FIX[:], frame, frameLength8kHz)
	}

	// Decimate again to 4 kHz.
	filtState[0], filtState[1] = 0, 0
	resamplerDown2((*[2]int32)(filtState[:2]), frame4FIX[:], frame8FIX[:], frameLength8kHz)
	short2floatArray(frame4kHz[:], frame4FIX[:], frameLength4kHz)

	// Low-pass filter.
	for i := frameLength4kHz - 1; i > 0; i-- {
		// silk_ADD_SAT16 on float values.
		s := frame4kHz[i] + frame4kHz[i-1]
		if s > int16MAX {
			s = int16MAX
		} else if s < int16MIN {
			s = int16MIN
		}
		frame4kHz[i] = float32(int16(s))
	}

	// FIRST STAGE, operating in 4 khz.
	targetPtr := sfLength4kHz << 2
	for k := 0; k < nbSubfr>>1; k++ {
		basisPtr := targetPtr - minLag4kHz

		dsp.CeltPitchXcorr(
			frame4kHz[targetPtr:],
			frame4kHz[targetPtr-maxLag4kHz:],
			xcorr[:],
			sfLength8kHz,
			maxLag4kHz-minLag4kHz+1,
		)

		// Calculate first vector products before loop.
		crossCorr := float64(xcorr[maxLag4kHz-minLag4kHz])
		normalizer := energyFLP(frame4kHz[targetPtr:], sfLength8kHz) +
			energyFLP(frame4kHz[basisPtr:], sfLength8kHz) +
			float64(float32(sfLength8kHz)*4000.0)

		c[0][minLag4kHz] += float32(float64(2*crossCorr) / normalizer)

		// From now on normalizer is computed recursively.
		for d := minLag4kHz + 1; d <= maxLag4kHz; d++ {
			basisPtr--

			crossCorr = float64(xcorr[maxLag4kHz-d])

			// Add contribution of new sample and remove contribution from
			// oldest sample.
			normalizer += float64(float64(frame4kHz[basisPtr])*float64(frame4kHz[basisPtr])) -
				float64(
					float64(
						frame4kHz[basisPtr+sfLength8kHz],
					)*float64(
						frame4kHz[basisPtr+sfLength8kHz],
					),
				)
			c[0][d] += float32(float64(2*crossCorr) / normalizer)
		}
		// Update target pointer.
		targetPtr += sfLength8kHz
	}

	// Apply short-lag bias.
	for i := maxLag4kHz; i >= minLag4kHz; i-- {
		c[0][i] -= float32(float32(c[0][i]*float32(i)) / 4096.0)
	}

	// Sort.
	lengthDSrch := 4 + 2*complexity
	insertionSortDecreasingFLP(c[0][minLag4kHz:], dSrch[:], maxLag4kHz-minLag4kHz+1, lengthDSrch)

	// Escape if correlation is very low already here.
	cmax := c[0][minLag4kHz]
	if cmax < 0.2 {
		for k := 0; k < nbSubfr; k++ {
			pitchOut[k] = 0
		}
		*ltpCorr = 0.0
		*lagIndex = 0
		*contourIndex = 0
		return 1
	}

	threshold := float32(searchThres1 * cmax)
	for i := 0; i < lengthDSrch; i++ {
		// Convert to 8 kHz indices for the sorted correlation that exceeds
		// the threshold.
		if c[0][minLag4kHz+i] > threshold {
			dSrch[i] = (dSrch[i] + minLag4kHz) << 1
		} else {
			lengthDSrch = i
			break
		}
	}

	for i := minLag8kHz - 5; i < maxLag8kHz+5; i++ {
		dComp[i] = 0
	}
	for i := 0; i < lengthDSrch; i++ {
		dComp[dSrch[i]] = 1
	}

	// Convolution.
	for i := maxLag8kHz + 3; i >= minLag8kHz; i-- {
		dComp[i] += dComp[i-1] + dComp[i-2]
	}

	lengthDSrch = 0
	for i := minLag8kHz; i < maxLag8kHz+1; i++ {
		if dComp[i+1] > 0 {
			dSrch[lengthDSrch] = i
			lengthDSrch++
		}
	}

	// Convolution.
	for i := maxLag8kHz + 3; i >= minLag8kHz; i-- {
		dComp[i] += dComp[i-1] + dComp[i-2] + dComp[i-3]
	}

	lengthDComp := 0
	for i := minLag8kHz; i < maxLag8kHz+4; i++ {
		if dComp[i] > 0 {
			dComp[lengthDComp] = int16(i - 2)
			lengthDComp++
		}
	}

	// SECOND STAGE, operating at 8 kHz, on lag sections with high
	// correlation.

	// Find energy of each subframe projected onto its history, for a range
	// of delays.
	c = [peMaxNbSubfr][(peMaxLag >> 1) + 5]float32{}

	var target []float32
	if fsKHz == 8 {
		target = frame
	} else {
		target = frame8kHz[:]
	}
	targetPtr = peLTPMemLengthMs * 8
	for k := 0; k < nbSubfr; k++ {
		energyTmp := energyFLP(target[targetPtr:], sfLength8kHz) + 1.0
		for j := 0; j < lengthDComp; j++ {
			d := int(dComp[j])
			basisPtr := targetPtr - d
			crossCorr := innerProductFLP(target[basisPtr:], target[targetPtr:], sfLength8kHz)
			if crossCorr > 0.0 {
				energy := energyFLP(target[basisPtr:], sfLength8kHz)
				c[k][d] = float32(float64(2*crossCorr) / (energy + energyTmp))
			} else {
				c[k][d] = 0.0
			}
		}
		targetPtr += sfLength8kHz
	}

	// Search over lag range and lags codebook; scale factor for lag
	// codebook, as a function of center lag.

	ccmax := float32(0.0) // This value doesn't matter
	ccmaxB := float32(-1000.0)

	cbimax := 0 // To avoid returning undefined lag values
	lag := -1   // To check if lag with strong enough correlation has been found

	var prevLagLog2 float32
	if prevLag > 0 {
		switch fsKHz {
		case 12:
			prevLag = (prevLag << 1) / 3
		case 16:
			prevLag = prevLag >> 1
		}
		prevLagLog2 = silkLog2(float64(float32(prevLag)))
	} else {
		prevLagLog2 = 0
	}

	// Set up stage 2 codebook based on number of subframes.
	var nbCbkSearch int
	if nbSubfr == peMaxNbSubfr {
		if fsKHz == 8 && complexity > silkPEMinComplex {
			// If input is 8 khz use a larger codebook here because it is
			// last stage.
			nbCbkSearch = peNbCbksStage2Ext
		} else {
			nbCbkSearch = peNbCbksStage2
		}
	} else {
		nbCbkSearch = peNbCbksStage2_10ms
	}

	for k := 0; k < lengthDSrch; k++ {
		d := dSrch[k]
		for j := 0; j < nbCbkSearch; j++ {
			cc[j] = 0.0
			for i := 0; i < nbSubfr; i++ {
				// Try all codebooks.
				cc[j] += c[i][d+tables.cbStage2(i, j)]
			}
		}
		// Find best codebook.
		ccmaxNew := float32(-1000.0)
		cbimaxNew := 0
		for i := 0; i < nbCbkSearch; i++ {
			if cc[i] > ccmaxNew {
				ccmaxNew = cc[i]
				cbimaxNew = i
			}
		}

		// Bias towards shorter lags.
		lagLog2 := silkLog2(float64(float32(d)))
		ccmaxNewB := ccmaxNew - float32(float32(peShortlagBias*float32(nbSubfr))*lagLog2)

		// Bias towards previous lag.
		if prevLag > 0 {
			deltaLagLog2Sqr := lagLog2 - prevLagLog2
			deltaLagLog2Sqr = float32(deltaLagLog2Sqr * deltaLagLog2Sqr)
			ccmaxNewB -= float32(
				float32(
					float32(float32(pePrevlagBias*float32(nbSubfr))*(*ltpCorr))*deltaLagLog2Sqr,
				) /
					(deltaLagLog2Sqr + 0.5),
			)
		}

		if ccmaxNewB > ccmaxB && // Find maximum biased correlation
			ccmaxNew > float32(
				float32(nbSubfr)*searchThres2,
			) { // Correlation needs to be high enough to be voiced
			ccmaxB = ccmaxNewB
			ccmax = ccmaxNew
			lag = d
			cbimax = cbimaxNew
		}
	}

	if lag == -1 {
		// No suitable candidate found.
		*pitchOut = [maxNbSubfr]int{}
		*ltpCorr = 0.0
		*lagIndex = 0
		*contourIndex = 0
		return 1
	}

	// Output normalized correlation.
	*ltpCorr = ccmax / float32(nbSubfr)

	if fsKHz > 8 {
		// Search in original signal.

		// Compensate for decimation.
		if fsKHz == 12 {
			lag = int(rshiftRound(smulbb(int32(lag), 3), 1))
		} else { // Fs_kHz == 16
			lag = lag << 1
		}

		lag = limitInt(lag, minLag, maxLag)
		startLag := maxInt(lag-2, minLag)
		endLag := minInt(lag+2, maxLag)
		lagNew := lag // to avoid undefined lag
		cbimax = 0    // to avoid undefined lag

		ccmax = -1000.0

		// Calculate the correlations and energies needed in stage 3.
		pAnaCalcCorrSt3(&crossCorrSt3, frame, startLag, sfLength, nbSubfr, tables)
		pAnaCalcEnergySt3(&energiesSt3, frame, startLag, sfLength, nbSubfr, tables)

		lagCounter := 0
		contourBias := peFlatcontourBias / float32(lag)

		// Set up cbk parameters according to complexity setting and frame
		// length.
		nbCbkSearch = tables.nbCbkSearchStage3()

		targetPtr = peLTPMemLengthMs * fsKHz
		energyTmp := energyFLP(frame[targetPtr:], nbSubfr*sfLength) + 1.0
		for d := startLag; d <= endLag; d++ {
			for j := 0; j < nbCbkSearch; j++ {
				crossCorr := 0.0
				energy := energyTmp
				for k := 0; k < nbSubfr; k++ {
					crossCorr += float64(crossCorrSt3[k][j][lagCounter])
					energy += float64(energiesSt3[k][j][lagCounter])
				}
				var ccmaxNew float32
				if crossCorr > 0.0 {
					ccmaxNew = float32(float64(2*crossCorr) / energy)
					// Reduce depending on flatness of contour.
					ccmaxNew = float32(ccmaxNew * (1.0 - float32(contourBias*float32(j))))
				} else {
					ccmaxNew = 0.0
				}

				if ccmaxNew > ccmax && (d+int(cbLagsStage3[0][j])) <= maxLag {
					ccmax = ccmaxNew
					lagNew = d
					cbimax = j
				}
			}
			lagCounter++
		}

		for k := 0; k < nbSubfr; k++ {
			pitchOut[k] = lagNew + tables.cbStage3(k, cbimax)
			pitchOut[k] = limitInt(pitchOut[k], minLag, peMaxLagMs*fsKHz)
		}
		*lagIndex = int16(lagNew - minLag)
		*contourIndex = int8(cbimax)
	} else { // Fs_kHz == 8
		// Save Lags.
		for k := 0; k < nbSubfr; k++ {
			pitchOut[k] = lag + tables.cbStage2(k, cbimax)
			pitchOut[k] = limitInt(pitchOut[k], minLag8kHz, peMaxLagMs*8)
		}
		*lagIndex = int16(lag - minLag8kHz)
		*contourIndex = int8(cbimax)
	}
	// Return as voiced.
	return 0
}

// pAnaCalcCorrSt3 is silk_P_Ana_calc_corr_st3: calculates the
// correlations used in stage 3 search. In order to cover the whole lag
// codebook for all the searched offset lags (lag +- 2), the following
// correlations are needed in each sub frame:
//
//	sf1: lag range [-8,...,7] total 16 correlations
//	sf2: lag range [-4,...,4] total 9 correlations
//	sf3: lag range [-3,....4] total 8 correltions
//	sf4: lag range [-6,....8] total 15 correlations
//
// In total 48 correlations. The direct implementation computed in worst
// case 4*12*5 = 240 correlations, but more likely around 120.
func pAnaCalcCorrSt3(
	crossCorrSt3 *pitchStage3Array,
	frame []float32,
	startLag, sfLength, nbSubfr int,
	t pitchLagTables,
) {
	var (
		scratchMem [pitchScratchSize]float32
		xcorr      [pitchScratchSize]float32
	)
	nbCbkSearch := t.nbCbkSearchStage3()

	targetPtr := sfLength << 2 // Pointer to middle of frame
	for k := 0; k < nbSubfr; k++ {
		lagCounter := 0

		// Calculate the correlations for each subframe.
		lagLow := t.lagRange(k, 0)
		lagHigh := t.lagRange(k, 1)
		dsp.CeltPitchXcorr(
			frame[targetPtr:],
			frame[targetPtr-startLag-lagHigh:],
			xcorr[:],
			sfLength,
			lagHigh-lagLow+1,
		)
		for j := lagLow; j <= lagHigh; j++ {
			scratchMem[lagCounter] = xcorr[lagHigh-j]
			lagCounter++
		}

		delta := t.lagRange(k, 0)
		for i := 0; i < nbCbkSearch; i++ {
			// Fill out the 3 dim array that stores the correlations for each
			// code_book vector for each start lag.
			idx := t.cbStage3(k, i) - delta
			for j := 0; j < peNbStage3Lags; j++ {
				crossCorrSt3[k][i][j] = scratchMem[idx+j]
			}
		}
		targetPtr += sfLength
	}
}

// pAnaCalcEnergySt3 is silk_P_Ana_calc_energy_st3: calculate the
// energies for first two subframes. The energies are calculated
// recursively.
func pAnaCalcEnergySt3(
	energiesSt3 *pitchStage3Array,
	frame []float32,
	startLag, sfLength, nbSubfr int,
	t pitchLagTables,
) {
	var scratchMem [pitchScratchSize]float32
	nbCbkSearch := t.nbCbkSearchStage3()

	targetPtr := sfLength << 2
	for k := 0; k < nbSubfr; k++ {
		lagCounter := 0

		// Calculate the energy for first lag.
		basisPtr := targetPtr - (startLag + t.lagRange(k, 0))
		energy := energyFLP(frame[basisPtr:], sfLength) + 1e-3
		scratchMem[lagCounter] = float32(energy)
		lagCounter++

		lagDiff := t.lagRange(k, 1) - t.lagRange(k, 0) + 1
		for i := 1; i < lagDiff; i++ {
			// Remove part outside new window.
			energy -= float64(
				float64(frame[basisPtr+sfLength-i]) * float64(frame[basisPtr+sfLength-i]),
			)

			// Add part that comes into window.
			energy += float64(float64(frame[basisPtr-i]) * float64(frame[basisPtr-i]))
			scratchMem[lagCounter] = float32(energy)
			lagCounter++
		}

		delta := t.lagRange(k, 0)
		for i := 0; i < nbCbkSearch; i++ {
			// Fill out the 3 dim array that stores the correlations for each
			// code_book vector for each start lag.
			idx := t.cbStage3(k, i) - delta
			for j := 0; j < peNbStage3Lags; j++ {
				energiesSt3[k][i][j] = scratchMem[idx+j]
			}
		}
		targetPtr += sfLength
	}
}

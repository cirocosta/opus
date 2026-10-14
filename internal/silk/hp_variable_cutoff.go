// Ported from libopus 1.6.1 silk/HP_variable_cutoff.c.

package silk

// Constants of silk_HP_variable_cutoff, as the C code computes them.
var (
	hpMinCutoffLogQ7 = lin2log(
		variableHPMinCutoffHz<<16,
	) - (16 << 7) // silk_lin2log( SILK_FIX_CONST( VARIABLE_HP_MIN_CUTOFF_HZ, 16 ) ) - ( 16 << 7 )
	hpMaxDeltaFreqQ7 = fixConstF32(
		variableHPMaxDeltaFreq,
		7,
	) // SILK_FIX_CONST( VARIABLE_HP_MAX_DELTA_FREQ, 7 )
	hpSmthCoef1Q16 = fixConstF32(
		variableHPSmthCoef1,
		16,
	) // SILK_FIX_CONST( VARIABLE_HP_SMTH_COEF1, 16 )
	hpSmth1MinQ15 = lshift(
		lin2log(variableHPMinCutoffHz),
		8,
	) // silk_LSHIFT( silk_lin2log( VARIABLE_HP_MIN_CUTOFF_HZ ), 8 )
	hpSmth1MaxQ15 = lshift(
		lin2log(variableHPMaxCutoffHz),
		8,
	) // silk_LSHIFT( silk_lin2log( VARIABLE_HP_MAX_CUTOFF_HZ ), 8 )
	variableHPSmthInitQ15 = lshift(lin2log(variableHPMinCutoffHz<<16)-(16<<7), 8)
)

// hpVariableCutoff is silk_HP_variable_cutoff: high-pass filter with
// cutoff frequency adaptation based on pitch lag statistics.
func hpVariableCutoff(stateFxx *[encoderNumChannels]encoderStateFLP) {
	psEncC1 := &stateFxx[0].sCmn

	// Adaptive cutoff frequency: estimate low end of pitch frequency
	// range.
	if psEncC1.prevSignalType == typeVoiced {
		// Difference, in log domain.
		pitchFreqHzQ16 := div32_16(
			lshift(mul(int32(psEncC1.fsKHz), 1000), 16),
			int32(psEncC1.prevLag),
		)
		pitchFreqLogQ7 := lin2log(pitchFreqHzQ16) - (16 << 7)

		// Adjustment based on quality.
		qualityQ15 := int32(psEncC1.inputQualityBandsQ15[0])
		pitchFreqLogQ7 = smlawb(pitchFreqLogQ7, smulwb(lshift(-qualityQ15, 2), qualityQ15),
			pitchFreqLogQ7-hpMinCutoffLogQ7)

		// delta_freq = pitch_freq_log - psEnc->variable_HP_smth1.
		deltaFreqQ7 := pitchFreqLogQ7 - (psEncC1.variableHPSmth1Q15 >> 8)
		if deltaFreqQ7 < 0 {
			// Less smoothing for decreasing pitch frequency, to track
			// something close to the minimum.
			deltaFreqQ7 = mul(deltaFreqQ7, 3)
		}

		// Limit delta, to reduce impact of outliers in pitch estimation.
		deltaFreqQ7 = limit32(deltaFreqQ7, -hpMaxDeltaFreqQ7, hpMaxDeltaFreqQ7)

		// Update smoother.
		psEncC1.variableHPSmth1Q15 = smlawb(psEncC1.variableHPSmth1Q15,
			smulbb(int32(psEncC1.speechActivityQ8), deltaFreqQ7), hpSmthCoef1Q16)

		// Limit frequency range.
		psEncC1.variableHPSmth1Q15 = limit32(
			psEncC1.variableHPSmth1Q15,
			hpSmth1MinQ15,
			hpSmth1MaxQ15,
		)
	}
}

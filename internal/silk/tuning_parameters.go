// Ported from libopus 1.6.1 silk/tuning_parameters.h.

package silk

// The float constants are typed float32 constants, as the C constants
// have the "f" suffix. Go rounds typed constant expressions to float32
// after each operation, which matches the C compiler's constant folding.
const (
	// Decay time for bitreservoir.
	bitreservoirDecayTimeMs = 500

	// Pitch estimator.

	// Level of noise floor for whitening filter LPC analysis in pitch
	// analysis.
	findPitchWhiteNoiseFraction = float32(1e-3)
	// Bandwidth expansion for whitening filter in pitch analysis.
	findPitchBandwidthExpansion = float32(0.99)

	// Linear prediction.

	// LPC analysis regularization.
	findLPCCondFac = float32(1e-5)
	// Max cumulative LTP gain.
	maxSumLogGainDB = float32(250.0)
	// LTP analysis defines.
	ltpCorrInvMax = float32(0.03)

	// High pass filtering.

	// Smoothing parameters for low end of pitch frequency range
	// estimation.
	variableHPSmthCoef1    = float32(0.1)
	variableHPSmthCoef2    = float32(0.015)
	variableHPMaxDeltaFreq = float32(0.4)
	// Min and max cut-off frequency values (-3 dB points).
	variableHPMinCutoffHz = 60
	variableHPMaxCutoffHz = 100

	// Various.

	// VAD threshold.
	speechActivityDTXThres = float32(0.05)
	// Speech Activity LBRR enable threshold.
	lbrrSpeechActivityThres = float32(0.3)

	// Perceptual parameters.

	// Reduction in coding SNR during low speech activity.
	bgSNRDecrDB = float32(2.0)
	// Factor for reducing quantization noise during voiced speech.
	harmSNRIncrDB = float32(2.0)

	// Threshold for sparseness measure above which to use lower
	// quantization offset during unvoiced.
	energyVariationThresholdQntOffset = float32(0.6)
	// Warping control.
	warpingMultiplier = float32(0.015)
	// Fraction added to first autocorrelation value.
	shapeWhiteNoiseFraction = float32(3e-5)
	// Noise shaping filter chirp factor.
	bandwidthExpansion = float32(0.94)
	// Harmonic noise shaping.
	harmonicShaping = float32(0.3)
	// Extra harmonic noise shaping for high bitrates or noisy input.
	highRateOrLowQualityHarmonicShaping = float32(0.2)
	// Parameter for shaping noise towards higher frequencies.
	hpNoiseCoef = float32(0.25)
	// Parameter for shaping noise even more towards higher frequencies
	// during voiced speech.
	harmHPNoiseCoef = float32(0.35)

	// Parameter for reducing noise at the very low frequencies.
	lowFreqShaping = float32(4.0)
	// Less reduction of noise at the very low frequencies for signals
	// with low SNR at low frequencies.
	lowQualityLowFreqShapingDecr = float32(0.5)
	// Subframe smoothing coefficient for HarmBoost, HarmShapeGain, Tilt
	// (lower -> more smoothing).
	subfrSmthCoef = float32(0.4)
	// Parameters defining the R/D tradeoff in the residual quantizer.
	lambdaOffset           = float32(1.2)
	lambdaSpeechAct        = float32(-0.2)
	lambdaDelayedDecisions = float32(-0.05)
	lambdaInputQuality     = float32(-0.1)
	lambdaCodingQuality    = float32(-0.2)
	lambdaQuantOffset      = float32(0.8)

	// Maximum time before allowing a bandwidth transition.
	maxBandwidthSwitchDelayMs = 5000
)

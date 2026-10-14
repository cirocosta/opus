// Ported from libopus 1.6.1 silk/define.h, silk/errors.h,
// silk/pitch_est_defines.h and silk/control.h (decoder flags).

package silk

// Error codes (errors.h).
const (
	NoError                         = 0
	EncInputInvalidNoOfSamples      = -101
	EncFsNotSupported               = -102
	EncPacketSizeNotSupported       = -103
	EncPayloadBufTooShort           = -104
	EncInvalidLossRate              = -105
	EncInvalidComplexitySetting     = -106
	EncInvalidInbandFECSetting      = -107
	EncInvalidDTXSetting            = -108
	EncInvalidCBRSetting            = -109
	EncInternalError                = -110
	EncInvalidNumberOfChannelsError = -111
	DecInvalidSamplingFrequency     = -200
	DecPayloadTooLarge              = -201
	DecPayloadError                 = -202
	DecInvalidFrameSize             = -203
)

// Decoder API flags (control.h): the lostFlag argument of Decode.
const (
	FlagDecodeNormal = 0 // FLAG_DECODE_NORMAL
	FlagPacketLost   = 1 // FLAG_PACKET_LOST
	FlagDecodeLBRR   = 2 // FLAG_DECODE_LBRR
)

// define.h.
const (
	encoderNumChannels = 2
	decoderNumChannels = 2

	maxFramesPerPacket = 3

	minTargetRateBps = 5000
	maxTargetRateBps = 80000

	lbrrNBMinRateBps = 12000
	lbrrMBMinRateBps = 14000
	lbrrWBMinRateBps = 16000

	nbSpeechFramesBeforeDTX = 10
	maxConsecutiveDTX       = 20
	dtxActivityThreshold    = float32(0.1)

	vadNoDecision = -1
	vadNoActivity = 0
	vadActivity   = 1

	maxFsKHz    = 16
	maxAPIFsKHz = 48

	typeNoVoiceActivity = 0
	typeUnvoiced        = 1
	typeVoiced          = 2

	codeIndependently             = 0
	codeIndependentlyNoLTPScaling = 1
	codeConditionally             = 2

	stereoQuantTabSize    = 16
	stereoQuantSubSteps   = 5
	stereoInterpLenMs     = 8
	stereoRatioSmoothCoef = 0.01

	pitchEstMinLagMs = 2
	pitchEstMaxLagMs = 18

	maxNbSubfr = 4

	ltpMemLengthMs     = 20
	subFrameLengthMs   = 5
	maxSubFrameLength  = subFrameLengthMs * maxFsKHz
	maxFrameLengthMs   = subFrameLengthMs * maxNbSubfr
	maxFrameLength     = maxFrameLengthMs * maxFsKHz
	laPitchMs          = 2
	laPitchMax         = laPitchMs * maxFsKHz
	maxFindPitchLPCOrd = 16

	findPitchLPCWinMs    = 20 + (laPitchMs << 1)
	findPitchLPCWinMs2SF = 10 + (laPitchMs << 1)
	findPitchLPCWinMax   = findPitchLPCWinMs * maxFsKHz

	laShapeMs      = 5
	laShapeMax     = laShapeMs * maxFsKHz
	shapeLPCWinMax = 15 * maxFsKHz

	minQGainDb          = 2
	maxQGainDb          = 88
	nLevelsQGain        = 64
	maxDeltaGainQuant   = 36
	minDeltaGainQuant   = -4
	offsetVLQ10         = 32
	offsetVHQ10         = 100
	offsetUVLQ10        = 100
	offsetUVHQ10        = 240
	quantLevelAdjustQ10 = 80

	maxLPCStabilizeIterations        = 16
	maxPredictionPowerGain           = float32(1e4)
	maxPredictionPowerGainAfterReset = float32(1e2)

	maxLPCOrder = 16
	minLPCOrder = 10

	ltpOrder  = 5
	nbLTPCbks = 3

	useHarmShaping            = 1
	maxShapeLPCOrder          = 24
	harmShapeFIRTaps          = 3
	maxDelDecStates           = 4
	ltpBufLength              = 512
	ltpMask                   = ltpBufLength - 1
	decisionDelay             = 40
	shellCodecFrameLength     = 16
	log2ShellCodecFrameLength = 4
	maxNbShellBlocks          = maxFrameLength / shellCodecFrameLength
	nRateLevels               = 10
	silkMaxPulses             = 16
	maxMatrixSize             = maxLPCOrder
	nsqLPCBufLength           = maxLPCOrder

	vadNBands                  = 4
	vadInternalSubframesLog2   = 2
	vadInternalSubframes       = 1 << vadInternalSubframesLog2
	vadNoiseLevelSmoothCoefQ16 = 1024
	vadNoiseLevelsBias         = 50
	vadNegativeOffsetQ5        = 128
	vadSNRFactorQ16            = 45000
	vadSNRSmoothCoefQ18        = 4096

	lsfCosTabSzFIX = 128

	nlsfWQ                    = 2
	nlsfVQMaxVectors          = 32
	nlsfQuantMaxAmplitude     = 4
	nlsfQuantMaxAmplitudeExt  = 10
	nlsfQuantLevelAdj         = 0.1
	nlsfQuantDelDecStatesLog2 = 2
	nlsfQuantDelDecStates     = 1 << nlsfQuantDelDecStatesLog2

	transitionTimeMs   = 5120
	transitionNB       = 3
	transitionNA       = 2
	transitionIntNum   = 5
	transitionFrames   = transitionTimeMs / maxFrameLengthMs
	transitionIntSteps = transitionFrames / (transitionIntNum - 1)

	bweAfterLossQ16 = 63570

	cngBufMaskMax           = 255
	cngGainSmthQ16          = 4634
	cngGainSmthThresholdQ16 = 46396
	cngNLSFSmthQ16          = 16348
)

// pitch_est_defines.h.
const (
	peMaxFsKHz          = 16
	peMaxNbSubfr        = 4
	peSubfrLengthMs     = 5
	peLTPMemLengthMs    = 4 * peSubfrLengthMs
	peMaxFrameLengthMs  = peLTPMemLengthMs + peMaxNbSubfr*peSubfrLengthMs
	peMaxFrameLength    = peMaxFrameLengthMs * peMaxFsKHz
	peMaxFrameLengthST1 = peMaxFrameLength >> 2
	peMaxFrameLengthST2 = peMaxFrameLength >> 1
	peMaxLagMs          = 18
	peMinLagMs          = 2
	peMaxLag            = peMaxLagMs * peMaxFsKHz
	peMinLag            = peMinLagMs * peMaxFsKHz
	peDSrchLength       = 24
	peNbStage3Lags      = 5
	peNbCbksStage2      = 3
	peNbCbksStage2Ext   = 11
	peNbCbksStage3Max   = 34
	peNbCbksStage3Mid   = 24
	peNbCbksStage3Min   = 16
	peNbCbksStage3_10ms = 12
	peNbCbksStage2_10ms = 3
	peShortlagBias      = float32(0.2)
	pePrevlagBias       = float32(0.2)
	peFlatcontourBias   = float32(0.05)
	silkPEMinComplex    = 0
	silkPEMidComplex    = 1
	silkPEMaxComplex    = 2
)

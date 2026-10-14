// Ported from libopus 1.6.1 silk/structs.h and silk/resampler_structs.h.

package silk

// Resampler limits (resampler_structs.h).
const (
	resamplerMaxFIROrder = 36 // SILK_RESAMPLER_MAX_FIR_ORDER
	resamplerMaxIIROrder = 6  // SILK_RESAMPLER_MAX_IIR_ORDER
)

// resamplerState is silk_resampler_state_struct. The C union sFIR
// (i32/i16) is two separate arrays here; each resampler function uses
// only one of them, and silk_resampler_init clears both.
type resamplerState struct {
	sIIR              [resamplerMaxIIROrder]int32
	sFIRi32           [resamplerMaxFIROrder]int32 // sFIR.i32
	sFIRi16           [resamplerMaxFIROrder]int16 // sFIR.i16
	delayBuf          [96]int16
	resamplerFunction int
	batchSize         int
	invRatioQ16       int32
	firOrder          int
	firFracs          int
	fsInKHz           int
	fsOutKHz          int
	inputDelay        int
	coefs             []int16

	// Scratch buffers for the ALLOC calls in the private resamplers.
	// They are not part of the C state.
	buf16 [2*resamplerMaxBatchSizeIn + resamplerOrderFIR12]int16
	buf32 [resamplerMaxBatchSizeIn + resamplerMaxFIROrder]int32
}

// nlsfCBStruct is silk_NLSF_CB_struct.
type nlsfCBStruct struct {
	nVectors           int16
	order              int16
	quantStepSizeQ16   int16
	invQuantStepSizeQ6 int16
	cb1NLSFQ8          []uint8 // CB1_NLSF_Q8
	cb1WghtQ9          []int16 // CB1_Wght_Q9
	cb1ICDF            []uint8 // CB1_iCDF
	predQ8             []uint8 // pred_Q8
	ecSel              []uint8 // ec_sel
	ecICDF             []uint8 // ec_iCDF
	ecRatesQ5          []uint8 // ec_Rates_Q5
	deltaMinQ15        []int16 // deltaMin_Q15
}

// stereoDecState is stereo_dec_state.
type stereoDecState struct {
	predPrevQ13 [2]int16
	sMid        [2]int16
	sSide       [2]int16
}

// sideInfoIndices is SideInfoIndices.
type sideInfoIndices struct {
	gainsIndices     [maxNbSubfr]int8
	ltpIndex         [maxNbSubfr]int8
	nlsfIndices      [maxLPCOrder + 1]int8
	lagIndex         int16
	contourIndex     int8
	signalType       int8
	quantOffsetType  int8
	nlsfInterpCoefQ2 int8
	perIndex         int8
	ltpScaleIndex    int8
	seed             int8
}

// plcStruct is silk_PLC_struct. The C struct also has enable_deep_plc,
// which only ENABLE_DEEP_PLC code reads; it is not ported.
type plcStruct struct {
	pitchLQ8        int32              // Pitch lag to use for voiced concealment
	ltpCoefQ14      [ltpOrder]int16    // LTP coefficients to use for voiced concealment
	prevLPCQ12      [maxLPCOrder]int16 //
	lastFrameLost   int                // Was previous frame lost
	randSeed        int32              // Seed for unvoiced signal generation
	randScaleQ14    int16              // Scaling of unvoiced random signal
	concEnergy      int32              //
	concEnergyShift int                //
	prevLTPScaleQ14 int16              //
	prevGainQ16     [2]int32           //
	fsKHz           int                //
	nbSubfr         int                //
	subfrLength     int                //
}

// cngStruct is silk_CNG_struct.
type cngStruct struct {
	cngExcBufQ14   [maxFrameLength]int32
	cngSmthNLSFQ15 [maxLPCOrder]int16
	cngSynthState  [maxLPCOrder]int32
	cngSmthGainQ16 int32
	randSeed       int32
	fsKHz          int
}

// decoderState is silk_decoder_state. The fields that silk_reset_decoder
// clears (from prev_gain_Q16, SILK_DECODER_STATE_RESET_START, to the end)
// are in the embedded decoderResetState. The scratch buffers are not
// cleared.
type decoderState struct {
	decoderResetState

	scratch decScratch
}

// decoderResetState holds the silk_decoder_state fields from
// prev_gain_Q16 to the end of the struct.
type decoderResetState struct {
	prevGainQ16          int32
	excQ14               [maxFrameLength]int32
	sLPCQ14Buf           [maxLPCOrder]int32
	outBuf               [maxFrameLength + 2*maxSubFrameLength]int16 // Buffer for output signal
	lagPrev              int                                         // Previous Lag
	lastGainIndex        int8                                        // Previous gain index
	fsKHz                int                                         // Sampling frequency in kHz
	fsAPIHz              int32                                       // API sample frequency (Hz)
	nbSubfr              int                                         // Number of 5 ms subframes in a frame
	frameLength          int                                         // Frame length (samples)
	subfrLength          int                                         // Subframe length (samples)
	ltpMemLength         int                                         // Length of LTP memory
	lpcOrder             int                                         // LPC order
	prevNLSFQ15          [maxLPCOrder]int16                          // Used to interpolate LSFs
	firstFrameAfterReset int                                         // Flag for deactivating NLSF interpolation
	pitchLagLowBitsICDF  []uint8                                     // iCDF table for low bits of pitch lag index
	pitchContourICDF     []uint8                                     // iCDF table for pitch contour index

	// For buffering payload in case of more frames per packet.
	nFramesDecoded   int
	nFramesPerPacket int

	// Specifically for entropy coding.
	ecPrevSignalType int
	ecPrevLagIndex   int16

	vadFlags  [maxFramesPerPacket]int
	lbrrFlag  int
	lbrrFlags [maxFramesPerPacket]int

	resamplerState resamplerState

	psNLSFCB *nlsfCBStruct // Pointer to NLSF codebook

	// Quantization indices.
	indices sideInfoIndices

	// CNG state.
	sCNG cngStruct

	// Stuff used for PLC.
	lossCnt        int
	prevSignalType int

	sPLC plcStruct
}

// decoderControl is silk_decoder_control.
type decoderControl struct {
	// Prediction and coding parameters.
	pitchL      [maxNbSubfr]int
	gainsQ16    [maxNbSubfr]int32
	predCoefQ12 [2][maxLPCOrder]int16 // Holds interpolated and final coefficients
	ltpCoefQ14  [ltpOrder * maxNbSubfr]int16
	ltpScaleQ14 int
}

// decScratch holds the per-channel scratch buffers that the C decoder
// allocates on the stack (ALLOC) in decode_frame, decode_core, PLC and
// CNG. They carry no state between calls.
type decScratch struct {
	pulses  [maxFrameLength + shellCodecFrameLength]int16 // decode_frame: pulses
	sLTP    [maxFrameLength]int16                         // decode_core / PLC: sLTP (ltp_mem_length)
	sLTPQ15 [2 * maxFrameLength]int32                     // decode_core: sLTP_Q15; PLC: sLTP_Q14
	resQ14  [maxSubFrameLength]int32                      // decode_core: res_Q14
	sLPCQ14 [maxSubFrameLength + maxLPCOrder]int32        // decode_core: sLPC_Q14
	excBuf  [2 * maxSubFrameLength]int16                  // PLC_energy: exc_buf
	cngSig  [maxFrameLength + maxLPCOrder]int32           // CNG: CNG_sig_Q14

	ctrl decoderControl // decode_frame: psDecCtrl
}

// nsqState is silk_nsq_state: the noise shaping quantization state.
type nsqState struct {
	xq            [2 * maxFrameLength]int16 // Buffer for quantized output signal
	sLTPShpQ14    [2 * maxFrameLength]int32
	sLPCQ14       [maxSubFrameLength + nsqLPCBufLength]int32
	sAR2Q14       [maxShapeLPCOrder]int32
	sLFARShpQ14   int32
	sDiffShpQ14   int32
	lagPrev       int
	sLTPBufIdx    int
	sLTPShpBufIdx int
	randSeed      int32
	prevGainQ16   int32
	rewhiteFlag   int
}

// vadState is silk_VAD_state.
type vadState struct {
	anaState       [2]int32         // Analysis filterbank state: 0-8 kHz
	anaState1      [2]int32         // Analysis filterbank state: 0-4 kHz
	anaState2      [2]int32         // Analysis filterbank state: 0-2 kHz
	xnrgSubfr      [vadNBands]int32 // Subframe energies
	nrgRatioSmthQ8 [vadNBands]int32 // Smoothed energy level in each band
	hpState        int16            // State of differentiator in the lowest band
	nl             [vadNBands]int32 // Noise energy level in each band
	invNL          [vadNBands]int32 // Inverse noise energy level in each band
	noiseLevelBias [vadNBands]int32 // Noise level estimator bias/offset
	counter        int32            // Frame counter used in the initial phase
}

// lpState is silk_LP_state: the variable cut-off low-pass filter state.
type lpState struct {
	inLPState         [2]int32 // Low pass filter state
	transitionFrameNo int32    // Counter which is mapped to a cut-off frequency
	mode              int      // Operating mode, <0: switch down, >0: switch up; 0: do nothing
	savedFsKHz        int32    // If non-zero, holds the last sampling rate before a bandwidth switching reset
}

// stereoEncState is stereo_enc_state.
type stereoEncState struct {
	predPrevQ13   [2]int16
	sMid          [2]int16
	sSide         [2]int16
	midSideAmpQ0  [4]int32
	smthWidthQ14  int16
	widthPrevQ14  int16
	silentSideLen int16
	predIx        [maxFramesPerPacket][2][3]int8
	midOnlyFlags  [maxFramesPerPacket]int8
}

// encoderState is silk_encoder_state.
type encoderState struct {
	inHPState                   [2]int32           // High pass filter state
	variableHPSmth1Q15          int32              // State of first smoother
	variableHPSmth2Q15          int32              // State of second smoother
	sLP                         lpState            // Low pass filter state
	sVAD                        vadState           // Voice activity detector state
	sNSQ                        nsqState           // Noise Shape Quantizer State
	prevNLSFqQ15                [maxLPCOrder]int16 // Previously quantized NLSF vector
	speechActivityQ8            int                // Speech activity
	allowBandwidthSwitch        int                // Flag indicating that switching of internal bandwidth is allowed
	lbrrPrevLastGainIndex       int8               //
	prevSignalType              int8               //
	prevLag                     int                //
	pitchLPCWinLength           int                //
	maxPitchLag                 int                // Highest possible pitch lag (samples)
	apiFsHz                     int32              // API sampling frequency (Hz)
	prevAPIFsHz                 int32              // Previous API sampling frequency (Hz)
	maxInternalFsHz             int                // Maximum internal sampling frequency (Hz)
	minInternalFsHz             int                // Minimum internal sampling frequency (Hz)
	desiredInternalFsHz         int                // Soft request for internal sampling frequency (Hz)
	fsKHz                       int                // Internal sampling frequency (kHz)
	nbSubfr                     int                // Number of 5 ms subframes in a frame
	frameLength                 int                // Frame length (samples)
	subfrLength                 int                // Subframe length (samples)
	ltpMemLength                int                // Length of LTP memory
	laPitch                     int                // Look-ahead for pitch analysis (samples)
	laShape                     int                // Look-ahead for noise shape analysis (samples)
	shapeWinLength              int                // Window length for noise shape analysis (samples)
	targetRateBps               int32              // Target bitrate (bps)
	packetSizeMs                int                // Number of milliseconds to put in each packet
	packetLossPerc              int                // Packet loss rate measured by farend
	frameCounter                int32              //
	complexity                  int                // Complexity setting
	nStatesDelayedDecision      int                // Number of states in delayed decision quantization
	useInterpolatedNLSFs        int                // Flag for using NLSF interpolation
	shapingLPCOrder             int                // Filter order for noise shaping filters
	predictLPCOrder             int                // Filter order for prediction filters
	pitchEstimationComplexity   int                // Complexity level for pitch estimator
	pitchEstimationLPCOrder     int                // Whitening filter order for pitch estimator
	pitchEstimationThresholdQ16 int32              // Threshold for pitch estimator
	sumLogGainQ7                int32              // Cumulative max prediction gain
	nlsfMSVQSurvivors           int                // Number of survivors in NLSF MSVQ
	firstFrameAfterReset        int                // Flag for deactivating NLSF interpolation, pitch prediction
	controlledSinceLastPayload  int                // Flag for ensuring codec_control only runs once per packet
	warpingQ16                  int                // Warping parameter for warped noise shaping
	useCBR                      int                // Flag to enable constant bitrate
	prefillFlag                 int                // Flag to indicate that only buffers are prefilled, no coding
	pitchLagLowBitsICDF         []uint8            // iCDF table for low bits of pitch lag index
	pitchContourICDF            []uint8            // iCDF table for pitch contour index
	psNLSFCB                    *nlsfCBStruct      // Pointer to NLSF codebook
	inputQualityBandsQ15        [vadNBands]int     //
	inputTiltQ15                int                //
	snrDBQ7                     int                // Quality setting
	vadFlags                    [maxFramesPerPacket]int8
	lbrrFlag                    int8
	lbrrFlags                   [maxFramesPerPacket]int
	indices                     sideInfoIndices
	pulses                      [maxFrameLength]int8

	// Input/output buffering.
	inputBuf          [maxFrameLength + 2]int16 // Buffer containing input signal
	inputBufIx        int
	nFramesPerPacket  int
	nFramesEncoded    int // Number of frames analyzed in current packet
	nChannelsAPI      int
	nChannelsInternal int
	channelNb         int

	// Specifically for entropy coding.
	ecPrevSignalType int
	ecPrevLagIndex   int16

	resamplerState resamplerState

	// DTX.
	useDTX          int // Flag to enable DTX
	inDTX           int // Flag to signal DTX period
	noSpeechCounter int // Counts concecutive nonactive frames, used by DTX

	// Inband Low Bitrate Redundancy (LBRR) data.
	useInBandFEC      int // Saves the API setting for query
	lbrrEnabled       int // Depends on useInBandFRC, bitrate and packet loss rate
	lbrrGainIncreases int // Gains increment for coding LBRR frames
	indicesLBRR       [maxFramesPerPacket]sideInfoIndices
	pulsesLBRR        [maxFramesPerPacket][maxFrameLength]int8
}

// Ported from libopus 1.6.1 silk/float/structs_FLP.h.

package silk

// shapeStateFLP is silk_shape_state_FLP: the noise shaping analysis
// state.
type shapeStateFLP struct {
	lastGainIndex     int8
	harmShapeGainSmth float32
	tiltSmth          float32
}

// encoderStateFLP is silk_encoder_state_FLP. The scratch buffers are
// not part of the C state.
type encoderStateFLP struct {
	sCmn   encoderState  // Common struct, shared with fixed-point code
	sShape shapeStateFLP // Noise shaping state

	// Buffer for find pitch and noise shape analysis.
	xBuf    [2*maxFrameLength + laShapeMax]float32
	ltpCorr float32 // Normalized correlation from pitch lag estimator

	scratch encScratch
}

// encoderControlFLP is silk_encoder_control_FLP.
type encoderControlFLP struct {
	// Prediction and coding parameters.
	gains    [maxNbSubfr]float32
	predCoef [2][maxLPCOrder]float32 // holds interpolated and final coefficients
	ltpCoef  [ltpOrder * maxNbSubfr]float32
	ltpScale float32
	pitchL   [maxNbSubfr]int

	// Noise shaping parameters.
	ar            [maxNbSubfr * maxShapeLPCOrder]float32
	lfMAShp       [maxNbSubfr]float32
	lfARShp       [maxNbSubfr]float32
	tilt          [maxNbSubfr]float32
	harmShapeGain [maxNbSubfr]float32
	lambda        float32
	inputQuality  float32
	codingQuality float32

	// Measures.
	predGain      float32
	ltpredCodGain float32
	resNrg        [maxNbSubfr]float32 // Residual energy per subframe

	// Parameters for CBR mode.
	gainsUnqQ16       [maxNbSubfr]int32
	lastGainIndexPrev int8
}

// stereoScratch holds the ALLOC buffers of silk_stereo_LR_to_MS.
type stereoScratch struct {
	side   [maxFrameLength + 2]int16
	lpMid  [maxFrameLength]int16
	hpMid  [maxFrameLength]int16
	lpSide [maxFrameLength]int16
	hpSide [maxFrameLength]int16
}

// encScratch holds the per-channel buffers that the C encoder keeps on
// the stack (ALLOC and large locals). They carry no state between calls.
type encScratch struct {
	// silk_setup_resamplers.
	tempResampler resamplerState
	xBufFIX       [2*maxFrameLength + laShapeMax]int16
	xBufAPIFsHz   [(2*maxFrameLengthMs + laShapeMs) * maxAPIFsKHz]int16

	// silk_encode_frame_FLP.
	sEncCtrl  encoderControlFLP
	resPitch  [2*maxFrameLength + laPitchMax]float32
	sNSQCopy  [2]nsqState
	ecBufCopy [1275]byte

	// silk_LBRR_encode_FLP.
	sNSQLBRR nsqState

	// silk_NSQ_wrapper_FLP and the NSQ.
	nsqParams nsqParams
	x16       [maxFrameLength]int16
	nsq       nsqScratch
}

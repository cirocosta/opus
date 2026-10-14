// Ported from libopus 1.6.1 src/opus_projection_decoder.c.

package opus

// ProjectionDecoder decodes ambisonics streams coded with the projection
// (demixing matrix) channel mapping family 3 (OpusProjectionDecoder).
// Create one with NewProjectionDecoder. A ProjectionDecoder is not safe
// for concurrent use.
type ProjectionDecoder struct {
	demixing mappingMatrix
	ms       MSDecoder
}

// projectionDecoderGetSize ports opus_projection_decoder_get_size. It
// returns 0 for invalid arguments.
func projectionDecoderGetSize(channels, streams, coupledStreams int) int32 {
	matrixSize := mappingMatrixGetSize(streams+coupledStreams, channels)
	if matrixSize == 0 {
		return 0
	}
	if streams < 1 || coupledStreams > streams || coupledStreams < 0 {
		// opus_multistream_decoder_get_size returns 0.
		return 0
	}
	return 1 // only checked against 0
}

// NewProjectionDecoder allocates and initializes a projection decoder
// (opus_projection_decoder_create). demixingMatrix is the demixing
// matrix as stored in the Ogg Opus header: (streams+coupledStreams) *
// channels little-endian int16 values in column-major order.
func NewProjectionDecoder(
	fs, channels, streams, coupledStreams int,
	demixingMatrix []byte,
) (*ProjectionDecoder, error) {
	// Allocate space for the projection decoder.
	if projectionDecoderGetSize(channels, streams, coupledStreams) == 0 {
		return nil, ErrAllocFail
	}
	st := new(ProjectionDecoder)
	// Initialize projection decoder with provided settings.
	if err := st.Init(fs, channels, streams, coupledStreams, demixingMatrix); err != nil {
		return nil, err
	}
	return st, nil
}

// Init initializes a projection decoder (opus_projection_decoder_init).
func (st *ProjectionDecoder) Init(
	fs, channels, streams, coupledStreams int,
	demixingMatrix []byte,
) error {
	var mapping [255]byte

	// Verify supplied matrix size.
	nbInputStreams := streams + coupledStreams
	expectedMatrixSize := nbInputStreams * channels * 2
	if expectedMatrixSize != len(demixingMatrix) || channels < 1 || channels > 255 {
		return ErrBadArg
	}

	// Convert demixing matrix input into internal format.
	buf := make([]int16, nbInputStreams*channels)
	for i := range buf {
		s := int(demixingMatrix[2*i+1])<<8 | int(demixingMatrix[2*i])
		s = ((s & 0xFFFF) ^ 0x8000) - 0x8000
		buf[i] = int16(s)
	}

	// Assign demixing matrix.
	if mappingMatrixGetSize(channels, nbInputStreams) == 0 {
		return ErrBadArg
	}
	mappingMatrixInit(&st.demixing, channels, nbInputStreams, 0, buf)

	// Set trivial mapping so each input channel pairs with a matrix
	// column.
	for i := 0; i < channels; i++ {
		mapping[i] = byte(i)
	}

	return st.ms.Init(fs, channels, streams, coupledStreams, mapping[:channels])
}

// Decode decodes a projection packet to 16-bit PCM
// (opus_projection_decode). The arguments are as for MSDecoder.Decode.
func (st *ProjectionDecoder) Decode(
	data []byte,
	pcm []int16,
	frameSize int,
	decodeFEC bool,
) (int, error) {
	if !st.ms.checkPCM(len(pcm), frameSize) {
		return 0, ErrBufferTooSmall
	}
	out := msOutput{kind: outInt16, i16: pcm, matrix: &st.demixing}
	return decodeResult(st.ms.decodeNative(data, &out, frameSize, decodeFEC, true))
}

// Decode24 decodes a projection packet to 24-bit PCM in int32 samples
// (opus_projection_decode24).
func (st *ProjectionDecoder) Decode24(
	data []byte,
	pcm []int32,
	frameSize int,
	decodeFEC bool,
) (int, error) {
	if !st.ms.checkPCM(len(pcm), frameSize) {
		return 0, ErrBufferTooSmall
	}
	out := msOutput{kind: outInt24, i24: pcm, matrix: &st.demixing}
	return decodeResult(st.ms.decodeNative(data, &out, frameSize, decodeFEC, false))
}

// DecodeFloat decodes a projection packet to float PCM
// (opus_projection_decode_float).
func (st *ProjectionDecoder) DecodeFloat(
	data []byte,
	pcm []float32,
	frameSize int,
	decodeFEC bool,
) (int, error) {
	if !st.ms.checkPCM(len(pcm), frameSize) {
		return 0, ErrBufferTooSmall
	}
	out := msOutput{kind: outFloat, f32: pcm, matrix: &st.demixing}
	return decodeResult(st.ms.decodeNative(data, &out, frameSize, decodeFEC, false))
}

// The controls of opus_projection_decoder_ctl are those of the
// multistream decoder.

// Channels returns the number of output channels.
func (st *ProjectionDecoder) Channels() int { return st.ms.Channels() }

// Bandwidth returns the bandwidth of the last packet of the first stream
// (OPUS_GET_BANDWIDTH).
func (st *ProjectionDecoder) Bandwidth() int { return st.ms.Bandwidth() }

// SampleRate returns the sampling rate (OPUS_GET_SAMPLE_RATE).
func (st *ProjectionDecoder) SampleRate() int { return st.ms.SampleRate() }

// Gain returns the output gain of the first stream (OPUS_GET_GAIN).
func (st *ProjectionDecoder) Gain() int { return st.ms.Gain() }

// LastPacketDuration returns the duration of the last packet of the
// first stream (OPUS_GET_LAST_PACKET_DURATION).
func (st *ProjectionDecoder) LastPacketDuration() int { return st.ms.LastPacketDuration() }

// PhaseInversionDisabled reports whether the first stream has phase
// inversion disabled (OPUS_GET_PHASE_INVERSION_DISABLED).
func (st *ProjectionDecoder) PhaseInversionDisabled() bool { return st.ms.PhaseInversionDisabled() }

// Complexity returns the complexity of the first stream
// (OPUS_GET_COMPLEXITY).
func (st *ProjectionDecoder) Complexity() int { return st.ms.Complexity() }

// FinalRange returns the XOR of the final range of all streams
// (OPUS_GET_FINAL_RANGE).
func (st *ProjectionDecoder) FinalRange() uint32 { return st.ms.FinalRange() }

// Reset resets all streams (OPUS_RESET_STATE).
func (st *ProjectionDecoder) Reset() { st.ms.Reset() }

// DecoderState returns the decoder of stream streamID
// (OPUS_MULTISTREAM_GET_DECODER_STATE).
func (st *ProjectionDecoder) DecoderState(streamID int) (*Decoder, error) {
	return st.ms.DecoderState(streamID)
}

// SetGain sets the output gain of all streams (OPUS_SET_GAIN).
func (st *ProjectionDecoder) SetGain(gain int) error { return st.ms.SetGain(gain) }

// SetComplexity sets the complexity of all streams
// (OPUS_SET_COMPLEXITY).
func (st *ProjectionDecoder) SetComplexity(complexity int) error {
	return st.ms.SetComplexity(complexity)
}

// SetPhaseInversionDisabled sets the phase inversion control of all
// streams (OPUS_SET_PHASE_INVERSION_DISABLED).
func (st *ProjectionDecoder) SetPhaseInversionDisabled(disabled bool) {
	st.ms.SetPhaseInversionDisabled(disabled)
}

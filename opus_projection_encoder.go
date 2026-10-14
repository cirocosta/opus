// Ported from libopus 1.6.1 src/opus_projection_encoder.c.

package opus

import "github.com/cirocosta/opus/internal/dsp"

// ProjectionEncoder encodes ambisonics with the projection (mixing
// matrix) channel mapping family 3 (OpusProjectionEncoder). Create one
// with NewProjectionAmbisonicsEncoder. A ProjectionEncoder is not safe
// for concurrent use.
type ProjectionEncoder struct {
	mixingMatrixSizeInBytes   int32
	demixingMatrixSizeInBytes int32
	mixing                    mappingMatrix
	demixing                  mappingMatrix
	ms                        MSEncoder

	// pcm16 is scratch space for the 16-bit view of the Encode24 input
	// (see Encode24).
	pcm16 []int16
}

// projectionCopyChannelInFloat ports opus_projection_copy_channel_in_float.
func projectionCopyChannelInFloat(dst []float32, dstStride int, src analysisPCM, srcStride, srcChannel, frameSize int, matrix *mappingMatrix) {
	mappingMatrixMultiplyChannelInFloat(matrix, src.f32, srcStride, dst, srcChannel, dstStride, frameSize)
}

// projectionCopyChannelInShort ports opus_projection_copy_channel_in_short.
func projectionCopyChannelInShort(dst []float32, dstStride int, src analysisPCM, srcStride, srcChannel, frameSize int, matrix *mappingMatrix) {
	mappingMatrixMultiplyChannelInShort(matrix, src.i16, srcStride, dst, srcChannel, dstStride, frameSize)
}

// projectionCopyChannelInInt24 ports opus_projection_copy_channel_in_int24.
func projectionCopyChannelInInt24(dst []float32, dstStride int, src analysisPCM, srcStride, srcChannel, frameSize int, matrix *mappingMatrix) {
	mappingMatrixMultiplyChannelInInt24(matrix, src.i32, srcStride, dst, srcChannel, dstStride, frameSize)
}

// getOrderPlusOneFromChannels ports get_order_plus_one_from_channels.
func getOrderPlusOneFromChannels(channels int, orderPlusOne *int) error {
	// Allowed numbers of channels:
	// (1 + n)^2 + 2j, for n = 0...14 and j = 0 or 1.
	if channels < 1 || channels > 227 {
		return ErrBadArg
	}

	orderPlusOneV := int(dsp.Isqrt32(uint32(channels)))
	acnChannels := orderPlusOneV * orderPlusOneV
	nondiegeticChannels := channels - acnChannels
	if nondiegeticChannels != 0 && nondiegeticChannels != 2 {
		return ErrBadArg
	}

	if orderPlusOne != nil {
		*orderPlusOne = orderPlusOneV
	}
	return nil
}

// getStreamsFromChannels ports get_streams_from_channels.
func getStreamsFromChannels(channels, mappingFamily int, streams, coupledStreams, orderPlusOne *int) error {
	if mappingFamily == 3 {
		if getOrderPlusOneFromChannels(channels, orderPlusOne) != nil {
			return ErrBadArg
		}
		if streams != nil {
			*streams = (channels + 1) / 2
		}
		if coupledStreams != nil {
			*coupledStreams = channels / 2
		}
		return nil
	}
	return ErrBadArg
}

// projectionMatrices returns the mixing and demixing matrices and their
// data for an ambisonics order (plus one), or false.
func projectionMatrices(orderPlusOne int) (mix, demix *mappingMatrix, mixData, demixData []int16, ok bool) {
	switch orderPlusOne {
	case 2:
		return &mappingMatrixFoaMixing, &mappingMatrixFoaDemixing,
			mappingMatrixFoaMixingData[:], mappingMatrixFoaDemixingData[:], true
	case 3:
		return &mappingMatrixSoaMixing, &mappingMatrixSoaDemixing,
			mappingMatrixSoaMixingData[:], mappingMatrixSoaDemixingData[:], true
	case 4:
		return &mappingMatrixToaMixing, &mappingMatrixToaDemixing,
			mappingMatrixToaMixingData[:], mappingMatrixToaDemixingData[:], true
	case 5:
		return &mappingMatrixFourthoaMixing, &mappingMatrixFourthoaDemixing,
			mappingMatrixFourthoaMixingData[:], mappingMatrixFourthoaDemixingData[:], true
	case 6:
		return &mappingMatrixFifthoaMixing, &mappingMatrixFifthoaDemixing,
			mappingMatrixFifthoaMixingData[:], mappingMatrixFifthoaDemixingData[:], true
	}
	return nil, nil, nil, nil, false
}

// projectionAmbisonicsEncoderGetSize ports
// opus_projection_ambisonics_encoder_get_size. It returns 0 for invalid
// arguments.
func projectionAmbisonicsEncoderGetSize(channels, mappingFamily int) int32 {
	var nbStreams, nbCoupledStreams, orderPlusOne int
	if getStreamsFromChannels(channels, mappingFamily, &nbStreams, &nbCoupledStreams, &orderPlusOne) != nil {
		return 0
	}
	mix, demix, _, _, ok := projectionMatrices(orderPlusOne)
	if !ok {
		return 0
	}
	mixingMatrixSize := mappingMatrixGetSize(mix.rows, mix.cols)
	if mixingMatrixSize == 0 {
		return 0
	}
	demixingMatrixSize := mappingMatrixGetSize(demix.rows, demix.cols)
	if demixingMatrixSize == 0 {
		return 0
	}
	if nbStreams < 1 || nbCoupledStreams > nbStreams || nbCoupledStreams < 0 {
		// opus_multistream_encoder_get_size returns 0.
		return 0
	}
	return 1 // only checked against 0
}

// NewProjectionAmbisonicsEncoder allocates and initializes a projection
// encoder (opus_projection_ambisonics_encoder_create). mappingFamily must
// be 3 and channels (n+1)^2 or (n+1)^2+2 for ambisonics order n of 1 to
// 5. Streams and CoupledStreams return the stream layout, and
// DemixingMatrix the matrix that a decoder needs.
func NewProjectionAmbisonicsEncoder(fs, channels, mappingFamily, application int) (*ProjectionEncoder, error) {
	// Allocate space for the projection encoder.
	if projectionAmbisonicsEncoderGetSize(channels, mappingFamily) == 0 {
		return nil, ErrAllocFail
	}
	st := new(ProjectionEncoder)
	// Initialize projection encoder with provided settings.
	if err := st.InitAmbisonics(fs, channels, mappingFamily, application); err != nil {
		return nil, err
	}
	return st, nil
}

// InitAmbisonics initializes a projection encoder
// (opus_projection_ambisonics_encoder_init).
func (st *ProjectionEncoder) InitAmbisonics(fs, channels, mappingFamily, application int) error {
	var streams, coupledStreams, orderPlusOne int
	var mapping [255]byte

	if getStreamsFromChannels(channels, mappingFamily, &streams, &coupledStreams, &orderPlusOne) != nil {
		return ErrBadArg
	}

	if mappingFamily != 3 {
		return ErrUnimplemented
	}
	// Assign mixing matrix based on available pre-computed matrices.
	mix, demix, mixData, demixData, ok := projectionMatrices(orderPlusOne)
	if !ok {
		return ErrBadArg
	}
	mappingMatrixInit(&st.mixing, mix.rows, mix.cols, mix.gain, mixData)
	st.mixingMatrixSizeInBytes = mappingMatrixGetSize(st.mixing.rows, st.mixing.cols)
	if st.mixingMatrixSizeInBytes == 0 {
		return ErrBadArg
	}

	// Assign demixing matrix based on available pre-computed matrices.
	mappingMatrixInit(&st.demixing, demix.rows, demix.cols, demix.gain, demixData)
	st.demixingMatrixSizeInBytes = mappingMatrixGetSize(st.demixing.rows, st.demixing.cols)
	if st.demixingMatrixSizeInBytes == 0 {
		return ErrBadArg
	}

	// Ensure matrices are large enough for desired coding scheme.
	if streams+coupledStreams > st.mixing.rows ||
		channels > st.mixing.cols ||
		channels > st.demixing.rows ||
		streams+coupledStreams > st.demixing.cols {
		return ErrBadArg
	}

	// Set trivial mapping so each input channel pairs with a matrix
	// column.
	for i := 0; i < channels; i++ {
		mapping[i] = byte(i)
	}

	// Initialize multistream encoder with provided settings.
	return st.ms.Init(fs, channels, streams, coupledStreams, mapping[:channels], application)
}

// Encode encodes a frame of 16-bit PCM (opus_projection_encode). The
// arguments and the result are as for MSEncoder.Encode.
func (st *ProjectionEncoder) Encode(pcm []int16, frameSize int, data []byte) (int, error) {
	if !st.ms.checkPCM(len(pcm), frameSize) {
		return 0, ErrBadArg
	}
	return encodeResult(st.ms.encodeNative(projectionCopyChannelInShort, analysisPCM{i16: pcm},
		frameSize, data, 16, downmixInt, false, &st.mixing))
}

// Encode24 encodes a frame of 24-bit PCM held in int32 samples
// (opus_projection_encode24).
//
// libopus passes the int32 input to the tonality analysis with the
// 16-bit downmix function (downmix_int), so the analysis reads the
// buffer as int16 values. This port reproduces that on a little-endian
// view of the input.
func (st *ProjectionEncoder) Encode24(pcm []int32, frameSize int, data []byte) (int, error) {
	if !st.ms.checkPCM(len(pcm), frameSize) {
		return 0, ErrBadArg
	}
	n := 2 * len(pcm)
	if cap(st.pcm16) < n {
		st.pcm16 = make([]int16, n)
	}
	v := st.pcm16[:n]
	for i, s := range pcm {
		v[2*i] = int16(uint32(s))
		v[2*i+1] = int16(uint32(s) >> 16)
	}
	return encodeResult(st.ms.encodeNative(projectionCopyChannelInInt24, analysisPCM{i16: v, i32: pcm},
		frameSize, data, 24, downmixInt, false, &st.mixing))
}

// EncodeFloat encodes a frame of float PCM (opus_projection_encode_float).
func (st *ProjectionEncoder) EncodeFloat(pcm []float32, frameSize int, data []byte) (int, error) {
	if !st.ms.checkPCM(len(pcm), frameSize) {
		return 0, ErrBadArg
	}
	return encodeResult(st.ms.encodeNative(projectionCopyChannelInFloat, analysisPCM{f32: pcm},
		frameSize, data, 24, downmixFloat, true, &st.mixing))
}

// Streams returns the number of streams.
func (st *ProjectionEncoder) Streams() int { return st.ms.Streams() }

// CoupledStreams returns the number of coupled streams.
func (st *ProjectionEncoder) CoupledStreams() int { return st.ms.CoupledStreams() }

// Channels returns the number of input channels.
func (st *ProjectionEncoder) Channels() int { return st.ms.Channels() }

// DemixingMatrixSize returns the size in bytes of the demixing matrix
// (OPUS_PROJECTION_GET_DEMIXING_MATRIX_SIZE).
func (st *ProjectionEncoder) DemixingMatrixSize() int {
	return st.ms.layout.nbChannels * (st.ms.layout.nbStreams + st.ms.layout.nbCoupledStreams) * 2
}

// DemixingMatrixGain returns the gain of the demixing matrix in S7.8 dB
// (OPUS_PROJECTION_GET_DEMIXING_MATRIX_GAIN).
func (st *ProjectionEncoder) DemixingMatrixGain() int { return st.demixing.gain }

// DemixingMatrix writes the demixing matrix to dst as little-endian int16
// values in column-major order, the format of the Ogg Opus header
// (OPUS_PROJECTION_GET_DEMIXING_MATRIX). len(dst) must be
// DemixingMatrixSize.
func (st *ProjectionEncoder) DemixingMatrix(dst []byte) error {
	// (I/O is in relation to the decoder's perspective).
	nbInputStreams := st.ms.layout.nbStreams + st.ms.layout.nbCoupledStreams
	nbOutputStreams := st.ms.layout.nbChannels

	internalShort := mappingMatrixGetData(&st.demixing)
	internalSize := nbInputStreams * nbOutputStreams * 2
	if dst == nil || len(dst) != internalSize {
		return ErrBadArg
	}

	// Copy demixing matrix subset to output destination.
	l := 0
	for i := 0; i < nbInputStreams; i++ {
		for j := 0; j < nbOutputStreams; j++ {
			k := st.demixing.rows*i + j
			dst[2*l] = byte(internalShort[k])
			dst[2*l+1] = byte(internalShort[k] >> 8)
			l++
		}
	}
	return nil
}

// MSEncoder returns the underlying multistream encoder, for the controls
// that the projection encoder forwards to it (such as SetBitrate,
// SetComplexity and FinalRange).
func (st *ProjectionEncoder) MSEncoder() *MSEncoder { return &st.ms }

// SetBitrate sets the total bitrate (OPUS_SET_BITRATE).
func (st *ProjectionEncoder) SetBitrate(bitrate int) error { return st.ms.SetBitrate(bitrate) }

// Bitrate returns the total bitrate (OPUS_GET_BITRATE).
func (st *ProjectionEncoder) Bitrate() int { return st.ms.Bitrate() }

// SetComplexity sets the complexity of all streams (OPUS_SET_COMPLEXITY).
func (st *ProjectionEncoder) SetComplexity(complexity int) error {
	return st.ms.SetComplexity(complexity)
}

// SetVBR enables or disables VBR (OPUS_SET_VBR).
func (st *ProjectionEncoder) SetVBR(enabled bool) { st.ms.SetVBR(enabled) }

// FinalRange returns the XOR of the final range coder states of the
// streams (OPUS_GET_FINAL_RANGE).
func (st *ProjectionEncoder) FinalRange() uint32 { return st.ms.FinalRange() }

// Lookahead returns the look-ahead in samples (OPUS_GET_LOOKAHEAD).
func (st *ProjectionEncoder) Lookahead() int { return st.ms.Lookahead() }

// SampleRate returns the sampling rate (OPUS_GET_SAMPLE_RATE).
func (st *ProjectionEncoder) SampleRate() int { return st.ms.SampleRate() }

// Reset resets the encoder state (OPUS_RESET_STATE).
func (st *ProjectionEncoder) Reset() { st.ms.Reset() }

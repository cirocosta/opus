// Ported from libopus 1.6.1 src/opus_multistream_decoder.c.

package opus

// MSDecoder is a multistream Opus decoder (OpusMSDecoder): it decodes
// packets that hold one Opus stream per coupled (stereo) or uncoupled
// (mono) stream and maps the decoded channels to the output channels.
// Create one with NewMSDecoder. An MSDecoder is not safe for concurrent
// use.
type MSDecoder struct {
	layout   channelLayout
	decoders []Decoder

	buf  [2 * maxFrameSamples48k]float32
	size [48]int16
}

// NewMSDecoder allocates and initializes a multistream decoder
// (opus_multistream_decoder_create). channels is the number of output
// channels (1 to 255), streams the number of streams, coupledStreams the
// number of them that are stereo, and mapping (channels entries) maps
// each output channel to a decoded channel: values below
// 2*coupledStreams are the left and right channels of the coupled
// streams, the following values are the mono streams, and 255 is a
// silent channel.
func NewMSDecoder(fs, channels, streams, coupledStreams int, mapping []byte) (*MSDecoder, error) {
	if !validStreamCounts(channels, streams, coupledStreams) {
		return nil, ErrBadArg
	}
	st := new(MSDecoder)
	if err := st.Init(fs, channels, streams, coupledStreams, mapping); err != nil {
		return nil, err
	}
	return st, nil
}

// Init initializes a multistream decoder
// (opus_multistream_decoder_init).
func (st *MSDecoder) Init(fs, channels, streams, coupledStreams int, mapping []byte) error {
	if !validStreamCounts(channels, streams, coupledStreams) {
		return ErrBadArg
	}
	if len(mapping) < channels {
		return ErrBadArg
	}

	st.layout.nbChannels = channels
	st.layout.nbStreams = streams
	st.layout.nbCoupledStreams = coupledStreams

	copy(st.layout.mapping[:channels], mapping[:channels])
	if !validateLayout(&st.layout) {
		return ErrBadArg
	}

	if cap(st.decoders) < streams {
		st.decoders = make([]Decoder, streams)
	}
	st.decoders = st.decoders[:streams]
	i := 0
	for ; i < st.layout.nbCoupledStreams; i++ {
		if err := st.decoders[i].Init(fs, 2); err != nil {
			return err
		}
	}
	for ; i < st.layout.nbStreams; i++ {
		if err := st.decoders[i].Init(fs, 1); err != nil {
			return err
		}
	}
	return nil
}

// msPacketValidate ports opus_multistream_packet_validate.
func (st *MSDecoder) msPacketValidate(data []byte, nbStreams int, fs int32) int {
	var toc byte
	samples := 0
	for s := 0; s < nbStreams; s++ {
		var packetOffset int32
		if len(data) <= 0 {
			return int(ErrInvalidPacket)
		}
		count := opusPacketParseImpl(data, s != nbStreams-1, &toc, nil, st.size[:],
			nil, &packetOffset, nil)
		if count < 0 {
			return count
		}
		tmpSamples := opusPacketGetNbSamples(data[:packetOffset], fs)
		if s != 0 && samples != tmpSamples {
			return int(ErrInvalidPacket)
		}
		samples = tmpSamples
		data = data[packetOffset:]
	}
	return samples
}

// msOutput is the destination of opus_multistream_decode_native: the
// pcm pointer, the copy_channel_out function (by kind) and its
// user_data (the demixing matrix of a projection decoder).
type msOutput struct {
	kind   int // outInt16, outInt24 or outFloat
	i16    []int16
	f32    []float32
	i24    []int32
	matrix *mappingMatrix
}

// Output kinds of msOutput.
const (
	outFloat = iota
	outInt16
	outInt24
)

// copyChannelOut ports opus_copy_channel_out_short/_float/_int24 and the
// opus_projection_copy_channel_out_* functions. src is nil for a muted
// channel.
// keep packet offsets and channel-output transitions in reference order
//
//revive:disable-next-line:cognitive-complexity
func (o *msOutput) copyChannelOut(
	dstStride, dstChannel int,
	src []float32,
	srcStride, frameSize int,
) {
	if o.matrix != nil {
		switch o.kind {
		case outInt16:
			if dstChannel == 0 {
				clear(o.i16[:frameSize*dstStride])
			}
			if src != nil {
				mappingMatrixMultiplyChannelOutShort(o.matrix, src, dstChannel, srcStride,
					o.i16, dstStride, frameSize)
			}
		case outInt24:
			if dstChannel == 0 {
				clear(o.i24[:frameSize*dstStride])
			}
			if src != nil {
				mappingMatrixMultiplyChannelOutInt24(o.matrix, src, dstChannel, srcStride,
					o.i24, dstStride, frameSize)
			}
		default:
			if dstChannel == 0 {
				clear(o.f32[:frameSize*dstStride])
			}
			if src != nil {
				mappingMatrixMultiplyChannelOutFloat(o.matrix, src, dstChannel, srcStride,
					o.f32, dstStride, frameSize)
			}
		}
		return
	}
	switch o.kind {
	case outInt16:
		dst := o.i16
		if src != nil {
			for i := 0; i < frameSize; i++ {
				dst[i*dstStride+dstChannel] = res2int16(src[i*srcStride])
			}
		} else {
			for i := 0; i < frameSize; i++ {
				dst[i*dstStride+dstChannel] = 0
			}
		}
	case outInt24:
		dst := o.i24
		if src != nil {
			for i := 0; i < frameSize; i++ {
				dst[i*dstStride+dstChannel] = res2int24(src[i*srcStride])
			}
		} else {
			for i := 0; i < frameSize; i++ {
				dst[i*dstStride+dstChannel] = 0
			}
		}
	default:
		dst := o.f32
		if src != nil {
			for i := 0; i < frameSize; i++ {
				dst[i*dstStride+dstChannel] = src[i*srcStride]
			}
		} else {
			for i := 0; i < frameSize; i++ {
				dst[i*dstStride+dstChannel] = 0
			}
		}
	}
}

// decodeNative ports opus_multistream_decode_native.
// keep packet offsets and channel-output transitions in reference order
//
//revive:disable-next-line:cognitive-complexity
func (st *MSDecoder) decodeNative(
	data []byte,
	out *msOutput,
	frameSize int,
	decodeFEC, softClip bool,
) int {
	doPLC := false
	if frameSize <= 0 {
		return int(ErrBadArg)
	}
	// Limit frame_size to avoid excessive stack allocations.
	fs := st.decoders[0].fs
	frameSize = min(frameSize, int(fs/25*3))
	buf := st.buf[:2*frameSize]
	length := len(data)

	if length == 0 {
		doPLC = true
	}
	if !doPLC && length < 2*st.layout.nbStreams-1 {
		return int(ErrInvalidPacket)
	}
	if !doPLC {
		ret := st.msPacketValidate(data, st.layout.nbStreams, fs)
		if ret < 0 {
			return ret
		} else if ret > frameSize {
			return int(ErrBufferTooSmall)
		}
	}
	for s := 0; s < st.layout.nbStreams; s++ {
		dec := &st.decoders[s]
		if !doPLC && length <= 0 {
			return int(ErrInternalError)
		}
		var packetOffset int32
		ret := dec.decodeNative(data, buf, frameSize, decodeFEC, s != st.layout.nbStreams-1,
			&packetOffset, softClip)
		if !doPLC {
			data = data[packetOffset:]
			length -= int(packetOffset)
		}
		if ret <= 0 {
			return ret
		}
		frameSize = ret
		if s < st.layout.nbCoupledStreams {
			// Copy "left" audio to the channel(s) where it belongs.
			prev := -1
			for {
				channel := getLeftChannel(&st.layout, s, prev)
				if channel == -1 {
					break
				}
				out.copyChannelOut(st.layout.nbChannels, channel, buf, 2, frameSize)
				prev = channel
			}
			// Copy "right" audio to the channel(s) where it belongs.
			prev = -1
			for {
				channel := getRightChannel(&st.layout, s, prev)
				if channel == -1 {
					break
				}
				out.copyChannelOut(st.layout.nbChannels, channel, buf[1:], 2, frameSize)
				prev = channel
			}
		} else {
			// Copy audio to the channel(s) where it belongs.
			prev := -1
			for {
				channel := getMonoChannel(&st.layout, s, prev)
				if channel == -1 {
					break
				}
				out.copyChannelOut(st.layout.nbChannels, channel, buf, 1, frameSize)
				prev = channel
			}
		}
	}
	// Handle muted channels.
	for c := 0; c < st.layout.nbChannels; c++ {
		if st.layout.mapping[c] == 255 {
			out.copyChannelOut(st.layout.nbChannels, c, nil, 0, frameSize)
		}
	}
	return frameSize
}

// checkPCM reports whether a buffer of n samples holds frameSize
// samples per output channel, after the 120 ms limit of
// opus_multistream_decode_native.
func (st *MSDecoder) checkPCM(n, frameSize int) bool {
	if frameSize <= 0 {
		return true // decodeNative reports OPUS_BAD_ARG
	}
	frameSize = min(frameSize, int(st.decoders[0].fs/25*3))
	return n >= frameSize*st.layout.nbChannels
}

// Decode decodes a multistream packet to 16-bit PCM
// (opus_multistream_decode). The arguments are as for Decoder.Decode,
// with pcm interleaved over all output channels. The output is
// soft-clipped.
func (st *MSDecoder) Decode(data []byte, pcm []int16, frameSize int, decodeFEC bool) (int, error) {
	if !st.checkPCM(len(pcm), frameSize) {
		return 0, ErrBufferTooSmall
	}
	out := msOutput{kind: outInt16, i16: pcm}
	return decodeResult(st.decodeNative(data, &out, frameSize, decodeFEC, true))
}

// Decode24 decodes a multistream packet to 24-bit PCM in int32 samples
// (opus_multistream_decode24).
func (st *MSDecoder) Decode24(
	data []byte,
	pcm []int32,
	frameSize int,
	decodeFEC bool,
) (int, error) {
	if !st.checkPCM(len(pcm), frameSize) {
		return 0, ErrBufferTooSmall
	}
	out := msOutput{kind: outInt24, i24: pcm}
	return decodeResult(st.decodeNative(data, &out, frameSize, decodeFEC, false))
}

// DecodeFloat decodes a multistream packet to float PCM
// (opus_multistream_decode_float).
func (st *MSDecoder) DecodeFloat(
	data []byte,
	pcm []float32,
	frameSize int,
	decodeFEC bool,
) (int, error) {
	if !st.checkPCM(len(pcm), frameSize) {
		return 0, ErrBufferTooSmall
	}
	out := msOutput{kind: outFloat, f32: pcm}
	return decodeResult(st.decodeNative(data, &out, frameSize, decodeFEC, false))
}

// Channels returns the number of output channels.
func (st *MSDecoder) Channels() int { return st.layout.nbChannels }

// Streams returns the number of streams.
func (st *MSDecoder) Streams() int { return st.layout.nbStreams }

// CoupledStreams returns the number of coupled (stereo) streams.
func (st *MSDecoder) CoupledStreams() int { return st.layout.nbCoupledStreams }

// Bandwidth returns the bandwidth of the last packet of the first stream
// (OPUS_GET_BANDWIDTH).
func (st *MSDecoder) Bandwidth() int { return st.decoders[0].Bandwidth() }

// SampleRate returns the sampling rate (OPUS_GET_SAMPLE_RATE).
func (st *MSDecoder) SampleRate() int { return st.decoders[0].SampleRate() }

// Gain returns the output gain of the first stream (OPUS_GET_GAIN).
func (st *MSDecoder) Gain() int { return st.decoders[0].Gain() }

// LastPacketDuration returns the duration of the last packet of the
// first stream (OPUS_GET_LAST_PACKET_DURATION).
func (st *MSDecoder) LastPacketDuration() int { return st.decoders[0].LastPacketDuration() }

// PhaseInversionDisabled reports whether the first stream has phase
// inversion disabled (OPUS_GET_PHASE_INVERSION_DISABLED).
func (st *MSDecoder) PhaseInversionDisabled() bool {
	return st.decoders[0].PhaseInversionDisabled()
}

// Complexity returns the complexity of the first stream
// (OPUS_GET_COMPLEXITY).
func (st *MSDecoder) Complexity() int { return st.decoders[0].Complexity() }

// FinalRange returns the XOR of the final range of all streams
// (OPUS_GET_FINAL_RANGE).
func (st *MSDecoder) FinalRange() uint32 {
	var value uint32
	for s := range st.decoders {
		value ^= st.decoders[s].FinalRange()
	}
	return value
}

// Reset resets all streams (OPUS_RESET_STATE).
func (st *MSDecoder) Reset() {
	for s := range st.decoders {
		st.decoders[s].Reset()
	}
}

// DecoderState returns the decoder of stream streamID
// (OPUS_MULTISTREAM_GET_DECODER_STATE).
func (st *MSDecoder) DecoderState(streamID int) (*Decoder, error) {
	if streamID < 0 || streamID >= st.layout.nbStreams {
		return nil, ErrBadArg
	}
	return &st.decoders[streamID], nil
}

// SetGain sets the output gain of all streams (OPUS_SET_GAIN).
func (st *MSDecoder) SetGain(gain int) error {
	for s := range st.decoders {
		if err := st.decoders[s].SetGain(gain); err != nil {
			return err
		}
	}
	return nil
}

// SetComplexity sets the complexity of all streams
// (OPUS_SET_COMPLEXITY).
func (st *MSDecoder) SetComplexity(complexity int) error {
	for s := range st.decoders {
		if err := st.decoders[s].SetComplexity(complexity); err != nil {
			return err
		}
	}
	return nil
}

// SetPhaseInversionDisabled sets the phase inversion control of all
// streams (OPUS_SET_PHASE_INVERSION_DISABLED).
func (st *MSDecoder) SetPhaseInversionDisabled(disabled bool) {
	for s := range st.decoders {
		st.decoders[s].SetPhaseInversionDisabled(disabled)
	}
}

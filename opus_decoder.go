// Ported from libopus 1.6.1 src/opus_decoder.c (OpusDecoder,
// opus_decoder_init, opus_decode_frame, opus_decode_native, opus_decode,
// opus_decode24, opus_decode_float, opus_decoder_ctl and
// opus_decoder_get_nb_samples).

package opus

import (
	"github.com/cirocosta/opus/internal/celt"
	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
	"github.com/cirocosta/opus/internal/silk"
)

// maxFrameSamples48k is the longest packet, 120 ms at 48 kHz.
const maxFrameSamples48k = 5760

// Decoder is an Opus decoder (OpusDecoder). Create one with NewDecoder.
// A Decoder is not safe for concurrent use.
type Decoder struct {
	channels         int
	fs               int32 // sampling rate at the API level
	decControl       silk.DecControl
	decodeGain       int
	complexity       int
	ignoreExtensions bool

	silkDec silk.Decoder
	celtDec celt.Decoder

	// Everything in decoderResetState gets cleared on a reset.
	decoderResetState

	// Scratch buffers for the C stack allocations.
	dec           entcode.Decoder
	size          [48]int16
	pcmTransition [2 * 240]float32 // F5*channels at 48 kHz
	redundantAud  [2 * 240]float32 // F5*channels at 48 kHz
	pcmSilk       [2 * 480]float32 // F10*channels at 48 kHz
	out           []float32        // opus_decode/opus_decode24 output
}

// decoderResetState holds the fields of OpusDecoder from
// OPUS_DECODER_RESET_START (stream_channels) to the end.
type decoderResetState struct {
	streamChannels     int
	bandwidth          int
	mode               int
	prevMode           int
	frameSize          int
	prevRedundancy     bool
	lastPacketDuration int
	softclipMem        [2]float32
	rangeFinal         uint32
}

// validDecoderRate reports whether fs is a sampling rate that the decoder
// supports.
func validDecoderRate(fs int) bool {
	return fs == 48000 || fs == 24000 || fs == 16000 || fs == 12000 || fs == 8000
}

// NewDecoder allocates and initializes a decoder state (opus_decoder_create).
// fs is the sampling rate to decode to (8000, 12000, 16000, 24000, or 48000
// Hz) and channels the number of channels (1 or 2) to decode.
func NewDecoder(fs, channels int) (*Decoder, error) {
	if !validDecoderRate(fs) || (channels != 1 && channels != 2) {
		return nil, ErrBadArg
	}
	st := new(Decoder)
	if err := st.Init(fs, channels); err != nil {
		return nil, err
	}
	return st, nil
}

// Init initializes (or reinitializes) a decoder state (opus_decoder_init).
func (st *Decoder) Init(fs, channels int) error {
	if !validDecoderRate(fs) || (channels != 1 && channels != 2) {
		return ErrBadArg
	}
	out := st.out
	// OPUS_CLEAR of the whole state.
	st.channels = 0
	st.decControl = silk.DecControl{}
	st.decodeGain = 0
	st.ignoreExtensions = false
	st.decoderResetState = decoderResetState{}

	st.streamChannels = channels
	st.channels = channels
	st.complexity = 0

	st.fs = int32(fs)
	st.decControl.APISampleRate = st.fs
	st.decControl.NChannelsAPI = int32(st.channels)

	// Reset decoder.
	if ret := st.silkDec.Init(); ret != silk.NoError {
		return ErrInternalError
	}

	// Initialize CELT decoder.
	if ret := st.celtDec.Init(st.fs, channels); ret != celt.OK {
		return ErrInternalError
	}
	st.celtDec.SetSignalling(0)

	st.prevMode = 0
	st.frameSize = fs / 400
	if cap(out) < maxFrameSamples48k*2 {
		out = make([]float32, maxFrameSamples48k*2)
	}
	st.out = out
	return nil
}

// smoothFade ports smooth_fade (ENABLE_RES24 variant, which is the one the
// float build compiles). out may alias in1 or in2.
func smoothFade(in1, in2, out []float32, overlap, channels int, window []float32, fs int32) {
	inc := int(48000 / fs)
	for c := 0; c < channels; c++ {
		for i := 0; i < overlap; i++ {
			w := float32(window[i*inc] * window[i*inc])
			out[i*channels+c] = float32(w*in2[i*channels+c]) +
				float32((1-w)*in1[i*channels+c])
		}
	}
}

// silencePacket is the 2-byte CELT silence frame that opus_decode_frame
// decodes for hybrid to SILK transitions.
var silencePacket = []byte{0xFF, 0xFF}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// decodeFrame ports opus_decode_frame. data holds at least length bytes
// (more may follow, as with the C pointer). It returns the number of
// samples per channel written to pcm or a negative error code.
// keep reference loss, FEC and mode-transition state updates in decode order
//
//revive:disable-next-line:cognitive-complexity
func (st *Decoder) decodeFrame(
	data []byte,
	length int,
	pcm []float32,
	frameSize int,
	decodeFEC bool,
) int {
	silkDec := &st.silkDec
	celtDec := &st.celtDec
	ch := st.channels
	var (
		audiosize       int
		mode            int
		bandwidth       int
		transition      bool
		redundancy      bool
		redundancyBytes int
		celtToSilk      bool
		redundantRng    uint32
		celtRet         int
		pcmTransition   []float32
	)

	F20 := int(st.fs / 50)
	F10 := F20 >> 1
	F5 := F10 >> 1
	F2_5 := F5 >> 1
	if frameSize < F2_5 {
		return int(ErrBufferTooSmall)
	}
	// Limit frame_size to avoid excessive stack allocations.
	frameSize = min(frameSize, int(st.fs/25*3))
	// Payloads of 1 (2 including ToC) or 0 trigger the PLC/DTX.
	if length <= 1 {
		data = nil
		// In that case, don't conceal more than what the ToC says.
		frameSize = min(frameSize, st.frameSize)
	}
	if data != nil {
		audiosize = st.frameSize
		mode = st.mode
		bandwidth = st.bandwidth
		st.dec.Init(data[:length])
	} else {
		audiosize = frameSize
		// Run PLC using last used mode (CELT if we ended with CELT
		// redundancy).
		if st.prevRedundancy {
			mode = modeCeltOnly
		} else {
			mode = st.prevMode
		}
		bandwidth = 0

		if mode == 0 {
			// If we haven't got any packet yet, all we can do is
			// return zeros.
			clear(pcm[:audiosize*ch])
			return audiosize
		}

		// Avoids trying to run the PLC on sizes other than 2.5 (CELT),
		// 5 (CELT), 10, or 20 (e.g. 12.5 or 30 ms).
		if audiosize > F20 {
			for {
				ret := st.decodeFrame(nil, 0, pcm, min(audiosize, F20), false)
				if ret < 0 {
					return ret
				}
				pcm = pcm[ret*ch:]
				audiosize -= ret
				if audiosize <= 0 {
					break
				}
			}
			return frameSize
		} else if audiosize < F20 {
			if audiosize > F10 {
				audiosize = F10
			} else if mode != modeSilkOnly && audiosize > F5 && audiosize < F10 {
				audiosize = F5
			}
		}
	}

	// In fixed-point, we can tell CELT to do the accumulation on top of
	// the SILK PCM buffer. This saves some stack space.
	celtAccum := mode != modeCeltOnly

	if data != nil && st.prevMode > 0 &&
		((mode == modeCeltOnly && st.prevMode != modeCeltOnly && !st.prevRedundancy) ||
			(mode != modeCeltOnly && st.prevMode == modeCeltOnly)) {
		transition = true
	}
	if transition && mode == modeCeltOnly {
		pcmTransition = st.pcmTransition[:F5*ch]
		st.decodeFrame(nil, 0, pcmTransition, min(F5, audiosize), false)
	}
	if audiosize > frameSize {
		return int(ErrBadArg)
	}
	frameSize = audiosize

	// SILK processing.
	if mode != modeCeltOnly {
		pcmTooSmall := frameSize < F10
		pcmPtr := pcm
		if pcmTooSmall {
			pcmPtr = st.pcmSilk[:F10*ch]
		}

		if st.prevMode == modeCeltOnly {
			silkDec.Reset()
		}

		// The SILK PLC cannot produce frames of less than 10 ms.
		st.decControl.PayloadSizeMs = max(10, 1000*audiosize/int(st.fs))

		if data != nil {
			st.decControl.NChannelsInternal = int32(st.streamChannels)
			if mode == modeSilkOnly {
				switch bandwidth {
				case BandwidthNarrowband:
					st.decControl.InternalSampleRate = 8000
				case BandwidthMediumband:
					st.decControl.InternalSampleRate = 12000
				case BandwidthWideband:
					st.decControl.InternalSampleRate = 16000
				default:
					st.decControl.InternalSampleRate = 16000
				}
			} else {
				// Hybrid mode.
				st.decControl.InternalSampleRate = 16000
			}
		}
		lostFlag := silk.FlagPacketLost
		if data != nil {
			lostFlag = 2 * b2i(decodeFEC)
		}
		decodedSamples := 0
		for {
			// Call SILK decoder.
			firstFrame := decodedSamples == 0
			n, silkRet := silkDec.Decode(&st.decControl, lostFlag, firstFrame, &st.dec, pcmPtr)
			silkFrameSize := int(n)
			if silkRet != silk.NoError {
				if lostFlag != 0 {
					// PLC failure should not be fatal.
					silkFrameSize = frameSize
					clear(pcmPtr[:frameSize*ch])
				} else {
					return int(ErrInternalError)
				}
			}
			pcmPtr = pcmPtr[min(silkFrameSize*ch, len(pcmPtr)):]
			decodedSamples += silkFrameSize
			if decodedSamples >= frameSize {
				break
			}
		}
		if pcmTooSmall {
			copy(pcm[:frameSize*ch], st.pcmSilk[:frameSize*ch])
		}
	}

	startBand := 0
	if !decodeFEC && mode != modeCeltOnly && data != nil &&
		st.dec.Tell()+17+20*b2i(mode == modeHybrid) <= 8*length {
		// Check if we have a redundant 0-8 kHz band.
		if mode == modeHybrid {
			redundancy = st.dec.DecBitLogp(12) != 0
		} else {
			redundancy = true
		}
		if redundancy {
			celtToSilk = st.dec.DecBitLogp(1) != 0
			// redundancy_bytes will be at least two, in the non-hybrid
			// case due to the ec_tell() check above.
			if mode == modeHybrid {
				redundancyBytes = int(st.dec.DecUint(256)) + 2
			} else {
				redundancyBytes = length - ((st.dec.Tell() + 7) >> 3)
			}
			length -= redundancyBytes
			// This is a sanity check. It should never happen for a
			// valid packet, so the exact behaviour is not normative.
			if length*8 < st.dec.Tell() {
				length = 0
				redundancyBytes = 0
				redundancy = false
			}
			// Shrink decoder because of raw bits.
			st.dec.Storage -= uint32(redundancyBytes)
		}
	}
	if mode != modeCeltOnly {
		startBand = 17
	}

	if redundancy {
		transition = false
	}

	if transition && mode != modeCeltOnly {
		pcmTransition = st.pcmTransition[:F5*ch]
		st.decodeFrame(nil, 0, pcmTransition, min(F5, audiosize), false)
	}

	if bandwidth != 0 {
		endband := 21
		switch bandwidth {
		case BandwidthNarrowband:
			endband = 13
		case BandwidthMediumband, BandwidthWideband:
			endband = 17
		case BandwidthSuperwideband:
			endband = 19
		case BandwidthFullband:
			endband = 21
		}
		celtDec.SetEndBand(endband)
	}
	celtDec.SetChannels(st.streamChannels)

	// Only allocation memory for redundancy if/when needed.
	var redundantAudio []float32
	if redundancy {
		redundantAudio = st.redundantAud[:F5*ch]
	}

	// 5 ms redundant frame for CELT->SILK.
	if redundancy && celtToSilk {
		// If the previous frame did not use CELT (the first redundancy
		// frame in a transition from SILK may have been lost) then the
		// CELT decoder is stale at this point and the redundancy audio
		// is not useful, however the final range is still needed (for
		// testing), so the redundancy is always decoded but the decoded
		// audio may not be used.
		celtDec.SetStartBand(0)
		celtDec.DecodeWithEC(data[length:length+redundancyBytes], redundantAudio, F5, nil, false)
		redundantRng = celtDec.FinalRange()
	}

	// MUST be after PLC.
	celtDec.SetStartBand(startBand)

	if mode != modeSilkOnly {
		celtFrameSize := min(F20, frameSize)
		// Make sure to discard any previous CELT state.
		if mode != st.prevMode && st.prevMode > 0 && !st.prevRedundancy {
			celtDec.Reset()
		}
		// Decode CELT.
		var celtData []byte
		if !decodeFEC && data != nil {
			celtData = data[:length]
		}
		celtRet = celtDec.DecodeWithEC(celtData, pcm, celtFrameSize, &st.dec, celtAccum)
		st.rangeFinal = celtDec.FinalRange()
	} else {
		if !celtAccum {
			clear(pcm[:frameSize*ch])
		}
		// For hybrid -> SILK transitions, we let the CELT MDCT do a
		// fade-out by decoding a silence frame.
		if st.prevMode == modeHybrid && (!redundancy || !celtToSilk || !st.prevRedundancy) {
			celtDec.SetStartBand(0)
			celtDec.DecodeWithEC(silencePacket, pcm, F2_5, nil, celtAccum)
		}
		st.rangeFinal = st.dec.Rng
	}

	window := celtDec.Mode().Window

	// 5 ms redundant frame for SILK->CELT.
	if redundancy && !celtToSilk {
		celtDec.Reset()
		celtDec.SetStartBand(0)

		celtDec.DecodeWithEC(data[length:length+redundancyBytes], redundantAudio, F5, nil, false)
		redundantRng = celtDec.FinalRange()
		smoothFade(pcm[ch*(frameSize-F2_5):], redundantAudio[ch*F2_5:],
			pcm[ch*(frameSize-F2_5):], F2_5, ch, window, st.fs)
	}
	// 5ms redundant frame for CELT->SILK; ignore if the previous frame
	// did not use CELT (the first redundancy frame in a transition from
	// SILK may have been lost).
	if redundancy && celtToSilk && (st.prevMode != modeSilkOnly || st.prevRedundancy) {
		for c := 0; c < ch; c++ {
			for i := 0; i < F2_5; i++ {
				pcm[ch*i+c] = redundantAudio[ch*i+c]
			}
		}
		smoothFade(redundantAudio[ch*F2_5:], pcm[ch*F2_5:], pcm[ch*F2_5:], F2_5, ch, window, st.fs)
	}
	if transition {
		if audiosize >= F5 {
			copy(pcm[:ch*F2_5], pcmTransition[:ch*F2_5])
			smoothFade(
				pcmTransition[ch*F2_5:],
				pcm[ch*F2_5:],
				pcm[ch*F2_5:],
				F2_5,
				ch,
				window,
				st.fs,
			)
		} else {
			// Not enough time to do a clean transition, but we do it
			// anyway. This will not preserve amplitude perfectly and may
			// introduce a bit of temporal aliasing, but it shouldn't be
			// too bad and that's pretty much the best we can do. In any
			// case, generating this transition it pretty silly in the
			// first place.
			smoothFade(pcmTransition, pcm, pcm, F2_5, ch, window, st.fs)
		}
	}

	if st.decodeGain != 0 {
		gain := dsp.CeltExp2(float32(6.48814081e-4) * float32(st.decodeGain))
		for i := 0; i < frameSize*ch; i++ {
			// MULT32_32_Q16 and SATURATE are a plain product in float.
			pcm[i] = float32(pcm[i] * gain)
		}
	}

	if length <= 1 {
		st.rangeFinal = 0
	} else {
		st.rangeFinal ^= redundantRng
	}

	st.prevMode = mode
	st.prevRedundancy = redundancy && !celtToSilk

	if celtRet < 0 {
		return celtRet
	}
	return audiosize
}

// decodeNative ports opus_decode_native. data is nil or empty for a lost
// packet. packetOffset, if not nil, receives the number of bytes of data
// used (for self-delimited multistream packets).
// keep reference loss, FEC and mode-transition state updates in decode order
//
//revive:disable-next-line:cognitive-complexity
func (st *Decoder) decodeNative(data []byte, pcm []float32, frameSize int, decodeFEC bool,
	selfDelimited bool, packetOffset *int32, softClip bool,
) int {
	ch := st.channels
	// For FEC/PLC, frame_size has to be to have a multiple of 2.5 ms.
	if (decodeFEC || len(data) == 0) && frameSize%int(st.fs/400) != 0 {
		return int(ErrBadArg)
	}
	if len(data) == 0 {
		pcmCount := 0
		for {
			ret := st.decodeFrame(nil, 0, pcm[pcmCount*ch:], frameSize-pcmCount, false)
			if ret < 0 {
				return ret
			}
			pcmCount += ret
			if pcmCount >= frameSize {
				break
			}
		}
		st.lastPacketDuration = pcmCount
		return pcmCount
	}

	packetMode := opusPacketGetMode(data[0])
	packetBandwidth := opusPacketGetBandwidth(data[0])
	packetFrameSize := int(opusPacketGetSamplesPerFrame(data[0], st.fs))
	packetStreamChannels := opusPacketGetNbChannels(data[0])

	var toc byte
	var offset int
	var padding []byte
	count := opusPacketParseImpl(data, selfDelimited, &toc, nil, st.size[:],
		&offset, packetOffset, &padding)
	if st.ignoreExtensions {
		padding = nil
	}
	if count < 0 {
		return count
	}
	// The extension iterator over padding is only used for QEXT, which
	// this build does not enable.
	_ = padding

	data = data[offset:]

	if decodeFEC {
		// If no FEC can be present, run the PLC (recursive call).
		if frameSize < packetFrameSize || packetMode == modeCeltOnly || st.mode == modeCeltOnly {
			return st.decodeNative(nil, pcm, frameSize, false, false, nil, softClip)
		}
		// Otherwise, run the PLC on everything except the size for
		// which we might have FEC.
		durationCopy := st.lastPacketDuration
		if frameSize-packetFrameSize != 0 {
			ret := st.decodeNative(nil, pcm, frameSize-packetFrameSize, false, false, nil, softClip)
			if ret < 0 {
				st.lastPacketDuration = durationCopy
				return ret
			}
		}
		// Complete with FEC.
		st.mode = packetMode
		st.bandwidth = packetBandwidth
		st.frameSize = packetFrameSize
		st.streamChannels = packetStreamChannels
		ret := st.decodeFrame(data, int(st.size[0]), pcm[ch*(frameSize-packetFrameSize):],
			packetFrameSize, true)
		if ret < 0 {
			return ret
		}
		st.lastPacketDuration = frameSize
		return frameSize
	}

	if count*packetFrameSize > frameSize {
		return int(ErrBufferTooSmall)
	}

	// Update the state as the last step to avoid updating it on an
	// invalid packet.
	st.mode = packetMode
	st.bandwidth = packetBandwidth
	st.frameSize = packetFrameSize
	st.streamChannels = packetStreamChannels

	nbSamples := 0
	for i := 0; i < count; i++ {
		ret := st.decodeFrame(data, int(st.size[i]), pcm[nbSamples*ch:], frameSize-nbSamples, false)
		if ret < 0 {
			return ret
		}
		data = data[st.size[i]:]
		nbSamples += ret
	}
	st.lastPacketDuration = nbSamples
	if softClip {
		opusPCMSoftClipImpl(pcm, nbSamples, ch, st.softclipMem[:])
	} else {
		st.softclipMem = [2]float32{}
	}
	return nbSamples
}

// outBuffer returns the scratch buffer of opus_decode and opus_decode24
// with room for n samples. It grows only for concealment requests longer
// than 120 ms.
func (st *Decoder) outBuffer(n int) []float32 {
	if cap(st.out) < n {
		st.out = make([]float32, n)
	}
	return st.out[:n]
}

// decodeResult converts a C return value into the Go results.
func decodeResult(ret int) (int, error) {
	if ret < 0 {
		return 0, Error(ret)
	}
	return ret, nil
}

// clampFrameSize applies the frame size check and limit shared by
// opus_decode and opus_decode24.
func (st *Decoder) clampFrameSize(data []byte, frameSize int, decodeFEC bool) (int, error) {
	if frameSize <= 0 {
		return 0, ErrBadArg
	}
	if len(data) > 0 && !decodeFEC {
		nbSamples := opusPacketGetNbSamples(data, st.fs)
		if nbSamples > 0 {
			frameSize = min(frameSize, nbSamples)
		} else {
			return 0, ErrInvalidPacket
		}
	}
	return frameSize, nil
}

// Decode decodes an Opus packet to 16-bit PCM (opus_decode). A nil or
// empty data indicates a lost packet. pcm receives frameSize samples per
// channel at most, interleaved; frameSize is the maximum number of
// samples per channel that fit in pcm. For a lost packet or FEC,
// frameSize must be a multiple of 2.5 ms and is the duration to
// conceal or recover. If decodeFEC is true, the decoder decodes the
// in-band FEC data of data for the previous, lost packet. It returns the
// number of decoded samples per channel. As in libopus, the float output
// is soft-clipped before the conversion.
func (st *Decoder) Decode(data []byte, pcm []int16, frameSize int, decodeFEC bool) (int, error) {
	frameSize, err := st.clampFrameSize(data, frameSize, decodeFEC)
	if err != nil {
		return 0, err
	}
	if len(pcm) < frameSize*st.channels {
		return 0, ErrBufferTooSmall
	}
	out := st.outBuffer(frameSize * st.channels)
	ret := st.decodeNative(data, out, frameSize, decodeFEC, false, nil, true)
	if ret > 0 {
		dsp.CeltFloat2Int16(out[:ret*st.channels], pcm[:ret*st.channels])
	}
	return decodeResult(ret)
}

// Decode24 decodes an Opus packet to 24-bit PCM in int32 samples
// (opus_decode24). The arguments are as for Decode. The output is not
// soft-clipped.
func (st *Decoder) Decode24(data []byte, pcm []int32, frameSize int, decodeFEC bool) (int, error) {
	frameSize, err := st.clampFrameSize(data, frameSize, decodeFEC)
	if err != nil {
		return 0, err
	}
	if len(pcm) < frameSize*st.channels {
		return 0, ErrBufferTooSmall
	}
	out := st.outBuffer(frameSize * st.channels)
	ret := st.decodeNative(data, out, frameSize, decodeFEC, false, nil, false)
	if ret > 0 {
		for i := 0; i < ret*st.channels; i++ {
			pcm[i] = res2int24(out[i])
		}
	}
	return decodeResult(ret)
}

// DecodeFloat decodes an Opus packet to float PCM, nominally in [-1, 1]
// (opus_decode_float). The arguments are as for Decode, except that pcm
// must hold frameSize samples per channel. The output is not
// soft-clipped.
func (st *Decoder) DecodeFloat(
	data []byte,
	pcm []float32,
	frameSize int,
	decodeFEC bool,
) (int, error) {
	if frameSize <= 0 {
		return 0, ErrBadArg
	}
	if len(pcm) < frameSize*st.channels {
		return 0, ErrBufferTooSmall
	}
	return decodeResult(st.decodeNative(data, pcm, frameSize, decodeFEC, false, nil, false))
}

// Channels returns the number of channels the decoder outputs.
func (st *Decoder) Channels() int { return st.channels }

// Bandwidth returns the bandwidth of the last decoded packet
// (OPUS_GET_BANDWIDTH), one of the Bandwidth constants, or 0 before the
// first packet.
func (st *Decoder) Bandwidth() int { return st.bandwidth }

// SetComplexity sets the decoder complexity, 0 to 10
// (OPUS_SET_COMPLEXITY). The decoder stores it, but in the default
// libopus build (no deep PLC or OSCE) it does not change the output.
func (st *Decoder) SetComplexity(complexity int) error {
	if complexity < 0 || complexity > 10 {
		return ErrBadArg
	}
	st.complexity = complexity
	st.celtDec.SetComplexity(complexity)
	return nil
}

// Complexity returns the decoder complexity (OPUS_GET_COMPLEXITY).
func (st *Decoder) Complexity() int { return st.complexity }

// FinalRange returns the final state of the range coder of the last
// decoded packet (OPUS_GET_FINAL_RANGE). It is 0 after a lost packet.
func (st *Decoder) FinalRange() uint32 { return st.rangeFinal }

// Reset resets the decoder to the state of a freshly initialized one,
// keeping its settings (OPUS_RESET_STATE).
func (st *Decoder) Reset() {
	st.decoderResetState = decoderResetState{}

	st.celtDec.Reset()
	st.silkDec.Reset()
	st.streamChannels = st.channels
	st.frameSize = int(st.fs / 400)
}

// SampleRate returns the sampling rate the decoder was initialized with
// (OPUS_GET_SAMPLE_RATE).
func (st *Decoder) SampleRate() int { return int(st.fs) }

// Pitch returns the pitch period of the last decoded frame, in samples
// at 48 kHz, or 0 if it was not voiced (OPUS_GET_PITCH).
func (st *Decoder) Pitch() int {
	if st.prevMode == modeCeltOnly {
		return st.celtDec.Pitch()
	}
	return st.decControl.PrevPitchLag
}

// SetGain sets the output gain in Q8 dB units, -32768 to 32767
// (OPUS_SET_GAIN).
func (st *Decoder) SetGain(gain int) error {
	if gain < -32768 || gain > 32767 {
		return ErrBadArg
	}
	st.decodeGain = gain
	return nil
}

// Gain returns the output gain (OPUS_GET_GAIN).
func (st *Decoder) Gain() int { return st.decodeGain }

// LastPacketDuration returns the duration, in samples per channel at the
// decoder rate, of the last decoded or concealed packet
// (OPUS_GET_LAST_PACKET_DURATION).
func (st *Decoder) LastPacketDuration() int { return st.lastPacketDuration }

// SetPhaseInversionDisabled disables (or re-enables) the use of phase
// inversion for intensity stereo (OPUS_SET_PHASE_INVERSION_DISABLED).
func (st *Decoder) SetPhaseInversionDisabled(disabled bool) {
	st.celtDec.SetPhaseInversionDisabled(b2i(disabled))
}

// PhaseInversionDisabled reports whether phase inversion is disabled
// (OPUS_GET_PHASE_INVERSION_DISABLED).
func (st *Decoder) PhaseInversionDisabled() bool {
	return st.celtDec.PhaseInversionDisabled() != 0
}

// SetIgnoreExtensions makes the decoder ignore packet extensions
// (OPUS_SET_IGNORE_EXTENSIONS).
func (st *Decoder) SetIgnoreExtensions(ignore bool) { st.ignoreExtensions = ignore }

// IgnoreExtensions reports whether the decoder ignores packet extensions
// (OPUS_GET_IGNORE_EXTENSIONS).
func (st *Decoder) IgnoreExtensions() bool { return st.ignoreExtensions }

// NbSamples returns the number of samples of an Opus packet at the
// decoder sampling rate (opus_decoder_get_nb_samples).
func (st *Decoder) NbSamples(packet []byte) (int, error) {
	ret := opusPacketGetNbSamples(packet, st.fs)
	return max(ret, 0), errorFromCode(ret)
}

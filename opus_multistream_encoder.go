// Ported from libopus 1.6.1 src/opus_multistream_encoder.c.

package opus

import (
	"math"

	"github.com/cirocosta/opus/internal/celt"
	"github.com/cirocosta/opus/internal/dsp"
)

// vorbisLayout ports VorbisLayout.
type vorbisLayout struct {
	nbStreams        int
	nbCoupledStreams int
	mapping          [8]byte
}

// vorbisMappings holds the Vorbis channel layouts; the index is the
// number of channels minus one.
var vorbisMappings = [8]vorbisLayout{
	{1, 0, [8]byte{0}},                      // 1: mono
	{1, 1, [8]byte{0, 1}},                   // 2: stereo
	{2, 1, [8]byte{0, 2, 1}},                // 3: 1-d surround
	{2, 2, [8]byte{0, 1, 2, 3}},             // 4: quadraphonic surround
	{3, 2, [8]byte{0, 4, 1, 2, 3}},          // 5: 5-channel surround
	{4, 2, [8]byte{0, 4, 1, 2, 3, 5}},       // 6: 5.1 surround
	{4, 3, [8]byte{0, 4, 1, 2, 3, 5, 6}},    // 7: 6.1 surround
	{5, 3, [8]byte{0, 6, 1, 2, 3, 4, 5, 7}}, // 8: 7.1 surround
}

// maxOverlap is MAX_OVERLAP (no ENABLE_QEXT).
const maxOverlap = 120

// msFrameTmp is MS_FRAME_TMP: the max size in case the encoder decides
// to return six frames (6 x 20 ms = 120 ms).
const msFrameTmp = 6*1275 + 12

// Mapping types (MappingType).
const (
	mappingTypeNone = iota
	mappingTypeSurround
	mappingTypeAmbisonics
)

// copyChannelInFunc is opus_copy_channel_in_func: it copies channel
// srcChannel of the interleaved src (srcStride channels) to dst, with
// samples dstStride apart, as opus_res. matrix is the user data of the
// projection encoder.
type copyChannelInFunc func(dst []float32, dstStride int, src analysisPCM, srcStride, srcChannel, frameSize int, matrix *mappingMatrix)

// MSEncoder is a multistream Opus encoder (OpusMSEncoder): it codes each
// coupled (stereo) or uncoupled (mono) stream with its own Encoder and
// packs the streams into one multistream packet. Create one with
// NewMSEncoder or NewMSSurroundEncoder. An MSEncoder is not safe for
// concurrent use.
type MSEncoder struct {
	layout           channelLayout
	lfeStream        int
	application      int
	fs               int32
	variableDuration int
	mappingType      int
	bitrateBps       int32
	encoders         []Encoder

	// windowMem and preemphMem are the surround analysis state (the
	// memory after the encoders in C): maxOverlap and 1 values per
	// channel.
	windowMem  []float32
	preemphMem []float32

	// Scratch buffers for the C stack allocations.
	buf      [2 * maxFrameSamples48k]float32
	bandSMR  []float32
	bandLogE [42]float32
	tmpData  [msFrameTmp]byte
	rp       Repacketizer
	bitrates [256]int32
	surIn    [maxFrameSamples48k + maxOverlap]float32
	surX     [maxFrameSamples48k]float32
	surFreq  [960]float32
}

// validateAmbisonics ports validate_ambisonics.
func validateAmbisonics(nbChannels int, nbStreams, nbCoupledStreams *int) bool {
	if nbChannels < 1 || nbChannels > 227 {
		return false
	}

	orderPlusOne := int(dsp.Isqrt32(uint32(nbChannels)))
	acnChannels := orderPlusOne * orderPlusOne
	nondiegeticChannels := nbChannels - acnChannels

	if nondiegeticChannels != 0 && nondiegeticChannels != 2 {
		return false
	}

	if nbStreams != nil {
		*nbStreams = acnChannels + b2i(nondiegeticChannels != 0)
	}
	if nbCoupledStreams != nil {
		*nbCoupledStreams = b2i(nondiegeticChannels != 0)
	}
	return true
}

// validateEncoderLayout ports validate_encoder_layout.
func validateEncoderLayout(layout *channelLayout) bool {
	for s := 0; s < layout.nbStreams; s++ {
		if s < layout.nbCoupledStreams {
			if getLeftChannel(layout, s, -1) == -1 || getRightChannel(layout, s, -1) == -1 {
				return false
			}
			continue
		}
		if getMonoChannel(layout, s, -1) == -1 {
			return false
		}
	}
	return true
}

// channelPos ports channel_pos: the position of each channel in the mix
// (0 don't mix, 1 left, 2 center, 3 right).
func channelPos(channels int, pos *[8]int) {
	switch channels {
	case 4:
		pos[0] = 1
		pos[1] = 3
		pos[2] = 1
		pos[3] = 3
	case 3, 5, 6:
		pos[0] = 1
		pos[1] = 2
		pos[2] = 3
		pos[3] = 1
		pos[4] = 3
		pos[5] = 0
	case 7:
		pos[0] = 1
		pos[1] = 2
		pos[2] = 3
		pos[3] = 1
		pos[4] = 3
		pos[5] = 2
		pos[6] = 0
	case 8:
		pos[0] = 1
		pos[1] = 2
		pos[2] = 3
		pos[3] = 1
		pos[4] = 3
		pos[5] = 1
		pos[6] = 3
		pos[7] = 0
	}
}

// logSumDiffTable is diff_table of logSum. Only the first 9 entries are
// initialized in C; the others are zero.
var logSumDiffTable = [17]float32{
	0.5000000, 0.2924813, 0.1609640, 0.0849625,
	0.0437314, 0.0221971, 0.0111839, 0.0056136,
	0.0028123,
}

// logSum ports logSum: a rough approximation of log2(2^a + 2^b).
func logSum(a, b float32) float32 {
	var maxV, diff float32
	if a > b {
		maxV = a
		diff = a - b
	} else {
		maxV = b
		diff = b - a
	}
	if !(diff < 8) { // inverted to catch NaNs
		return maxV
	}
	low := int(math.Floor(float64(2 * diff)))
	frac := 2*diff - float32(low)
	return maxV + logSumDiffTable[low] + float32(frac*(logSumDiffTable[low+1]-logSumDiffTable[low]))
}

// surroundAnalysis ports surround_analysis: it computes the band
// signal-to-mask ratios of every channel in bandLogE (21 per channel).
// keep per-channel band masking and overlap-history updates in reference order
//
//revive:disable-next-line:cognitive-complexity
func (st *MSEncoder) surroundAnalysis(
	celtMode *celt.Mode,
	pcm analysisPCM,
	bandLogE []float32,
	mem, preemphMem []float32,
	length, overlap, channels int,
	rate int32,
	copyChannelIn copyChannelInFunc,
	_ *mappingMatrix,
) {
	var pos [8]int
	var bandE [21]float32
	var maskLogE [3][21]float32

	upsample := celt.ResamplingFactor(rate)
	frameSize := length * upsample

	// LM = log2(frame_size / 120)
	LM := 0
	for ; LM < celtMode.MaxLM; LM++ {
		if celtMode.ShortMdctSize<<LM == frameSize {
			break
		}
	}

	freqSize := celtMode.ShortMdctSize << LM

	in := st.surIn[:frameSize+overlap]
	x := st.surX[:length]
	freq := st.surFreq[:freqSize]

	channelPos(channels, &pos)

	for c := 0; c < 3; c++ {
		for i := 0; i < 21; i++ {
			maskLogE[c][i] = -28
		}
	}

	for c := 0; c < channels; c++ {
		nbFrames := frameSize / freqSize
		copy(in[:overlap], mem[c*overlap:c*overlap+overlap])
		copyChannelIn(x, 1, pcm, channels, c, length, nil)
		celt.Preemphasis(
			x,
			in[overlap:],
			frameSize,
			1,
			upsample,
			celtMode.Preemph[:],
			&preemphMem[c],
			false,
		)
		{
			sum := dsp.CeltInnerProd(in, in, frameSize+overlap)
			// This should filter out both NaNs and ridiculous signals
			// that could cause NaNs further down.
			if !(sum < 1e18) || dsp.CeltIsnan(sum) {
				clear(in)
				preemphMem[c] = 0
			}
		}
		bandE = [21]float32{}
		for frame := 0; frame < nbFrames; frame++ {
			var tmpE [21]float32
			dsp.CltMDCTForward(celtMode.MDCT, in[freqSize*frame:], freq, celtMode.Window,
				overlap, celtMode.MaxLM-LM, 1)
			if upsample != 1 {
				bound := freqSize / upsample
				i := 0
				for ; i < bound; i++ {
					freq[i] *= float32(upsample)
				}
				for ; i < freqSize; i++ {
					freq[i] = 0
				}
			}

			celt.ComputeBandEnergies(celtMode, freq, tmpE[:], 21, 1, LM)
			// If we have multiple frames, take the max energy.
			for i := 0; i < 21; i++ {
				bandE[i] = dsp.Max32(bandE[i], tmpE[i])
			}
		}
		ble := bandLogE[21*c : 21*c+21]
		celt.Amp2Log2(celtMode, 21, 21, bandE[:], ble, 1)
		// Apply spreading function with -6 dB/band going up and -12
		// dB/band going down.
		for i := 1; i < 21; i++ {
			ble[i] = dsp.Max32(ble[i], ble[i-1]-1)
		}
		for i := 19; i >= 0; i-- {
			ble[i] = dsp.Max32(ble[i], ble[i+1]-2)
		}
		switch pos[c] {
		case 1:
			for i := 0; i < 21; i++ {
				maskLogE[0][i] = logSum(maskLogE[0][i], ble[i])
			}
		case 3:
			for i := 0; i < 21; i++ {
				maskLogE[2][i] = logSum(maskLogE[2][i], ble[i])
			}
		case 2:
			for i := 0; i < 21; i++ {
				maskLogE[0][i] = logSum(maskLogE[0][i], ble[i]-.5)
				maskLogE[2][i] = logSum(maskLogE[2][i], ble[i]-.5)
			}
		}
		copy(mem[c*overlap:c*overlap+overlap], in[frameSize:frameSize+overlap])
	}
	for i := 0; i < 21; i++ {
		maskLogE[1][i] = dsp.Min32(maskLogE[0][i], maskLogE[2][i])
	}
	channelOffset := float32(.5 * dsp.CeltLog2(float32(2)/float32(channels-1)))
	for c := 0; c < 3; c++ {
		for i := 0; i < 21; i++ {
			maskLogE[c][i] += channelOffset
		}
	}
	for c := 0; c < channels; c++ {
		ble := bandLogE[21*c : 21*c+21]
		if pos[c] != 0 {
			mask := &maskLogE[pos[c]-1]
			for i := 0; i < 21; i++ {
				ble[i] = ble[i] - mask[i]
			}
		} else {
			for i := 0; i < 21; i++ {
				ble[i] = 0
			}
		}
	}
}

// NewMSEncoder allocates and initializes a multistream encoder
// (opus_multistream_encoder_create). channels is the number of input
// channels (1 to 255), streams the number of streams, coupledStreams the
// number of them that are stereo, and mapping (channels entries) maps
// each input channel to a coded channel: values below 2*coupledStreams
// are the left and right channels of the coupled streams, the following
// values are the mono streams, and 255 drops the channel. application is
// as for NewEncoder.
func NewMSEncoder(
	fs, channels, streams, coupledStreams int,
	mapping []byte,
	application int,
) (*MSEncoder, error) {
	if !validMSArgs(channels, streams, coupledStreams) {
		return nil, ErrBadArg
	}
	st := new(MSEncoder)
	if err := st.Init(fs, channels, streams, coupledStreams, mapping, application); err != nil {
		return nil, err
	}
	return st, nil
}

// Init initializes a multistream encoder (opus_multistream_encoder_init).
func (st *MSEncoder) Init(
	fs, channels, streams, coupledStreams int,
	mapping []byte,
	application int,
) error {
	st.lfeStream = -1
	return st.initImpl(fs, channels, streams, coupledStreams, mapping, application, mappingTypeNone)
}

// initImpl ports opus_multistream_encoder_init_impl. The caller sets
// lfeStream first for surround mappings.
// keep mapping validation, subencoder setup and buffer reuse in one lifecycle
//
//revive:disable-next-line:cognitive-complexity
func (st *MSEncoder) initImpl(fs, channels, streams, coupledStreams int, mapping []byte,
	application int, mappingType int,
) error {
	if !validMSArgs(channels, streams, coupledStreams) {
		return ErrBadArg
	}
	if !validEncoderArgs(fs, 1, application) {
		return ErrBadArg
	}
	if len(mapping) < channels {
		return ErrBadArg
	}

	st.layout.nbChannels = channels
	st.layout.nbStreams = streams
	st.layout.nbCoupledStreams = coupledStreams
	if mappingType != mappingTypeSurround {
		st.lfeStream = -1
	}
	st.bitrateBps = Auto
	st.application = application
	st.fs = int32(fs)
	st.variableDuration = FramesizeArg
	for i := 0; i < st.layout.nbChannels; i++ {
		st.layout.mapping[i] = mapping[i]
	}
	if !validateLayout(&st.layout) {
		return ErrBadArg
	}
	if !validateEncoderLayout(&st.layout) {
		return ErrBadArg
	}
	if mappingType == mappingTypeAmbisonics && !validateAmbisonics(st.layout.nbChannels, nil, nil) {
		return ErrBadArg
	}
	if cap(st.encoders) < streams {
		st.encoders = make([]Encoder, streams)
	}
	st.encoders = st.encoders[:streams]
	i := 0
	for ; i < st.layout.nbCoupledStreams; i++ {
		if err := st.encoders[i].Init(fs, 2, application); err != nil {
			return err
		}
		if i == st.lfeStream {
			st.encoders[i].setLFE(1)
		}
	}
	for ; i < st.layout.nbStreams; i++ {
		if err := st.encoders[i].Init(fs, 1, application); err != nil {
			return err
		}
		if i == st.lfeStream {
			st.encoders[i].setLFE(1)
		}
	}
	if cap(st.bandSMR) < 21*channels {
		st.bandSMR = make([]float32, 21*channels)
	}
	st.bandSMR = st.bandSMR[:21*channels]
	if mappingType == mappingTypeSurround {
		if cap(st.windowMem) < channels*maxOverlap {
			st.windowMem = make([]float32, channels*maxOverlap)
			st.preemphMem = make([]float32, channels)
		}
		st.windowMem = st.windowMem[:channels*maxOverlap]
		st.preemphMem = st.preemphMem[:channels]
		clear(st.preemphMem)
		clear(st.windowMem)
	}
	st.mappingType = mappingType
	return nil
}

// NewMSSurroundEncoder allocates and initializes a multistream encoder
// for a standard channel mapping family
// (opus_multistream_surround_encoder_create): 0 (mono or stereo), 1
// (Vorbis channel order, 1 to 8 channels, with surround masking and an
// LFE stream for 5.1 and up), 2 (ambisonics) or 255 (independent mono
// streams). Streams, CoupledStreams and Mapping return the chosen
// layout.
func NewMSSurroundEncoder(fs, channels, mappingFamily, application int) (*MSEncoder, error) {
	if channels > 255 || channels < 1 {
		return nil, ErrBadArg
	}
	st := new(MSEncoder)
	if err := st.InitSurround(fs, channels, mappingFamily, application); err != nil {
		return nil, err
	}
	return st, nil
}

// InitSurround initializes a multistream encoder for a channel mapping
// family (opus_multistream_surround_encoder_init).
func (st *MSEncoder) InitSurround(fs, channels, mappingFamily, application int) error {
	if channels > 255 || channels < 1 {
		return ErrBadArg
	}
	config, err := surroundMapping(channels, mappingFamily)
	if err != nil {
		return err
	}
	st.lfeStream = config.lfeStream
	return st.initImpl(fs, channels, config.layout.nbStreams,
		config.layout.nbCoupledStreams, config.layout.mapping[:channels],
		application, config.mappingType)
}

// surroundConfig separates family-specific channel mapping from encoder state.
type surroundConfig struct {
	layout                 channelLayout
	lfeStream, mappingType int
}

// surroundMapping requires a channel count from 1 to 255.
func surroundMapping(channels, family int) (surroundConfig, error) {
	switch family {
	case 0:
		if channels > 2 {
			return surroundConfig{}, ErrUnimplemented
		}
		return identitySurroundMapping(channels, 1, channels-1), nil
	case 1:
		if channels > 8 {
			return surroundConfig{}, ErrUnimplemented
		}
		return vorbisSurroundMapping(channels), nil
	case 255:
		return identitySurroundMapping(channels, channels, 0), nil
	case 2:
		return ambisonicSurroundMapping(channels)
	default:
		return surroundConfig{}, ErrUnimplemented
	}
}

func identitySurroundMapping(channels, streams, coupled int) surroundConfig {
	config := surroundConfig{
		layout: channelLayout{
			nbChannels:       channels,
			nbStreams:        streams,
			nbCoupledStreams: coupled,
		},
		lfeStream: -1,
	}
	for i := 0; i < channels; i++ {
		config.layout.mapping[i] = byte(i)
	}
	return config
}

func vorbisSurroundMapping(channels int) surroundConfig {
	mapping := &vorbisMappings[channels-1]
	config := surroundConfig{
		layout: channelLayout{
			nbChannels: channels, nbStreams: mapping.nbStreams,
			nbCoupledStreams: mapping.nbCoupledStreams,
		},
		lfeStream: -1,
	}
	copy(config.layout.mapping[:channels], mapping.mapping[:channels])
	if channels >= 6 {
		config.lfeStream = mapping.nbStreams - 1
	}
	if channels > 2 {
		config.mappingType = mappingTypeSurround
	}
	return config
}

func ambisonicSurroundMapping(channels int) (surroundConfig, error) {
	config := surroundConfig{
		layout:    channelLayout{nbChannels: channels},
		lfeStream: -1, mappingType: mappingTypeAmbisonics,
	}
	layout := &config.layout
	if !validateAmbisonics(channels, &layout.nbStreams, &layout.nbCoupledStreams) {
		return surroundConfig{}, ErrBadArg
	}
	mono := layout.nbStreams - layout.nbCoupledStreams
	for i := 0; i < mono; i++ {
		layout.mapping[i] = byte(i + layout.nbCoupledStreams*2)
	}
	for i := 0; i < layout.nbCoupledStreams*2; i++ {
		layout.mapping[i+mono] = byte(i)
	}
	return config, nil
}

// surroundRateAllocation ports surround_rate_allocation.
func (st *MSEncoder) surroundRateAllocation(rate []int32, frameSize int, fs int32) {
	nbLfe := b2i(st.lfeStream != -1)
	nbCoupled := st.layout.nbCoupledStreams
	nbUncoupled := st.layout.nbStreams - nbCoupled - nbLfe
	nbNormal := 2*nbCoupled + nbUncoupled
	var bitrate int32

	// Give each non-LFE channel enough bits per channel for coding band
	// energy.
	channelOffset := int32(40 * max(50, int(fs)/frameSize))

	switch st.bitrateBps {
	case Auto:
		bitrate = int32(nbNormal)*(channelOffset+fs+10000) + 8000*int32(nbLfe)
	case BitrateMax:
		bitrate = int32(nbNormal*750000 + nbLfe*128000)
	default:
		bitrate = st.bitrateBps
	}

	// Give LFE some basic stream_channel allocation but never exceed 1/20
	// of the total rate for the non-energy part to avoid problems at
	// really low rate.
	lfeOffset := min(bitrate/20, 3000) + int32(15*max(50, int(fs)/frameSize))

	// We give each stream (coupled or uncoupled) a starting bitrate. This
	// models the main saving of coupled channels over uncoupled.
	streamOffset := (bitrate - channelOffset*int32(nbNormal) - lfeOffset*int32(nbLfe)) / int32(
		nbNormal,
	) / 2
	streamOffset = max(0, min(20000, streamOffset))

	// Coupled streams get twice the mono rate after the offset is
	// allocated.
	const coupledRatio = 512
	// Should depend on the bitrate, for now we assume LFE gets 1/8 the
	// bits of mono.
	const lfeRatio = 32

	total := int64(nbUncoupled<<8 + coupledRatio*nbCoupled + nbLfe*lfeRatio)
	channelRate := int32(
		256 * int64(
			bitrate-lfeOffset*int32(
				nbLfe,
			)-streamOffset*int32(
				nbCoupled+nbUncoupled,
			)-channelOffset*int32(
				nbNormal,
			),
		) / total,
	)

	for i := 0; i < st.layout.nbStreams; i++ {
		if i < st.layout.nbCoupledStreams {
			rate[i] = 2*channelOffset + max(0, streamOffset+(channelRate*coupledRatio>>8))
		} else if i != st.lfeStream {
			rate[i] = channelOffset + max(0, streamOffset+channelRate)
		} else {
			rate[i] = max(0, lfeOffset+(channelRate*lfeRatio>>8))
		}
	}
}

// ambisonicsRateAllocation ports ambisonics_rate_allocation.
func (st *MSEncoder) ambisonicsRateAllocation(rate []int32, frameSize int, fs int32) {
	var totalRate int32
	nbChannels := st.layout.nbStreams + st.layout.nbCoupledStreams

	switch st.bitrateBps {
	case Auto:
		totalRate = int32(
			st.layout.nbCoupledStreams+st.layout.nbStreams,
		)*(fs+60*fs/int32(frameSize)) +
			int32(
				st.layout.nbStreams,
			)*15000
	case BitrateMax:
		totalRate = int32(nbChannels * 750000)
	default:
		totalRate = st.bitrateBps
	}

	// Allocate equal number of bits to Ambisonic (uncoupled) and
	// non-diegetic (coupled) streams.
	perStreamRate := totalRate / int32(st.layout.nbStreams)
	for i := 0; i < st.layout.nbStreams; i++ {
		rate[i] = perStreamRate
	}
}

// rateAllocation ports rate_allocation.
func (st *MSEncoder) rateAllocation(rate []int32, frameSize int) int32 {
	var rateSum int32
	fs := int32(st.encoders[0].SampleRate())

	if st.mappingType == mappingTypeAmbisonics {
		st.ambisonicsRateAllocation(rate, frameSize, fs)
	} else {
		st.surroundRateAllocation(rate, frameSize, fs)
	}

	for i := 0; i < st.layout.nbStreams; i++ {
		rate[i] = max(rate[i], 500)
		rateSum += rate[i]
	}
	return rateSum
}

// encodeNative ports opus_multistream_encode_native.
// keep shared multistream budgets and channel state transitions in reference order
//
//revive:disable-next-line:cognitive-complexity
func (st *MSEncoder) encodeNative(
	copyChannelIn copyChannelInFunc,
	pcm analysisPCM,
	analysisFrameSize int,
	data []byte,
	lsbDepth int,
	downmix downmixFunc,
	floatAPI bool,
	matrix *mappingMatrix,
) int {
	var celtMode *celt.Mode
	maxDataBytes := len(data)

	first := &st.encoders[0]
	fs := int32(first.SampleRate())
	vbr := first.VBR()
	if st.application != ApplicationRestrictedSilk {
		celtMode = first.celtEnc.Mode()
	}

	frameSize := frameSizeSelect(st.application, analysisFrameSize, st.variableDuration, fs)
	if frameSize <= 0 {
		return int(ErrBadArg)
	}

	// Smallest packet the encoder can produce.
	smallestPacket := st.layout.nbStreams*2 - 1
	// 100 ms needs an extra byte per stream for the ToC.
	if int(fs)/frameSize == 10 {
		smallestPacket += st.layout.nbStreams
	}
	if maxDataBytes < smallestPacket {
		return int(ErrBufferTooSmall)
	}
	buf := st.buf[:2*frameSize]

	bandSMR := st.bandSMR
	if st.mappingType == mappingTypeSurround && st.application != ApplicationRestrictedSilk {
		st.surroundAnalysis(celtMode, pcm, bandSMR, st.windowMem, st.preemphMem, frameSize,
			celtMode.Overlap, st.layout.nbChannels, fs, copyChannelIn, matrix)
	}

	// Compute bitrate allocation between streams (this could be a lot
	// better).
	bitrates := st.bitrates[:st.layout.nbStreams]
	rateSum := st.rateAllocation(bitrates, frameSize)

	if !vbr {
		if st.bitrateBps == Auto {
			maxDataBytes = min(
				maxDataBytes,
				int((bitrateToBits(rateSum, fs, int32(frameSize))+4)/8),
			)
		} else if st.bitrateBps != BitrateMax {
			maxDataBytes = min(maxDataBytes, max(smallestPacket,
				int((bitrateToBits(st.bitrateBps, fs, int32(frameSize))+4)/8)))
		}
	}
	for s := 0; s < st.layout.nbStreams; s++ {
		enc := &st.encoders[s]
		_ = enc.SetBitrate(int(bitrates[s]))
		switch st.mappingType {
		case mappingTypeSurround:
			equivRate := st.bitrateBps
			if frameSize*50 < int(fs) {
				equivRate -= int32(60 * (int(fs)/frameSize - 50) * st.layout.nbChannels)
			}
			nc := int32(st.layout.nbChannels)
			if equivRate > 10000*nc {
				_ = enc.SetBandwidth(BandwidthFullband)
			} else if equivRate > 7000*nc {
				_ = enc.SetBandwidth(BandwidthSuperwideband)
			} else if equivRate > 5000*nc {
				_ = enc.SetBandwidth(BandwidthWideband)
			} else {
				_ = enc.SetBandwidth(BandwidthNarrowband)
			}
			if s < st.layout.nbCoupledStreams {
				// To preserve the spatial image, force stereo CELT on
				// coupled streams.
				_ = enc.SetForceMode(modeCeltOnly)
				_ = enc.SetForceChannels(2)
			}
		case mappingTypeAmbisonics:
			_ = enc.SetForceMode(modeCeltOnly)
		}
	}

	// Counting ToC.
	totSize := 0
	for s := 0; s < st.layout.nbStreams; s++ {
		var c1, c2 int
		rp := &st.rp
		rp.Reset()
		enc := &st.encoders[s]
		surround := st.mappingType == mappingTypeSurround &&
			st.application != ApplicationRestrictedSilk
		if s < st.layout.nbCoupledStreams {
			left := getLeftChannel(&st.layout, s, -1)
			right := getRightChannel(&st.layout, s, -1)
			copyChannelIn(buf, 2, pcm, st.layout.nbChannels, left, frameSize, matrix)
			copyChannelIn(buf[1:], 2, pcm, st.layout.nbChannels, right, frameSize, matrix)
			if surround {
				for i := 0; i < 21; i++ {
					st.bandLogE[i] = bandSMR[21*left+i]
					st.bandLogE[21+i] = bandSMR[21*right+i]
				}
			}
			c1 = left
			c2 = right
		} else {
			chn := getMonoChannel(&st.layout, s, -1)
			copyChannelIn(buf, 1, pcm, st.layout.nbChannels, chn, frameSize, matrix)
			if surround {
				for i := 0; i < 21; i++ {
					st.bandLogE[i] = bandSMR[21*chn+i]
				}
			}
			c1 = chn
			c2 = -1
		}
		if surround {
			enc.setEnergyMask(st.bandLogE[:])
		}
		// Number of bytes left (+Toc).
		currMax := maxDataBytes - totSize
		// Reserve one byte for the last stream and two for the others.
		currMax -= max(0, 2*(st.layout.nbStreams-s-1)-1)
		// For 100 ms, reserve an extra byte per stream for the ToC.
		if int(fs)/frameSize == 10 {
			currMax -= st.layout.nbStreams - s - 1
		}
		currMax = min(currMax, msFrameTmp)
		// Repacketizer will add one or two bytes for self-delimited
		// frames.
		if s != st.layout.nbStreams-1 {
			if currMax > 253 {
				currMax -= 2
			} else {
				currMax--
			}
		}
		if !vbr && s == st.layout.nbStreams-1 {
			_ = enc.SetBitrate(int(bitsToBitrate(int32(currMax*8), fs, int32(frameSize))))
		}
		length := enc.encodeNative(buf, frameSize, st.tmpData[:max(currMax, 0)], currMax, lsbDepth,
			pcm, analysisFrameSize, c1, c2, st.layout.nbChannels, downmix, floatAPI)
		if length < 0 {
			return length
		}
		// We need to use the repacketizer to add the self-delimiting
		// lengths while taking into account the fact that the encoder
		// can now return more than one frame at a time (e.g. 60 ms
		// CELT-only).
		ret := rp.catImpl(st.tmpData[:length], false)
		// If the opus_repacketizer_cat() fails, then something's
		// seriously wrong with the encoder.
		if ret != OK {
			return int(ErrInternalError)
		}
		length = int(rp.outRangeImpl(0, rp.nbFrames, data[totSize:maxDataBytes],
			s != st.layout.nbStreams-1, !vbr && s == st.layout.nbStreams-1, nil))
		totSize += length
	}
	return totSize
}

// copyChannelInFloat ports opus_copy_channel_in_float.
func copyChannelInFloat(
	dst []float32,
	dstStride int,
	src analysisPCM,
	srcStride, srcChannel, frameSize int,
	_ *mappingMatrix,
) {
	floatSrc := src.f32
	for i := 0; i < frameSize; i++ {
		dst[i*dstStride] = floatSrc[i*srcStride+srcChannel]
	}
}

// copyChannelInShort ports opus_copy_channel_in_short.
func copyChannelInShort(
	dst []float32,
	dstStride int,
	src analysisPCM,
	srcStride, srcChannel, frameSize int,
	_ *mappingMatrix,
) {
	shortSrc := src.i16
	for i := 0; i < frameSize; i++ {
		dst[i*dstStride] = dsp.Int16ToRes(shortSrc[i*srcStride+srcChannel])
	}
}

// copyChannelInInt24 ports opus_copy_channel_in_int24.
func copyChannelInInt24(
	dst []float32,
	dstStride int,
	src analysisPCM,
	srcStride, srcChannel, frameSize int,
	_ *mappingMatrix,
) {
	shortSrc := src.i32
	for i := 0; i < frameSize; i++ {
		dst[i*dstStride] = dsp.Int24ToRes(shortSrc[i*srcStride+srcChannel])
	}
}

// checkPCM checks that pcm holds frameSize samples of every channel.
func (st *MSEncoder) checkPCM(n, frameSize int) bool {
	return frameSize > 0 && n >= frameSize*st.layout.nbChannels
}

// Encode encodes a frame of 16-bit PCM (opus_multistream_encode). pcm
// holds frameSize samples per channel, interleaved. The other arguments
// and the result are as for Encoder.Encode.
func (st *MSEncoder) Encode(pcm []int16, frameSize int, data []byte) (int, error) {
	if !st.checkPCM(len(pcm), frameSize) {
		return 0, ErrBadArg
	}
	return encodeResult(st.encodeNative(copyChannelInShort, analysisPCM{i16: pcm}, frameSize, data,
		16, downmixInt, false, nil))
}

// Encode24 encodes a frame of 24-bit PCM held in int32 samples
// (opus_multistream_encode24).
func (st *MSEncoder) Encode24(pcm []int32, frameSize int, data []byte) (int, error) {
	if !st.checkPCM(len(pcm), frameSize) {
		return 0, ErrBadArg
	}
	return encodeResult(st.encodeNative(copyChannelInInt24, analysisPCM{i32: pcm}, frameSize, data,
		24, downmixInt24, false, nil))
}

// EncodeFloat encodes a frame of float PCM (opus_multistream_encode_float).
func (st *MSEncoder) EncodeFloat(pcm []float32, frameSize int, data []byte) (int, error) {
	if !st.checkPCM(len(pcm), frameSize) {
		return 0, ErrBadArg
	}
	return encodeResult(st.encodeNative(copyChannelInFloat, analysisPCM{f32: pcm}, frameSize, data,
		24, downmixFloat, true, nil))
}

// Channels returns the number of input channels.
func (st *MSEncoder) Channels() int { return st.layout.nbChannels }

// Streams returns the number of streams.
func (st *MSEncoder) Streams() int { return st.layout.nbStreams }

// CoupledStreams returns the number of coupled (stereo) streams.
func (st *MSEncoder) CoupledStreams() int { return st.layout.nbCoupledStreams }

// Mapping returns a copy of the channel mapping: the coded channel of
// each input channel.
func (st *MSEncoder) Mapping() []byte {
	return append([]byte(nil), st.layout.mapping[:st.layout.nbChannels]...)
}

// SetBitrate sets the total bitrate of all streams in bits per second, or
// Auto or BitrateMax (OPUS_SET_BITRATE). Explicit rates are limited to
// 500 b/s to 750 kb/s per channel.
func (st *MSEncoder) SetBitrate(bitrate int) error {
	value := int32(max(min(bitrate, math.MaxInt32), math.MinInt32))
	if value != Auto && value != BitrateMax {
		if value <= 0 {
			return ErrBadArg
		}
		nc := int32(st.layout.nbChannels)
		value = min(750000*nc, max(500*nc, value))
	}
	st.bitrateBps = value
	return nil
}

// Bitrate returns the sum of the bitrates of the streams
// (OPUS_GET_BITRATE).
func (st *MSEncoder) Bitrate() int {
	value := 0
	for s := range st.encoders {
		value += st.encoders[s].Bitrate()
	}
	return value
}

// FinalRange returns the XOR of the final range coder states of all the
// streams of the last packet (OPUS_GET_FINAL_RANGE).
func (st *MSEncoder) FinalRange() uint32 {
	var value uint32
	for s := range st.encoders {
		value ^= st.encoders[s].FinalRange()
	}
	return value
}

// EncoderState returns the encoder of stream streamID
// (OPUS_MULTISTREAM_GET_ENCODER_STATE).
func (st *MSEncoder) EncoderState(streamID int) (*Encoder, error) {
	if streamID < 0 || streamID >= st.layout.nbStreams {
		return nil, ErrBadArg
	}
	return &st.encoders[streamID], nil
}

// SetExpertFrameDuration sets the frame duration selection
// (OPUS_SET_EXPERT_FRAME_DURATION). As in libopus, the multistream
// encoder does not validate the value; invalid values make Encode fail.
func (st *MSEncoder) SetExpertFrameDuration(duration int) { st.variableDuration = duration }

// ExpertFrameDuration returns the frame duration selection
// (OPUS_GET_EXPERT_FRAME_DURATION).
func (st *MSEncoder) ExpertFrameDuration() int { return st.variableDuration }

// Reset resets the state of all the streams and of the surround analysis
// (OPUS_RESET_STATE).
func (st *MSEncoder) Reset() {
	if st.mappingType == mappingTypeSurround {
		clear(st.preemphMem)
		clear(st.windowMem)
	}
	for s := range st.encoders {
		st.encoders[s].Reset()
	}
}

// forEach applies set to every stream encoder and returns the first
// error, like the int32 SET requests of opus_multistream_encoder_ctl.
func (st *MSEncoder) forEach(set func(enc *Encoder) error) error {
	for s := range st.encoders {
		if err := set(&st.encoders[s]); err != nil {
			return err
		}
	}
	return nil
}

// SetLSBDepth sets the input resolution of all streams (OPUS_SET_LSB_DEPTH).
func (st *MSEncoder) SetLSBDepth(depth int) error {
	return st.forEach(func(e *Encoder) error { return e.SetLSBDepth(depth) })
}

// SetComplexity sets the complexity of all streams (OPUS_SET_COMPLEXITY).
func (st *MSEncoder) SetComplexity(complexity int) error {
	return st.forEach(func(e *Encoder) error { return e.SetComplexity(complexity) })
}

// SetVBR enables or disables VBR on all streams (OPUS_SET_VBR).
func (st *MSEncoder) SetVBR(enabled bool) {
	for s := range st.encoders {
		st.encoders[s].SetVBR(enabled)
	}
}

// SetVBRConstraint enables or disables constrained VBR on all streams
// (OPUS_SET_VBR_CONSTRAINT).
func (st *MSEncoder) SetVBRConstraint(enabled bool) {
	for s := range st.encoders {
		st.encoders[s].SetVBRConstraint(enabled)
	}
}

// SetMaxBandwidth sets the maximum bandwidth of all streams
// (OPUS_SET_MAX_BANDWIDTH).
func (st *MSEncoder) SetMaxBandwidth(bandwidth int) error {
	return st.forEach(func(e *Encoder) error { return e.SetMaxBandwidth(bandwidth) })
}

// SetBandwidth sets the bandwidth of all streams (OPUS_SET_BANDWIDTH).
// Surround encoders override it on every frame.
func (st *MSEncoder) SetBandwidth(bandwidth int) error {
	return st.forEach(func(e *Encoder) error { return e.SetBandwidth(bandwidth) })
}

// SetSignal sets the signal type hint of all streams (OPUS_SET_SIGNAL).
func (st *MSEncoder) SetSignal(signal int) error {
	return st.forEach(func(e *Encoder) error { return e.SetSignal(signal) })
}

// SetApplication sets the application of all streams
// (OPUS_SET_APPLICATION).
func (st *MSEncoder) SetApplication(application int) error {
	return st.forEach(func(e *Encoder) error { return e.SetApplication(application) })
}

// SetInbandFEC sets the in-band FEC mode of all streams
// (OPUS_SET_INBAND_FEC).
func (st *MSEncoder) SetInbandFEC(fec int) error {
	return st.forEach(func(e *Encoder) error { return e.SetInbandFEC(fec) })
}

// SetPacketLossPerc sets the expected packet loss of all streams
// (OPUS_SET_PACKET_LOSS_PERC).
func (st *MSEncoder) SetPacketLossPerc(percent int) error {
	return st.forEach(func(e *Encoder) error { return e.SetPacketLossPerc(percent) })
}

// SetDTX enables or disables DTX on all streams (OPUS_SET_DTX).
func (st *MSEncoder) SetDTX(enabled bool) {
	for s := range st.encoders {
		st.encoders[s].SetDTX(enabled)
	}
}

// SetForceMode forces the coding mode of all streams
// (OPUS_SET_FORCE_MODE).
func (st *MSEncoder) SetForceMode(mode int) error {
	return st.forEach(func(e *Encoder) error { return e.SetForceMode(mode) })
}

// SetForceChannels forces the channel count of all streams
// (OPUS_SET_FORCE_CHANNELS). Values above 1 fail on mono streams.
func (st *MSEncoder) SetForceChannels(channels int) error {
	return st.forEach(func(e *Encoder) error { return e.SetForceChannels(channels) })
}

// SetPredictionDisabled disables prediction on all streams
// (OPUS_SET_PREDICTION_DISABLED).
func (st *MSEncoder) SetPredictionDisabled(disabled bool) {
	for s := range st.encoders {
		st.encoders[s].SetPredictionDisabled(disabled)
	}
}

// SetPhaseInversionDisabled disables phase inversion on all streams
// (OPUS_SET_PHASE_INVERSION_DISABLED).
func (st *MSEncoder) SetPhaseInversionDisabled(disabled bool) {
	for s := range st.encoders {
		st.encoders[s].SetPhaseInversionDisabled(disabled)
	}
}

// The getters below query the first stream, like libopus.

// LSBDepth returns the input resolution (OPUS_GET_LSB_DEPTH).
func (st *MSEncoder) LSBDepth() int { return st.encoders[0].LSBDepth() }

// VBR reports whether VBR is enabled (OPUS_GET_VBR).
func (st *MSEncoder) VBR() bool { return st.encoders[0].VBR() }

// Application returns the application (OPUS_GET_APPLICATION).
func (st *MSEncoder) Application() int { return st.encoders[0].Application() }

// Bandwidth returns the bandwidth of the last frame of the first stream
// (OPUS_GET_BANDWIDTH).
func (st *MSEncoder) Bandwidth() int { return st.encoders[0].Bandwidth() }

// Complexity returns the complexity (OPUS_GET_COMPLEXITY).
func (st *MSEncoder) Complexity() int { return st.encoders[0].Complexity() }

// PacketLossPerc returns the expected packet loss
// (OPUS_GET_PACKET_LOSS_PERC).
func (st *MSEncoder) PacketLossPerc() int { return st.encoders[0].PacketLossPerc() }

// DTX reports whether DTX is enabled (OPUS_GET_DTX).
func (st *MSEncoder) DTX() bool { return st.encoders[0].DTX() }

// VoiceRatio returns the voice ratio (OPUS_GET_VOICE_RATIO).
func (st *MSEncoder) VoiceRatio() int { return st.encoders[0].VoiceRatio() }

// VBRConstraint reports whether constrained VBR is enabled
// (OPUS_GET_VBR_CONSTRAINT).
func (st *MSEncoder) VBRConstraint() bool { return st.encoders[0].VBRConstraint() }

// Signal returns the signal type hint (OPUS_GET_SIGNAL).
func (st *MSEncoder) Signal() int { return st.encoders[0].Signal() }

// Lookahead returns the look-ahead in samples (OPUS_GET_LOOKAHEAD).
func (st *MSEncoder) Lookahead() int { return st.encoders[0].Lookahead() }

// SampleRate returns the sampling rate (OPUS_GET_SAMPLE_RATE).
func (st *MSEncoder) SampleRate() int { return st.encoders[0].SampleRate() }

// InbandFEC returns the in-band FEC setting (OPUS_GET_INBAND_FEC).
func (st *MSEncoder) InbandFEC() int { return st.encoders[0].InbandFEC() }

// ForceChannels returns the forced channel count
// (OPUS_GET_FORCE_CHANNELS).
func (st *MSEncoder) ForceChannels() int { return st.encoders[0].ForceChannels() }

// PredictionDisabled reports whether prediction is disabled
// (OPUS_GET_PREDICTION_DISABLED).
func (st *MSEncoder) PredictionDisabled() bool { return st.encoders[0].PredictionDisabled() }

// PhaseInversionDisabled reports whether phase inversion is disabled
// (OPUS_GET_PHASE_INVERSION_DISABLED).
func (st *MSEncoder) PhaseInversionDisabled() bool { return st.encoders[0].PhaseInversionDisabled() }

// Ported from libopus 1.6.1 src/opus_encoder.c (OpusEncoder,
// opus_encoder_init, opus_encode_native, opus_encode_frame_native,
// opus_encode, opus_encode24, opus_encode_float, opus_encoder_ctl and
// their static helpers). The downmix functions and is_digital_silence
// live in opus_encoder_downmix.go.

package opus

import (
	"math"
	"math/bits"

	"github.com/cirocosta/opus/internal/celt"
	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
	"github.com/cirocosta/opus/internal/silk"
)

const (
	// maxEncoderBuffer is MAX_ENCODER_BUFFER (no ENABLE_QEXT).
	maxEncoderBuffer = 480
	// pseudoSNRThreshold is PSEUDO_SNR_THRESHOLD: 10^(25/10).
	pseudoSNRThreshold float32 = 316.23
	// packetSizeCap is packet_size_cap in opus_encode_native.
	packetSizeCap = 1276
	// maxEncFrameSize is the longest frame that opus_encode_frame_native
	// codes: 60 ms at 48 kHz.
	maxEncFrameSize = 2880

	// SILK constants from silk/define.h and silk/tuning_parameters.h.
	dtxActivityThreshold    float32 = 0.1 // DTX_ACTIVITY_THRESHOLD
	nbSpeechFramesBeforeDTX         = 10  // NB_SPEECH_FRAMES_BEFORE_DTX
	maxConsecutiveDTX               = 20  // MAX_CONSECUTIVE_DTX
	vadNoDecision                   = -1  // VAD_NO_DECISION
	typeNoVoiceActivity             = 0   // TYPE_NO_VOICE_ACTIVITY
	variableHPMinCutoffHz           = 60  // VARIABLE_HP_MIN_CUTOFF_HZ

	// veryRmall is VERY_SMALL.
	verySmall float32 = 1e-30
)

// Fixed-point constants of opus_encoder.c, computed like SILK_FIX_CONST
// (in double precision, or float for float arguments).
var (
	// SILK_FIX_CONST(VARIABLE_HP_SMTH_COEF2, 16), VARIABLE_HP_SMTH_COEF2
	// being 0.015f.
	variableHPSmthCoef2Q16 = fixConstF32(0.015, 16)
	// SILK_FIX_CONST(1.5 * 3.14159 / 1000, 19).
	hpFcCoefQ19 = fixConstF64(fixConstDiv(1.5*3.14159, 1000), 19)
	// SILK_FIX_CONST(0.92, 9).
	hpR092Q9 = fixConstF64(0.92, 9)
	// SILK_FIX_CONST(0.01, 16).
	fec001Q16 = fixConstF64(0.01, 16)
)

// fixConstDiv divides at run time, so that the quotient is rounded to
// double like in C instead of being computed exactly.
func fixConstDiv(a, b float64) float64 { return a / b }

// fixConstF32 is SILK_FIX_CONST for a float constant: the product is
// rounded to float before the double addition.
func fixConstF32(c float32, q uint) int32 {
	return int32(float64(c*float32(int64(1)<<q)) + 0.5)
}

// fixConstF64 is SILK_FIX_CONST for a double constant.
func fixConstF64(c float64, q uint) int32 {
	return int32(float64(c*float64(int64(1)<<q)) + 0.5)
}

// stereoWidthState is StereoWidthState.
type stereoWidthState struct {
	XX, XY, YY    float32
	smoothedWidth float32
	maxFollower   float32
}

// Transition tables for the voice and music. First column is the middle
// (memoriless) threshold. The second column is the hysteresis
// (difference with the middle).
var (
	monoVoiceBandwidthThresholds = [8]int{
		9000, 700, // NB<->MB
		9000, 700, // MB<->WB
		13500, 1000, // WB<->SWB
		14000, 2000, // SWB<->FB
	}
	monoMusicBandwidthThresholds = [8]int{
		9000, 700, // NB<->MB
		9000, 700, // MB<->WB
		11000, 1000, // WB<->SWB
		12000, 2000, // SWB<->FB
	}
	stereoVoiceBandwidthThresholds = [8]int{
		9000, 700, // NB<->MB
		9000, 700, // MB<->WB
		13500, 1000, // WB<->SWB
		14000, 2000, // SWB<->FB
	}
	stereoMusicBandwidthThresholds = [8]int{
		9000, 700, // NB<->MB
		9000, 700, // MB<->WB
		11000, 1000, // WB<->SWB
		12000, 2000, // SWB<->FB
	}
)

// Threshold bit-rates for switching between mono and stereo.
const (
	stereoVoiceThreshold = 19000
	stereoMusicThreshold = 17000
)

// modeThresholds holds the threshold bit-rates for switching between
// SILK/hybrid and CELT-only: [mono, stereo][voice, music].
var modeThresholds = [2][2]int32{
	{64000, 10000}, // mono
	{44000, 10000}, // stereo
}

var fecThresholds = [10]int32{
	12000, 1000, // NB
	14000, 1000, // MB
	16000, 1000, // WB
	20000, 1000, // SWB
	22000, 1000, // FB
}

// Encoder is an Opus encoder (OpusEncoder). Create one with NewEncoder.
// An Encoder is not safe for concurrent use.
type Encoder struct {
	silkMode          silk.EncControl
	application       int
	channels          int
	delayCompensation int
	forceChannels     int
	signalType        int
	userBandwidth     int
	maxBandwidth      int
	userForcedMode    int
	voiceRatio        int
	fs                int32
	useVBR            int
	vbrConstraint     int
	variableDuration  int
	bitrateBps        int32
	userBitrateBps    int32
	lsbDepth          int
	encoderBuffer     int
	lfe               int
	useDTX            int // general DTX for both SILK and CELT
	fecConfig         int
	analysis          tonalityAnalysisState

	// Everything in encoderResetState gets cleared on a reset.
	encoderResetState

	// silkEnc is nil for ApplicationRestrictedCelt and celtEnc is nil
	// for ApplicationRestrictedSilk, like the C pointers.
	silkEnc *silk.Encoder
	celtEnc *celt.Encoder

	scratch *encoderScratch
}

// encoderResetState holds the fields of OpusEncoder from
// OPUS_ENCODER_RESET_START (stream_channels) to the end. As in C, a reset
// also clears the energy mask.
type encoderResetState struct {
	streamChannels       int
	hybridStereoWidthQ14 int16
	variableHPSmth2Q15   int32
	prevHBGain           float32
	hpMem                [4]float32
	mode                 int
	prevMode             int
	prevChannels         int
	prevFramesize        int
	bandwidth            int
	// autoBandwidth is the bandwidth determined automatically from the
	// rate (before any other adjustment).
	autoBandwidth     int
	silkBWSwitch      int
	first             int
	energyMasking     []float32
	widthMem          stereoWidthState
	detectedBandwidth int
	nbNoActivityMsQ1  int
	peakSignalEnergy  float32
	nonfinalFrame     int // current frame is not the final in a packet
	rangeFinal        uint32
	delayBuffer       [maxEncoderBuffer * 2]float32
}

// encoderScratch holds the buffers of the C stack allocations. They do
// not carry state between calls.
type encoderScratch struct {
	in         [2 * 6 * maxEncFrameSize]float32 // opus_encode input conversion (120 ms)
	pcmBuf     [2 * (maxEncFrameSize + 48000/250)]float32
	tmpPrefill [2 * 48000 / 400]float32
	enc        entcode.Encoder
	rp         Repacketizer // encode_multiframe_packet
	padRP      Repacketizer // opus_packet_pad
	padBuf     []byte
	tmpData    []byte
	dummy      [2]byte
}

// validEncoderArgs checks the arguments of opus_encoder_init.
func validEncoderArgs(fs, channels, application int) bool {
	if fs != 48000 && fs != 24000 && fs != 16000 && fs != 12000 && fs != 8000 {
		return false
	}
	if channels != 1 && channels != 2 {
		return false
	}
	switch application {
	case ApplicationVoIP, ApplicationAudio, ApplicationRestrictedLowdelay,
		ApplicationRestrictedSilk, ApplicationRestrictedCelt:
		return true
	}
	return false
}

// NewEncoder allocates and initializes an encoder state
// (opus_encoder_create). fs is the sampling rate of the input signal
// (8000, 12000, 16000, 24000, or 48000 Hz), channels the number of
// channels (1 or 2), and application one of ApplicationVoIP,
// ApplicationAudio, ApplicationRestrictedLowdelay,
// ApplicationRestrictedSilk or ApplicationRestrictedCelt.
func NewEncoder(fs, channels, application int) (*Encoder, error) {
	if !validEncoderArgs(fs, channels, application) {
		return nil, ErrBadArg
	}
	st := new(Encoder)
	if err := st.Init(fs, channels, application); err != nil {
		return nil, err
	}
	return st, nil
}

// Init initializes (or reinitializes) an encoder state
// (opus_encoder_init). The arguments are as for NewEncoder.
func (st *Encoder) Init(fs, channels, application int) error {
	if !validEncoderArgs(fs, channels, application) {
		return ErrBadArg
	}
	silkEnc, celtEnc, scratch := st.silkEnc, st.celtEnc, st.scratch
	// OPUS_CLEAR of the whole state. The sub-encoders are initialized
	// below.
	*st = Encoder{}
	if scratch == nil {
		scratch = new(encoderScratch)
	}
	st.scratch = scratch

	st.streamChannels = channels
	st.channels = channels
	st.fs = int32(fs)

	if application != ApplicationRestrictedCelt {
		if silkEnc == nil {
			silkEnc = new(silk.Encoder)
		}
		st.silkEnc = silkEnc
		if ret := st.silkEnc.Init(st.channels, &st.silkMode); ret != 0 {
			return ErrInternalError
		}
	}

	// Default SILK parameters.
	st.silkMode.NChannelsAPI = int32(channels)
	st.silkMode.NChannelsInternal = int32(channels)
	st.silkMode.APISampleRate = st.fs
	st.silkMode.MaxInternalSampleRate = 16000
	st.silkMode.MinInternalSampleRate = 8000
	st.silkMode.DesiredInternalSampleRate = 16000
	st.silkMode.PayloadSizeMs = 20
	st.silkMode.BitRate = 25000
	st.silkMode.PacketLossPercentage = 0
	st.silkMode.Complexity = 9
	st.silkMode.UseInBandFEC = 0
	st.silkMode.UseDRED = 0
	st.silkMode.UseDTX = 0
	st.silkMode.UseCBR = 0
	st.silkMode.ReducedDependency = 0

	// Initialize CELT encoder.
	if application != ApplicationRestrictedSilk {
		if celtEnc == nil {
			celtEnc = new(celt.Encoder)
		}
		st.celtEnc = celtEnc
		if ret := st.celtEnc.Init(st.fs, channels); ret != celt.OK {
			return ErrInternalError
		}
		st.celtEnc.SetSignalling(0)
		st.celtEnc.SetComplexity(st.silkMode.Complexity)
	}

	st.useVBR = 1
	// Makes constrained VBR the default (safer for real-time use).
	st.vbrConstraint = 1
	st.userBitrateBps = Auto
	st.bitrateBps = 3000 + st.fs*int32(channels)
	st.application = application
	st.signalType = Auto
	st.userBandwidth = Auto
	st.maxBandwidth = BandwidthFullband
	st.forceChannels = Auto
	st.userForcedMode = Auto
	st.voiceRatio = -1
	if application != ApplicationRestrictedCelt && application != ApplicationRestrictedSilk {
		st.encoderBuffer = int(st.fs / 100)
	} else {
		st.encoderBuffer = 0
	}
	st.lsbDepth = 24
	st.variableDuration = FramesizeArg

	// Delay compensation of 4 ms (2.5 ms for SILK's extra look-ahead +
	// 1.5 ms for SILK resamplers and stereo prediction).
	st.delayCompensation = int(st.fs / 250)

	st.hybridStereoWidthQ14 = 1 << 14
	st.prevHBGain = 1
	st.variableHPSmth2Q15 = silkLin2log(variableHPMinCutoffHz) << 8
	st.first = 1
	st.mode = modeHybrid
	st.bandwidth = BandwidthFullband

	tonalityAnalysisInit(&st.analysis, st.fs)
	st.analysis.application = st.application
	return nil
}

// genTOC is gen_toc.
func genTOC(mode, framerate, bandwidth, channels int) byte {
	var toc byte
	period := 0
	for framerate < 400 {
		framerate <<= 1
		period++
	}
	if mode == modeSilkOnly {
		toc = byte((bandwidth - BandwidthNarrowband) << 5)
		toc |= byte((period - 2) << 3)
	} else if mode == modeCeltOnly {
		tmp := bandwidth - BandwidthMediumband
		if tmp < 0 {
			tmp = 0
		}
		toc = 0x80
		toc |= byte(tmp << 5)
		toc |= byte(period << 3)
	} else { // Hybrid
		toc = 0x60
		toc |= byte((bandwidth - BandwidthSuperwideband) << 4)
		toc |= byte((period - 2) << 3)
	}
	if channels == 2 {
		toc |= 1 << 2
	}
	return toc
}

// silkBiquadRes is the float build of silk_biquad_res: a second order
// ARMA filter over len samples of in, stride apart.
func silkBiquadRes(in []float32, bQ28 *[3]int32, aQ28 *[2]int32, s []float32, out []float32, length, stride int) {
	var a [2]float32
	var b [3]float32
	a[0] = float32(aQ28[0]) * (1.0 / (1 << 28))
	a[1] = float32(aQ28[1]) * (1.0 / (1 << 28))
	b[0] = float32(bQ28[0]) * (1.0 / (1 << 28))
	b[1] = float32(bQ28[1]) * (1.0 / (1 << 28))
	b[2] = float32(bQ28[2]) * (1.0 / (1 << 28))

	for k := 0; k < length; k++ {
		inval := in[k*stride]
		vout := s[0] + float32(b[0]*inval)

		s[0] = s[1] - float32(vout*a[0]) + float32(b[1]*inval)

		s[1] = float32(-vout*a[1]) + float32(b[2]*inval) + verySmall

		out[k*stride] = vout
	}
}

// hpCutoff is hp_cutoff.
func hpCutoff(in []float32, cutoffHz int32, out []float32, hpMem []float32, length, channels int, fs int32) {
	var bQ28 [3]int32
	var aQ28 [2]int32

	fcQ19 := silkSMULBB(hpFcCoefQ19, cutoffHz) / (fs / 1000)

	rQ28 := int32(1<<28) - hpR092Q9*fcQ19

	// b = r * [ 1; -2; 1 ];
	// a = [ 1; -2 * r * ( 1 - 0.5 * Fc^2 ); r^2 ];
	bQ28[0] = rQ28
	bQ28[1] = -rQ28 << 1
	bQ28[2] = rQ28

	// -r * ( 2 - Fc * Fc );
	rQ22 := rQ28 >> 6
	aQ28[0] = silkSMULWW(rQ22, silkSMULWW(fcQ19, fcQ19)-(2<<22))
	aQ28[1] = silkSMULWW(rQ22, rQ22)

	silkBiquadRes(in, &bQ28, &aQ28, hpMem, out, length, channels)
	if channels == 2 {
		silkBiquadRes(in[1:], &bQ28, &aQ28, hpMem[2:], out[1:], length, channels)
	}
}

// dcReject is the float build of dc_reject.
func dcReject(in []float32, cutoffHz int32, out []float32, hpMem []float32, length, channels int, fs int32) {
	coef := float32(float32(6.3)*float32(cutoffHz)) / float32(fs)
	coef2 := 1 - coef
	if channels == 2 {
		m0 := hpMem[0]
		m2 := hpMem[2]
		for i := 0; i < length; i++ {
			x0 := in[2*i+0]
			x1 := in[2*i+1]
			out0 := x0 - m0
			out1 := x1 - m2
			m0 = float32(coef*x0) + verySmall + float32(coef2*m0)
			m2 = float32(coef*x1) + verySmall + float32(coef2*m2)
			out[2*i+0] = out0
			out[2*i+1] = out1
		}
		hpMem[0] = m0
		hpMem[2] = m2
	} else {
		m0 := hpMem[0]
		for i := 0; i < length; i++ {
			x := in[i]
			y := x - m0
			m0 = float32(coef*x) + verySmall + float32(coef2*m0)
			out[i] = y
		}
		hpMem[0] = m0
	}
}

// stereoFade is stereo_fade. out may alias in.
func stereoFade(in, out []float32, g1, g2 float32, overlap48, frameSize, channels int, window []float32, fs int32) {
	inc := max(1, int(48000/fs))
	overlap := overlap48 / inc
	g1 = 1 - g1
	g2 = 1 - g2
	i := 0
	for ; i < overlap; i++ {
		w := window[i*inc]
		w = float32(w * w)
		g := float32(w*g2) + float32((1-w)*g1)
		diff := float32(.5 * (in[i*channels] - in[i*channels+1]))
		diff = float32(g * diff)
		out[i*channels] = out[i*channels] - diff
		out[i*channels+1] = out[i*channels+1] + diff
	}
	for ; i < frameSize; i++ {
		diff := float32(.5 * (in[i*channels] - in[i*channels+1]))
		diff = float32(g2 * diff)
		out[i*channels] = out[i*channels] - diff
		out[i*channels+1] = out[i*channels+1] + diff
	}
}

// gainFade is gain_fade. out may alias in.
func gainFade(in, out []float32, g1, g2 float32, overlap48, frameSize, channels int, window []float32, fs int32) {
	inc := max(1, int(48000/fs))
	overlap := overlap48 / inc
	if channels == 1 {
		for i := 0; i < overlap; i++ {
			w := window[i*inc]
			w = float32(w * w)
			g := float32(w*g2) + float32((1-w)*g1)
			out[i] = float32(g * in[i])
		}
	} else {
		for i := 0; i < overlap; i++ {
			w := window[i*inc]
			w = float32(w * w)
			g := float32(w*g2) + float32((1-w)*g1)
			out[i*2] = float32(g * in[i*2])
			out[i*2+1] = float32(g * in[i*2+1])
		}
	}
	for c := 0; c < channels; c++ {
		for i := overlap; i < frameSize; i++ {
			out[i*channels+c] = float32(g2 * in[i*channels+c])
		}
	}
}

// bitsToBitrate is bits_to_bitrate.
func bitsToBitrate(bits, fs, frameSize int32) int32 {
	return bits * (6 * fs / frameSize) / 6
}

// bitrateToBits is bitrate_to_bits.
func bitrateToBits(bitrate, fs, frameSize int32) int32 {
	return bitrate * 6 / (6 * fs / frameSize)
}

// userBitrateToBitrate is user_bitrate_to_bitrate.
func (st *Encoder) userBitrateToBitrate(frameSize, maxDataBytes int) int32 {
	var userBitrate int32
	if frameSize == 0 {
		frameSize = int(st.fs / 400)
	}
	maxBitrate := bitsToBitrate(int32(maxDataBytes*8), st.fs, int32(frameSize))
	if st.userBitrateBps == Auto {
		userBitrate = 60*st.fs/int32(frameSize) + st.fs*int32(st.channels)
	} else if st.userBitrateBps == BitrateMax {
		userBitrate = 1500000
	} else {
		userBitrate = st.userBitrateBps
	}
	return min(userBitrate, maxBitrate)
}

// frameSizeSelect is frame_size_select.
func frameSizeSelect(application, frameSize, variableDuration int, fs int32) int {
	var newSize int
	f := int(fs)
	if frameSize < f/400 {
		return -1
	}
	if variableDuration == FramesizeArg {
		newSize = frameSize
	} else if variableDuration >= Framesize2_5ms && variableDuration <= Framesize120ms {
		if variableDuration <= Framesize40ms {
			newSize = (f / 400) << (variableDuration - Framesize2_5ms)
		} else {
			newSize = (variableDuration - Framesize2_5ms - 2) * f / 50
		}
	} else {
		return -1
	}
	if newSize > frameSize {
		return -1
	}
	if 400*newSize != f && 200*newSize != f && 100*newSize != f &&
		50*newSize != f && 25*newSize != f && 50*newSize != 3*f &&
		50*newSize != 4*f && 50*newSize != 5*f && 50*newSize != 6*f {
		return -1
	}
	if application == ApplicationRestrictedSilk && newSize < f/100 {
		return -1
	}
	return newSize
}

// computeStereoWidth is compute_stereo_width (float build).
func computeStereoWidth(pcm []float32, frameSize int, fs int32, mem *stereoWidthState) float32 {
	var xx, xy, yy float32
	frameRate := int(fs) / frameSize
	shortAlpha := float32(25) / float32(max(50, frameRate))
	// Unroll by 4. The frame size is always a multiple of 4 *except* for
	// 2.5 ms frames at 12 kHz. Since this setting is very rare (and very
	// stupid), we just discard the last two samples.
	for i := 0; i < frameSize-3; i += 4 {
		var pxx, pxy, pyy float32
		x := pcm[2*i]
		y := pcm[2*i+1]
		pxx = float32(x * x)
		pxy = float32(x * y)
		pyy = float32(y * y)
		x = pcm[2*i+2]
		y = pcm[2*i+3]
		pxx += float32(x * x)
		pxy += float32(x * y)
		pyy += float32(y * y)
		x = pcm[2*i+4]
		y = pcm[2*i+5]
		pxx += float32(x * x)
		pxy += float32(x * y)
		pyy += float32(y * y)
		x = pcm[2*i+6]
		y = pcm[2*i+7]
		pxx += float32(x * x)
		pxy += float32(x * y)
		pyy += float32(y * y)

		xx += pxx
		xy += pxy
		yy += pyy
	}
	if !(xx < 1e9) || dsp.CeltIsnan(xx) || !(yy < 1e9) || dsp.CeltIsnan(yy) {
		xy = 0
		xx = 0
		yy = 0
	}
	mem.XX += float32(shortAlpha * (xx - mem.XX))
	// Rewritten to avoid overflows on abrupt sign change.
	mem.XY = float32((1-shortAlpha)*mem.XY) + float32(shortAlpha*xy)
	mem.YY += float32(shortAlpha * (yy - mem.YY))
	mem.XX = dsp.Max32(0, mem.XX)
	mem.XY = dsp.Max32(0, mem.XY)
	mem.YY = dsp.Max32(0, mem.YY)
	if dsp.Max32(mem.XX, mem.YY) > 8e-4 {
		sqrtXX := dsp.CeltSqrt(mem.XX)
		sqrtYY := dsp.CeltSqrt(mem.YY)
		qrrtXX := dsp.CeltSqrt(sqrtXX)
		qrrtYY := dsp.CeltSqrt(sqrtYY)
		// Inter-channel correlation.
		mem.XY = dsp.Min32(mem.XY, float32(sqrtXX*sqrtYY))
		corr := dsp.FracDiv32(mem.XY, 1e-15+float32(sqrtXX*sqrtYY))
		// Approximate loudness difference.
		ldiff := float32(1*dsp.Abs16(qrrtXX-qrrtYY)) / (1e-15 + qrrtXX + qrrtYY)
		width := float32(dsp.Min16(1, dsp.CeltSqrt(1-float32(corr*corr))) * ldiff)
		// Smoothing over one second.
		mem.smoothedWidth += (width - mem.smoothedWidth) / float32(frameRate)
		// Peak follower.
		mem.maxFollower = dsp.Max16(mem.maxFollower-float32(.02)/float32(frameRate), mem.smoothedWidth)
	}
	return dsp.Min32(1, float32(20*mem.maxFollower))
}

// decideFEC is decide_fec.
func decideFEC(useInBandFEC, packetLossPerc, lastFEC, mode int, bandwidth *int, rate int32) int {
	if useInBandFEC == 0 || packetLossPerc == 0 || mode == modeCeltOnly {
		return 0
	}
	origBandwidth := *bandwidth
	for {
		// Compute threshold for using FEC at the current bandwidth
		// setting.
		lbrrRateThresBps := fecThresholds[2*(*bandwidth-BandwidthNarrowband)]
		hysteresis := fecThresholds[2*(*bandwidth-BandwidthNarrowband)+1]
		if lastFEC == 1 {
			lbrrRateThresBps -= hysteresis
		}
		if lastFEC == 0 {
			lbrrRateThresBps += hysteresis
		}
		lbrrRateThresBps = silkSMULWB(lbrrRateThresBps*int32(125-min(packetLossPerc, 25)), fec001Q16)
		// If loss <= 5%, we look at whether we have enough rate to
		// enable FEC. If loss > 5%, we decrease the bandwidth until we
		// can enable FEC.
		if rate > lbrrRateThresBps {
			return 1
		} else if packetLossPerc <= 5 {
			return 0
		} else if *bandwidth > BandwidthNarrowband {
			*bandwidth--
		} else {
			break
		}
	}
	// Couldn't find any bandwidth to enable FEC, keep original
	// bandwidth.
	*bandwidth = origBandwidth
	return 0
}

// silkRateTable is rate_table of compute_silk_rate_for_hybrid.
var silkRateTable = [7][5]int{
	//  |total| |-------- SILK------------|
	//          |-- No FEC -| |--- FEC ---|
	//           10ms   20ms   10ms   20ms
	{0, 0, 0, 0, 0},
	{12000, 10000, 10000, 11000, 11000},
	{16000, 13500, 13500, 15000, 15000},
	{20000, 16000, 16000, 18000, 18000},
	{24000, 18000, 18000, 21000, 21000},
	{32000, 22000, 22000, 28000, 28000},
	{64000, 38000, 38000, 50000, 50000},
}

// computeSilkRateForHybrid is compute_silk_rate_for_hybrid.
func computeSilkRateForHybrid(rate, bandwidth, frame20ms, vbr, fec, channels int) int {
	var silkRate int
	// Do the allocation per-channel.
	rate /= channels
	entry := 1 + frame20ms + 2*fec
	N := len(silkRateTable)
	i := 1
	for ; i < N; i++ {
		if silkRateTable[i][0] > rate {
			break
		}
	}
	if i == N {
		silkRate = silkRateTable[i-1][entry]
		// For now, just give 50% of the extra bits to SILK.
		silkRate += (rate - silkRateTable[i-1][0]) / 2
	} else {
		lo := int32(silkRateTable[i-1][entry])
		hi := int32(silkRateTable[i][entry])
		x0 := int32(silkRateTable[i-1][0])
		x1 := int32(silkRateTable[i][0])
		silkRate = int((lo*(x1-int32(rate)) + hi*(int32(rate)-x0)) / (x1 - x0))
	}
	if vbr == 0 {
		// Tiny boost to SILK for CBR. We should probably tune this
		// better.
		silkRate += 100
	}
	if bandwidth == BandwidthSuperwideband {
		silkRate += 300
	}
	silkRate *= channels
	// Small adjustment for stereo (calibrated for 32 kb/s, haven't tried
	// other bitrates).
	if channels == 2 && rate >= 12000 {
		silkRate -= 1000
	}
	return silkRate
}

// computeEquivRate is compute_equiv_rate: the equivalent bitrate
// corresponding to 20 ms frames, complexity 10 VBR operation.
func computeEquivRate(bitrate int32, channels, frameRate, vbr, mode, complexity, loss int) int32 {
	equiv := bitrate
	// Take into account overhead from smaller frames.
	if frameRate > 50 {
		equiv -= int32((40*channels + 20) * (frameRate - 50))
	}
	// CBR is about a 8% penalty for both SILK and CELT.
	if vbr == 0 {
		equiv -= equiv / 12
	}
	// Complexity makes about 10% difference (from 0 to 10) in general.
	equiv = equiv * int32(90+complexity) / 100
	if mode == modeSilkOnly || mode == modeHybrid {
		// SILK complexity 0-1 uses the non-delayed-decision NSQ, which
		// costs about 20%.
		if complexity < 2 {
			equiv = equiv * 4 / 5
		}
		equiv -= equiv * int32(loss) / int32(6*loss+10)
	} else if mode == modeCeltOnly {
		// CELT complexity 0-4 doesn't have the pitch filter, which costs
		// about 10%.
		if complexity < 5 {
			equiv = equiv * 9 / 10
		}
	} else {
		// Mode not known yet. Half the SILK loss.
		equiv -= equiv * int32(loss) / int32(12*loss+20)
	}
	return equiv
}

// computeFrameEnergy is compute_frame_energy (float build).
func computeFrameEnergy(pcm []float32, frameSize, channels int) float32 {
	length := frameSize * channels
	return dsp.CeltInnerProd(pcm, pcm, length) / float32(length)
}

// decideDTXMode is decide_dtx_mode: it reports whether DTX should be
// turned on. activity tells whether this frame contains speech/music,
// nbNoActivityMsQ1 counts the consecutive milliseconds (Q1) with no
// activity and frameSizeMsQ1 is the update length in milliseconds (Q1).
func decideDTXMode(activity int, nbNoActivityMsQ1 *int, frameSizeMsQ1 int) bool {
	if activity == 0 {
		// The number of consecutive DTX frames should be within the
		// allowed bounds. Note that the allowed bound is defined in the
		// SILK headers and assumes 20 ms frames. As this function can be
		// called with any frame length, a conversion to milliseconds is
		// done before the comparisons.
		*nbNoActivityMsQ1 += frameSizeMsQ1
		if *nbNoActivityMsQ1 > nbSpeechFramesBeforeDTX*20*2 {
			if *nbNoActivityMsQ1 <= (nbSpeechFramesBeforeDTX+maxConsecutiveDTX)*20*2 {
				// Valid frame for DTX!
				return true
			}
			*nbNoActivityMsQ1 = nbSpeechFramesBeforeDTX * 20 * 2
		}
	} else {
		*nbNoActivityMsQ1 = 0
	}
	return false
}

// computeRedundancyBytes is compute_redundancy_bytes.
func computeRedundancyBytes(maxDataBytes int, bitrateBps int32, frameRate, channels int) int {
	baseBits := int32(40*channels + 20)

	// Equivalent rate for 5 ms frames.
	redundancyRate := bitrateBps + baseBits*int32(200-frameRate)
	// For VBR, further increase the bitrate if we can afford it. It's
	// pretty short and we'll avoid artefacts.
	redundancyRate = 3 * redundancyRate / 2
	redundancyBytes := int(redundancyRate / 1600)

	// Compute the max rate we can use given CBR or VBR with cap.
	availableBits := int32(maxDataBytes)*8 - 2*baseBits
	redundancyBytesCap := int((availableBits*240/int32(240+48000/frameRate) + baseBits) / 8)
	redundancyBytes = min(redundancyBytes, redundancyBytesCap)
	// It we can't get enough bits for redundancy to be worth it, rely on
	// the decoder PLC.
	if redundancyBytes > 4+8*channels {
		redundancyBytes = min(257, redundancyBytes)
	} else {
		redundancyBytes = 0
	}
	return redundancyBytes
}

// padPacket is opus_packet_pad on the scratch repacketizer.
func (st *Encoder) padPacket(data []byte, length, newLen int) int {
	sc := st.scratch
	ret := packetPadScratch(data, int32(length), int32(newLen), true, nil, &sc.padRP, st.padScratch(length))
	if ret > 0 {
		return OK
	}
	return int(ret)
}

// encodeNative is opus_encode_native. pcm holds frameSize samples per
// channel (opus_res) and data[:outDataBytes] receives the packet.
// analysisPCM, analysisSize, c1, c2, analysisChannels and downmix describe
// the input of the tonality analysis. It returns the packet length or a
// negative error code.
func (st *Encoder) encodeNative(pcm []float32, frameSize int, data []byte, outDataBytes int, lsbDepth int,
	analysisPCM analysisPCM, analysisSize, c1, c2, analysisChannels int, downmix downmixFunc, floatAPI bool) int {
	var (
		ret         int
		prefill     int
		redundancy  int
		celtToSilk  int
		toCelt      int
		voiceEst    int // probability of voice in Q7
		equivRate   int32
		cbrBytes    = -1
		stereoWidth float32
		celtMode    *celt.Mode
		info        analysisInfo
	)
	analysisReadPosBak := -1
	analysisReadSubframeBak := -1

	// Just avoid insane packet sizes here, but the real bounds are
	// applied later on.
	maxDataBytes := min(packetSizeCap*6, outDataBytes)

	st.rangeFinal = 0
	if frameSize <= 0 || maxDataBytes <= 0 {
		return int(ErrBadArg)
	}

	// Cannot encode 100 ms in 1 byte.
	if maxDataBytes == 1 && int(st.fs) == frameSize*10 {
		return int(ErrBufferTooSmall)
	}

	lsbDepth = min(lsbDepth, st.lsbDepth)

	if st.application != ApplicationRestrictedSilk {
		celtMode = st.celtEnc.Mode()
	}
	isSilence := isDigitalSilence(pcm, frameSize, st.channels, lsbDepth)
	info.Valid = 0
	if st.silkMode.Complexity >= 7 && st.fs >= 16000 && st.fs <= 48000 && st.application != ApplicationRestrictedSilk {
		analysisReadPosBak = st.analysis.readPos
		analysisReadSubframeBak = st.analysis.readSubframe
		runAnalysis(&st.analysis, celtMode, analysisPCM, analysisSize, frameSize,
			c1, c2, analysisChannels, st.fs, lsbDepth, downmix, &info)
	} else if st.analysis.initialized {
		tonalityAnalysisReset(&st.analysis)
	}

	// Reset voice_ratio if this frame is not silent or if analysis is
	// disabled. Otherwise, preserve voice_ratio from the last non-silent
	// frame.
	if !isSilence {
		st.voiceRatio = -1
	}
	st.detectedBandwidth = 0
	if info.Valid != 0 {
		if st.signalType == Auto {
			var prob float32
			if st.prevMode == 0 {
				prob = info.MusicProb
			} else if st.prevMode == modeCeltOnly {
				prob = info.MusicProbMax
			} else {
				prob = info.MusicProbMin
			}
			st.voiceRatio = int(math.Floor(.5 + float64(float32(100*float32(1-prob)))))
		}

		analysisBandwidth := info.Bandwidth
		if analysisBandwidth <= 12 {
			st.detectedBandwidth = BandwidthNarrowband
		} else if analysisBandwidth <= 14 {
			st.detectedBandwidth = BandwidthMediumband
		} else if analysisBandwidth <= 16 {
			st.detectedBandwidth = BandwidthWideband
		} else if analysisBandwidth <= 18 {
			st.detectedBandwidth = BandwidthSuperwideband
		} else {
			st.detectedBandwidth = BandwidthFullband
		}
	}

	// Track the peak signal energy.
	if info.Valid == 0 || info.ActivityProbability > dtxActivityThreshold {
		if !isSilence {
			st.peakSignalEnergy = dsp.Max32(float32(0.999*st.peakSignalEnergy),
				computeFrameEnergy(pcm, frameSize, st.channels))
		}
	}
	if st.channels == 2 && st.forceChannels != 1 {
		stereoWidth = computeStereoWidth(pcm, frameSize, st.fs, &st.widthMem)
	} else {
		stereoWidth = 0
	}
	st.bitrateBps = st.userBitrateToBitrate(frameSize, maxDataBytes)

	frameRate := int(st.fs) / frameSize
	if st.useVBR == 0 {
		cbrBytes = min(int((bitrateToBits(st.bitrateBps, st.fs, int32(frameSize))+4)/8), maxDataBytes)
		st.bitrateBps = bitsToBitrate(int32(cbrBytes*8), st.fs, int32(frameSize))
		// Make sure we provide at least one byte to avoid failing.
		maxDataBytes = max(1, cbrBytes)
	}
	if maxDataBytes < 3 || st.bitrateBps < int32(3*frameRate*8) ||
		(frameRate < 50 && (maxDataBytes*frameRate < 300 || st.bitrateBps < 2400)) {
		// If the space is too low to do something useful, emit 'PLC'
		// frames.
		tocmode := st.mode
		bw := st.bandwidth
		if bw == 0 {
			bw = BandwidthNarrowband
		}
		packetCode := 0
		numMultiframes := 0

		if tocmode == 0 {
			tocmode = modeSilkOnly
		}
		if frameRate > 100 {
			tocmode = modeCeltOnly
		}
		// 40 ms -> 2 x 20 ms if in CELT_ONLY or HYBRID mode.
		if frameRate == 25 && tocmode != modeSilkOnly {
			frameRate = 50
			packetCode = 1
		}

		// >= 60 ms frames.
		if frameRate <= 16 {
			// 1 x 60 ms, 2 x 40 ms, 2 x 60 ms.
			if outDataBytes == 1 || (tocmode == modeSilkOnly && frameRate != 10) {
				tocmode = modeSilkOnly

				packetCode = b2i(frameRate <= 12)
				if frameRate == 12 {
					frameRate = 25
				} else {
					frameRate = 16
				}
			} else {
				numMultiframes = 50 / frameRate
				frameRate = 50
				packetCode = 3
			}
		}

		if tocmode == modeSilkOnly && bw > BandwidthWideband {
			bw = BandwidthWideband
		} else if tocmode == modeCeltOnly && bw == BandwidthMediumband {
			bw = BandwidthNarrowband
		} else if tocmode == modeHybrid && bw <= BandwidthSuperwideband {
			bw = BandwidthSuperwideband
		}

		data[0] = genTOC(tocmode, frameRate, bw, st.streamChannels)
		data[0] |= byte(packetCode)

		ret = 1
		if packetCode > 1 {
			ret = 2
		}

		maxDataBytes = max(maxDataBytes, ret)

		if packetCode == 3 {
			data[1] = byte(numMultiframes)
		}

		if st.useVBR == 0 {
			ret = st.padPacket(data, ret, maxDataBytes)
			if ret == OK {
				ret = maxDataBytes
			} else {
				ret = int(ErrInternalError)
			}
		}
		return ret
	}
	maxRate := bitsToBitrate(int32(maxDataBytes*8), st.fs, int32(frameSize))

	// Equivalent 20-ms rate for mode/channel/bandwidth decisions.
	equivRate = computeEquivRate(st.bitrateBps, st.channels, int(st.fs)/frameSize,
		st.useVBR, 0, st.silkMode.Complexity, st.silkMode.PacketLossPercentage)

	if st.signalType == SignalVoice {
		voiceEst = 127
	} else if st.signalType == SignalMusic {
		voiceEst = 0
	} else if st.voiceRatio >= 0 {
		voiceEst = st.voiceRatio * 327 >> 8
		// For AUDIO, never be more than 90% confident of having speech.
		if st.application == ApplicationAudio {
			voiceEst = min(voiceEst, 115)
		}
	} else if st.application == ApplicationVoIP {
		voiceEst = 115
	} else {
		voiceEst = 48
	}

	if st.forceChannels != Auto && st.channels == 2 {
		st.streamChannels = st.forceChannels
	} else {
		// Rate-dependent mono-stereo decision.
		if st.channels == 2 {
			stereoThreshold := int32(stereoMusicThreshold + ((voiceEst * voiceEst * (stereoVoiceThreshold - stereoMusicThreshold)) >> 14))
			if st.streamChannels == 2 {
				stereoThreshold -= 1000
			} else {
				stereoThreshold += 1000
			}
			if equivRate > stereoThreshold {
				st.streamChannels = 2
			} else {
				st.streamChannels = 1
			}
		} else {
			st.streamChannels = st.channels
		}
	}
	// Update equivalent rate for channels decision.
	equivRate = computeEquivRate(st.bitrateBps, st.streamChannels, int(st.fs)/frameSize,
		st.useVBR, 0, st.silkMode.Complexity, st.silkMode.PacketLossPercentage)

	// Allow SILK DTX if DTX is enabled but the generalized DTX cannot be
	// used, e.g. because of the complexity setting or sample rate.
	st.silkMode.UseDTX = b2i(st.useDTX != 0 && !(info.Valid != 0 || isSilence))

	// Mode selection depending on application and signal type.
	if st.application == ApplicationRestrictedSilk {
		st.mode = modeSilkOnly
	} else if st.application == ApplicationRestrictedLowdelay || st.application == ApplicationRestrictedCelt {
		st.mode = modeCeltOnly
	} else if st.userForcedMode == Auto {
		// Interpolate based on stereo width.
		modeVoice := int32(float32((1-stereoWidth)*float32(modeThresholds[0][0])) +
			float32(stereoWidth*float32(modeThresholds[1][0])))
		modeMusic := int32(float32((1-stereoWidth)*float32(modeThresholds[1][1])) +
			float32(stereoWidth*float32(modeThresholds[1][1])))
		// Interpolate based on speech/music probability.
		threshold := modeMusic + int32((voiceEst*voiceEst*int(modeVoice-modeMusic))>>14)
		// Bias towards SILK for VoIP because of some useful features.
		if st.application == ApplicationVoIP {
			threshold += 8000
		}

		// Hysteresis.
		if st.prevMode == modeCeltOnly {
			threshold -= 4000
		} else if st.prevMode > 0 {
			threshold += 4000
		}

		if equivRate >= threshold {
			st.mode = modeCeltOnly
		} else {
			st.mode = modeSilkOnly
		}

		// When FEC is enabled and there's enough packet loss, use SILK.
		// Unless the FEC is set to 2, in which case we don't switch to
		// SILK if we're confident we have music.
		if st.silkMode.UseInBandFEC != 0 && st.silkMode.PacketLossPercentage > (128-voiceEst)>>4 &&
			(st.fecConfig != 2 || voiceEst > 25) {
			st.mode = modeSilkOnly
		}
		// When encoding voice and DTX is enabled but the generalized DTX
		// cannot be used, use SILK in order to make use of its DTX.
		if st.silkMode.UseDTX != 0 && voiceEst > 100 {
			st.mode = modeSilkOnly
		}

		// If max_data_bytes represents less than 6 kb/s, switch to
		// CELT-only mode.
		rate := int32(6000)
		if frameRate > 50 {
			rate = 9000
		}
		if int32(maxDataBytes) < bitrateToBits(rate, st.fs, int32(frameSize))/8 {
			st.mode = modeCeltOnly
		}
	} else {
		st.mode = st.userForcedMode
	}

	// Override the chosen mode to make sure we meet the requested frame
	// size.
	if st.mode != modeCeltOnly && frameSize < int(st.fs/100) {
		st.mode = modeCeltOnly
	}
	if st.lfe != 0 && st.application != ApplicationRestrictedSilk {
		st.mode = modeCeltOnly
	}

	if st.prevMode > 0 &&
		((st.mode != modeCeltOnly && st.prevMode == modeCeltOnly) ||
			(st.mode == modeCeltOnly && st.prevMode != modeCeltOnly)) {
		redundancy = 1
		celtToSilk = b2i(st.mode != modeCeltOnly)
		if celtToSilk == 0 {
			// Switch to SILK/hybrid if frame size is 10 ms or more.
			if frameSize >= int(st.fs/100) {
				st.mode = st.prevMode
				toCelt = 1
			} else {
				redundancy = 0
			}
		}
	}

	// When encoding multiframes, we can ask for a switch to CELT only in
	// the last frame. This switch is processed above as the requested
	// mode shouldn't interrupt stereo->mono transition.
	if st.streamChannels == 1 && st.prevChannels == 2 && st.silkMode.ToMono == 0 &&
		st.mode != modeCeltOnly && st.prevMode != modeCeltOnly {
		// Delay stereo->mono transition by two frames so that SILK can do
		// a smooth downmix.
		st.silkMode.ToMono = 1
		st.streamChannels = 2
	} else {
		st.silkMode.ToMono = 0
	}

	// Update equivalent rate with mode decision.
	equivRate = computeEquivRate(st.bitrateBps, st.streamChannels, int(st.fs)/frameSize,
		st.useVBR, st.mode, st.silkMode.Complexity, st.silkMode.PacketLossPercentage)

	if st.mode != modeCeltOnly && st.prevMode == modeCeltOnly {
		var dummy silk.EncControl
		st.silkEnc.Init(st.channels, &dummy)
		prefill = 1
	}

	// Automatic (rate-dependent) bandwidth selection.
	if st.mode == modeCeltOnly || st.first != 0 || st.silkMode.AllowBandwidthSwitch != 0 {
		var voiceBandwidthThresholds, musicBandwidthThresholds *[8]int
		var bandwidthThresholds [8]int
		bandwidth := BandwidthFullband

		if st.channels == 2 && st.forceChannels != 1 {
			voiceBandwidthThresholds = &stereoVoiceBandwidthThresholds
			musicBandwidthThresholds = &stereoMusicBandwidthThresholds
		} else {
			voiceBandwidthThresholds = &monoVoiceBandwidthThresholds
			musicBandwidthThresholds = &monoMusicBandwidthThresholds
		}
		// Interpolate bandwidth thresholds depending on voice estimation.
		for i := 0; i < 8; i++ {
			bandwidthThresholds[i] = musicBandwidthThresholds[i] +
				((voiceEst * voiceEst * (voiceBandwidthThresholds[i] - musicBandwidthThresholds[i])) >> 14)
		}
		for {
			threshold := bandwidthThresholds[2*(bandwidth-BandwidthMediumband)]
			hysteresis := bandwidthThresholds[2*(bandwidth-BandwidthMediumband)+1]
			if st.first == 0 {
				if st.autoBandwidth >= bandwidth {
					threshold -= hysteresis
				} else {
					threshold += hysteresis
				}
			}
			if equivRate >= int32(threshold) {
				break
			}
			bandwidth--
			if bandwidth <= BandwidthNarrowband {
				break
			}
		}
		// We don't use mediumband anymore, except when explicitly
		// requested or during mode transitions.
		if bandwidth == BandwidthMediumband {
			bandwidth = BandwidthWideband
		}
		st.bandwidth = bandwidth
		st.autoBandwidth = bandwidth
		// Prevents any transition to SWB/FB until the SILK layer has
		// fully switched to WB mode and turned the variable LP filter
		// off.
		if st.first == 0 && st.mode != modeCeltOnly && st.silkMode.InWBmodeWithoutVariableLP == 0 &&
			st.bandwidth > BandwidthWideband {
			st.bandwidth = BandwidthWideband
		}
	}

	if st.bandwidth > st.maxBandwidth {
		st.bandwidth = st.maxBandwidth
	}

	if st.userBandwidth != Auto {
		st.bandwidth = st.userBandwidth
	}

	// This prevents us from using hybrid at unsafe CBR/max rates.
	if st.mode != modeCeltOnly && maxRate < 15000 {
		st.bandwidth = min(st.bandwidth, BandwidthWideband)
	}

	// Prevents Opus from wasting bits on frequencies that are above the
	// Nyquist rate of the input signal.
	if st.fs <= 24000 && st.bandwidth > BandwidthSuperwideband {
		st.bandwidth = BandwidthSuperwideband
	}
	if st.fs <= 16000 && st.bandwidth > BandwidthWideband {
		st.bandwidth = BandwidthWideband
	}
	if st.fs <= 12000 && st.bandwidth > BandwidthMediumband {
		st.bandwidth = BandwidthMediumband
	}
	if st.fs <= 8000 && st.bandwidth > BandwidthNarrowband {
		st.bandwidth = BandwidthNarrowband
	}
	// Use detected bandwidth to reduce the encoded bandwidth.
	if st.detectedBandwidth != 0 && st.userBandwidth == Auto {
		var minDetectedBandwidth int
		sc := int32(st.streamChannels)
		// Makes bandwidth detection more conservative just in case the
		// detector gets it wrong when we could have coded a high
		// bandwidth transparently. When operating in SILK/hybrid mode,
		// we don't go below wideband to avoid more complicated switches
		// that require redundancy.
		if equivRate <= 18000*sc && st.mode == modeCeltOnly {
			minDetectedBandwidth = BandwidthNarrowband
		} else if equivRate <= 24000*sc && st.mode == modeCeltOnly {
			minDetectedBandwidth = BandwidthMediumband
		} else if equivRate <= 30000*sc {
			minDetectedBandwidth = BandwidthWideband
		} else if equivRate <= 44000*sc {
			minDetectedBandwidth = BandwidthSuperwideband
		} else {
			minDetectedBandwidth = BandwidthFullband
		}

		st.detectedBandwidth = max(st.detectedBandwidth, minDetectedBandwidth)
		st.bandwidth = min(st.bandwidth, st.detectedBandwidth)
	}
	st.silkMode.LBRRCoded = decideFEC(st.silkMode.UseInBandFEC, st.silkMode.PacketLossPercentage,
		st.silkMode.LBRRCoded, st.mode, &st.bandwidth, equivRate)
	if st.application != ApplicationRestrictedSilk {
		st.celtEnc.SetLSBDepth(lsbDepth)
	}

	// CELT mode doesn't support mediumband, use wideband instead.
	if st.mode == modeCeltOnly && st.bandwidth == BandwidthMediumband {
		st.bandwidth = BandwidthWideband
	}
	if st.lfe != 0 {
		st.bandwidth = BandwidthNarrowband
	}

	currBandwidth := st.bandwidth

	if st.application == ApplicationRestrictedSilk && currBandwidth > BandwidthWideband {
		currBandwidth = BandwidthWideband
		st.bandwidth = currBandwidth
	}
	// Chooses the appropriate mode for speech. *NEVER* switch to/from
	// CELT-only mode here as this will invalidate some assumptions.
	if st.mode == modeSilkOnly && currBandwidth > BandwidthWideband {
		st.mode = modeHybrid
	}
	if st.mode == modeHybrid && currBandwidth <= BandwidthWideband {
		st.mode = modeSilkOnly
	}

	// Can't support higher than >60 ms frames, and >20 ms when in Hybrid
	// or CELT-only modes.
	if (frameSize > int(st.fs/50) && st.mode != modeSilkOnly) || frameSize > int(3*st.fs/50) {
		var encFrameSize int
		var repacketizeLen int
		totSize := 0
		dtxCount := 0
		sc := st.scratch

		if st.mode == modeSilkOnly {
			if frameSize == int(2*st.fs/25) { // 80 ms -> 2x 40 ms
				encFrameSize = int(st.fs / 25)
			} else if frameSize == int(3*st.fs/25) { // 120 ms -> 2x 60 ms
				encFrameSize = int(3 * st.fs / 50)
			} else { // 100 ms -> 5x 20 ms
				encFrameSize = int(st.fs / 50)
			}
		} else {
			encFrameSize = int(st.fs / 50)
		}

		nbFrames := frameSize / encFrameSize

		if analysisReadPosBak != -1 {
			// Reset analysis position to the beginning of the first frame
			// so we can use it one frame at a time.
			st.analysis.readPos = analysisReadPosBak
			st.analysis.readSubframe = analysisReadSubframeBak
		}

		// Worst cases:
		// 2 frames: Code 2 with different compressed sizes
		// >2 frames: Code 3 VBR
		maxHeaderBytes := 2 + (nbFrames-1)*2
		if nbFrames == 2 {
			maxHeaderBytes = 3
		}

		if st.useVBR != 0 || st.userBitrateBps == BitrateMax {
			repacketizeLen = outDataBytes
		} else {
			repacketizeLen = min(cbrBytes, outDataBytes)
		}
		maxLenSum := nbFrames + repacketizeLen - maxHeaderBytes

		rp := &sc.rp
		rp.Reset()

		bakToMono := st.silkMode.ToMono
		if bakToMono != 0 {
			st.forceChannels = 1
		} else {
			st.prevChannels = st.streamChannels
		}

		// tmp_data has max_len_sum bytes in C. The frames never use more
		// than the sum of their curr_max, so size it by that bound.
		perFrame := int(bitrateToBits(st.bitrateBps, st.fs, int32(encFrameSize)) / 8)
		if need := min(maxLenSum, nbFrames*max(perFrame, 0)); len(sc.tmpData) < need {
			sc.tmpData = make([]byte, max(need, 2*packetSizeCap*6))
		}
		for i := 0; i < nbFrames; i++ {
			st.silkMode.ToMono = 0
			st.nonfinalFrame = b2i(i < nbFrames-1)

			// When switching from SILK/Hybrid to CELT, only ask for a
			// switch at the last frame.
			frameToCelt := toCelt != 0 && i == nbFrames-1
			frameRedundancy := redundancy != 0 && (frameToCelt || (toCelt == 0 && i == 0))

			currMax := min(int(bitrateToBits(st.bitrateBps, st.fs, int32(encFrameSize))/8), maxLenSum/nbFrames)
			currMax = min(maxLenSum-totSize, currMax)
			if analysisReadPosBak != -1 {
				// Get analysis for current frame.
				tonalityGetInfo(&st.analysis, &info, encFrameSize)
			}
			framePCM := pcm[i*(st.channels*encFrameSize):]
			isSilence = isDigitalSilence(framePCM, encFrameSize, st.channels, lsbDepth)

			currData := sc.tmpData[totSize : totSize+max(currMax, 0)]
			tmpLen := st.encodeFrameNative(framePCM, encFrameSize, currData, currMax, floatAPI,
				&info, isSilence, b2i(frameRedundancy), celtToSilk, prefill,
				equivRate, b2i(frameToCelt))
			if tmpLen < 0 {
				return int(ErrInternalError)
			} else if tmpLen == 1 {
				dtxCount++
			}
			ret = rp.catImpl(sc.tmpData[totSize:totSize+tmpLen], false)
			if ret < 0 {
				return int(ErrInternalError)
			}
			totSize += tmpLen
		}
		ret = int(rp.outRangeImpl(0, nbFrames, data[:repacketizeLen], false,
			st.useVBR == 0 && dtxCount != nbFrames, nil))
		if ret < 0 {
			ret = int(ErrInternalError)
		}
		st.silkMode.ToMono = bakToMono
		return ret
	}
	return st.encodeFrameNative(pcm, frameSize, data, maxDataBytes, floatAPI,
		&info, isSilence, redundancy, celtToSilk, prefill,
		equivRate, toCelt)
}

// encodeFrameNative is opus_encode_frame_native: it codes one frame of
// at most 60 ms (20 ms outside SILK-only mode) into
// data[:origMaxDataBytes] and returns the packet length or a negative
// error code.
func (st *Encoder) encodeFrameNative(pcm []float32, frameSize int, data []byte, origMaxDataBytes int,
	floatAPI bool, info *analysisInfo,
	isSilence bool, redundancy, celtToSilk, prefill int, equivRate int32, toCelt int) int {
	var (
		celtMode        *celt.Mode
		ret             int
		nBytes          int32
		startBand       int
		redundancyBytes int // number of bytes to use for redundancy frame
		nbComprBytes    int
		redundantRng    uint32
		hpFreqSmth1     int32
		delayComp       int
	)
	sc := st.scratch
	silkEnc := st.silkEnc
	celtEnc := st.celtEnc
	ch := st.channels
	activity := vadNoDecision

	maxDataBytes := min(origMaxDataBytes, 1276)
	st.rangeFinal = 0
	if st.application != ApplicationRestrictedSilk {
		celtMode = celtEnc.Mode()
	}
	currBandwidth := st.bandwidth
	if st.application == ApplicationRestrictedLowdelay || st.application == ApplicationRestrictedCelt ||
		st.application == ApplicationRestrictedSilk {
		delayComp = 0
	} else {
		delayComp = st.delayCompensation
	}
	totalBuffer := delayComp

	frameRate := int(st.fs) / frameSize

	if isSilence {
		activity = 0
	} else if info.Valid != 0 {
		activity = b2i(info.ActivityProbability >= dtxActivityThreshold)
		if activity == 0 {
			// Mark as active if this noise frame is sufficiently loud.
			noiseEnergy := computeFrameEnergy(pcm, frameSize, ch)
			activity = b2i(st.peakSignalEnergy < float32(pseudoSNRThreshold*noiseEnergy))
		}
	} else if st.mode == modeCeltOnly {
		noiseEnergy := computeFrameEnergy(pcm, frameSize, ch)
		// Boosting peak energy a bit because we didn't just average the
		// active frames.
		activity = b2i(st.peakSignalEnergy < float32(pseudoSNRThreshold*float32(.5*noiseEnergy)))
	}

	// For the first frame at a new SILK bandwidth.
	if st.silkBWSwitch != 0 {
		redundancy = 1
		celtToSilk = 1
		st.silkBWSwitch = 0
		// Do a prefill without resetting the sampling rate control.
		prefill = 2
	}

	// If we decided to go with CELT, make sure redundancy is off, no
	// matter what we decided earlier.
	if st.mode == modeCeltOnly {
		redundancy = 0
	}

	if redundancy != 0 {
		redundancyBytes = computeRedundancyBytes(maxDataBytes, st.bitrateBps, frameRate, st.streamChannels)
		if redundancyBytes == 0 {
			redundancy = 0
		}
	}
	if st.application == ApplicationRestrictedSilk {
		redundancy = 0
		redundancyBytes = 0
	}

	bitsTarget := int(min(int32(8*(maxDataBytes-redundancyBytes)),
		bitrateToBits(st.bitrateBps, st.fs, int32(frameSize)))) - 8

	// The payload starts after the TOC byte (data += 1 in C).
	payload := data[1:]

	enc := &sc.enc
	enc.Init(payload[:origMaxDataBytes-1])

	pcmBuf := sc.pcmBuf[:(totalBuffer+frameSize)*ch]
	copy(pcmBuf, st.delayBuffer[(st.encoderBuffer-totalBuffer)*ch:(st.encoderBuffer-totalBuffer)*ch+totalBuffer*ch])

	if st.mode == modeCeltOnly {
		hpFreqSmth1 = silkLin2log(variableHPMinCutoffHz) << 8
	} else {
		hpFreqSmth1 = silkEnc.VariableHPSmth1Q15()
	}

	st.variableHPSmth2Q15 = silkSMLAWB(st.variableHPSmth2Q15,
		hpFreqSmth1-st.variableHPSmth2Q15, variableHPSmthCoef2Q16)

	// Convert from log scale to Hertz.
	cutoffHz := silkLog2lin(st.variableHPSmth2Q15 >> 8)

	if st.application == ApplicationVoIP {
		hpCutoff(pcm, cutoffHz, pcmBuf[totalBuffer*ch:], st.hpMem[:], frameSize, ch, st.fs)
	} else {
		dcReject(pcm, 3, pcmBuf[totalBuffer*ch:], st.hpMem[:], frameSize, ch, st.fs)
	}
	if floatAPI {
		x := pcmBuf[totalBuffer*ch:]
		sum := dsp.CeltInnerProd(x, x, frameSize*ch)
		// This should filter out both NaNs and ridiculous signals that
		// could cause NaNs further down.
		if !(sum < 1e9) || dsp.CeltIsnan(sum) {
			clear(x[:frameSize*ch])
			st.hpMem = [4]float32{}
		}
	}

	// SILK processing.
	hbGain := float32(1)
	if st.mode != modeCeltOnly {
		// Distribute bits between SILK and CELT.
		totalBitRate := bitsToBitrate(int32(bitsTarget), st.fs, int32(frameSize))
		if st.mode == modeHybrid {
			// Base rate for SILK.
			st.silkMode.BitRate = int32(computeSilkRateForHybrid(int(totalBitRate),
				currBandwidth, b2i(int(st.fs) == 50*frameSize), st.useVBR, st.silkMode.LBRRCoded,
				st.streamChannels))
			if st.energyMasking == nil {
				// Increasingly attenuate high band when it gets allocated
				// fewer bits.
				celtRate := totalBitRate - st.silkMode.BitRate
				hbGain = 1 - dsp.CeltExp2(float32(float32(-celtRate)*(1.0/1024)))
			}
		} else {
			// SILK gets all bits.
			st.silkMode.BitRate = totalBitRate
		}

		// Surround masking for SILK.
		if st.energyMasking != nil && st.useVBR != 0 && st.lfe == 0 {
			var maskSum float32
			end := 17
			srate := int16(16000)
			if st.bandwidth == BandwidthNarrowband {
				end = 13
				srate = 8000
			} else if st.bandwidth == BandwidthMediumband {
				end = 15
				srate = 12000
			}
			for c := 0; c < ch; c++ {
				for i := 0; i < end; i++ {
					mask := dsp.Max32(dsp.Min32(st.energyMasking[21*c+i], .5), -2.0)
					if mask > 0 {
						mask = float32(.5 * mask)
					}
					maskSum += mask
				}
			}
			// Conservative rate reduction, we cut the masking in half.
			maskingDepth := float32(float32(maskSum/float32(end)) * float32(ch))
			maskingDepth += .2
			rateOffset := int32(float32(float32(srate) * maskingDepth))
			rateOffset = max(rateOffset, -2*st.silkMode.BitRate/3)
			// Split the rate change between the SILK and CELT part for
			// hybrid.
			if st.bandwidth == BandwidthSuperwideband || st.bandwidth == BandwidthFullband {
				st.silkMode.BitRate += 3 * rateOffset / 5
			} else {
				st.silkMode.BitRate += rateOffset
			}
		}

		st.silkMode.PayloadSizeMs = 1000 * frameSize / int(st.fs)
		st.silkMode.NChannelsAPI = int32(ch)
		st.silkMode.NChannelsInternal = int32(st.streamChannels)
		if currBandwidth == BandwidthNarrowband {
			st.silkMode.DesiredInternalSampleRate = 8000
		} else if currBandwidth == BandwidthMediumband {
			st.silkMode.DesiredInternalSampleRate = 12000
		} else {
			st.silkMode.DesiredInternalSampleRate = 16000
		}
		if st.mode == modeHybrid {
			// Don't allow bandwidth reduction at lowest bitrates in
			// hybrid mode.
			st.silkMode.MinInternalSampleRate = 16000
		} else {
			st.silkMode.MinInternalSampleRate = 8000
		}

		st.silkMode.MaxInternalSampleRate = 16000
		if st.mode == modeSilkOnly {
			effectiveMaxRate := bitsToBitrate(int32(maxDataBytes*8), st.fs, int32(frameSize))
			if frameRate > 50 {
				effectiveMaxRate = effectiveMaxRate * 2 / 3
			}
			if effectiveMaxRate < 8000 {
				st.silkMode.MaxInternalSampleRate = 12000
				st.silkMode.DesiredInternalSampleRate = min(12000, st.silkMode.DesiredInternalSampleRate)
			}
			if effectiveMaxRate < 7000 {
				st.silkMode.MaxInternalSampleRate = 8000
				st.silkMode.DesiredInternalSampleRate = min(8000, st.silkMode.DesiredInternalSampleRate)
			}
		}

		st.silkMode.UseCBR = b2i(st.useVBR == 0)

		// Call SILK encoder for the low band.

		// Max bits for SILK, counting ToC, redundancy bytes, and
		// optionally redundancy.
		st.silkMode.MaxBits = (maxDataBytes - 1) * 8
		if redundancy != 0 && redundancyBytes >= 2 {
			// Counting 1 bit for redundancy position and 20 bits for
			// flag+size (only for hybrid).
			st.silkMode.MaxBits -= redundancyBytes*8 + 1
			if st.mode == modeHybrid {
				st.silkMode.MaxBits -= 20
			}
		}
		if st.silkMode.UseCBR != 0 {
			// When we're in CBR mode, but we have non-SILK data to
			// encode, switch SILK to VBR with cap to save on complexity.
			// Any variations will be absorbed by CELT and/or DRED and we
			// can still produce a constant bitrate without wasting bits.
			if st.mode == modeHybrid {
				// Allow SILK to steal up to 25% of the remaining bits.
				otherBits := int16(max(0, int32(st.silkMode.MaxBits)-st.silkMode.BitRate*int32(frameSize)/st.fs))
				st.silkMode.MaxBits = max(0, st.silkMode.MaxBits-int(otherBits)*3/4)
				st.silkMode.UseCBR = 0
			}
		} else {
			// Constrained VBR.
			if st.mode == modeHybrid {
				// Compute SILK bitrate corresponding to the max total
				// bits available.
				maxBitRate := computeSilkRateForHybrid(int(int32(st.silkMode.MaxBits)*st.fs/int32(frameSize)),
					currBandwidth, b2i(int(st.fs) == 50*frameSize), st.useVBR, st.silkMode.LBRRCoded,
					st.streamChannels)
				st.silkMode.MaxBits = int(bitrateToBits(int32(maxBitRate), st.fs, int32(frameSize)))
			}
		}

		if prefill != 0 && st.application != ApplicationRestrictedSilk {
			var zero int32
			// Use a smooth onset for the SILK prefill to avoid the
			// encoder trying to encode a discontinuity. The exact
			// location is what we need to avoid leaving any "gap" in the
			// audio when mixing with the redundant CELT frame. Here we
			// can afford to overwrite st->delay_buffer because the only
			// thing that uses it before it gets rewritten is
			// tmp_prefill[] and even then only the part after the ramp
			// really gets used (rather than sent to the encoder and
			// discarded).
			prefillOffset := ch * (st.encoderBuffer - st.delayCompensation - int(st.fs/400))
			db := st.delayBuffer[prefillOffset:]
			gainFade(db, db, 0, 1, celtMode.Overlap, int(st.fs/400), ch, celtMode.Window, st.fs)
			clear(st.delayBuffer[:prefillOffset])
			silkEnc.Encode(&st.silkMode, st.delayBuffer[:], st.encoderBuffer, nil, &zero, prefill, activity)
			// Prevent a second switch in the real encode call.
			st.silkMode.OpusCanSwitch = 0
		}

		pcmSilk := pcmBuf[totalBuffer*ch:]
		if r := silkEnc.Encode(&st.silkMode, pcmSilk, frameSize, enc, &nBytes, 0, activity); r != 0 {
			// Handle error.
			return int(ErrInternalError)
		}
		ret = 0

		// Extract SILK internal bandwidth for signaling in first byte.
		if st.mode == modeSilkOnly {
			if st.silkMode.InternalSampleRate == 8000 {
				currBandwidth = BandwidthNarrowband
			} else if st.silkMode.InternalSampleRate == 12000 {
				currBandwidth = BandwidthMediumband
			} else if st.silkMode.InternalSampleRate == 16000 {
				currBandwidth = BandwidthWideband
			}
		}

		st.silkMode.OpusCanSwitch = b2i(st.silkMode.SwitchReady != 0 && st.nonfinalFrame == 0)

		if activity == vadNoDecision {
			activity = b2i(st.silkMode.SignalType != typeNoVoiceActivity)
		}
		if nBytes == 0 {
			st.rangeFinal = 0
			data[0] = genTOC(st.mode, int(st.fs)/frameSize, currBandwidth, st.streamChannels)
			return 1
		}

		// FIXME: How do we allocate the redundancy for CBR?
		if st.silkMode.OpusCanSwitch != 0 {
			if st.application != ApplicationRestrictedSilk {
				redundancyBytes = computeRedundancyBytes(maxDataBytes, st.bitrateBps, frameRate, st.streamChannels)
				redundancy = b2i(redundancyBytes != 0)
			}
			celtToSilk = 0
			st.silkBWSwitch = 1
		}
	}

	// CELT processing.
	if st.application != ApplicationRestrictedSilk {
		endband := 21

		switch currBandwidth {
		case BandwidthNarrowband:
			endband = 13
		case BandwidthMediumband, BandwidthWideband:
			endband = 17
		case BandwidthSuperwideband:
			endband = 19
		case BandwidthFullband:
			endband = 21
		}
		celtEnc.SetEndBand(endband)
		celtEnc.SetChannels(st.streamChannels)
		celtEnc.SetBitrate(BitrateMax)
	}
	if st.mode != modeSilkOnly {
		celtPred := 2
		// We may still decide to disable prediction later.
		if st.silkMode.ReducedDependency != 0 {
			celtPred = 0
		}
		celtEnc.SetPrediction(celtPred)
	}

	n400 := int(st.fs / 400)
	tmpPrefill := sc.tmpPrefill[:ch*n400]
	if st.mode != modeSilkOnly && st.mode != st.prevMode && st.prevMode > 0 &&
		st.application != ApplicationRestrictedCelt {
		off := (st.encoderBuffer - totalBuffer - n400) * ch
		copy(tmpPrefill, st.delayBuffer[off:off+ch*n400])
	}

	if ch*(st.encoderBuffer-(frameSize+totalBuffer)) > 0 {
		n := ch * (st.encoderBuffer - frameSize - totalBuffer)
		copy(st.delayBuffer[:n], st.delayBuffer[ch*frameSize:ch*frameSize+n])
		copy(st.delayBuffer[n:n+(frameSize+totalBuffer)*ch], pcmBuf[:(frameSize+totalBuffer)*ch])
	} else {
		off := (frameSize + totalBuffer - st.encoderBuffer) * ch
		copy(st.delayBuffer[:st.encoderBuffer*ch], pcmBuf[off:off+st.encoderBuffer*ch])
	}
	// gain_fade() and stereo_fade() need to be after the buffer copying
	// because we don't want any of this to affect the SILK part.
	if (st.prevHBGain < 1 || hbGain < 1) && celtMode != nil {
		gainFade(pcmBuf, pcmBuf, st.prevHBGain, hbGain, celtMode.Overlap, frameSize, ch, celtMode.Window, st.fs)
	}
	st.prevHBGain = hbGain
	if st.mode != modeHybrid || st.streamChannels == 1 {
		if equivRate > 32000 {
			st.silkMode.StereoWidthQ14 = 16384
		} else if equivRate < 16000 {
			st.silkMode.StereoWidthQ14 = 0
		} else {
			st.silkMode.StereoWidthQ14 = int(16384 - 2048*(32000-equivRate)/(equivRate-14000))
		}
	}
	if st.energyMasking == nil && ch == 2 {
		// Apply stereo width reduction (at low bitrates).
		if st.hybridStereoWidthQ14 < (1<<14) || st.silkMode.StereoWidthQ14 < (1<<14) {
			g1 := float32(st.hybridStereoWidthQ14)
			g2 := float32(st.silkMode.StereoWidthQ14)
			g1 *= 1.0 / 16384
			g2 *= 1.0 / 16384
			if celtMode != nil {
				stereoFade(pcmBuf, pcmBuf, g1, g2, celtMode.Overlap, frameSize, ch, celtMode.Window, st.fs)
			}
			st.hybridStereoWidthQ14 = int16(st.silkMode.StereoWidthQ14)
		}
	}

	hybrid := b2i(st.mode == modeHybrid)
	if st.mode != modeCeltOnly && enc.Tell()+17+20*hybrid <= 8*(maxDataBytes-1) {
		// For SILK mode, the redundancy is inferred from the length.
		if st.mode == modeHybrid {
			enc.EncBitLogp(redundancy, 12)
		}
		if redundancy != 0 {
			var maxRedundancy int
			enc.EncBitLogp(celtToSilk, 1)
			if st.mode == modeHybrid {
				// Reserve the 8 bits needed for the redundancy length,
				// and at least a few bits for CELT if possible.
				maxRedundancy = (maxDataBytes - 1) - ((enc.Tell() + 8 + 3 + 7) >> 3)
			} else {
				maxRedundancy = (maxDataBytes - 1) - ((enc.Tell() + 7) >> 3)
			}
			// Target the same bit-rate for redundancy as for the rest, up
			// to a max of 257 bytes.
			redundancyBytes = min(maxRedundancy, redundancyBytes)
			redundancyBytes = min(257, max(2, redundancyBytes))
			if st.mode == modeHybrid {
				enc.EncUint(uint32(redundancyBytes-2), 256)
			}
		}
	} else {
		redundancy = 0
	}

	if redundancy == 0 {
		st.silkBWSwitch = 0
		redundancyBytes = 0
	}
	if st.mode != modeCeltOnly {
		startBand = 17
	}

	if st.mode == modeSilkOnly {
		ret = (enc.Tell() + 7) >> 3
		enc.Done()
		nbComprBytes = ret
	} else {
		nbComprBytes = (maxDataBytes - 1) - redundancyBytes
		enc.Shrink(uint32(nbComprBytes))
	}

	if redundancy != 0 || st.mode != modeSilkOnly {
		celtEnc.SetAnalysis(info)
	}
	if st.mode == modeHybrid {
		silkInfo := celt.SILKInfo{SignalType: st.silkMode.SignalType, Offset: st.silkMode.Offset}
		celtEnc.SetSILKInfo(&silkInfo)
	}

	// 5 ms redundant frame for CELT->SILK.
	if redundancy != 0 && celtToSilk != 0 {
		celtEnc.SetStartBand(0)
		celtEnc.SetVBR(0)
		celtEnc.SetBitrate(BitrateMax)
		err := celtEnc.EncodeWithEC(pcmBuf, int(st.fs/200), payload[nbComprBytes:nbComprBytes+redundancyBytes], redundancyBytes, nil)
		if err < 0 {
			return int(ErrInternalError)
		}
		redundantRng = celtEnc.FinalRange()
		celtEnc.Reset()
	}

	if st.application != ApplicationRestrictedSilk {
		celtEnc.SetStartBand(startBand)
	}

	data[0] = 0
	if st.mode != modeSilkOnly {
		celtEnc.SetVBR(st.useVBR)
		if st.mode == modeHybrid {
			if st.useVBR != 0 {
				celtEnc.SetBitrate(st.bitrateBps - st.silkMode.BitRate)
				celtEnc.SetVBRConstraint(0)
			}
		} else {
			if st.useVBR != 0 {
				celtEnc.SetVBR(1)
				celtEnc.SetVBRConstraint(st.vbrConstraint)
				celtEnc.SetBitrate(st.bitrateBps)
			}
		}
		if st.mode != st.prevMode && st.prevMode > 0 && st.application != ApplicationRestrictedCelt {
			celtEnc.Reset()

			// Prefilling.
			celtEnc.EncodeWithEC(tmpPrefill, n400, sc.dummy[:], 2, nil)
			celtEnc.SetPrediction(0)
		}
		// If false, we already busted the budget and we'll end up with a
		// "PLC frame".
		if enc.Tell() <= 8*nbComprBytes {
			ret = celtEnc.EncodeWithEC(pcmBuf, frameSize, nil, nbComprBytes, enc)
			if ret < 0 {
				return int(ErrInternalError)
			}
			// Put CELT->SILK redundancy data in the right place.
			if redundancy != 0 && celtToSilk != 0 && st.mode == modeHybrid && nbComprBytes != ret {
				copy(payload[ret:ret+redundancyBytes], payload[nbComprBytes:nbComprBytes+redundancyBytes])
				nbComprBytes = ret + redundancyBytes
			}
		}
		st.rangeFinal = celtEnc.FinalRange()
	} else {
		st.rangeFinal = enc.Rng
	}

	// 5 ms redundant frame for SILK->CELT.
	if redundancy != 0 && celtToSilk == 0 {
		N2 := int(st.fs / 200)
		N4 := int(st.fs / 400)

		celtEnc.Reset()
		celtEnc.SetStartBand(0)
		celtEnc.SetPrediction(0)
		celtEnc.SetVBR(0)
		celtEnc.SetBitrate(BitrateMax)

		if st.mode == modeHybrid {
			// Shrink packet to what the encoder actually used.
			nbComprBytes = ret
			enc.Shrink(uint32(nbComprBytes))
		}
		// NOTE: We could speed this up slightly (at the expense of code
		// size) by just adding a function that prefills the buffer.
		celtEnc.EncodeWithEC(pcmBuf[ch*(frameSize-N2-N4):], N4, sc.dummy[:], 2, nil)

		err := celtEnc.EncodeWithEC(pcmBuf[ch*(frameSize-N2):], N2, payload[nbComprBytes:nbComprBytes+redundancyBytes], redundancyBytes, nil)
		if err < 0 {
			return int(ErrInternalError)
		}
		redundantRng = celtEnc.FinalRange()
	}

	// Signalling the mode in the first byte.
	data[0] |= genTOC(st.mode, int(st.fs)/frameSize, currBandwidth, st.streamChannels)

	st.rangeFinal ^= redundantRng

	if toCelt != 0 {
		st.prevMode = modeCeltOnly
	} else {
		st.prevMode = st.mode
	}
	st.prevChannels = st.streamChannels
	st.prevFramesize = frameSize

	st.first = 0

	// DTX decision.
	if st.useDTX != 0 && st.silkMode.UseDTX == 0 {
		if decideDTXMode(activity, &st.nbNoActivityMsQ1, 2*1000*frameSize/int(st.fs)) {
			st.rangeFinal = 0
			data[0] = genTOC(st.mode, int(st.fs)/frameSize, currBandwidth, st.streamChannels)
			return 1
		}
	} else {
		st.nbNoActivityMsQ1 = 0
	}

	// In the unlikely case that the SILK encoder busted its target, tell
	// the decoder to call the PLC.
	if enc.Tell() > (maxDataBytes-1)*8 {
		if maxDataBytes < 2 {
			return int(ErrBufferTooSmall)
		}
		data[1] = 0
		ret = 1
		st.rangeFinal = 0
	} else if st.mode == modeSilkOnly && redundancy == 0 {
		// When in LPC only mode it's perfectly reasonable to strip off
		// trailing zero bytes as the required range decoder behavior is
		// to fill these in. This can't be done when the MDCT modes are
		// used because the decoder needs to know the actual length for
		// allocation purposes.
		for ret > 2 && data[ret] == 0 {
			ret--
		}
	}
	// Count ToC and redundancy.
	ret += 1 + redundancyBytes
	applyPadding := st.useVBR == 0
	if applyPadding {
		if st.padPacket(data, ret, origMaxDataBytes) != OK {
			return int(ErrInternalError)
		}
		ret = origMaxDataBytes
	}
	return ret
}

// padScratch returns the packet copy buffer of the padding functions,
// with room for n bytes.
func (st *Encoder) padScratch(n int) []byte {
	sc := st.scratch
	if len(sc.padBuf) < n {
		sc.padBuf = make([]byte, max(n, 2*packetSizeCap))
	}
	return sc.padBuf
}

// SILK fixed-point helpers (silk/SigProc_FIX.h, silk/lin2log.c and
// silk/log2lin.c) used by the encoder.

// silkSMULBB is silk_SMULBB.
func silkSMULBB(a32, b32 int32) int32 { return int32(int16(a32)) * int32(int16(b32)) }

// silkSMULWB is silk_SMULWB.
func silkSMULWB(a32, b32 int32) int32 { return int32((int64(a32) * int64(int16(b32))) >> 16) }

// silkSMLAWB is silk_SMLAWB.
func silkSMLAWB(a32, b32, c32 int32) int32 {
	return int32(int64(a32) + ((int64(b32) * int64(int16(c32))) >> 16))
}

// silkSMULWW is silk_SMULWW.
func silkSMULWW(a32, b32 int32) int32 { return int32((int64(a32) * int64(b32)) >> 16) }

// silkLin2log is silk_lin2log: approximation of 128 * log2().
func silkLin2log(inLin int32) int32 {
	lz := int32(bits.LeadingZeros32(uint32(inLin)))
	fracQ7 := int32(bits.RotateLeft32(uint32(inLin), -int(24-lz))) & 0x7f
	// Piece-wise parabolic approximation.
	return silkSMLAWB(fracQ7, fracQ7*(128-fracQ7), 179) + (31-lz)<<7
}

// silkLog2lin is silk_log2lin: approximation of 2^().
func silkLog2lin(inLogQ7 int32) int32 {
	if inLogQ7 < 0 {
		return 0
	} else if inLogQ7 >= 3967 {
		return math.MaxInt32
	}
	out := int32(1) << (inLogQ7 >> 7)
	fracQ7 := inLogQ7 & 0x7F
	if inLogQ7 < 2048 {
		// Piece-wise parabolic approximation.
		out = out + (out*silkSMLAWB(fracQ7, silkSMULBB(fracQ7, 128-fracQ7), -174))>>7
	} else {
		// Piece-wise parabolic approximation.
		out = out + (out>>7)*silkSMLAWB(fracQ7, silkSMULBB(fracQ7, 128-fracQ7), -174)
	}
	return out
}

// encodeResult converts an encoder return code to the Go results.
func encodeResult(ret int) (int, error) {
	if ret < 0 {
		return 0, Error(ret)
	}
	return ret, nil
}

// Encode encodes a frame of 16-bit PCM (opus_encode). pcm holds
// frameSize samples per channel, interleaved; frameSize must be a valid
// Opus frame duration for the sampling rate (2.5, 5, 10, 20, 40, 60, 80,
// 100 or 120 ms), and with SetExpertFrameDuration it may be longer than
// the coded frame. data receives the packet and len(data) is the maximum
// packet size; 4000 bytes is recommended. Encode returns the packet
// length. A length of 1 or 2 bytes means that the packet does not need
// to be transmitted (DTX).
func (st *Encoder) Encode(pcm []int16, frameSize int, data []byte) (int, error) {
	if frameSize <= 0 || len(pcm) < frameSize*st.channels {
		return 0, ErrBadArg
	}
	fs := frameSizeSelect(st.application, frameSize, st.variableDuration, st.fs)
	if fs <= 0 {
		return 0, ErrBadArg
	}
	in := st.scratch.in[:fs*st.channels]
	for i := range in {
		in[i] = dsp.Int16ToRes(pcm[i])
	}
	return encodeResult(st.encodeNative(in, fs, data, len(data), 16,
		analysisPCM{i16: pcm}, frameSize, 0, -2, st.channels, downmixInt, true))
}

// Encode24 encodes a frame of 24-bit PCM held in int32 samples
// (opus_encode24). The arguments are as for Encode.
func (st *Encoder) Encode24(pcm []int32, frameSize int, data []byte) (int, error) {
	if frameSize <= 0 || len(pcm) < frameSize*st.channels {
		return 0, ErrBadArg
	}
	fs := frameSizeSelect(st.application, frameSize, st.variableDuration, st.fs)
	if fs <= 0 {
		return 0, ErrBadArg
	}
	in := st.scratch.in[:fs*st.channels]
	for i := range in {
		in[i] = dsp.Int24ToRes(pcm[i])
	}
	return encodeResult(st.encodeNative(in, fs, data, len(data), 24,
		analysisPCM{i32: pcm}, frameSize, 0, -2, st.channels, downmixInt24, true))
}

// EncodeFloat encodes a frame of float PCM, nominally in [-1, 1]
// (opus_encode_float). Samples outside that range are allowed but are
// clipped by the decoder. The other arguments are as for Encode.
func (st *Encoder) EncodeFloat(pcm []float32, frameSize int, data []byte) (int, error) {
	if frameSize <= 0 || len(pcm) < frameSize*st.channels {
		return 0, ErrBadArg
	}
	fs := frameSizeSelect(st.application, frameSize, st.variableDuration, st.fs)
	return encodeResult(st.encodeNative(pcm, fs, data, len(data), 24,
		analysisPCM{f32: pcm}, frameSize, 0, -2, st.channels, downmixFloat, true))
}

// SetApplication changes the application (OPUS_SET_APPLICATION) to
// ApplicationVoIP, ApplicationAudio or ApplicationRestrictedLowdelay. It
// fails for encoders created with a restricted SILK or CELT application,
// and for any change after the first encoded frame.
func (st *Encoder) SetApplication(application int) error {
	if st.application == ApplicationRestrictedSilk || st.application == ApplicationRestrictedCelt {
		return ErrBadArg
	}
	if (application != ApplicationVoIP && application != ApplicationAudio &&
		application != ApplicationRestrictedLowdelay) ||
		(st.first == 0 && st.application != application) {
		return ErrBadArg
	}
	st.application = application
	st.analysis.application = application
	return nil
}

// Application returns the application (OPUS_GET_APPLICATION).
func (st *Encoder) Application() int { return st.application }

// SetBitrate sets the target bitrate in bits per second, or Auto or
// BitrateMax (OPUS_SET_BITRATE). Rates below 500 b/s are raised to 500
// b/s and rates above 750 kb/s per channel are lowered to that.
func (st *Encoder) SetBitrate(bitrate int) error {
	value := int32(max(min(bitrate, math.MaxInt32), math.MinInt32))
	if value != Auto && value != BitrateMax {
		if value <= 0 {
			return ErrBadArg
		} else if value <= 500 {
			value = 500
		} else if value > int32(750000*st.channels) {
			value = int32(750000 * st.channels)
		}
	}
	st.userBitrateBps = value
	return nil
}

// Bitrate returns the bitrate that the encoder targets for the last frame
// size, in bits per second (OPUS_GET_BITRATE).
func (st *Encoder) Bitrate() int { return int(st.userBitrateToBitrate(st.prevFramesize, 1276)) }

// SetForceChannels forces mono or stereo coding (1 or 2), or Auto
// (OPUS_SET_FORCE_CHANNELS).
func (st *Encoder) SetForceChannels(channels int) error {
	if (channels < 1 || channels > st.channels) && channels != Auto {
		return ErrBadArg
	}
	st.forceChannels = channels
	return nil
}

// ForceChannels returns the forced channel count or Auto
// (OPUS_GET_FORCE_CHANNELS).
func (st *Encoder) ForceChannels() int { return st.forceChannels }

// SetMaxBandwidth limits the coded bandwidth to one of the Bandwidth
// constants (OPUS_SET_MAX_BANDWIDTH).
func (st *Encoder) SetMaxBandwidth(bandwidth int) error {
	if bandwidth < BandwidthNarrowband || bandwidth > BandwidthFullband {
		return ErrBadArg
	}
	st.maxBandwidth = bandwidth
	st.silkMode.MaxInternalSampleRate = silkMaxRateForBandwidth(bandwidth)
	return nil
}

// silkMaxRateForBandwidth is the SILK maximum internal sampling rate that
// the bandwidth controls set.
func silkMaxRateForBandwidth(bandwidth int) int32 {
	switch bandwidth {
	case BandwidthNarrowband:
		return 8000
	case BandwidthMediumband:
		return 12000
	}
	return 16000
}

// MaxBandwidth returns the maximum bandwidth (OPUS_GET_MAX_BANDWIDTH).
func (st *Encoder) MaxBandwidth() int { return st.maxBandwidth }

// SetBandwidth forces the coded bandwidth to one of the Bandwidth
// constants, or Auto (OPUS_SET_BANDWIDTH).
func (st *Encoder) SetBandwidth(bandwidth int) error {
	if (bandwidth < BandwidthNarrowband || bandwidth > BandwidthFullband) && bandwidth != Auto {
		return ErrBadArg
	}
	st.userBandwidth = bandwidth
	st.silkMode.MaxInternalSampleRate = silkMaxRateForBandwidth(bandwidth)
	return nil
}

// Bandwidth returns the bandwidth of the last coded frame
// (OPUS_GET_BANDWIDTH).
func (st *Encoder) Bandwidth() int { return st.bandwidth }

// SetDTX enables or disables discontinuous transmission (OPUS_SET_DTX).
func (st *Encoder) SetDTX(enabled bool) { st.useDTX = b2i(enabled) }

// DTX reports whether DTX is enabled (OPUS_GET_DTX).
func (st *Encoder) DTX() bool { return st.useDTX != 0 }

// SetComplexity sets the encoder complexity, 0 to 10
// (OPUS_SET_COMPLEXITY).
func (st *Encoder) SetComplexity(complexity int) error {
	if complexity < 0 || complexity > 10 {
		return ErrBadArg
	}
	st.silkMode.Complexity = complexity
	if st.application != ApplicationRestrictedSilk {
		st.celtEnc.SetComplexity(complexity)
	}
	return nil
}

// Complexity returns the encoder complexity (OPUS_GET_COMPLEXITY).
func (st *Encoder) Complexity() int { return st.silkMode.Complexity }

// SetInbandFEC configures the SILK in-band forward error correction
// (OPUS_SET_INBAND_FEC): 0 disables it, 1 enables it and may switch to
// SILK to use it, and 2 enables it without switching to SILK when the
// signal is confidently music.
func (st *Encoder) SetInbandFEC(fec int) error {
	if fec < 0 || fec > 2 {
		return ErrBadArg
	}
	st.fecConfig = fec
	st.silkMode.UseInBandFEC = b2i(fec != 0)
	return nil
}

// InbandFEC returns the in-band FEC setting (OPUS_GET_INBAND_FEC).
func (st *Encoder) InbandFEC() int { return st.fecConfig }

// SetPacketLossPerc sets the expected packet loss, 0 to 100 percent
// (OPUS_SET_PACKET_LOSS_PERC).
func (st *Encoder) SetPacketLossPerc(percent int) error {
	if percent < 0 || percent > 100 {
		return ErrBadArg
	}
	st.silkMode.PacketLossPercentage = percent
	if st.application != ApplicationRestrictedSilk {
		st.celtEnc.SetPacketLossPerc(percent)
	}
	return nil
}

// PacketLossPerc returns the expected packet loss
// (OPUS_GET_PACKET_LOSS_PERC).
func (st *Encoder) PacketLossPerc() int { return st.silkMode.PacketLossPercentage }

// SetVBR enables variable bitrate (the default) or selects constant
// bitrate (OPUS_SET_VBR).
func (st *Encoder) SetVBR(enabled bool) {
	st.useVBR = b2i(enabled)
	st.silkMode.UseCBR = 1 - st.useVBR
}

// VBR reports whether variable bitrate is enabled (OPUS_GET_VBR).
func (st *Encoder) VBR() bool { return st.useVBR != 0 }

// SetVoiceRatio sets the voice probability in percent, or -1 for
// automatic detection (OPUS_SET_VOICE_RATIO). The encoder resets it on
// every non-silent frame.
func (st *Encoder) SetVoiceRatio(ratio int) error {
	if ratio < -1 || ratio > 100 {
		return ErrBadArg
	}
	st.voiceRatio = ratio
	return nil
}

// VoiceRatio returns the voice ratio (OPUS_GET_VOICE_RATIO).
func (st *Encoder) VoiceRatio() int { return st.voiceRatio }

// SetVBRConstraint enables (the default) or disables constrained VBR
// (OPUS_SET_VBR_CONSTRAINT).
func (st *Encoder) SetVBRConstraint(enabled bool) { st.vbrConstraint = b2i(enabled) }

// VBRConstraint reports whether constrained VBR is enabled
// (OPUS_GET_VBR_CONSTRAINT).
func (st *Encoder) VBRConstraint() bool { return st.vbrConstraint != 0 }

// SetSignal hints the signal type: Auto, SignalVoice or SignalMusic
// (OPUS_SET_SIGNAL).
func (st *Encoder) SetSignal(signal int) error {
	if signal != Auto && signal != SignalVoice && signal != SignalMusic {
		return ErrBadArg
	}
	st.signalType = signal
	return nil
}

// Signal returns the signal type hint (OPUS_GET_SIGNAL).
func (st *Encoder) Signal() int { return st.signalType }

// Lookahead returns the encoder look-ahead in samples at the input
// sampling rate (OPUS_GET_LOOKAHEAD). Decoders see this much delay.
func (st *Encoder) Lookahead() int {
	value := int(st.fs / 400)
	if st.application != ApplicationRestrictedLowdelay && st.application != ApplicationRestrictedCelt {
		value += st.delayCompensation
	}
	return value
}

// SampleRate returns the sampling rate the encoder was initialized with
// (OPUS_GET_SAMPLE_RATE).
func (st *Encoder) SampleRate() int { return int(st.fs) }

// Channels returns the number of input channels.
func (st *Encoder) Channels() int { return st.channels }

// FinalRange returns the final state of the range coder of the last
// packet (OPUS_GET_FINAL_RANGE). A decoder of the packet ends with the
// same value.
func (st *Encoder) FinalRange() uint32 { return st.rangeFinal }

// SetLSBDepth sets the resolution of the input signal, 8 to 24 bits
// (OPUS_SET_LSB_DEPTH). The encoder treats quieter signals as digital
// silence.
func (st *Encoder) SetLSBDepth(depth int) error {
	if depth < 8 || depth > 24 {
		return ErrBadArg
	}
	st.lsbDepth = depth
	return nil
}

// LSBDepth returns the input resolution (OPUS_GET_LSB_DEPTH).
func (st *Encoder) LSBDepth() int { return st.lsbDepth }

// SetExpertFrameDuration selects the coded frame duration with one of
// the Framesize constants (OPUS_SET_EXPERT_FRAME_DURATION). FramesizeArg
// (the default) uses the frameSize argument of Encode.
func (st *Encoder) SetExpertFrameDuration(duration int) error {
	if duration < FramesizeArg || duration > Framesize120ms {
		return ErrBadArg
	}
	st.variableDuration = duration
	return nil
}

// ExpertFrameDuration returns the frame duration setting
// (OPUS_GET_EXPERT_FRAME_DURATION).
func (st *Encoder) ExpertFrameDuration() int { return st.variableDuration }

// SetPredictionDisabled disables inter-frame prediction, which makes
// frames almost independent at a quality cost
// (OPUS_SET_PREDICTION_DISABLED).
func (st *Encoder) SetPredictionDisabled(disabled bool) {
	st.silkMode.ReducedDependency = b2i(disabled)
}

// PredictionDisabled reports whether prediction is disabled
// (OPUS_GET_PREDICTION_DISABLED).
func (st *Encoder) PredictionDisabled() bool { return st.silkMode.ReducedDependency != 0 }

// SetPhaseInversionDisabled disables the use of phase inversion for
// intensity stereo (OPUS_SET_PHASE_INVERSION_DISABLED).
func (st *Encoder) SetPhaseInversionDisabled(disabled bool) {
	if st.application != ApplicationRestrictedSilk {
		st.celtEnc.SetPhaseInversionDisabled(b2i(disabled))
	}
}

// PhaseInversionDisabled reports whether phase inversion is disabled
// (OPUS_GET_PHASE_INVERSION_DISABLED).
func (st *Encoder) PhaseInversionDisabled() bool {
	if st.application != ApplicationRestrictedSilk {
		return st.celtEnc.PhaseInversionDisabled() != 0
	}
	return false
}

// SetForceMode forces the coding mode (OPUS_SET_FORCE_MODE, a private
// libopus control): ModeSilkOnly, ModeHybrid, ModeCeltOnly or Auto.
func (st *Encoder) SetForceMode(mode int) error {
	if (mode < modeSilkOnly || mode > modeCeltOnly) && mode != Auto {
		return ErrBadArg
	}
	st.userForcedMode = mode
	return nil
}

// Coding modes for SetForceMode.
const (
	ModeSilkOnly = modeSilkOnly // MODE_SILK_ONLY
	ModeHybrid   = modeHybrid   // MODE_HYBRID
	ModeCeltOnly = modeCeltOnly // MODE_CELT_ONLY
)

// setLFE is OPUS_SET_LFE: it marks the stream as a low-frequency effects
// channel.
func (st *Encoder) setLFE(lfe int) {
	st.lfe = lfe
	if st.application != ApplicationRestrictedSilk {
		st.celtEnc.SetLFE(lfe)
	}
}

// setEnergyMask is OPUS_SET_ENERGY_MASK: the encoder keeps a reference
// to mask (21 values per channel) for the surround masking.
func (st *Encoder) setEnergyMask(mask []float32) {
	st.energyMasking = mask
	if st.application != ApplicationRestrictedSilk {
		st.celtEnc.SetEnergyMask(mask)
	}
}

// InDTX reports whether the last frame was in DTX (OPUS_GET_IN_DTX).
func (st *Encoder) InDTX() bool {
	if st.silkMode.UseDTX != 0 && (st.prevMode == modeSilkOnly || st.prevMode == modeHybrid) {
		// DTX determined by Silk.
		v := st.silkEnc.NoSpeechCounter(0) >= nbSpeechFramesBeforeDTX
		// Stereo: check second channel unless only the middle channel
		// was encoded.
		if v && st.silkMode.NChannelsInternal == 2 && st.silkEnc.PrevDecodeOnlyMiddle() == 0 {
			v = st.silkEnc.NoSpeechCounter(1) >= nbSpeechFramesBeforeDTX
		}
		return v
	} else if st.useDTX != 0 {
		// DTX determined by Opus.
		return st.nbNoActivityMsQ1 >= nbSpeechFramesBeforeDTX*20*2
	}
	return false
}

// Reset resets the encoder to the state of a freshly initialized one,
// keeping its settings (OPUS_RESET_STATE).
func (st *Encoder) Reset() {
	var dummy silk.EncControl
	tonalityAnalysisReset(&st.analysis)

	st.encoderResetState = encoderResetState{}

	if st.application != ApplicationRestrictedSilk {
		st.celtEnc.Reset()
	}
	if st.application != ApplicationRestrictedCelt {
		st.silkEnc.Init(st.channels, &dummy)
	}
	st.streamChannels = st.channels
	st.hybridStereoWidthQ14 = 1 << 14
	st.prevHBGain = 1
	st.first = 1
	st.mode = modeHybrid
	st.bandwidth = BandwidthFullband
	st.variableHPSmth2Q15 = silkLin2log(variableHPMinCutoffHz) << 8
}

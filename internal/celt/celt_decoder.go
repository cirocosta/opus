// Ported from libopus 1.6.1 celt/celt_decoder.c (float build without
// ENABLE_DEEP_PLC and ENABLE_DRED; no ENABLE_QEXT, no CUSTOM_MODES).

package celt

import (
	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
)

const (
	// plcPitchLagMax is PLC_PITCH_LAG_MAX: the maximum pitch lag to allow
	// in the pitch-based PLC (a pitch of 66.67 Hz).
	plcPitchLagMax = 720
	// plcPitchLagMin is PLC_PITCH_LAG_MIN: the minimum pitch lag to allow
	// in the pitch-based PLC (a pitch of 480 Hz).
	plcPitchLagMin = 100

	frameNone        = 0 // FRAME_NONE
	frameNormal      = 1 // FRAME_NORMAL
	framePLCNoise    = 2 // FRAME_PLC_NOISE
	framePLCPeriodic = 3 // FRAME_PLC_PERIODIC

	// decodeBufferSize is DECODE_BUFFER_SIZE.
	decodeBufferSize = decPitchBufSize

	// maxOverlap is the overlap of the static mode.
	maxOverlap = 120
	// decodeMemStride is DECODE_BUFFER_SIZE+overlap, the size of the
	// decoder history of one channel.
	decodeMemStride = decodeBufferSize + maxOverlap
	// maxFrame is the largest frame of the static mode (20 ms).
	maxFrame = 960
)

// decoderState holds the fields of OpusCustomDecoder from rng onward,
// which OPUS_RESET_STATE clears.
type decoderState struct {
	rng                 uint32
	err                 int
	lastPitchIndex      int
	lossDuration        int
	plcDuration         int
	lastFrameType       int
	skipPLC             bool
	postfilterPeriod    int
	postfilterPeriodOld int
	postfilterGain      float32
	postfilterGainOld   float32
	postfilterTapset    int
	postfilterTapsetOld int
	prefilterAndFold    bool

	preemphMemD [2]float32

	// decodeMem is _decode_mem: channels*(DECODE_BUFFER_SIZE+overlap)
	// samples, one block per channel.
	decodeMem      [2 * decodeMemStride]float32
	oldBandE       [2 * maxBands]float32
	oldLogE        [2 * maxBands]float32
	oldLogE2       [2 * maxBands]float32
	backgroundLogE [2 * maxBands]float32
	lpc            [2 * dsp.CeltLPCOrder]float32
}

// decoderScratch holds per-call buffers (the C stack allocations).
type decoderScratch struct {
	dec          entcode.Decoder
	X            [2 * maxFrame]float32
	freq         [maxFrame]float32
	deemph       [maxFrame]float32
	lpPitchBuf   [decodeBufferSize >> 1]float32
	etmp         [maxOverlap]float32
	exc          [MaxPeriod + dsp.CeltLPCOrder]float32
	firTmp       [MaxPeriod]float32
	tfRes        [maxBands]int
	cap          [maxBands]int
	offsets      [maxBands]int
	fineQuant    [maxBands]int
	pulses       [maxBands]int
	finePriority [maxBands]int
	collapse     [2 * maxBands]uint8
}

// Decoder is OpusCustomDecoder (CELTDecoder), the CELT decoder state.
type Decoder struct {
	mode           *Mode
	overlap        int
	channels       int
	streamChannels int

	downsample int
	start, end int
	signalling int
	disableInv bool
	complexity int

	// Everything in st gets cleared on a reset.
	st decoderState

	scratch decoderScratch
}

// NewDecoder allocates a decoder and initializes it with Init. It returns
// nil and the error code when Init fails.
func NewDecoder(samplingRate int32, channels int) (*Decoder, int) {
	d := new(Decoder)
	if ret := d.Init(samplingRate, channels); ret != OK {
		return nil, ret
	}
	return d, OK
}

// Init is celt_decoder_init: it initializes the decoder for the static
// 48 kHz mode, decoding at samplingRate (48, 24, 16, 12 or 8 kHz) with
// channels (1 or 2) output channels.
func (d *Decoder) Init(samplingRate int32, channels int) int {
	ret := d.customInit(StaticMode(), channels)
	if ret != OK {
		return ret
	}
	d.downsample = ResamplingFactor(samplingRate)
	if d.downsample == 0 {
		return BadArg
	}
	return OK
}

// customInit is opus_custom_decoder_init.
func (d *Decoder) customInit(mode *Mode, channels int) int {
	if channels < 0 || channels > 2 {
		return BadArg
	}
	*d = Decoder{}
	d.mode = mode
	d.overlap = mode.Overlap
	d.channels = channels
	d.streamChannels = channels

	d.downsample = 1
	d.start = 0
	d.end = d.mode.EffEBands
	d.signalling = 1
	d.disableInv = channels == 1

	d.Reset()
	return OK
}

// Reset is OPUS_RESET_STATE.
func (d *Decoder) Reset() {
	d.st = decoderState{}
	for i := 0; i < 2*d.mode.NbEBands; i++ {
		d.st.oldLogE[i] = -28
		d.st.oldLogE2[i] = -28
	}
	d.st.skipPLC = true
	d.st.lastFrameType = frameNone
}

// SetComplexity is OPUS_SET_COMPLEXITY. Only the deep PLC of
// ENABLE_DEEP_PLC reads the decoder complexity, so in this build it does
// not change the output.
func (d *Decoder) SetComplexity(value int) int {
	if value < 0 || value > 10 {
		return BadArg
	}
	d.complexity = value
	return OK
}

// Complexity is OPUS_GET_COMPLEXITY.
func (d *Decoder) Complexity() int { return d.complexity }

// SetStartBand is CELT_SET_START_BAND.
func (d *Decoder) SetStartBand(value int) int {
	if value < 0 || value >= d.mode.NbEBands {
		return BadArg
	}
	d.start = value
	return OK
}

// StartBand returns the start band.
func (d *Decoder) StartBand() int { return d.start }

// SetEndBand is CELT_SET_END_BAND.
func (d *Decoder) SetEndBand(value int) int {
	if value < 1 || value > d.mode.NbEBands {
		return BadArg
	}
	d.end = value
	return OK
}

// EndBand returns the end band.
func (d *Decoder) EndBand() int { return d.end }

// SetChannels is CELT_SET_CHANNELS: it sets the number of channels coded
// in the stream (1 or 2).
func (d *Decoder) SetChannels(value int) int {
	if value < 1 || value > 2 {
		return BadArg
	}
	d.streamChannels = value
	return OK
}

// Channels returns the number of output channels.
func (d *Decoder) Channels() int { return d.channels }

// StreamChannels returns the number of coded channels.
func (d *Decoder) StreamChannels() int { return d.streamChannels }

// GetAndClearError is CELT_GET_AND_CLEAR_ERROR.
func (d *Decoder) GetAndClearError() int {
	v := d.st.err
	d.st.err = 0
	return v
}

// Lookahead is OPUS_GET_LOOKAHEAD.
func (d *Decoder) Lookahead() int { return d.overlap / d.downsample }

// Pitch is OPUS_GET_PITCH.
func (d *Decoder) Pitch() int { return d.st.postfilterPeriod }

// Mode is CELT_GET_MODE.
func (d *Decoder) Mode() *Mode { return d.mode }

// SetSignalling is CELT_SET_SIGNALLING.
func (d *Decoder) SetSignalling(value int) { d.signalling = value }

// FinalRange is OPUS_GET_FINAL_RANGE.
func (d *Decoder) FinalRange() uint32 { return d.st.rng }

// SetPhaseInversionDisabled is OPUS_SET_PHASE_INVERSION_DISABLED.
func (d *Decoder) SetPhaseInversionDisabled(value int) int {
	if value < 0 || value > 1 {
		return BadArg
	}
	d.disableInv = value != 0
	return OK
}

// PhaseInversionDisabled is OPUS_GET_PHASE_INVERSION_DISABLED.
func (d *Decoder) PhaseInversionDisabled() int { return b2i(d.disableInv) }

// decodeMemCh returns decode_mem[c].
func (d *Decoder) decodeMemCh(c int) []float32 {
	return d.st.decodeMem[c*(decodeBufferSize+d.overlap) : (c+1)*(decodeBufferSize+d.overlap)]
}

// deemphasisStereoSimple is deemphasis_stereo_simple: the special case
// for stereo with no downsampling and no accumulation.
func deemphasisStereoSimple(in [2][]float32, pcm []float32, N int, coef0 float32, mem []float32) {
	x0 := in[0]
	x1 := in[1]
	m0 := mem[0]
	m1 := mem[1]
	for j := 0; j < N; j++ {
		// Add VERY_SMALL to x[] first to reduce dependency chain.
		tmp0 := x0[j] + dsp.VerySmall + m0
		tmp1 := x1[j] + dsp.VerySmall + m1
		m0 = float32(coef0 * tmp0)
		m1 = float32(coef0 * tmp1)
		pcm[2*j] = dsp.Sig2Res(tmp0)
		pcm[2*j+1] = dsp.Sig2Res(tmp1)
	}
	mem[0] = m0
	mem[1] = m1
}

// deemphasis is deemphasis.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func deemphasis(
	in [2][]float32,
	pcm []float32,
	N, C, downsample int,
	coef []float32,
	mem []float32,
	accum bool,
	scratch []float32,
) {
	// Short version for common case.
	if downsample == 1 && C == 2 && !accum {
		deemphasisStereoSimple(in, pcm, N, coef[0], mem)
		return
	}
	applyDownsampling := false
	coef0 := coef[0]
	Nd := N / downsample
	for c := 0; c < C; c++ {
		m := mem[c]
		x := in[c]
		y := pcm[c:]
		if downsample > 1 {
			// Shortcut for the standard (non-custom modes) case.
			for j := 0; j < N; j++ {
				tmp := x[j] + dsp.VerySmall + m
				m = float32(coef0 * tmp)
				scratch[j] = tmp
			}
			applyDownsampling = true
		} else if accum {
			for j := 0; j < N; j++ {
				tmp := x[j] + m + dsp.VerySmall
				m = float32(coef0 * tmp)
				y[j*C] = y[j*C] + dsp.Sig2Res(tmp)
			}
		} else {
			for j := 0; j < N; j++ {
				tmp := x[j] + dsp.VerySmall + m
				m = float32(coef0 * tmp)
				y[j*C] = dsp.Sig2Res(tmp)
			}
		}
		mem[c] = m

		if applyDownsampling {
			// Perform down-sampling.
			if accum {
				for j := 0; j < Nd; j++ {
					y[j*C] = y[j*C] + dsp.Sig2Res(scratch[j*downsample])
				}
			} else {
				for j := 0; j < Nd; j++ {
					y[j*C] = dsp.Sig2Res(scratch[j*downsample])
				}
			}
		}
	}
}

// celtSynthesis is celt_synthesis. outSyn[c] is out_syn[c].
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func celtSynthesis(
	mode *Mode,
	X []float32,
	outSyn [2][]float32,
	oldBandE []float32,
	start, effEnd, C, CC int,
	isTransient bool,
	LM, downsample int,
	silence bool,
	freqBuf []float32,
) {
	overlap := mode.Overlap
	nbEBands := mode.NbEBands
	N := mode.ShortMdctSize << uint(LM)
	freq := freqBuf[:N] // Interleaved signal MDCTs
	M := 1 << uint(LM)

	var B, NB, shift int
	if isTransient {
		B = M
		NB = mode.ShortMdctSize
		shift = mode.MaxLM
	} else {
		B = 1
		NB = mode.ShortMdctSize << uint(LM)
		shift = mode.MaxLM - LM
	}

	if CC == 2 && C == 1 {
		// Copying a mono streams to two channels.
		denormaliseBands(mode, X, freq, oldBandE, start, effEnd, M, downsample, silence)
		// Store a temporary copy in the output buffer because the IMDCT
		// destroys its input.
		freq2 := outSyn[1][overlap/2:]
		copy(freq2[:N], freq)
		for b := 0; b < B; b++ {
			dsp.CltMDCTBackward(
				mode.MDCT,
				freq2[b:],
				outSyn[0][NB*b:],
				mode.Window,
				overlap,
				shift,
				B,
			)
		}
		for b := 0; b < B; b++ {
			dsp.CltMDCTBackward(
				mode.MDCT,
				freq[b:],
				outSyn[1][NB*b:],
				mode.Window,
				overlap,
				shift,
				B,
			)
		}
	} else if CC == 1 && C == 2 {
		// Downmixing a stereo stream to mono.
		freq2 := outSyn[0][overlap/2:]
		denormaliseBands(mode, X, freq, oldBandE, start, effEnd, M, downsample, silence)
		// Use the output buffer as temp array before downmixing.
		denormaliseBands(
			mode,
			X[N:],
			freq2,
			oldBandE[nbEBands:],
			start,
			effEnd,
			M,
			downsample,
			silence,
		)
		for i := 0; i < N; i++ {
			freq[i] = float32(.5*freq[i]) + float32(.5*freq2[i])
		}
		for b := 0; b < B; b++ {
			dsp.CltMDCTBackward(
				mode.MDCT,
				freq[b:],
				outSyn[0][NB*b:],
				mode.Window,
				overlap,
				shift,
				B,
			)
		}
	} else {
		// Normal case (mono or stereo).
		for c := 0; c < CC; c++ {
			denormaliseBands(
				mode,
				X[c*N:],
				freq,
				oldBandE[c*nbEBands:],
				start,
				effEnd,
				M,
				downsample,
				silence,
			)
			for b := 0; b < B; b++ {
				dsp.CltMDCTBackward(
					mode.MDCT,
					freq[b:],
					outSyn[c][NB*b:],
					mode.Window,
					overlap,
					shift,
					B,
				)
			}
		}
	}
	// Saturate IMDCT output so that we can't overflow in the pitch
	// postfilter or in the deemphasis (SATURATE is the identity in the
	// float build).
}

// tfDecode is tf_decode.
func tfDecode(start, end int, isTransient bool, tfRes []int, LM int, dec *entcode.Decoder) {
	budget := dec.Storage * 8
	tell := uint32(dec.Tell())
	logp := uint32(4)
	if isTransient {
		logp = 2
	}
	tfSelectRsv := 0
	if LM > 0 && tell+logp+1 <= budget {
		tfSelectRsv = 1
	}
	budget -= uint32(tfSelectRsv)
	tfChanged := 0
	curr := 0
	nextLogp := uint32(5)
	if isTransient {
		nextLogp = 4
	}
	for i := start; i < end; i++ {
		if tell+logp <= budget {
			curr ^= dec.DecBitLogp(uint(logp))
			tell = uint32(dec.Tell())
			tfChanged |= curr
		}
		tfRes[i] = curr
		logp = nextLogp
	}
	tfSelect := 0
	it := 4 * b2i(isTransient)
	if tfSelectRsv != 0 && tfSelectTable[LM][it+0+tfChanged] != tfSelectTable[LM][it+2+tfChanged] {
		tfSelect = dec.DecBitLogp(1)
	}
	for i := start; i < end; i++ {
		tfRes[i] = int(tfSelectTable[LM][it+2*tfSelect+tfRes[i]])
	}
}

// plcPitchSearch is celt_plc_pitch_search.
func (d *Decoder) plcPitchSearch(C int) int {
	lp := d.scratch.lpPitchBuf[:]
	var mem [2][]float32
	for c := 0; c < C; c++ {
		mem[c] = d.decodeMemCh(c)
	}
	dsp.PitchDownsample(mem[:C], lp, decodeBufferSize>>1, C, 2)
	pitchIndex := dsp.PitchSearch(lp[plcPitchLagMax>>1:], lp,
		decodeBufferSize-plcPitchLagMax, plcPitchLagMax-plcPitchLagMin)
	return plcPitchLagMax - pitchIndex
}

// prefilterAndFold is prefilter_and_fold.
func (d *Decoder) prefilterAndFold(N int) {
	mode := d.mode
	overlap := d.overlap
	CC := d.channels
	etmp := d.scratch.etmp[:overlap]
	for c := 0; c < CC; c++ {
		mem := d.decodeMemCh(c)
		// Apply the pre-filter to the MDCT overlap for the next frame
		// because the post-filter will be re-applied in the decoder after
		// the MDCT overlap.
		combFilter(etmp, 0, mem, decodeBufferSize-N,
			d.st.postfilterPeriodOld, d.st.postfilterPeriod, overlap,
			-d.st.postfilterGainOld, -d.st.postfilterGain,
			d.st.postfilterTapsetOld, d.st.postfilterTapset, nil, 0)

		// Simulate TDAC on the concealed audio so that it blends with the
		// MDCT of the next frame.
		for i := 0; i < overlap/2; i++ {
			mem[decodeBufferSize-N+i] = float32(mode.Window[i]*etmp[overlap-1-i]) +
				float32(mode.Window[overlap-i-1]*etmp[i])
		}
	}
}

// plcLagWindow is 0.008f*0.008f, rounded to float as in C.
var plcLagWindow = func() float32 {
	x := float32(0.008)
	return x * x
}()

// decodeLost is celt_decode_lost.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func (d *Decoder) decodeLost(N, LM int) {
	C := d.channels
	var decodeMem, outSyn [2][]float32
	mode := d.mode
	nbEBands := mode.NbEBands
	overlap := mode.Overlap
	eBands := mode.EBands
	st := &d.st
	const maxPeriod = MaxPeriod

	for c := 0; c < C; c++ {
		decodeMem[c] = d.decodeMemCh(c)
		outSyn[c] = decodeMem[c][decodeBufferSize-N:]
	}
	oldBandE := st.oldBandE[:]
	backgroundLogE := st.backgroundLogE[:]
	lpc := st.lpc[:]

	lossDuration := st.lossDuration
	start := d.start
	currFrameType := framePLCPeriodic
	if st.plcDuration >= 40 || start != 0 || st.skipPLC {
		currFrameType = framePLCNoise
	}

	if currFrameType == framePLCNoise {
		// Noise-based PLC/CNG.
		X := d.scratch.X[:C*N] // Interleaved normalised MDCTs
		end := d.end
		effEnd := max(start, min(end, mode.EffEBands))

		for c := 0; c < C; c++ {
			copy(
				decodeMem[c][:decodeBufferSize-N+overlap],
				decodeMem[c][N:N+decodeBufferSize-N+overlap],
			)
		}

		if st.prefilterAndFold {
			d.prefilterAndFold(N)
		}

		// Energy decay.
		decay := float32(.5)
		if lossDuration == 0 {
			decay = 1.5
		}
		for c := 0; c < C; c++ {
			for i := start; i < end; i++ {
				oldBandE[c*nbEBands+i] = dsp.Max32(
					backgroundLogE[c*nbEBands+i],
					oldBandE[c*nbEBands+i]-decay,
				)
			}
		}
		seed := st.rng
		for c := 0; c < C; c++ {
			for i := start; i < effEnd; i++ {
				boffs := N*c + int(eBands[i])<<uint(LM)
				blen := int(eBands[i+1]-eBands[i]) << uint(LM)
				for j := 0; j < blen; j++ {
					seed = celtLCGRand(seed)
					X[boffs+j] = float32(int32(seed) >> 20)
				}
				renormaliseVector(X[boffs:], blen, 1)
			}
		}
		st.rng = seed

		celtSynthesis(
			mode,
			X,
			outSyn,
			oldBandE,
			start,
			effEnd,
			C,
			C,
			false,
			LM,
			d.downsample,
			false,
			d.scratch.freq[:],
		)

		// Run the postfilter with the last parameters.
		for c := 0; c < C; c++ {
			st.postfilterPeriod = max(st.postfilterPeriod, CombFilterMinPeriod)
			st.postfilterPeriodOld = max(st.postfilterPeriodOld, CombFilterMinPeriod)
			os := decodeBufferSize - N
			combFilter(
				decodeMem[c],
				os,
				decodeMem[c],
				os,
				st.postfilterPeriodOld,
				st.postfilterPeriod,
				mode.ShortMdctSize,
				st.postfilterGainOld,
				st.postfilterGain,
				st.postfilterTapsetOld,
				st.postfilterTapset,
				mode.Window,
				overlap,
			)
			if LM != 0 {
				os += mode.ShortMdctSize
				combFilter(
					decodeMem[c],
					os,
					decodeMem[c],
					os,
					st.postfilterPeriod,
					st.postfilterPeriod,
					N-mode.ShortMdctSize,
					st.postfilterGain,
					st.postfilterGain,
					st.postfilterTapset,
					st.postfilterTapset,
					mode.Window,
					overlap,
				)
			}
		}
		st.postfilterPeriodOld = st.postfilterPeriod
		st.postfilterGainOld = st.postfilterGain
		st.postfilterTapsetOld = st.postfilterTapset

		st.prefilterAndFold = false
		// Skip regular PLC until we get two consecutive packets.
		st.skipPLC = true
	} else {
		// Pitch-based PLC.
		fade := float32(1)
		var pitchIndex int

		if st.lastFrameType != framePLCPeriodic {
			pitchIndex = d.plcPitchSearch(C)
			st.lastPitchIndex = pitchIndex
		} else {
			pitchIndex = st.lastPitchIndex
			fade = .8
		}

		// We want the excitation for 2 pitch periods in order to look for
		// a decaying signal, but we can't get more than MAX_PERIOD.
		excLength := min(2*pitchIndex, maxPeriod)

		// exc[i] is excBuf[dsp.CeltLPCOrder+i].
		excBuf := d.scratch.exc[:]
		exc := excBuf[dsp.CeltLPCOrder:]
		firTmp := d.scratch.firTmp[:excLength]
		window := mode.Window
		for c := 0; c < C; c++ {
			var S1 float32
			buf := decodeMem[c]
			for i := 0; i < maxPeriod+dsp.CeltLPCOrder; i++ {
				excBuf[i] = buf[decodeBufferSize-maxPeriod-dsp.CeltLPCOrder+i]
			}

			if st.lastFrameType != framePLCPeriodic {
				var ac [dsp.CeltLPCOrder + 1]float32
				// Compute LPC coefficients for the last MAX_PERIOD samples
				// before the first loss so we can work in the
				// excitation-filter domain.
				dsp.CeltAutocorr(exc, ac[:], window, overlap, dsp.CeltLPCOrder, maxPeriod)
				// Add a noise floor of -40 dB.
				ac[0] = float32(ac[0] * 1.0001)
				// Use lag windowing to stabilize the Levinson-Durbin
				// recursion.
				for i := 1; i <= dsp.CeltLPCOrder; i++ {
					// ac[i] *= exp(-.5*(2*M_PI*.002*i)*(2*M_PI*.002*i));
					ac[i] -= float32(float32(float32(ac[i]*plcLagWindow)*float32(i)) * float32(i))
				}
				dsp.CeltLPC(lpc[c*dsp.CeltLPCOrder:], ac[:], dsp.CeltLPCOrder)
			}
			// Initialize the LPC history with the samples just before the
			// start of the region for which we're computing the
			// excitation. Compute the excitation for exc_length samples
			// before the loss. We need the copy because celt_fir() cannot
			// filter in-place.
			dsp.CeltFIR(
				excBuf[maxPeriod-excLength:],
				lpc[c*dsp.CeltLPCOrder:],
				firTmp,
				excLength,
				dsp.CeltLPCOrder,
			)
			copy(exc[maxPeriod-excLength:maxPeriod], firTmp)

			// Check if the waveform is decaying, and if so how fast. We do
			// this to avoid adding energy when concealing in a segment with
			// decaying energy.
			var decay float32
			{
				E1 := float32(1)
				E2 := float32(1)
				decayLength := excLength >> 1
				for i := 0; i < decayLength; i++ {
					e := exc[maxPeriod-decayLength+i]
					E1 += float32(e * e)
					e = exc[maxPeriod-2*decayLength+i]
					E2 += float32(e * e)
				}
				E1 = dsp.Min32(E1, E2)
				decay = dsp.CeltSqrt(dsp.FracDiv32(E1, E2))
			}

			// Move the decoder memory one frame to the left to give us room
			// to add the data for the new frame. We ignore the overlap that
			// extends past the end of the buffer, because we aren't going
			// to use it.
			copy(buf[:decodeBufferSize-N], buf[N:decodeBufferSize])

			// Extrapolate from the end of the excitation with a period of
			// "pitch_index", scaling down each period by an additional
			// factor of "decay".
			extrapolationOffset := maxPeriod - pitchIndex
			// We need to extrapolate enough samples to cover a complete
			// MDCT window (including overlap/2 samples on both sides).
			extrapolationLen := N + overlap
			// We also apply fading if this is not the first loss.
			attenuation := float32(fade * decay)
			for i, j := 0, 0; i < extrapolationLen; i, j = i+1, j+1 {
				if j >= pitchIndex {
					j -= pitchIndex
					attenuation = float32(attenuation * decay)
				}
				buf[decodeBufferSize-N+i] = float32(attenuation * exc[extrapolationOffset+j])
				// Compute the energy of the previously decoded signal whose
				// excitation we're copying.
				tmp := buf[decodeBufferSize-maxPeriod-N+extrapolationOffset+j]
				S1 += float32(tmp * tmp)
			}
			{
				var lpcMem [dsp.CeltLPCOrder]float32
				// Copy the last decoded samples (prior to the overlap
				// region) to synthesis filter memory so we can have a
				// continuous signal.
				for i := 0; i < dsp.CeltLPCOrder; i++ {
					lpcMem[i] = buf[decodeBufferSize-N-1-i]
				}
				// Apply the synthesis filter to convert the excitation back
				// into the signal domain.
				dsp.CeltIIR(
					buf[decodeBufferSize-N:],
					lpc[c*dsp.CeltLPCOrder:],
					buf[decodeBufferSize-N:],
					extrapolationLen,
					dsp.CeltLPCOrder,
					lpcMem[:],
				)
			}

			// Check if the synthesis energy is higher than expected, which
			// can happen with the signal changes during our window. If so,
			// attenuate.
			{
				var S2 float32
				for i := 0; i < extrapolationLen; i++ {
					tmp := buf[decodeBufferSize-N+i]
					S2 += float32(tmp * tmp)
				}
				// This checks for an "explosion" in the synthesis. The
				// float test is written this way to catch NaNs in the
				// output of the IIR filter at the same time.
				if !(S1 > float32(0.2*S2)) {
					for i := 0; i < extrapolationLen; i++ {
						buf[decodeBufferSize-N+i] = 0
					}
				} else if S1 < S2 {
					ratio := dsp.CeltSqrt(dsp.FracDiv32(S1+1, S2+1))
					for i := 0; i < overlap; i++ {
						tmpG := 1 - float32(window[i]*(1-ratio))
						buf[decodeBufferSize-N+i] = float32(tmpG * buf[decodeBufferSize-N+i])
					}
					for i := overlap; i < extrapolationLen; i++ {
						buf[decodeBufferSize-N+i] = float32(ratio * buf[decodeBufferSize-N+i])
					}
				}
			}
		}

		st.prefilterAndFold = true
	}

	// Saturate to something large to avoid wrap-around.
	st.lossDuration = min(10000, lossDuration+(1<<uint(LM)))
	st.plcDuration = min(10000, st.plcDuration+(1<<uint(LM)))
	st.lastFrameType = currFrameType
}

// DecodeWithEC is celt_decode_with_ec_dred, which celt_decode_with_ec
// calls with the same arguments. It decodes one frame of frameSize
// samples per channel (at the output sampling rate) into pcm,
// interleaved, and returns the number of samples per channel or a
// negative error code. pcm holds opus_res values (float, normalized to
// +/-1). When accum is true, the decoded samples are added to pcm.
//
// A nil data, or data with at most 1 byte, is a lost frame (PLC). When
// dec is nil, the decoder reads data with its own range decoder;
// otherwise dec must already be initialized on data (hybrid mode, after
// SILK) and len(data) is the size of the whole packet payload.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func (d *Decoder) DecodeWithEC(
	data []byte,
	pcm []float32,
	frameSize int,
	dec *entcode.Decoder,
	accum bool,
) int {
	st := &d.st
	CC := d.channels
	C := d.streamChannels
	mode := d.mode
	nbEBands := mode.NbEBands
	overlap := mode.Overlap
	eBands := mode.EBands
	start := d.start
	end := d.end
	frameSize *= d.downsample
	length := len(data)

	oldBandE := st.oldBandE[:]
	oldLogE := st.oldLogE[:]
	oldLogE2 := st.oldLogE2[:]
	backgroundLogE := st.backgroundLogE[:]

	LM := 0
	for LM = 0; LM <= mode.MaxLM; LM++ {
		if mode.ShortMdctSize<<uint(LM) == frameSize {
			break
		}
	}
	if LM > mode.MaxLM {
		return BadArg
	}
	M := 1 << uint(LM)

	if length > 1275 || pcm == nil {
		return BadArg
	}

	N := M * mode.ShortMdctSize
	var decodeMem, outSyn [2][]float32
	for c := 0; c < CC; c++ {
		decodeMem[c] = d.decodeMemCh(c)
		outSyn[c] = decodeMem[c][decodeBufferSize-N:]
	}

	effEnd := end
	if effEnd > mode.EffEBands {
		effEnd = mode.EffEBands
	}

	if data == nil || length <= 1 {
		d.decodeLost(N, LM)
		deemphasis(
			outSyn,
			pcm,
			N,
			CC,
			d.downsample,
			mode.Preemph[:],
			st.preemphMemD[:],
			accum,
			d.scratch.deemph[:],
		)
		return frameSize / d.downsample
	}
	// Check if there are at least two packets received consecutively
	// before turning on the pitch-based PLC.
	if st.lossDuration == 0 {
		st.skipPLC = false
	}

	if dec == nil {
		dec = &d.scratch.dec
		dec.Init(data)
	}

	if C == 1 {
		for i := 0; i < nbEBands; i++ {
			oldBandE[i] = dsp.Max32(oldBandE[i], oldBandE[nbEBands+i])
		}
	}

	totalBits := int32(length * 8)
	tell := int32(dec.Tell())

	silence := false
	if tell >= totalBits {
		silence = true
	} else if tell == 1 {
		silence = dec.DecBitLogp(15) != 0
	}
	if silence {
		// Pretend we've read all the remaining bits.
		tell = int32(length * 8)
		dec.NbitsTotal += int(tell) - dec.Tell()
	}

	var postfilterGain float32
	postfilterPitch := 0
	postfilterTapset := 0
	if start == 0 && tell+16 <= totalBits {
		if dec.DecBitLogp(1) != 0 {
			octave := int(dec.DecUint(6))
			postfilterPitch = (16 << uint(octave)) + int(dec.DecBits(uint(4+octave))) - 1
			qg := int(dec.DecBits(3))
			if int32(dec.Tell())+2 <= totalBits {
				postfilterTapset = dec.DecIcdf(tapsetIcdf, 2)
			}
			postfilterGain = float32(.09375 * float32(qg+1))
		}
		tell = int32(dec.Tell())
	}

	isTransient := false
	if LM > 0 && tell+3 <= totalBits {
		isTransient = dec.DecBitLogp(3) != 0
		tell = int32(dec.Tell())
	}

	shortBlocks := isTransient

	// Decode the global flags (first symbols in the stream).
	intraEner := false
	if tell+3 <= totalBits {
		intraEner = dec.DecBitLogp(3) != 0
	}
	// If recovering from packet loss, make sure we make the energy
	// prediction safe to reduce the risk of getting loud artifacts.
	if !intraEner && st.lossDuration != 0 {
		for c := 0; c < 2; c++ {
			var safety float32
			missing := min(10, st.lossDuration>>uint(LM))
			switch LM {
			case 0:
				safety = 1.5
			case 1:
				safety = .5
			}
			for i := start; i < end; i++ {
				if oldBandE[c*nbEBands+i] < dsp.Max32(
					oldLogE[c*nbEBands+i],
					oldLogE2[c*nbEBands+i],
				) {
					// If energy is going down already, continue the trend.
					E0 := oldBandE[c*nbEBands+i]
					E1 := oldLogE[c*nbEBands+i]
					E2 := oldLogE2[c*nbEBands+i]
					slope := dsp.Max32(E1-E0, float32(.5*(E2-E0)))
					slope = dsp.Min32(slope, 2)
					E0 -= dsp.Max32(0, float32(float32(1+missing)*slope))
					oldBandE[c*nbEBands+i] = dsp.Max32(-20, E0)
				} else {
					// Otherwise take the min of the last frames.
					oldBandE[c*nbEBands+i] = dsp.Min32(
						dsp.Min32(oldBandE[c*nbEBands+i], oldLogE[c*nbEBands+i]),
						oldLogE2[c*nbEBands+i],
					)
				}
				// Shorter frames have more natural fluctuations -- play it
				// safe.
				oldBandE[c*nbEBands+i] -= safety
			}
		}
	}
	// Get band energies.
	unquantCoarseEnergy(mode, start, end, oldBandE, intraEner, dec, C, LM)

	tfRes := d.scratch.tfRes[:nbEBands]
	tfDecode(start, end, isTransient, tfRes, LM, dec)

	tell = int32(dec.Tell())
	spreadDecision := spreadNormal
	if tell+4 <= totalBits {
		spreadDecision = dec.DecIcdf(spreadIcdf, 5)
	}

	caps := d.scratch.cap[:nbEBands]
	initCaps(mode, caps, LM, C)

	offsets := d.scratch.offsets[:nbEBands]

	dynallocLogp := 6
	totalBits <<= bitRes
	tell = int32(dec.TellFrac())
	for i := start; i < end; i++ {
		width := C * int(eBands[i+1]-eBands[i]) << uint(LM)
		// quanta is 6 bits, but no more than 1 bit/sample and no less than
		// 1/8 bit/sample.
		quanta := min(width<<bitRes, max(6<<bitRes, width))
		dynallocLoopLogp := dynallocLogp
		boost := 0
		for tell+int32(dynallocLoopLogp<<bitRes) < totalBits && boost < caps[i] {
			flag := dec.DecBitLogp(uint(dynallocLoopLogp))
			tell = int32(dec.TellFrac())
			if flag == 0 {
				break
			}
			boost += quanta
			totalBits -= int32(quanta)
			dynallocLoopLogp = 1
		}
		offsets[i] = boost
		// Making dynalloc more likely.
		if boost > 0 {
			dynallocLogp = max(2, dynallocLogp-1)
		}
	}

	fineQuant := d.scratch.fineQuant[:nbEBands]
	allocTrim := 5
	if tell+(6<<bitRes) <= totalBits {
		allocTrim = dec.DecIcdf(trimIcdf, 7)
	}

	bits := (int32(length*8) << bitRes) - int32(dec.TellFrac()) - 1
	antiCollapseRsv := int32(0)
	if isTransient && LM >= 2 && bits >= int32((LM+2)<<bitRes) {
		antiCollapseRsv = 1 << bitRes
	}
	bits -= antiCollapseRsv

	pulses := d.scratch.pulses[:nbEBands]
	finePriority := d.scratch.finePriority[:nbEBands]

	var intensity, dualStereo int
	var balance int32
	rc := rangeCoder{dec: dec}
	codedBands := cltComputeAllocation(mode, start, end, offsets, caps,
		allocTrim, &intensity, &dualStereo, bits, &balance, pulses,
		fineQuant, finePriority, C, LM, rc, false, 0, 0)

	unquantFineEnergy(mode, start, end, oldBandE, nil, fineQuant, dec, C)

	X := d.scratch.X[:C*N] // Interleaved normalised MDCTs

	for c := 0; c < CC; c++ {
		copy(decodeMem[c][:decodeBufferSize-N+overlap], decodeMem[c][N:decodeBufferSize+overlap])
	}

	// Decode fixed codebook.
	collapseMasks := d.scratch.collapse[:C*nbEBands]

	var Y []float32
	if C == 2 {
		Y = X[N:]
	}
	quantAllBands(false, mode, start, end, X, Y, collapseMasks,
		nil, pulses, shortBlocks, spreadDecision, dualStereo != 0, intensity, tfRes,
		int32(length*(8<<bitRes))-antiCollapseRsv, balance, rc, LM, codedBands, &st.rng, 0,
		d.disableInv)

	antiCollapseOn := false
	if antiCollapseRsv > 0 {
		antiCollapseOn = dec.DecBits(1) != 0
	}
	unquantEnergyFinalise(mode, start, end, oldBandE,
		fineQuant, finePriority, length*8-dec.Tell(), dec, C)
	if antiCollapseOn {
		antiCollapse(mode, X, collapseMasks, LM, C, N,
			start, end, oldBandE, oldLogE, oldLogE2, pulses, st.rng, false)
	}

	if silence {
		for i := 0; i < C*nbEBands; i++ {
			oldBandE[i] = -28
		}
	}
	if st.prefilterAndFold {
		d.prefilterAndFold(N)
	}
	celtSynthesis(mode, X, outSyn, oldBandE, start, effEnd,
		C, CC, isTransient, LM, d.downsample, silence, d.scratch.freq[:])

	for c := 0; c < CC; c++ {
		st.postfilterPeriod = max(st.postfilterPeriod, CombFilterMinPeriod)
		st.postfilterPeriodOld = max(st.postfilterPeriodOld, CombFilterMinPeriod)
		os := decodeBufferSize - N
		combFilter(
			decodeMem[c],
			os,
			decodeMem[c],
			os,
			st.postfilterPeriodOld,
			st.postfilterPeriod,
			mode.ShortMdctSize,
			st.postfilterGainOld,
			st.postfilterGain,
			st.postfilterTapsetOld,
			st.postfilterTapset,
			mode.Window,
			overlap,
		)
		if LM != 0 {
			os += mode.ShortMdctSize
			combFilter(
				decodeMem[c],
				os,
				decodeMem[c],
				os,
				st.postfilterPeriod,
				postfilterPitch,
				N-mode.ShortMdctSize,
				st.postfilterGain,
				postfilterGain,
				st.postfilterTapset,
				postfilterTapset,
				mode.Window,
				overlap,
			)
		}
	}
	st.postfilterPeriodOld = st.postfilterPeriod
	st.postfilterGainOld = st.postfilterGain
	st.postfilterTapsetOld = st.postfilterTapset
	st.postfilterPeriod = postfilterPitch
	st.postfilterGain = postfilterGain
	st.postfilterTapset = postfilterTapset
	if LM != 0 {
		st.postfilterPeriodOld = st.postfilterPeriod
		st.postfilterGainOld = st.postfilterGain
		st.postfilterTapsetOld = st.postfilterTapset
	}

	if C == 1 {
		copy(oldBandE[nbEBands:2*nbEBands], oldBandE[:nbEBands])
	}

	if !isTransient {
		copy(oldLogE2[:2*nbEBands], oldLogE[:2*nbEBands])
		copy(oldLogE[:2*nbEBands], oldBandE[:2*nbEBands])
	} else {
		for i := 0; i < 2*nbEBands; i++ {
			oldLogE[i] = dsp.Min32(oldLogE[i], oldBandE[i])
		}
	}
	// In normal circumstances, we only allow the noise floor to increase
	// by up to 2.4 dB/second, but when we're in DTX we give the weight of
	// all missing packets to the update packet.
	maxBackgroundIncrease := float32(float32(min(160, st.lossDuration+M)) * 0.001)
	for i := 0; i < 2*nbEBands; i++ {
		backgroundLogE[i] = dsp.Min32(backgroundLogE[i]+maxBackgroundIncrease, oldBandE[i])
	}
	// In case start or end were to change.
	for c := 0; c < 2; c++ {
		for i := 0; i < start; i++ {
			oldBandE[c*nbEBands+i] = 0
			oldLogE[c*nbEBands+i] = -28
			oldLogE2[c*nbEBands+i] = -28
		}
		for i := end; i < nbEBands; i++ {
			oldBandE[c*nbEBands+i] = 0
			oldLogE[c*nbEBands+i] = -28
			oldLogE2[c*nbEBands+i] = -28
		}
	}
	st.rng = dec.Rng

	deemphasis(
		outSyn,
		pcm,
		N,
		CC,
		d.downsample,
		mode.Preemph[:],
		st.preemphMemD[:],
		accum,
		d.scratch.deemph[:],
	)
	st.lossDuration = 0
	st.plcDuration = 0
	st.lastFrameType = frameNormal
	st.prefilterAndFold = false
	if dec.Tell() > 8*length {
		return InternalError
	}
	if dec.Error != 0 {
		st.err = 1
	}
	return frameSize / d.downsample
}

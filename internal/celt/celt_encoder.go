// Ported from libopus 1.6.1 celt/celt_encoder.c (float build, no
// ENABLE_QEXT, no CUSTOM_MODES).

package celt

import (
	"math"

	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
)

// BitrateMax is OPUS_BITRATE_MAX: the bitrate that lets the encoder use
// the whole packet.
const BitrateMax = -1

// encoderState holds the fields of OpusCustomEncoder from rng onward,
// which OPUS_RESET_STATE clears.
type encoderState struct {
	rng            uint32
	spreadDecision int
	delayedIntra   float32
	tonalAverage   int
	lastCodedBands int
	hfAverage      int
	tapsetDecision int

	prefilterPeriod int
	prefilterGain   float32
	prefilterTapset int
	consecTransient int
	analysis        AnalysisInfo
	silkInfo        SILKInfo

	preemphMemE [2]float32

	// VBR-related parameters.
	vbrReservoir int32
	vbrDrift     int32
	vbrOffset    int32
	vbrCount     int32
	overlapMax   float32
	stereoSaving float32
	intensity    int
	energyMask   []float32
	specAvg      float32

	// inMem is in_mem: channels*overlap samples.
	inMem [2 * maxOverlap]float32
	// prefilterMem is prefilter_mem: channels*COMBFILTER_MAXPERIOD
	// samples.
	prefilterMem [2 * CombFilterMaxPeriod]float32
	oldBandE     [2 * maxBands]float32
	oldLogE      [2 * maxBands]float32
	oldLogE2     [2 * maxBands]float32
	energyError  [2 * maxBands]float32
}

// encoderScratch holds per-call buffers (the C stack allocations).
type encoderScratch struct {
	enc      entcode.Encoder
	in       [2 * (maxFrame + maxOverlap)]float32
	freq     [2 * maxFrame]float32
	X        [2 * maxFrame]float32
	tmp      [maxFrame + maxOverlap]float32
	pre      [2 * (maxFrame + CombFilterMaxPeriod)]float32
	preCh    [2][]float32
	pitchBuf [(CombFilterMaxPeriod + maxFrame) >> 1]float32

	bandE            [2 * maxBands]float32
	bandLogE         [2 * maxBands]float32
	bandLogE2        [2 * maxBands]float32
	errs             [2 * maxBands]float32
	surroundDynalloc [2 * maxBands]float32

	fineQuant    [maxBands]int
	pulses       [maxBands]int
	cap          [maxBands]int
	offsets      [maxBands]int
	importance   [maxBands]int
	spreadWeight [maxBands]int
	finePriority [maxBands]int
	tfRes        [maxBands]int
	collapse     [2 * maxBands]uint8
}

// Encoder is OpusCustomEncoder (CELTEncoder), the CELT encoder state.
type Encoder struct {
	mode           *Mode
	channels       int
	streamChannels int

	forceIntra     bool
	clip           bool
	disablePF      bool
	complexity     int
	upsample       int
	start, end     int
	bitrate        int32
	vbr            int
	signalling     int
	constrainedVBR int
	lossRate       int
	lsbDepth       int
	lfe            int
	disableInv     bool

	// Everything in st gets cleared on a reset.
	st encoderState

	scratch encoderScratch
}

// NewEncoder allocates an encoder and initializes it with Init. It
// returns nil and the error code when Init fails.
func NewEncoder(samplingRate int32, channels int) (*Encoder, int) {
	e := new(Encoder)
	if ret := e.Init(samplingRate, channels); ret != OK {
		return nil, ret
	}
	return e, OK
}

// Init is celt_encoder_init: it initializes the encoder for the static
// 48 kHz mode, encoding input at samplingRate (48, 24, 16, 12 or 8 kHz)
// with channels (1 or 2) input channels.
func (e *Encoder) Init(samplingRate int32, channels int) int {
	ret := e.customInit(StaticMode(), channels)
	if ret != OK {
		return ret
	}
	e.upsample = ResamplingFactor(samplingRate)
	if e.upsample == 0 {
		return BadArg
	}
	return OK
}

// customInit is opus_custom_encoder_init_arch.
func (e *Encoder) customInit(mode *Mode, channels int) int {
	if channels < 0 || channels > 2 {
		return BadArg
	}
	*e = Encoder{}
	e.mode = mode
	e.channels = channels
	e.streamChannels = channels

	e.upsample = 1
	e.start = 0
	e.end = e.mode.EffEBands
	e.signalling = 1

	e.constrainedVBR = 1
	e.clip = true

	e.bitrate = BitrateMax
	e.vbr = 0
	e.forceIntra = false
	e.complexity = 5
	e.lsbDepth = 24

	e.Reset()
	return OK
}

// Reset is OPUS_RESET_STATE. Like the C encoder, it also clears the
// analysis info, the SILK info and the energy mask.
func (e *Encoder) Reset() {
	e.st = encoderState{}
	for i := 0; i < e.channels*e.mode.NbEBands; i++ {
		e.st.oldLogE[i] = -28
		e.st.oldLogE2[i] = -28
	}
	e.st.vbrOffset = 0
	e.st.delayedIntra = 1
	e.st.spreadDecision = spreadNormal
	e.st.tonalAverage = 256
	e.st.hfAverage = 0
	e.st.tapsetDecision = 0
}

// SetComplexity is OPUS_SET_COMPLEXITY.
func (e *Encoder) SetComplexity(value int) int {
	if value < 0 || value > 10 {
		return BadArg
	}
	e.complexity = value
	return OK
}

// SetStartBand is CELT_SET_START_BAND.
func (e *Encoder) SetStartBand(value int) int {
	if value < 0 || value >= e.mode.NbEBands {
		return BadArg
	}
	e.start = value
	return OK
}

// SetEndBand is CELT_SET_END_BAND.
func (e *Encoder) SetEndBand(value int) int {
	if value < 1 || value > e.mode.NbEBands {
		return BadArg
	}
	e.end = value
	return OK
}

// SetPrediction is CELT_SET_PREDICTION: 0 disables the prefilter and
// forces intra energy, 1 disables the prefilter, 2 enables both.
func (e *Encoder) SetPrediction(value int) int {
	if value < 0 || value > 2 {
		return BadArg
	}
	e.disablePF = value <= 1
	e.forceIntra = value == 0
	return OK
}

// SetPacketLossPerc is OPUS_SET_PACKET_LOSS_PERC.
func (e *Encoder) SetPacketLossPerc(value int) int {
	if value < 0 || value > 100 {
		return BadArg
	}
	e.lossRate = value
	return OK
}

// SetVBRConstraint is OPUS_SET_VBR_CONSTRAINT.
func (e *Encoder) SetVBRConstraint(value int) int {
	e.constrainedVBR = value
	return OK
}

// SetVBR is OPUS_SET_VBR.
func (e *Encoder) SetVBR(value int) int {
	e.vbr = value
	return OK
}

// SetBitrate is OPUS_SET_BITRATE. value is in bits per second, or
// [BitrateMax].
func (e *Encoder) SetBitrate(value int32) int {
	if value <= 500 && value != BitrateMax {
		return BadArg
	}
	value = min(value, int32(750000*e.channels))
	e.bitrate = value
	return OK
}

// SetChannels is CELT_SET_CHANNELS: it sets the number of channels coded
// in the stream (1 or 2).
func (e *Encoder) SetChannels(value int) int {
	if value < 1 || value > 2 {
		return BadArg
	}
	e.streamChannels = value
	return OK
}

// SetLSBDepth is OPUS_SET_LSB_DEPTH.
func (e *Encoder) SetLSBDepth(value int) int {
	if value < 8 || value > 24 {
		return BadArg
	}
	e.lsbDepth = value
	return OK
}

// LSBDepth is OPUS_GET_LSB_DEPTH.
func (e *Encoder) LSBDepth() int { return e.lsbDepth }

// SetPhaseInversionDisabled is OPUS_SET_PHASE_INVERSION_DISABLED.
func (e *Encoder) SetPhaseInversionDisabled(value int) int {
	if value < 0 || value > 1 {
		return BadArg
	}
	e.disableInv = value != 0
	return OK
}

// PhaseInversionDisabled is OPUS_GET_PHASE_INVERSION_DISABLED.
func (e *Encoder) PhaseInversionDisabled() int { return b2i(e.disableInv) }

// SetSignalling is CELT_SET_SIGNALLING. Without CUSTOM_MODES the C
// encoder asserts that signalling is 0 when it encodes; this port does
// not write the custom-mode header either, but the value still enters
// the CBR packet size computation, as in C.
func (e *Encoder) SetSignalling(value int) { e.signalling = value }

// SetAnalysis is CELT_SET_ANALYSIS: it copies *info into the encoder. A
// nil info is ignored.
func (e *Encoder) SetAnalysis(info *AnalysisInfo) {
	if info != nil {
		e.st.analysis = *info
	}
}

// SetSILKInfo is CELT_SET_SILK_INFO: it copies *info into the encoder. A
// nil info is ignored.
func (e *Encoder) SetSILKInfo(info *SILKInfo) {
	if info != nil {
		e.st.silkInfo = *info
	}
}

// Mode is CELT_GET_MODE.
func (e *Encoder) Mode() *Mode { return e.mode }

// FinalRange is OPUS_GET_FINAL_RANGE.
func (e *Encoder) FinalRange() uint32 { return e.st.rng }

// SetLFE is OPUS_SET_LFE.
func (e *Encoder) SetLFE(value int) int {
	e.lfe = value
	return OK
}

// SetEnergyMask is OPUS_SET_ENERGY_MASK. The encoder keeps a reference
// to mask (the C pointer), which must hold channels*21 values while it
// is set; nil clears it. Reset also clears it.
func (e *Encoder) SetEnergyMask(mask []float32) int {
	e.st.energyMask = mask
	return OK
}

// Channels returns the number of input channels.
func (e *Encoder) Channels() int { return e.channels }

// StreamChannels returns the number of coded channels.
func (e *Encoder) StreamChannels() int { return e.streamChannels }

// invTable is the table of 6*64/x of transient_analysis, trained on real
// data to minimize the average error.
var invTable = [128]uint8{
	255, 255, 156, 110, 86, 70, 59, 51, 45, 40, 37, 33, 31, 28, 26, 25,
	23, 22, 21, 20, 19, 18, 17, 16, 16, 15, 15, 14, 13, 13, 12, 12,
	12, 12, 11, 11, 11, 10, 10, 10, 9, 9, 9, 9, 9, 9, 8, 8,
	8, 8, 8, 7, 7, 7, 7, 7, 7, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 2,
}

// transientAnalysis is transient_analysis. tmp is scratch of at least
// length values.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func transientAnalysis(
	in []float32,
	length, C int,
	tfEstimate *float32,
	tfChan *int,
	allowWeakTransients bool,
	weakTransient *bool,
	toneFreq, toneishness float32,
	tmp []float32,
) bool {
	var maskMetric int32
	// Forward masking: 6.7 dB/ms.
	forwardDecay := float32(.0625)

	*weakTransient = false
	// For lower bitrates, let's be more conservative and have a forward
	// masking decay of 3.3 dB/ms. This avoids having to code transients
	// at very low bitrate (mostly for hybrid), which can result in
	// unstable energy and/or partial collapse.
	if allowWeakTransients {
		forwardDecay = .03125
	}
	len2 := length / 2
	for c := 0; c < C; c++ {
		var unmask int32
		var mem0, mem1 float32
		// High-pass filter: (1 - 2*z^-1 + z^-2) / (1 - z^-1 + .5*z^-2)
		for i := 0; i < length; i++ {
			x := in[i+c*length]
			y := mem0 + x
			// Modified code to shorten dependency chains.
			mem00 := mem0
			mem0 = mem0 - x + float32(.5*mem1)
			mem1 = x - mem00
			tmp[i] = y
		}
		// First few samples are bad because we don't propagate the memory.
		clear(tmp[:12])

		var mean float32
		mem0 = 0
		// Grouping by two to reduce complexity. Forward pass to compute
		// the post-echo threshold.
		for i := 0; i < len2; i++ {
			x2 := float32(tmp[2*i]*tmp[2*i]) + float32(tmp[2*i+1]*tmp[2*i+1])
			mean += x2
			mem0 = x2 + float32((1-forwardDecay)*mem0)
			tmp[i] = float32(forwardDecay * mem0)
		}

		mem0 = 0
		var maxE float32
		// Backward pass to compute the pre-echo threshold.
		for i := len2 - 1; i >= 0; i-- {
			// Backward masking: 13.9 dB/ms.
			mem0 = tmp[i] + float32(.875*mem0)
			tmp[i] = float32(.125 * mem0)
			maxE = dsp.Max16(maxE, float32(.125*mem0))
		}

		// Compute the ratio of the "frame energy" over the harmonic mean
		// of the energy. As a compromise with the old transient
		// detector, frame energy is the geometric mean of the energy and
		// half the max. The C expression is evaluated in double after
		// the first product.
		mean = float32(math.Sqrt(float64(float32(mean*maxE)) * .5 * float64(len2)))
		// Inverse of the mean energy in Q15+6.
		norm := float32(len2) / (1e-15 + mean)
		// Compute harmonic mean discarding the unreliable boundaries.
		// The data is smooth, so we only take 1/4th of the samples.
		unmask = 0
		for i := 12; i < len2-5; i += 4 {
			f := math.Floor(float64(float32(float32(64*norm) * (tmp[i] + 1e-15))))
			if 127 < f {
				f = 127
			}
			if 0 > f {
				f = 0
			}
			unmask += int32(invTable[int(f)])
		}
		// Normalize, compensate for the 1/4th of the sample and the
		// factor of 6 in the inverse table.
		unmask = 64 * unmask * 4 / int32(6*(len2-17))
		if unmask > maskMetric {
			*tfChan = c
			maskMetric = unmask
		}
	}
	isTransient := maskMetric > 200
	// Prevent the transient detector from confusing the partial cycle of
	// a very low frequency tone with a transient.
	if toneishness > .98 && toneFreq < 0.026 {
		isTransient = false
		maskMetric = 0
	}
	// For low bitrates, define "weak transients" that need to be handled
	// differently to avoid partial collapse.
	if allowWeakTransients && isTransient && maskMetric < 600 {
		isTransient = false
		*weakTransient = true
	}
	// Arbitrary metric for VBR boost.
	tfMax := dsp.Max16(0, float32(math.Sqrt(float64(27*maskMetric)))-42)
	// *tf_estimate = 1 + MIN16(1, sqrt(MAX16(0, tf_max-30))/20);
	p := float64(float32(float32(0.0069) * dsp.Min16(163, tfMax)))
	d := p - 0.139
	if 0 > d {
		d = 0
	}
	*tfEstimate = float32(math.Sqrt(d))
	return isTransient
}

// patchTransientDecision is patch_transient_decision: it looks for
// sudden increases of energy to decide whether we need to patch the
// transient decision.
func patchTransientDecision(newE, oldE []float32, nbEBands, start, end, C int) bool {
	var meanDiff float32
	var spreadOld [26]float32
	// Apply an aggressive (-6 dB/Bark) spreading function to the old
	// frame to avoid false detection caused by irrelevant bands.
	if C == 1 {
		spreadOld[start] = oldE[start]
		for i := start + 1; i < end; i++ {
			spreadOld[i] = dsp.Max32(spreadOld[i-1]-1, oldE[i])
		}
	} else {
		spreadOld[start] = dsp.Max32(oldE[start], oldE[start+nbEBands])
		for i := start + 1; i < end; i++ {
			spreadOld[i] = dsp.Max32(spreadOld[i-1]-1, dsp.Max32(oldE[i], oldE[i+nbEBands]))
		}
	}
	for i := end - 2; i >= start; i-- {
		spreadOld[i] = dsp.Max32(spreadOld[i], spreadOld[i+1]-1)
	}
	// Compute mean increase.
	for c := 0; c < C; c++ {
		for i := max(2, start); i < end-1; i++ {
			x1 := dsp.Max32(0, newE[i+c*nbEBands])
			x2 := dsp.Max32(0, spreadOld[i])
			meanDiff = meanDiff + dsp.Max32(0, x1-x2)
		}
	}
	meanDiff = meanDiff / float32(C*(end-1-max(2, start)))
	return meanDiff > 1
}

// computeMDCTs is compute_mdcts: it applies the window and computes the
// MDCT for all sub-frames and all channels in a frame.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func computeMDCTs(mode *Mode, shortBlocks int, in, out []float32, C, CC, LM, upsample int) {
	overlap := mode.Overlap
	var N, B, shift int
	if shortBlocks != 0 {
		B = shortBlocks
		N = mode.ShortMdctSize
		shift = mode.MaxLM
	} else {
		B = 1
		N = mode.ShortMdctSize << uint(LM)
		shift = mode.MaxLM - LM
	}
	for c := 0; c < CC; c++ {
		for b := 0; b < B; b++ {
			// Interleaving the sub-frames while doing the MDCTs.
			dsp.CltMDCTForward(
				mode.MDCT,
				in[c*(B*N+overlap)+b*N:],
				out[b+c*N*B:],
				mode.Window,
				overlap,
				shift,
				B,
			)
		}
	}
	if CC == 2 && C == 1 {
		for i := 0; i < B*N; i++ {
			out[i] = float32(.5*out[i]) + float32(.5*out[B*N+i])
		}
	}
	if upsample != 1 {
		for c := 0; c < C; c++ {
			bound := B * N / upsample
			for i := 0; i < bound; i++ {
				out[c*B*N+i] = float32(out[c*B*N+i] * float32(upsample))
			}
			clear(out[c*B*N+bound : c*B*N+B*N])
		}
	}
}

// Preemphasis is celt_preemphasis: it applies the pre-emphasis filter
// coef to N samples of channel 0 of pcm (interleaved with CC channels;
// pass pcm[c:] for channel c), scaled to the celt_sig range, and writes
// them to inp. upsample > 1 inserts zeros. clip limits the input to
// +/-65536 (in the celt_sig scale) first. *mem holds the filter state.
func Preemphasis(pcm, inp []float32, N, CC, upsample int, coef []float32, mem *float32, clip bool) {
	coef0 := coef[0]
	m := *mem

	// Fast path for the normal 48kHz case and no clipping.
	if coef[1] == 0 && upsample == 1 && !clip {
		for i := 0; i < N; i++ {
			x := dsp.Res2Sig(pcm[CC*i])
			// Apply pre-emphasis.
			inp[i] = x - m
			m = float32(coef0 * x)
		}
		*mem = m
		return
	}

	Nu := N / upsample
	if upsample != 1 {
		clear(inp[:N])
	}
	for i := 0; i < Nu; i++ {
		inp[i*upsample] = dsp.Res2Sig(pcm[CC*i])
	}

	if clip {
		// Clip input to avoid encoding non-portable files.
		for i := 0; i < Nu; i++ {
			inp[i*upsample] = dsp.Max32(-65536, dsp.Min32(65536, inp[i*upsample]))
		}
	}
	for i := 0; i < N; i++ {
		x := inp[i]
		// Apply pre-emphasis.
		inp[i] = x - m
		m = float32(coef0 * x)
	}
	*mem = m
}

// l1Metric is l1_metric.
func l1Metric(tmp []float32, N, LM int, bias float32) float32 {
	var L1 float32
	for i := 0; i < N; i++ {
		L1 += dsp.Abs16(tmp[i])
	}
	// When in doubt, prefer good freq resolution.
	L1 = L1 + float32(float32(float32(LM)*bias)*L1)
	return L1
}

// tfAnalysis is tf_analysis.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func tfAnalysis(
	m *Mode,
	length int,
	isTransient bool,
	tfRes []int,
	lambda int,
	X []float32,
	N0, LM int,
	tfEstimate float32,
	tfChan int,
	importance []int,
) int {
	var metric, path0, path1 [maxBands]int
	var tmp, tmp1 [maxBandSize]float32
	var selcost [2]int
	tfSelect := 0
	it := b2i(isTransient)

	bias := float32(.04 * dsp.Max16(-.25, .5-tfEstimate))

	for i := 0; i < length; i++ {
		bestLevel := 0
		N := int(m.EBands[i+1]-m.EBands[i]) << uint(LM)
		// Band is too narrow to be split down to LM=-1.
		narrow := m.EBands[i+1]-m.EBands[i] == 1
		copy(tmp[:N], X[tfChan*N0+int(m.EBands[i])<<uint(LM):])
		// Just add the right channel if we're in stereo.
		lm := 0
		if isTransient {
			lm = LM
		}
		L1 := l1Metric(tmp[:], N, lm, bias)
		bestL1 := L1
		// Check the -1 case for transients.
		if isTransient && !narrow {
			copy(tmp1[:N], tmp[:N])
			haar1(tmp1[:], N>>uint(LM), 1<<uint(LM))
			L1 = l1Metric(tmp1[:], N, LM+1, bias)
			if L1 < bestL1 {
				bestL1 = L1
				bestLevel = -1
			}
		}
		kmax := LM
		if !isTransient && !narrow {
			kmax++
		}
		for k := 0; k < kmax; k++ {
			var B int
			if isTransient {
				B = LM - k - 1
			} else {
				B = k + 1
			}
			haar1(tmp[:], N>>uint(k), 1<<uint(k))
			L1 = l1Metric(tmp[:], N, B, bias)
			if L1 < bestL1 {
				bestL1 = L1
				bestLevel = k + 1
			}
		}
		// metric is in Q1 to be able to select the mid-point (-0.5) for
		// narrower bands.
		if isTransient {
			metric[i] = 2 * bestLevel
		} else {
			metric[i] = -2 * bestLevel
		}
		// For bands that can't be split to -1, set the metric to the
		// half-way point to avoid biasing the decision.
		if narrow && (metric[i] == 0 || metric[i] == -2*LM) {
			metric[i]--
		}
	}
	tsel := &tfSelectTable[LM]
	// Search for the optimal tf resolution, including tf_select.
	tfSelect = 0
	for sel := 0; sel < 2; sel++ {
		cost0 := importance[0] * iabs(metric[0]-2*int(tsel[4*it+2*sel+0]))
		cost1 := importance[0] * iabs(metric[0]-2*int(tsel[4*it+2*sel+1]))
		if !isTransient {
			cost1 += lambda
		}
		for i := 1; i < length; i++ {
			curr0 := min(cost0, cost1+lambda)
			curr1 := min(cost0+lambda, cost1)
			cost0 = curr0 + importance[i]*iabs(metric[i]-2*int(tsel[4*it+2*sel+0]))
			cost1 = curr1 + importance[i]*iabs(metric[i]-2*int(tsel[4*it+2*sel+1]))
		}
		cost0 = min(cost0, cost1)
		selcost[sel] = cost0
	}
	// For now, we're conservative and only allow tf_select=1 for
	// transients. If tests confirm it's useful for non-transients, we
	// could allow it.
	if selcost[1] < selcost[0] && isTransient {
		tfSelect = 1
	}
	cost0 := importance[0] * iabs(metric[0]-2*int(tsel[4*it+2*tfSelect+0]))
	cost1 := importance[0] * iabs(metric[0]-2*int(tsel[4*it+2*tfSelect+1]))
	if !isTransient {
		cost1 += lambda
	}
	// Viterbi forward pass.
	for i := 1; i < length; i++ {
		var curr0, curr1 int
		from0 := cost0
		from1 := cost1 + lambda
		if from0 < from1 {
			curr0 = from0
			path0[i] = 0
		} else {
			curr0 = from1
			path0[i] = 1
		}

		from0 = cost0 + lambda
		from1 = cost1
		if from0 < from1 {
			curr1 = from0
			path1[i] = 0
		} else {
			curr1 = from1
			path1[i] = 1
		}
		cost0 = curr0 + importance[i]*iabs(metric[i]-2*int(tsel[4*it+2*tfSelect+0]))
		cost1 = curr1 + importance[i]*iabs(metric[i]-2*int(tsel[4*it+2*tfSelect+1]))
	}
	if cost0 < cost1 {
		tfRes[length-1] = 0
	} else {
		tfRes[length-1] = 1
	}
	// Viterbi backward pass to check the decisions.
	for i := length - 2; i >= 0; i-- {
		if tfRes[i+1] == 1 {
			tfRes[i] = path1[i+1]
		} else {
			tfRes[i] = path0[i+1]
		}
	}
	return tfSelect
}

// tfEncode is tf_encode.
func tfEncode(
	start, end int,
	isTransient bool,
	tfRes []int,
	LM, tfSelect int,
	enc *entcode.Encoder,
) {
	it := b2i(isTransient)
	budget := enc.Storage * 8
	tell := uint32(enc.Tell())
	logp := uint32(4)
	if isTransient {
		logp = 2
	}
	// Reserve space to code the tf_select decision.
	tfSelectRsv := LM > 0 && tell+logp+1 <= budget
	budget -= uint32(b2i(tfSelectRsv))
	curr := 0
	tfChanged := 0
	for i := start; i < end; i++ {
		if tell+logp <= budget {
			enc.EncBitLogp(tfRes[i]^curr, uint(logp))
			tell = uint32(enc.Tell())
			curr = tfRes[i]
			tfChanged |= curr
		} else {
			tfRes[i] = curr
		}
		if isTransient {
			logp = 4
		} else {
			logp = 5
		}
	}
	// Only code tf_select if it would actually make a difference.
	if tfSelectRsv && tfSelectTable[LM][4*it+0+tfChanged] != tfSelectTable[LM][4*it+2+tfChanged] {
		enc.EncBitLogp(tfSelect, 1)
	} else {
		tfSelect = 0
	}
	for i := start; i < end; i++ {
		tfRes[i] = int(tfSelectTable[LM][4*it+2*tfSelect+tfRes[i]])
	}
}

// allocTrimAnalysis is alloc_trim_analysis.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func allocTrimAnalysis(
	m *Mode,
	X []float32,
	bandLogE []float32,
	end, LM, C, N0 int,
	analysis *AnalysisInfo,
	stereoSaving *float32,
	tfEstimate float32,
	intensity int,
	surroundTrim float32,
	equivRate int32,
) int {
	var diff float32
	trim := float32(5)
	// At low bitrate, reducing the trim seems to help. At higher
	// bitrates, it's less clear what's best, so we're keeping it as it
	// was before, at least for now.
	if equivRate < 64000 {
		trim = 4
	} else if equivRate < 80000 {
		frac := (equivRate - 64000) >> 10
		trim = 4 + float32((1.0/16.0)*float32(frac))
	}
	if C == 2 {
		var sum float32
		// Compute inter-channel correlation for low frequencies.
		for i := 0; i < 8; i++ {
			off := int(m.EBands[i]) << uint(LM)
			partial := dsp.CeltInnerProd(
				X[off:],
				X[N0+off:],
				int(m.EBands[i+1]-m.EBands[i])<<uint(LM),
			)
			sum = sum + partial
		}
		sum = float32((1.0 / 8) * sum)
		sum = dsp.Min16(1, dsp.Abs16(sum))
		minXC := sum
		for i := 8; i < intensity; i++ {
			off := int(m.EBands[i]) << uint(LM)
			partial := dsp.CeltInnerProd(
				X[off:],
				X[N0+off:],
				int(m.EBands[i+1]-m.EBands[i])<<uint(LM),
			)
			minXC = dsp.Min16(minXC, dsp.Abs16(partial))
		}
		minXC = dsp.Min16(1, dsp.Abs16(minXC))
		// Mid-side savings estimations based on the LF average.
		logXC := dsp.CeltLog2(1.001 - float32(sum*sum))
		// Mid-side savings estimations based on min correlation.
		logXC2 := dsp.Max16(float32(.5*logXC), dsp.CeltLog2(1.001-float32(minXC*minXC)))

		trim += dsp.Max16(-4, float32(.75*logXC))
		*stereoSaving = dsp.Min16(*stereoSaving+0.25, -float32(.5*logXC2))
	}

	// Estimate spectral tilt.
	for c := 0; c < C; c++ {
		for i := 0; i < end-1; i++ {
			diff += float32(bandLogE[i+c*m.NbEBands] * float32(2+2*i-end))
		}
	}
	diff /= float32(C * (end - 1))
	trim -= dsp.Max32(-2, dsp.Min32(2, (diff+1)/6))
	trim -= surroundTrim
	trim -= float32(2 * tfEstimate)
	if analysis.Valid != 0 {
		trim -= dsp.Max16(-2, dsp.Min16(2, float32(2*(analysis.TonalitySlope+.05))))
	}

	trimIndex := floorInt(.5 + trim)
	trimIndex = max(0, min(10, trimIndex))
	return trimIndex
}

// stereoAnalysis is stereo_analysis.
func stereoAnalysis(m *Mode, X []float32, LM, N0 int) bool {
	sumLR := float32(1e-15)
	sumMS := float32(1e-15)

	// Use the L1 norm to model the entropy of the L/R signal vs the M/S
	// signal.
	for i := 0; i < 13; i++ {
		for j := int(m.EBands[i]) << uint(LM); j < int(m.EBands[i+1])<<uint(LM); j++ {
			L := X[j]
			R := X[N0+j]
			M := L + R
			S := L - R
			sumLR = sumLR + (dsp.Abs32(L) + dsp.Abs32(R))
			sumMS = sumMS + (dsp.Abs32(M) + dsp.Abs32(S))
		}
	}
	sumMS = float32(0.707107 * sumMS)
	thetas := 13
	// We don't need thetas for lower bands with LM<=1.
	if LM <= 1 {
		thetas -= 8
	}
	return float32(float32((int(m.EBands[13])<<uint(LM+1))+thetas)*sumMS) >
		float32(float32(int(m.EBands[13])<<uint(LM+1))*sumLR)
}

// medianOf5 is median_of_5.
func medianOf5(x []float32) float32 {
	var t0, t1, t3, t4 float32
	t2 := x[2]
	if x[0] > x[1] {
		t0 = x[1]
		t1 = x[0]
	} else {
		t0 = x[0]
		t1 = x[1]
	}
	if x[3] > x[4] {
		t3 = x[4]
		t4 = x[3]
	} else {
		t3 = x[3]
		t4 = x[4]
	}
	if t0 > t3 {
		t3 = t0
		t1, t4 = t4, t1
	}
	if t2 > t1 {
		if t1 < t3 {
			return dsp.Min32(t2, t3)
		}
		return dsp.Min32(t4, t1)
	}
	if t2 < t3 {
		return dsp.Min32(t1, t3)
	}
	return dsp.Min32(t2, t4)
}

// medianOf3 is median_of_3.
func medianOf3(x []float32) float32 {
	var t0, t1 float32
	if x[0] > x[1] {
		t0 = x[1]
		t1 = x[0]
	} else {
		t0 = x[0]
		t1 = x[1]
	}
	t2 := x[2]
	if t1 < t2 {
		return t1
	} else if t0 < t2 {
		return t2
	}
	return t0
}

// dynallocAnalysis is dynalloc_analysis. It returns maxDepth.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func dynallocAnalysis(
	bandLogE, bandLogE2, oldBandE []float32,
	nbEBands, start, end, C int,
	offsets []int,
	lsbDepth int,
	logN []int16,
	isTransient bool,
	vbr, constrainedVBR int,
	eBands []int16,
	LM,
	effectiveBytes int,
	totBoostOut *int32,
	lfe bool,
	surroundDynalloc []float32,
	analysis *AnalysisInfo,
	importance, spreadWeight []int,
	toneFreq, toneishness float32,
) float32 {
	var totBoost int32
	var follower, noiseFloor [2 * maxBands]float32
	var bandLogE3 [maxBands]float32
	clear(offsets[:nbEBands])
	// Dynamic allocation code.
	maxDepth := float32(-31.9)
	for i := 0; i < end; i++ {
		// Noise floor must take into account eMeans, the depth, the
		// width of the bands and the preemphasis filter (approx. square
		// of bark band ID).
		noiseFloor[i] = float32(0.0625*float32(logN[i])) + .5 + float32(9-lsbDepth) - eMeans[i] +
			float32(float32(.0062*float32(i+5))*float32(i+5))
	}
	for c := 0; c < C; c++ {
		for i := 0; i < end; i++ {
			maxDepth = dsp.Max32(maxDepth, bandLogE[c*nbEBands+i]-noiseFloor[i])
		}
	}
	{
		// Compute a really simple masking model to avoid taking into
		// account completely masked bands when computing the spreading
		// decision.
		var mask, sig [maxBands]float32
		for i := 0; i < end; i++ {
			mask[i] = bandLogE[i] - noiseFloor[i]
		}
		if C == 2 {
			for i := 0; i < end; i++ {
				mask[i] = dsp.Max32(mask[i], bandLogE[nbEBands+i]-noiseFloor[i])
			}
		}
		copy(sig[:end], mask[:end])
		for i := 1; i < end; i++ {
			mask[i] = dsp.Max32(mask[i], mask[i-1]-2)
		}
		for i := end - 2; i >= 0; i-- {
			mask[i] = dsp.Max32(mask[i], mask[i+1]-3)
		}
		for i := 0; i < end; i++ {
			// Compute SMR: Mask is never more than 72 dB below the peak
			// and never below the noise floor.
			smr := sig[i] - dsp.Max32(dsp.Max32(0, maxDepth-12), mask[i])
			// Clamp SMR to make sure we're not shifting by something
			// negative or too large.
			shift := min(5, max(0, int(-int32(floorInt(.5+smr)))))
			spreadWeight[i] = 32 >> uint(shift)
		}
	}
	// Make sure that dynamic allocation can't make us bust the budget.
	// We enable the feature starting at 24 kb/s for 20-ms frames and 96
	// kb/s for 2.5 ms frames.
	if effectiveBytes >= 30+5*LM && !lfe {
		last := 0
		for c := 0; c < C; c++ {
			copy(bandLogE3[:end], bandLogE2[c*nbEBands:c*nbEBands+end])
			if LM == 0 {
				// For 2.5 ms frames, the first 8 bands have just one
				// bin, so the energy is highly unreliable (high
				// variance). For that reason, we take the max with the
				// previous energy so that at least 2 bins are getting
				// used.
				for i := 0; i < min(8, end); i++ {
					bandLogE3[i] = dsp.Max32(bandLogE2[c*nbEBands+i], oldBandE[c*nbEBands+i])
				}
			}
			f := follower[c*nbEBands:]
			f[0] = bandLogE3[0]
			for i := 1; i < end; i++ {
				// The last band to be at least 3 dB higher than the
				// previous one is the last we'll consider. Otherwise, we
				// run into problems on bandlimited signals.
				if bandLogE3[i] > bandLogE3[i-1]+.5 {
					last = i
				}
				f[i] = dsp.Min32(f[i-1]+1.5, bandLogE3[i])
			}
			for i := last - 1; i >= 0; i-- {
				f[i] = dsp.Min32(f[i], dsp.Min32(f[i+1]+2, bandLogE3[i]))
			}

			// Combine with a median filter to avoid dynalloc triggering
			// unnecessarily. The "offset" value controls how
			// conservative we are -- a higher offset reduces the impact
			// of the median filter and makes dynalloc use more bits.
			offset := float32(1)
			for i := 2; i < end-2; i++ {
				f[i] = dsp.Max32(f[i], medianOf5(bandLogE3[i-2:])-offset)
			}
			tmp := medianOf3(bandLogE3[0:]) - offset
			f[0] = dsp.Max32(f[0], tmp)
			f[1] = dsp.Max32(f[1], tmp)
			tmp = medianOf3(bandLogE3[end-3:]) - offset
			f[end-2] = dsp.Max32(f[end-2], tmp)
			f[end-1] = dsp.Max32(f[end-1], tmp)

			for i := 0; i < end; i++ {
				f[i] = dsp.Max32(f[i], noiseFloor[i])
			}
		}
		if C == 2 {
			for i := start; i < end; i++ {
				// Consider 24 dB "cross-talk".
				follower[nbEBands+i] = dsp.Max32(follower[nbEBands+i], follower[i]-4)
				follower[i] = dsp.Max32(follower[i], follower[nbEBands+i]-4)
				follower[i] = float32(
					.5 * (dsp.Max32(0, bandLogE[i]-follower[i]) + dsp.Max32(0, bandLogE[nbEBands+i]-follower[nbEBands+i])),
				)
			}
		} else {
			for i := start; i < end; i++ {
				follower[i] = dsp.Max32(0, bandLogE[i]-follower[i])
			}
		}
		for i := start; i < end; i++ {
			follower[i] = dsp.Max32(follower[i], surroundDynalloc[i])
		}
		for i := start; i < end; i++ {
			importance[i] = floorInt(.5 + float32(13*dsp.CeltExp2(dsp.Min32(follower[i], 4))))
		}
		// For non-transient CBR/CVBR frames, halve the dynalloc
		// contribution.
		if (vbr == 0 || constrainedVBR != 0) && !isTransient {
			for i := start; i < end; i++ {
				follower[i] = float32(.5 * follower[i])
			}
		}
		for i := start; i < end; i++ {
			if i < 8 {
				follower[i] = float32(follower[i] * 2)
			}
			if i >= 12 {
				follower[i] = float32(.5 * follower[i])
			}
		}
		// Compensate for Opus' under-allocation on tones.
		if toneishness > .98 {
			freqBin := truncInt(math.Floor(.5 + float64(float32(toneFreq*120))/math.Pi))
			for i := start; i < end; i++ {
				lo := int(eBands[i])
				hi := int(eBands[i+1])
				if freqBin >= lo && freqBin <= hi {
					follower[i] += 2
				}
				if freqBin >= lo-1 && freqBin <= hi+1 {
					follower[i]++
				}
				if freqBin >= lo-2 && freqBin <= hi+2 {
					follower[i]++
				}
				if freqBin >= lo-3 && freqBin <= hi+3 {
					follower[i] += .5
				}
			}
			if freqBin >= int(eBands[end]) {
				follower[end-1] += 2
				follower[end-2]++
			}
		}
		if analysis.Valid != 0 {
			for i := start; i < min(LeakBands, end); i++ {
				follower[i] = follower[i] + float32((1.0/64.0)*float32(analysis.LeakBoost[i]))
			}
		}
		for i := start; i < end; i++ {
			var boost, boostBits int
			follower[i] = dsp.Min32(follower[i], 4)
			width := C * int(eBands[i+1]-eBands[i]) << uint(LM)
			if width < 6 {
				boost = truncInt(float64(follower[i]))
				boostBits = boost * width << bitRes
			} else if width > 48 {
				boost = truncInt(float64(float32(follower[i] * 8)))
				boostBits = (boost * width << bitRes) / 8
			} else {
				boost = truncInt(float64(float32(follower[i]*float32(width)) / 6))
				boostBits = boost * 6 << bitRes
			}
			// For CBR and non-transient CVBR frames, limit dynalloc to
			// 2/3 of the bits.
			if (vbr == 0 || (constrainedVBR != 0 && !isTransient)) &&
				(int(totBoost)+boostBits)>>bitRes>>3 > 2*effectiveBytes/3 {
				caps := int32((2 * effectiveBytes / 3) << bitRes << 3)
				offsets[i] = int(caps - totBoost)
				totBoost = caps
				break
			}
			offsets[i] = boost
			totBoost += int32(boostBits)
		}
	} else {
		for i := start; i < end; i++ {
			importance[i] = 13
		}
	}
	*totBoostOut = totBoost
	return maxDepth
}

// toneLPC is tone_lpc: it computes the LPC coefficients using a
// least-squares fit for both forward and backward prediction. It returns
// true when the fit fails.
func toneLPC(x []float32, length, delay int, lpc *[2]float32) bool {
	var r00, r01, r11, r02, r12, r22 float32
	// Compute correlations as if using the forward prediction covariance
	// method.
	for i := 0; i < length-2*delay; i++ {
		r00 += float32(x[i] * x[i])
		r01 += float32(x[i] * x[i+delay])
		r02 += float32(x[i] * x[i+2*delay])
	}
	var edges float32
	for i := 0; i < delay; i++ {
		edges += float32(x[length+i-2*delay]*x[length+i-2*delay]) - float32(x[i]*x[i])
	}
	r11 = r00 + edges
	edges = 0
	for i := 0; i < delay; i++ {
		edges += float32(x[length+i-delay]*x[length+i-delay]) - float32(x[i+delay]*x[i+delay])
	}
	r22 = r11 + edges
	edges = 0
	for i := 0; i < delay; i++ {
		edges += float32(x[length+i-2*delay]*x[length+i-delay]) - float32(x[i]*x[i+delay])
	}
	r12 = r01 + edges
	// Reverse and sum to get the backward contribution.
	{
		R00 := r00 + r22
		R01 := r01 + r12
		R11 := 2 * r11
		R02 := 2 * r02
		R12 := r12 + r01
		R22 := r00 + r22
		r00 = R00
		r01 = R01
		r11 = R11
		r02 = R02
		r12 = R12
		r22 = R22
	}
	_ = r22
	// Solve A*x=b, where A=[r00, r01; r01, r11] and b=[r02; r12].
	den := float32(r00*r11) - float32(r01*r01)
	if den < float32(.001*float32(r00*r11)) {
		return true
	}
	num1 := float32(r02*r11) - float32(r01*r12)
	if num1 >= den {
		lpc[1] = 1
	} else if num1 <= -den {
		lpc[1] = -1
	} else {
		lpc[1] = dsp.FracDiv32(num1, den)
	}
	num0 := float32(r00*r12) - float32(r02*r01)
	if float32(.5*num0) >= den {
		lpc[0] = 1.999999
	} else if float32(.5*num0) <= -den {
		lpc[0] = -1.999999
	} else {
		lpc[0] = dsp.FracDiv32(num0, den)
	}
	return false
}

// toneDetect is tone_detect: it detects pure or nearly pure tones so we
// can prevent them from causing problems with the encoder. x is scratch
// of at least N values.
func toneDetect(in []float32, CC, N int, toneishness *float32, Fs int32, x []float32) float32 {
	delay := 1
	var lpc [2]float32
	var freq float32
	if CC == 2 {
		for i := 0; i < N; i++ {
			x[i] = in[i] + in[i+N]
		}
	} else {
		copy(x[:N], in[:N])
	}
	fail := toneLPC(x, N, delay, &lpc)
	// If our LPC filter resonates too close to DC, retry the analysis
	// with down-sampling.
	for delay <= int(Fs/3000) && (fail || (lpc[0] > 1 && lpc[1] < 0)) {
		delay *= 2
		fail = toneLPC(x, N, delay, &lpc)
	}
	// Check that our filter has complex roots.
	if !fail && float64(float32(lpc[0]*lpc[0]))+float64(3.999999*float64(lpc[1])) < 0 {
		// Squared radius of the poles.
		*toneishness = -lpc[1]
		freq = float32(math.Acos(float64(float32(.5*lpc[0]))) / float64(delay))
	} else {
		freq = -1
		*toneishness = 0
	}
	return freq
}

// runPrefilter is run_prefilter.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func (e *Encoder) runPrefilter(
	in []float32,
	CC, N, prefilterTapset int,
	pitch *int,
	gain *float32,
	qgain *int,
	enabled bool,
	complexity int,
	tfEstimate float32,
	nbAvailableBytes int,
	analysis *AnalysisInfo,
	toneFreq, toneishness float32,
) bool {
	st := &e.st
	var pitchIndex int
	var gain1 float32
	var pfOn bool
	var qg int
	var before, after [2]float32
	cancelPitch := false
	prefilterMem := st.prefilterMem[:]

	maxPeriod := CombFilterMaxPeriod
	minPeriod := CombFilterMinPeriod
	mode := e.mode
	overlap := mode.Overlap

	pre := &e.scratch.preCh
	pre[0] = e.scratch.pre[:N+maxPeriod]
	pre[1] = e.scratch.pre[N+maxPeriod : 2*(N+maxPeriod)]

	for c := 0; c < CC; c++ {
		copy(pre[c][:maxPeriod], prefilterMem[c*maxPeriod:(c+1)*maxPeriod])
		copy(pre[c][maxPeriod:maxPeriod+N], in[c*(N+overlap)+overlap:])
	}

	// If we detect that the signal is dominated by a single tone, don't
	// rely on the standard pitch estimator, as it can become unreliable.
	if enabled && toneishness > .99 {
		multiple := 1
		// Using aliased version of the postfilter above 24 kHz. First
		// value is purposely slightly above pi to avoid triggering for
		// Fs=48kHz.
		if toneFreq >= 3.1416 {
			toneFreq = 3.141593 - toneFreq
		}
		// If the pitch is too high for our post-filter, apply pitch
		// doubling until we can get something that fits (not ideal, but
		// better than nothing).
		for toneFreq >= float32(float32(multiple)*0.39) {
			multiple++
		}
		if toneFreq > 0.006148 {
			pitchIndex = min(
				truncInt(math.Floor(.5+2.*math.Pi*float64(multiple)/float64(toneFreq))),
				CombFilterMaxPeriod-2,
			)
		} else {
			// If the pitch is too low, using a very high pitch will
			// actually give us an improvement due to the DC component of
			// the filter that will be close to our tone. Again, not
			// ideal, but if we only have a single tone, it's better than
			// nothing.
			pitchIndex = CombFilterMinPeriod
		}
		gain1 = .75
	} else if enabled && complexity >= 5 {
		pitchBuf := e.scratch.pitchBuf[:(maxPeriod+N)>>1]
		dsp.PitchDownsample(pre[:CC], pitchBuf, (maxPeriod+N)>>1, CC, 2)
		// Don't search for the fir last 1.5 octave of the range because
		// there's too many false-positives due to short-term correlation.
		pitchIndex = dsp.PitchSearch(pitchBuf[maxPeriod>>1:], pitchBuf, N, maxPeriod-3*minPeriod)
		pitchIndex = maxPeriod - pitchIndex

		gain1 = dsp.RemoveDoubling(
			pitchBuf,
			maxPeriod,
			minPeriod,
			N,
			&pitchIndex,
			st.prefilterPeriod,
			st.prefilterGain,
		)
		if pitchIndex > maxPeriod-2 {
			pitchIndex = maxPeriod - 2
		}
		gain1 = float32(.7 * gain1)
		if e.lossRate > 2 {
			gain1 = float32(.5 * gain1)
		}
		if e.lossRate > 4 {
			gain1 = float32(.5 * gain1)
		}
		if e.lossRate > 8 {
			gain1 = 0
		}
	} else {
		gain1 = 0
		pitchIndex = CombFilterMinPeriod
	}
	if analysis.Valid != 0 {
		gain1 = float32(gain1 * analysis.MaxPitchRatio)
	}
	// Gain threshold for enabling the prefilter/postfilter.
	pfThreshold := float32(.2)

	// Adjusting the threshold based on rate and continuity.
	if iabs(pitchIndex-st.prefilterPeriod)*10 > pitchIndex {
		pfThreshold += .2
		// Completely disable the prefilter on strong transients without
		// continuity.
		if tfEstimate > .98 {
			gain1 = 0
		}
	}
	if nbAvailableBytes < 25 {
		pfThreshold += .1
	}
	if nbAvailableBytes < 35 {
		pfThreshold += .1
	}
	if st.prefilterGain > .4 {
		pfThreshold -= .1
	}
	if st.prefilterGain > .55 {
		pfThreshold -= .1
	}

	// Hard threshold at 0.2.
	pfThreshold = dsp.Max16(pfThreshold, .2)
	if gain1 < pfThreshold {
		gain1 = 0
		pfOn = false
		qg = 0
	} else {
		// This block is not gated by a total bits check only because of
		// the nbAvailableBytes check above.
		if dsp.Abs16(gain1-st.prefilterGain) < .1 {
			gain1 = st.prefilterGain
		}

		qg = floorInt(.5+float32(float32(gain1*32)/3)) - 1
		qg = max(0, min(7, qg))
		gain1 = float32(0.09375 * float32(qg+1))
		pfOn = true
	}

	for c := 0; c < CC; c++ {
		offset := mode.ShortMdctSize - overlap
		st.prefilterPeriod = max(st.prefilterPeriod, CombFilterMinPeriod)
		copy(in[c*(N+overlap):c*(N+overlap)+overlap], st.inMem[c*overlap:(c+1)*overlap])
		for i := 0; i < N; i++ {
			before[c] += dsp.Abs32(in[c*(N+overlap)+overlap+i])
		}
		if offset != 0 {
			combFilter(
				in,
				c*(N+overlap)+overlap,
				pre[c],
				maxPeriod,
				st.prefilterPeriod,
				st.prefilterPeriod,
				offset,
				-st.prefilterGain,
				-st.prefilterGain,
				st.prefilterTapset,
				st.prefilterTapset,
				nil,
				0,
			)
		}

		combFilter(in, c*(N+overlap)+overlap+offset, pre[c], maxPeriod+offset,
			st.prefilterPeriod, pitchIndex, N-offset, -st.prefilterGain, -gain1,
			st.prefilterTapset, prefilterTapset, mode.Window, overlap)
		for i := 0; i < N; i++ {
			after[c] += dsp.Abs32(in[c*(N+overlap)+overlap+i])
		}
	}

	if CC == 2 {
		var thresh [2]float32
		thresh[0] = float32(float32(.25*gain1)*before[0]) + float32(.01*before[1])
		thresh[1] = float32(float32(.25*gain1)*before[1]) + float32(.01*before[0])
		// Don't use the filter if one channel gets significantly worse.
		if after[0]-before[0] > thresh[0] || after[1]-before[1] > thresh[1] {
			cancelPitch = true
		}
		// Use the filter only if at least one channel gets significantly
		// better.
		if before[0]-after[0] < thresh[0] && before[1]-after[1] < thresh[1] {
			cancelPitch = true
		}
	} else {
		// Check that the mono channel actually got better.
		if after[0] > before[0] {
			cancelPitch = true
		}
	}
	// If needed, revert to a gain of zero.
	if cancelPitch {
		for c := 0; c < CC; c++ {
			offset := mode.ShortMdctSize - overlap
			copy(in[c*(N+overlap)+overlap:c*(N+overlap)+overlap+N], pre[c][maxPeriod:maxPeriod+N])
			combFilter(in, c*(N+overlap)+overlap+offset, pre[c], maxPeriod+offset,
				st.prefilterPeriod, pitchIndex, overlap, -st.prefilterGain, 0,
				st.prefilterTapset, prefilterTapset, mode.Window, overlap)
		}
		gain1 = 0
		pfOn = false
		qg = 0
	}

	for c := 0; c < CC; c++ {
		copy(st.inMem[c*overlap:(c+1)*overlap], in[c*(N+overlap)+N:c*(N+overlap)+N+overlap])

		if N > maxPeriod {
			copy(prefilterMem[c*maxPeriod:(c+1)*maxPeriod], pre[c][N:N+maxPeriod])
		} else {
			copy(
				prefilterMem[c*maxPeriod:c*maxPeriod+maxPeriod-N],
				prefilterMem[c*maxPeriod+N:(c+1)*maxPeriod],
			)
			copy(
				prefilterMem[c*maxPeriod+maxPeriod-N:(c+1)*maxPeriod],
				pre[c][maxPeriod:maxPeriod+N],
			)
		}
	}

	*gain = gain1
	*pitch = pitchIndex
	*qgain = qg
	return pfOn
}

// computeVBR is compute_vbr.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func computeVBR(
	mode *Mode,
	analysis *AnalysisInfo,
	baseTarget int32,
	LM int,
	bitrate int32,
	lastCodedBands, C,
	intensity, constrainedVBR int,
	stereoSaving float32,
	totBoost int32,
	tfEstimate float32,
	pitchChange bool,
	maxDepth float32,
	lfe bool,
	hasSurroundMask bool,
	surroundMasking, temporalVBR float32,
) int32 {
	nbEBands := mode.NbEBands
	eBands := mode.EBands

	codedBands := nbEBands
	if lastCodedBands != 0 {
		codedBands = lastCodedBands
	}
	codedBins := int(eBands[codedBands]) << uint(LM)
	if C == 2 {
		codedBins += int(eBands[min(intensity, codedBands)]) << uint(LM)
	}

	target := baseTarget

	if analysis.Valid != 0 && float64(analysis.Activity) < .4 {
		target -= int32(
			truncInt(float64(float32(float32(codedBins<<bitRes) * (.4 - analysis.Activity)))),
		)
	}
	// Stereo savings.
	if C == 2 {
		codedStereoBands := min(intensity, codedBands)
		codedStereoDof := (int(eBands[codedStereoBands]) << uint(LM)) - codedStereoBands
		// Maximum fraction of the bits we can save if the signal is mono.
		maxFrac := float32(float32(0.8)*float32(codedStereoDof)) / float32(codedBins)
		stereoSaving = dsp.Min16(stereoSaving, 1)
		target -= int32(truncInt(float64(dsp.Min32(float32(maxFrac*float32(target)),
			float32((stereoSaving-0.1)*float32(codedStereoDof<<bitRes))))))
	}
	// Boost the rate according to dynalloc (minus the dynalloc average
	// for calibration).
	target += totBoost - (19 << uint(LM))
	// Apply transient boost, compensating for average boost.
	tfCalibration := float32(0.044)
	target += int32(truncInt(float64(float32((tfEstimate - tfCalibration) * float32(target)))))

	// Apply tonality boost.
	if analysis.Valid != 0 && !lfe {
		// Tonality boost (compensating for the average).
		tonal := dsp.Max16(0, analysis.Tonality-.15) - 0.12
		tonalTarget := target + int32(
			truncInt(float64(float32(float32(float32(codedBins<<bitRes)*1.2)*tonal))),
		)
		if pitchChange {
			tonalTarget += int32(truncInt(float64(float32(float32(codedBins<<bitRes) * .8))))
		}
		target = tonalTarget
	}

	if hasSurroundMask && !lfe {
		surroundTarget := target + int32(
			truncInt(float64(float32(surroundMasking*float32(codedBins<<bitRes)))),
		)
		target = max(target/4, surroundTarget)
	}

	{
		bins := int(eBands[nbEBands-2]) << uint(LM)
		floorDepth := int32(truncInt(float64(float32(float32((C*bins)<<bitRes) * maxDepth))))
		floorDepth = max(floorDepth, target>>2)
		target = min(target, floorDepth)
	}

	// Make VBR less aggressive for constrained VBR because we can't keep
	// a higher bitrate for long. Needs tuning.
	if (!hasSurroundMask || lfe) && constrainedVBR != 0 {
		target = baseTarget + int32(truncInt(float64(float32(0.67*float32(target-baseTarget)))))
	}

	if !hasSurroundMask && tfEstimate < .2 {
		amount := float32(.0000031 * float32(max(0, min(32000, 96000-bitrate))))
		tvbrFactor := float32(temporalVBR * amount)
		target += int32(truncInt(float64(float32(tvbrFactor * float32(target)))))
	}

	// Don't allow more than doubling the rate.
	target = min(2*baseTarget, target)

	return target
}

var (
	intensityThresholds = []float32{
		// 0  1  2  3  4  5  6  7  8  9 10 11 12 13 14 15 16 17 18 19  20  off
		1, 2, 3, 4, 5, 6, 7, 8, 16, 24, 36, 44, 50, 56, 62, 67, 72, 79, 88, 106, 134,
	}
	intensityHisteresis = []float32{
		1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 3, 3, 4, 5, 6, 8, 8,
	}
)

// EncodeWithEC is celt_encode_with_ec. It encodes one frame of frameSize
// samples per channel (at the input sampling rate) from pcm, interleaved
// opus_res values (float, normalized to +/-1), and returns the number of
// bytes written or a negative error code.
//
// When enc is nil, the encoder writes to compressed with its own range
// coder, and compressed must hold at least min(nbCompressedBytes, 1275)
// bytes. Otherwise enc must already be initialized (hybrid mode, after
// SILK) on a buffer of at least nbCompressedBytes bytes, compressed is
// not used, and the encoder finishes enc with Done.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func (e *Encoder) EncodeWithEC(
	pcm []float32,
	frameSize int,
	compressed []byte,
	nbCompressedBytes int,
	enc *entcode.Encoder,
) int {
	st := &e.st
	sc := &e.scratch
	var bits int32
	var shortBlocks int
	isTransient := false
	CC := e.channels
	C := e.streamChannels
	var tfSelect int
	var nbFilledBytes, nbAvailableBytes int
	var minAllowed int32
	var codedBands int
	var allocTrim int
	pitchIndex := CombFilterMinPeriod
	var gain1 float32
	dualStereo := 0
	var effectiveBytes int
	var vbrRate int32
	var totalBits int32
	var totalBoost int32
	var balance int32
	var tell int32
	var tell0Frac int32
	prefilterTapset := 0
	var pfOn bool
	var antiCollapseRsv int
	antiCollapseOn := 0
	silence := false
	tfChan := 0
	var tfEstimate float32
	pitchChange := false
	var totBoost int32
	var sampleMax float32
	var maxDepth float32
	transientGotDisabled := false
	var surroundMasking float32
	var temporalVBR float32
	var surroundTrim float32
	var equivRate int32
	weakTransient := false
	var enableTfAnalysis bool
	var toneFreq float32
	var toneishness float32
	packetSizeCap := 1275

	mode := e.mode
	nbEBands := mode.NbEBands
	overlap := mode.Overlap
	eBands := mode.EBands
	start := e.start
	end := e.end
	hybrid := start != 0
	tfEstimate = 0
	if nbCompressedBytes < 2 || pcm == nil {
		return BadArg
	}

	frameSize *= e.upsample
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
	N := M * mode.ShortMdctSize

	prefilterMem := st.prefilterMem[:]
	oldBandE := st.oldBandE[:]
	oldLogE := st.oldLogE[:]
	oldLogE2 := st.oldLogE2[:]
	energyError := st.energyError[:]

	if enc == nil {
		tell0Frac = 1
		tell = 1
		nbFilledBytes = 0
	} else {
		tell0Frac = int32(enc.TellFrac())
		tell = int32(enc.Tell())
		nbFilledBytes = int(tell+4) >> 3
	}

	// Can't produce more than 1275 output bytes for the main payload.
	nbCompressedBytes = min(nbCompressedBytes, packetSizeCap)

	if e.vbr != 0 && e.bitrate != BitrateMax {
		vbrRate = bitrateToBits(e.bitrate, mode.Fs, int32(frameSize)) << bitRes
		effectiveBytes = int(vbrRate >> (3 + bitRes))
	} else {
		vbrRate = 0
		tmp := e.bitrate * int32(frameSize)
		if tell > 1 {
			tmp += tell * mode.Fs
		}
		if e.bitrate != BitrateMax {
			nbCompressedBytes = max(2, min(nbCompressedBytes,
				int((tmp+4*mode.Fs)/(8*mode.Fs))-b2i(e.signalling != 0)))
			if enc != nil {
				enc.Shrink(uint32(nbCompressedBytes))
			}
		}
		effectiveBytes = nbCompressedBytes - nbFilledBytes
	}
	nbAvailableBytes = nbCompressedBytes - nbFilledBytes
	equivRate = (int32(nbCompressedBytes)*8*50)<<uint(3-LM) - int32((40*C+20)*((400>>uint(LM))-50))
	if e.bitrate != BitrateMax {
		equivRate = min(equivRate, e.bitrate-int32((40*C+20)*((400>>uint(LM))-50)))
	}

	if enc == nil {
		sc.enc.Init(compressed[:nbCompressedBytes])
		enc = &sc.enc
	}

	if vbrRate > 0 {
		// Computes the max bit-rate allowed in VBR mode to avoid
		// violating the target rate and buffering. We must do this up
		// front so that bust-prevention logic triggers correctly if we
		// don't have enough bits.
		if e.constrainedVBR != 0 {
			// We could use any multiple of vbr_rate as bound (depending
			// on the delay). This is clamped to ensure we use at least
			// two bytes if the encoder was entirely empty, but to allow 0
			// in hybrid mode.
			vbrBound := vbrRate
			lo := int32(0)
			if tell == 1 {
				lo = 2
			}
			maxAllowed := min(
				int(max(lo, (vbrRate+vbrBound-st.vbrReservoir)>>(bitRes+3))),
				nbAvailableBytes,
			)
			if maxAllowed < nbAvailableBytes {
				nbCompressedBytes = nbFilledBytes + maxAllowed
				nbAvailableBytes = maxAllowed
				enc.Shrink(uint32(nbCompressedBytes))
			}
		}
	}
	totalBits = int32(nbCompressedBytes * 8)

	effEnd := end
	if effEnd > mode.EffEBands {
		effEnd = mode.EffEBands
	}

	in := sc.in[:CC*(N+overlap)]

	sampleMax = dsp.Max32(st.overlapMax, dsp.CeltMaxabsRes(pcm[:C*(N-overlap)/e.upsample]))
	st.overlapMax = dsp.CeltMaxabsRes(
		pcm[C*(N-overlap)/e.upsample : C*(N-overlap)/e.upsample+C*overlap/e.upsample],
	)
	sampleMax = dsp.Max32(sampleMax, st.overlapMax)
	silence = sampleMax <= float32(1)/float32(int32(1)<<uint(e.lsbDepth))
	if tell == 1 {
		enc.EncBitLogp(b2i(silence), 15)
	} else {
		silence = false
	}
	if silence {
		// In VBR mode there is no need to send more than the minimum.
		if vbrRate > 0 {
			nbCompressedBytes = min(nbCompressedBytes, nbFilledBytes+2)
			effectiveBytes = nbCompressedBytes
			totalBits = int32(nbCompressedBytes * 8)
			nbAvailableBytes = 2
			enc.Shrink(uint32(nbCompressedBytes))
		}
		// Pretend we've filled all the remaining bits with zeros (that's
		// what the initialiser did anyway).
		tell = int32(nbCompressedBytes * 8)
		enc.NbitsTotal += int(tell) - enc.Tell()
	}
	for c := 0; c < CC; c++ {
		needClip := e.clip && sampleMax > 65536
		Preemphasis(pcm[c:], in[c*(N+overlap)+overlap:], N, CC, e.upsample,
			mode.Preemph[:], &st.preemphMemE[c], needClip)
		copy(
			in[c*(N+overlap):c*(N+overlap)+overlap],
			prefilterMem[(1+c)*CombFilterMaxPeriod-overlap:(1+c)*CombFilterMaxPeriod],
		)
	}

	toneFreq = toneDetect(in, CC, N+overlap, &toneishness, mode.Fs, sc.tmp[:])
	isTransient = false
	shortBlocks = 0
	if e.complexity >= 1 && e.lfe == 0 {
		// Reduces the likelihood of energy instability on fricatives at
		// low bitrate in hybrid mode. It seems like we still want to have
		// real transients on vowels though (small SILK quantization
		// offset value).
		allowWeakTransients := hybrid && effectiveBytes < 15 && st.silkInfo.SignalType != 2
		isTransient = transientAnalysis(
			in,
			N+overlap,
			CC,
			&tfEstimate,
			&tfChan,
			allowWeakTransients,
			&weakTransient,
			toneFreq,
			toneishness,
			sc.tmp[:],
		)
	}
	toneishness = dsp.Min32(toneishness, 1-tfEstimate)
	// Find pitch period and gain.
	{
		var qg int
		enabled := ((e.lfe != 0 && nbAvailableBytes > 3) || nbAvailableBytes > 12*C) && !hybrid &&
			!silence &&
			tell+16 <= totalBits &&
			!e.disablePF

		prefilterTapset = st.tapsetDecision
		pfOn = e.runPrefilter(
			in,
			CC,
			N,
			prefilterTapset,
			&pitchIndex,
			&gain1,
			&qg,
			enabled,
			e.complexity,
			tfEstimate,
			nbAvailableBytes,
			&st.analysis,
			toneFreq,
			toneishness,
		)
		if (gain1 > .4 || st.prefilterGain > .4) &&
			(st.analysis.Valid == 0 || float64(st.analysis.Tonality) > .3) &&
			(float64(pitchIndex) > 1.26*float64(st.prefilterPeriod) || float64(pitchIndex) < .79*float64(st.prefilterPeriod)) {
			pitchChange = true
		}
		if !pfOn {
			if !hybrid && tell+16 <= totalBits {
				enc.EncBitLogp(0, 1)
			}
		} else {
			// This block is not gated by a total bits check only because
			// of the nbAvailableBytes check above.
			enc.EncBitLogp(1, 1)
			pitchIndex++
			octave := entcode.Ilog(uint32(pitchIndex)) - 5
			enc.EncUint(uint32(octave), 6)
			enc.EncBits(uint32(pitchIndex-(16<<uint(octave))), uint(4+octave))
			pitchIndex--
			enc.EncBits(uint32(qg), 3)
			enc.EncIcdf(prefilterTapset, tapsetIcdf, 2)
		}
	}
	if LM > 0 && int32(enc.Tell())+3 <= totalBits {
		if isTransient {
			shortBlocks = M
		}
	} else {
		isTransient = false
		transientGotDisabled = true
	}

	freq := sc.freq[:CC*N]
	bandE := sc.bandE[:nbEBands*CC]
	bandLogE := sc.bandLogE[:nbEBands*CC]

	secondMdct := shortBlocks != 0 && e.complexity >= 8
	bandLogE2 := sc.bandLogE2[:C*nbEBands]
	if secondMdct {
		computeMDCTs(mode, 0, in, freq, C, CC, LM, e.upsample)
		computeBandEnergies(mode, freq, bandE, effEnd, C, LM)
		amp2Log2(mode, effEnd, end, bandE, bandLogE2, C)
		for c := 0; c < C; c++ {
			for i := 0; i < end; i++ {
				bandLogE2[nbEBands*c+i] += float32(.5 * float32(LM))
			}
		}
	}

	computeMDCTs(mode, shortBlocks, in, freq, C, CC, LM, e.upsample)
	if CC == 2 && C == 1 {
		tfChan = 0
	}
	computeBandEnergies(mode, freq, bandE, effEnd, C, LM)

	if e.lfe != 0 {
		for i := 2; i < end; i++ {
			bandE[i] = dsp.Min32(bandE[i], float32(1e-4*bandE[0]))
			bandE[i] = dsp.Max32(bandE[i], 1e-15)
		}
	}
	amp2Log2(mode, effEnd, end, bandE, bandLogE, C)

	surroundDynalloc := sc.surroundDynalloc[:C*nbEBands]
	clear(surroundDynalloc[:end])
	// This computes how much masking takes place between surround
	// channels.
	if !hybrid && st.energyMask != nil && e.lfe == 0 {
		var maskAvg, diff float32
		count := 0
		maskEnd := max(2, st.lastCodedBands)
		for c := 0; c < C; c++ {
			for i := 0; i < maskEnd; i++ {
				mask := dsp.Max32(dsp.Min32(st.energyMask[nbEBands*c+i], .25), -2.0)
				if mask > 0 {
					mask = float32(.5 * mask)
				}
				mask16 := mask
				maskAvg += float32(mask16 * float32(eBands[i+1]-eBands[i]))
				count += int(eBands[i+1] - eBands[i])
				diff += float32(mask16 * float32(1+2*i-maskEnd))
			}
		}
		maskAvg = maskAvg / float32(count)
		maskAvg += .2
		diff = float32(diff*6) / float32(C*(maskEnd-1)*(maskEnd+1)*maskEnd)
		// Again, being conservative.
		diff = float32(.5 * diff)
		diff = dsp.Max32(dsp.Min32(diff, .031), -.031)
		// Find the band that's in the middle of the coded spectrum.
		midband := 0
		for eBands[midband+1] < eBands[maskEnd]/2 {
			midband++
		}
		countDynalloc := 0
		for i := 0; i < maskEnd; i++ {
			var unmask float32
			lin := maskAvg + float32(diff*float32(i-midband))
			if C == 2 {
				unmask = dsp.Max32(st.energyMask[i], st.energyMask[nbEBands+i])
			} else {
				unmask = st.energyMask[i]
			}
			unmask = dsp.Min32(unmask, .0)
			unmask -= lin
			if unmask > .25 {
				surroundDynalloc[i] = unmask - .25
				countDynalloc++
			}
		}
		if countDynalloc >= 3 {
			// If we need dynalloc in many bands, it's probably because
			// our initial masking rate was too low.
			maskAvg += .25
			if maskAvg > 0 {
				// Something went really wrong in the original
				// calculations, disabling masking.
				maskAvg = 0
				diff = 0
				clear(surroundDynalloc[:maskEnd])
			} else {
				for i := 0; i < maskEnd; i++ {
					surroundDynalloc[i] = dsp.Max32(0, surroundDynalloc[i]-.25)
				}
			}
		}
		maskAvg += .2
		// Convert to 1/64th units used for the trim.
		surroundTrim = float32(64 * diff)
		surroundMasking = maskAvg
	}
	// Temporal VBR (but not for LFE).
	if e.lfe == 0 {
		follow := float32(-10.0)
		var frameAvg float32
		var offset float32
		if shortBlocks != 0 {
			offset = float32(.5 * float32(LM))
		}
		for i := start; i < end; i++ {
			follow = dsp.Max32(follow-1.0, bandLogE[i]-offset)
			if C == 2 {
				follow = dsp.Max32(follow, bandLogE[i+nbEBands]-offset)
			}
			frameAvg += follow
		}
		frameAvg /= float32(end - start)
		temporalVBR = frameAvg - st.specAvg
		temporalVBR = dsp.Min32(3, dsp.Max32(-1.5, temporalVBR))
		st.specAvg += float32(.02 * temporalVBR)
	}

	if !secondMdct {
		copy(bandLogE2, bandLogE[:C*nbEBands])
	}

	// Last chance to catch any transient we might have missed in the
	// time-domain analysis.
	if LM > 0 && int32(enc.Tell())+3 <= totalBits && !isTransient && e.complexity >= 5 &&
		e.lfe == 0 &&
		!hybrid {
		if patchTransientDecision(bandLogE, oldBandE, nbEBands, start, end, C) {
			isTransient = true
			shortBlocks = M
			computeMDCTs(mode, shortBlocks, in, freq, C, CC, LM, e.upsample)
			computeBandEnergies(mode, freq, bandE, effEnd, C, LM)
			amp2Log2(mode, effEnd, end, bandE, bandLogE, C)
			// Compensate for the scaling of short vs long mdcts.
			for c := 0; c < C; c++ {
				for i := 0; i < end; i++ {
					bandLogE2[nbEBands*c+i] += float32(.5 * float32(LM))
				}
			}
			tfEstimate = .2
		}
	}

	if LM > 0 && int32(enc.Tell())+3 <= totalBits {
		enc.EncBitLogp(b2i(isTransient), 3)
	}

	X := sc.X[:C*N]

	// Band normalisation.
	normaliseBands(mode, freq, X, bandE, effEnd, C, M)

	enableTfAnalysis = effectiveBytes >= 15*C && !hybrid && e.complexity >= 2 && e.lfe == 0 &&
		toneishness < .98

	offsets := sc.offsets[:nbEBands]
	importance := sc.importance[:nbEBands]
	spreadWeight := sc.spreadWeight[:nbEBands]

	maxDepth = dynallocAnalysis(
		bandLogE,
		bandLogE2,
		oldBandE,
		nbEBands,
		start,
		end,
		C,
		offsets,
		e.lsbDepth,
		mode.LogN,
		isTransient,
		e.vbr,
		e.constrainedVBR,
		eBands,
		LM,
		effectiveBytes,
		&totBoost,
		e.lfe != 0,
		surroundDynalloc,
		&st.analysis,
		importance,
		spreadWeight,
		toneFreq,
		toneishness,
	)

	tfRes := sc.tfRes[:nbEBands]
	// Disable variable tf resolution for hybrid and at very low bitrate.
	if enableTfAnalysis {
		lambda := max(80, 20480/effectiveBytes+2)
		tfSelect = tfAnalysis(
			mode,
			effEnd,
			isTransient,
			tfRes,
			lambda,
			X,
			N,
			LM,
			tfEstimate,
			tfChan,
			importance,
		)
		for i := effEnd; i < end; i++ {
			tfRes[i] = tfRes[effEnd-1]
		}
	} else if hybrid && weakTransient {
		// For weak transients, we rely on the fact that improving time
		// resolution using TF on a long window is imperfect and will not
		// result in an energy collapse at low bitrate.
		for i := 0; i < end; i++ {
			tfRes[i] = 1
		}
		tfSelect = 0
	} else if hybrid && effectiveBytes < 15 && st.silkInfo.SignalType != 2 {
		// For low bitrate hybrid, we force temporal resolution to 5 ms
		// rather than 2.5 ms.
		for i := 0; i < end; i++ {
			tfRes[i] = 0
		}
		tfSelect = b2i(isTransient)
	} else {
		for i := 0; i < end; i++ {
			tfRes[i] = b2i(isTransient)
		}
		tfSelect = 0
	}

	errs := sc.errs[:C*nbEBands]
	for c := 0; c < C; c++ {
		for i := start; i < end; i++ {
			// When the energy is stable, slightly bias energy
			// quantization towards the previous error to make the gain
			// more stable (a constant offset is better than
			// fluctuations).
			if dsp.Abs32(bandLogE[i+c*nbEBands]-oldBandE[i+c*nbEBands]) < 2 {
				bandLogE[i+c*nbEBands] -= float32(0.25 * energyError[i+c*nbEBands])
			}
		}
	}
	quantCoarseEnergy(mode, start, end, effEnd, bandLogE,
		oldBandE, uint32(totalBits), errs, enc,
		C, LM, nbAvailableBytes, e.forceIntra,
		&st.delayedIntra, e.complexity >= 4, e.lossRate, e.lfe != 0)

	tfEncode(start, end, isTransient, tfRes, LM, tfSelect, enc)

	if int32(enc.Tell())+4 <= totalBits {
		if e.lfe != 0 {
			st.tapsetDecision = 0
			st.spreadDecision = spreadNormal
		} else if hybrid {
			if e.complexity == 0 {
				st.spreadDecision = spreadNone
			} else if isTransient {
				st.spreadDecision = spreadNormal
			} else {
				st.spreadDecision = spreadAggressive
			}
		} else if shortBlocks != 0 || e.complexity < 3 || nbAvailableBytes < 10*C {
			if e.complexity == 0 {
				st.spreadDecision = spreadNone
			} else {
				st.spreadDecision = spreadNormal
			}
		} else {
			// Disable new spreading+tapset estimator until we can show it
			// works better than the old one. So far it seems like
			// spreading_decision() works best.
			st.spreadDecision = spreadingDecision(mode, X,
				&st.tonalAverage, st.spreadDecision, &st.hfAverage,
				&st.tapsetDecision, pfOn && shortBlocks == 0, effEnd, C, M, spreadWeight)
		}
		enc.EncIcdf(st.spreadDecision, spreadIcdf, 5)
	} else {
		st.spreadDecision = spreadNormal
	}

	// For LFE, everything interesting is in the first band.
	if e.lfe != 0 {
		offsets[0] = min(8, effectiveBytes/3)
	}
	caps := sc.cap[:nbEBands]
	initCaps(mode, caps, LM, C)

	dynallocLogp := 6
	totalBits <<= bitRes
	totalBoost = 0
	tell = int32(enc.TellFrac())
	for i := start; i < end; i++ {
		width := C * int(eBands[i+1]-eBands[i]) << uint(LM)
		// quanta is 6 bits, but no more than 1 bit/sample and no less
		// than 1/8 bit/sample.
		quanta := min(width<<bitRes, max(6<<bitRes, width))
		dynallocLoopLogp := dynallocLogp
		boost := 0
		j := 0
		for j = 0; tell+int32(dynallocLoopLogp<<bitRes) < totalBits-totalBoost && boost < caps[i]; j++ {
			flag := j < offsets[i]
			enc.EncBitLogp(b2i(flag), uint(dynallocLoopLogp))
			tell = int32(enc.TellFrac())
			if !flag {
				break
			}
			boost += quanta
			totalBoost += int32(quanta)
			dynallocLoopLogp = 1
		}
		// Making dynalloc more likely.
		if j != 0 {
			dynallocLogp = max(2, dynallocLogp-1)
		}
		offsets[i] = boost
	}

	if C == 2 {
		// Always use MS for 2.5 ms frames until we can do a better
		// analysis.
		if LM != 0 {
			dualStereo = b2i(stereoAnalysis(mode, X, LM, N))
		}

		st.intensity = hysteresisDecision(float32(equivRate/1000),
			intensityThresholds, intensityHisteresis, 21, st.intensity)
		st.intensity = min(end, max(start, st.intensity))
	}

	allocTrim = 5
	if tell+(6<<bitRes) <= totalBits-totalBoost {
		if start > 0 || e.lfe != 0 {
			st.stereoSaving = 0
			allocTrim = 5
		} else {
			allocTrim = allocTrimAnalysis(mode, X, bandLogE,
				end, LM, C, N, &st.analysis, &st.stereoSaving, tfEstimate,
				st.intensity, surroundTrim, equivRate)
		}
		enc.EncIcdf(allocTrim, trimIcdf, 7)
		tell = int32(enc.TellFrac())
	}

	// In VBR mode the frame size must not be reduced so much that it
	// would result in the encoder running out of bits. The margin of 2
	// bytes ensures that none of the bust-prevention logic in the decoder
	// will have triggered so far.
	minAllowed = ((tell + totalBoost + (1 << (bitRes + 3)) - 1) >> (bitRes + 3)) + 2
	// Take into account the 37 bits we need to have left in the packet to
	// signal a redundant frame in hybrid mode. Creating a shorter packet
	// would create an entropy coder desync.
	if hybrid {
		minAllowed = max(
			minAllowed,
			(tell0Frac+(37<<bitRes)+totalBoost+(1<<(bitRes+3))-1)>>(bitRes+3),
		)
	}
	// Variable bitrate.
	if vbrRate > 0 {
		var alpha float32
		var delta int32
		// The target rate in 8th bits per frame.
		var target, baseTarget int32
		lmDiff := mode.MaxLM - LM

		// Don't attempt to use more than 510 kb/s, even for frames
		// smaller than 20 ms. The CELT allocator will just not be able to
		// use more than that anyway.
		nbCompressedBytes = min(nbCompressedBytes, packetSizeCap>>uint(3-LM))
		if !hybrid {
			baseTarget = vbrRate - int32((40*C+20)<<bitRes)
		} else {
			baseTarget = max(0, vbrRate-int32((9*C+4)<<bitRes))
		}

		if e.constrainedVBR != 0 {
			baseTarget += st.vbrOffset >> uint(lmDiff)
		}

		if !hybrid {
			target = computeVBR(mode, &st.analysis, baseTarget, LM, equivRate,
				st.lastCodedBands, C, st.intensity, e.constrainedVBR,
				st.stereoSaving, totBoost, tfEstimate, pitchChange, maxDepth,
				e.lfe != 0, st.energyMask != nil, surroundMasking,
				temporalVBR)
		} else {
			target = baseTarget
			// Tonal frames (offset<100) need more bits than noisy
			// (offset>100) ones.
			if st.silkInfo.Offset < 100 {
				target += 12 << bitRes >> uint(3-LM)
			}
			if st.silkInfo.Offset > 100 {
				target -= 18 << bitRes >> uint(3-LM)
			}
			// Boosting bitrate on transients and vowels with significant
			// temporal spikes.
			target += int32(truncInt(float64(float32((tfEstimate - .25) * (50 << bitRes)))))
			// If we have a strong transient, let's make sure it has
			// enough bits to code the first two bands, so that it can use
			// folding rather than noise.
			if tfEstimate > .7 {
				target = max(target, 50<<bitRes)
			}
		}
		// The current offset is removed from the target and the space
		// used so far is added.
		target = target + tell

		nbAvailableBytes = int((target + (1 << (bitRes + 2))) >> (bitRes + 3))
		nbAvailableBytes = max(int(minAllowed), nbAvailableBytes)
		nbAvailableBytes = min(nbCompressedBytes, nbAvailableBytes)

		// By how much did we "miss" the target on that frame.
		delta = target - vbrRate

		target = int32(nbAvailableBytes << (bitRes + 3))

		// If the frame is silent we don't adjust our drift, otherwise the
		// encoder will shoot to very high rates after hitting a span of
		// silence, but we do allow the bitres to refill. This means that
		// we'll undershoot our target in CVBR/VBR modes on files with
		// lots of silence.
		if silence {
			nbAvailableBytes = 2
			target = 2 * 8 << bitRes
			delta = 0
		}

		if st.vbrCount < 970 {
			st.vbrCount++
			alpha = dsp.CeltRcp(float32(st.vbrCount + 20))
		} else {
			alpha = .001
		}
		// How many bits have we used in excess of what we're allowed.
		if e.constrainedVBR != 0 {
			st.vbrReservoir += target - vbrRate
		}

		// Compute the offset we need to apply in order to reach the
		// target.
		if e.constrainedVBR != 0 {
			st.vbrDrift += int32(
				truncInt(
					float64(
						float32(
							alpha * float32((delta*(1<<uint(lmDiff)))-st.vbrOffset-st.vbrDrift),
						),
					),
				),
			)
			st.vbrOffset = -st.vbrDrift
		}

		if e.constrainedVBR != 0 && st.vbrReservoir < 0 {
			// We're under the min value -- increase rate.
			adjust := int((-st.vbrReservoir) / (8 << bitRes))
			// Unless we're just coding silence.
			if !silence {
				nbAvailableBytes += adjust
			}
			st.vbrReservoir = 0
		}
		nbCompressedBytes = min(nbCompressedBytes, nbAvailableBytes)
		// This moves the raw bits to take into account the new
		// compressed size.
		enc.Shrink(uint32(nbCompressedBytes))
	}

	// Bit allocation.
	fineQuant := sc.fineQuant[:nbEBands]
	pulses := sc.pulses[:nbEBands]
	finePriority := sc.finePriority[:nbEBands]

	// bits = packet size - where we are - safety
	bits = (int32(nbCompressedBytes*8) << bitRes) - int32(enc.TellFrac()) - 1
	if isTransient && LM >= 2 && bits >= int32((LM+2)<<bitRes) {
		antiCollapseRsv = 1 << bitRes
	}
	bits -= int32(antiCollapseRsv)
	signalBandwidth := end - 1
	if st.analysis.Valid != 0 {
		var minBandwidth int
		if equivRate < 32000*int32(C) {
			minBandwidth = 13
		} else if equivRate < 48000*int32(C) {
			minBandwidth = 16
		} else if equivRate < 60000*int32(C) {
			minBandwidth = 18
		} else if equivRate < 80000*int32(C) {
			minBandwidth = 19
		} else {
			minBandwidth = 20
		}
		signalBandwidth = max(st.analysis.Bandwidth, minBandwidth)
	}
	if e.lfe != 0 {
		signalBandwidth = 1
	}
	codedBands = cltComputeAllocation(
		mode,
		start,
		end,
		offsets,
		caps,
		allocTrim,
		&st.intensity,
		&dualStereo,
		bits,
		&balance,
		pulses,
		fineQuant,
		finePriority,
		C,
		LM,
		rangeCoder{enc: enc},
		true,
		st.lastCodedBands,
		signalBandwidth,
	)
	if st.lastCodedBands != 0 {
		st.lastCodedBands = min(st.lastCodedBands+1, max(st.lastCodedBands-1, codedBands))
	} else {
		st.lastCodedBands = codedBands
	}

	quantFineEnergy(mode, start, end, oldBandE, errs, nil, fineQuant, enc, C)
	clear(energyError[:nbEBands*CC])

	// Residual quantisation.
	collapseMasks := sc.collapse[:C*nbEBands]
	var Y []float32
	if C == 2 {
		Y = X[N:]
	}
	quantAllBands(true, mode, start, end, X, Y, collapseMasks,
		bandE, pulses, shortBlocks != 0, st.spreadDecision,
		dualStereo != 0, st.intensity, tfRes, int32(nbCompressedBytes*(8<<bitRes)-antiCollapseRsv),
		balance, rangeCoder{enc: enc}, LM, codedBands, &st.rng, e.complexity, e.disableInv)

	if antiCollapseRsv > 0 {
		antiCollapseOn = b2i(st.consecTransient < 2)
		enc.EncBits(uint32(antiCollapseOn), 1)
	}
	quantEnergyFinalise(
		mode,
		start,
		end,
		oldBandE,
		errs,
		fineQuant,
		finePriority,
		nbCompressedBytes*8-enc.Tell(),
		enc,
		C,
	)
	for c := 0; c < C; c++ {
		for i := start; i < end; i++ {
			energyError[i+c*nbEBands] = dsp.Max32(-0.5, dsp.Min32(0.5, errs[i+c*nbEBands]))
		}
	}
	if silence {
		for i := 0; i < C*nbEBands; i++ {
			oldBandE[i] = -28
		}
	}

	st.prefilterPeriod = pitchIndex
	st.prefilterGain = gain1
	st.prefilterTapset = prefilterTapset

	if CC == 2 && C == 1 {
		copy(oldBandE[nbEBands:2*nbEBands], oldBandE[:nbEBands])
	}

	if !isTransient {
		copy(oldLogE2[:CC*nbEBands], oldLogE[:CC*nbEBands])
		copy(oldLogE[:CC*nbEBands], oldBandE[:CC*nbEBands])
	} else {
		for i := 0; i < CC*nbEBands; i++ {
			oldLogE[i] = dsp.Min32(oldLogE[i], oldBandE[i])
		}
	}
	// In case start or end were to change.
	for c := 0; c < CC; c++ {
		for i := 0; i < start; i++ {
			oldBandE[c*nbEBands+i] = 0
			oldLogE2[c*nbEBands+i] = -28
			oldLogE[c*nbEBands+i] = -28
		}
		for i := end; i < nbEBands; i++ {
			oldBandE[c*nbEBands+i] = 0
			oldLogE2[c*nbEBands+i] = -28
			oldLogE[c*nbEBands+i] = -28
		}
	}

	if isTransient || transientGotDisabled {
		st.consecTransient++
	} else {
		st.consecTransient = 0
	}
	st.rng = enc.Rng

	// If there's any room left (can only happen for very high rates),
	// it's already filled with zeros.
	enc.Done()

	if enc.Error != 0 {
		return InternalError
	}
	return nbCompressedBytes
}

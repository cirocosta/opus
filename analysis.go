// Ported from libopus 1.6.1 src/analysis.h and src/analysis.c (float
// build).

package opus

import (
	"math"

	"github.com/cirocosta/opus/internal/celt"
	"github.com/cirocosta/opus/internal/dsp"
)

const (
	nbFrames        = 8   // NB_FRAMES
	nbTBands        = 18  // NB_TBANDS
	analysisBufSize = 720 // ANALYSIS_BUF_SIZE: 30 ms at 24 kHz
	// analysisCountMax is ANALYSIS_COUNT_MAX. At that point we can stop
	// counting frames because it no longer matters.
	analysisCountMax = 10000
	detectSize       = 100 // DETECT_SIZE

	transitionPenalty = 10 // TRANSITION_PENALTY
	nbTonalSkipBands  = 9  // NB_TONAL_SKIP_BANDS

	leakageOffset = float32(2.5) // LEAKAGE_OFFSET
	leakageSlope  = float32(2)   // LEAKAGE_SLOPE

	// scaleEner is the factor of SCALE_ENER in the float build.
	scaleEner = float32(1.0 / 32768 / 32768)
)

// analysisInfo is AnalysisInfo from celt/celt.h. The CELT encoder reads
// it, so the type lives in the celt package.
type analysisInfo = celt.AnalysisInfo

// tonalityAnalysisState is TonalityAnalysisState. The arch field is
// omitted (no RTCD). tonalityAnalysisReset clears every field except
// application and fs, like the C code clears everything from angle on.
type tonalityAnalysisState struct {
	application int
	fs          int32

	angle            [240]float32
	dAngle           [240]float32
	d2Angle          [240]float32
	inmem            [analysisBufSize]float32
	memFill          int // number of usable samples in the buffer
	prevBandTonality [nbTBands]float32
	prevTonality     float32
	prevBandwidth    int
	e                [nbFrames][nbTBands]float32
	logE             [nbFrames][nbTBands]float32
	lowE             [nbTBands]float32
	highE            [nbTBands]float32
	meanE            [nbTBands + 1]float32
	mem              [32]float32
	cmean            [8]float32
	std              [9]float32
	eTracker         float32
	lowECount        float32
	eCount           int
	count            int
	analysisOffset   int
	writePos         int
	readPos          int
	readSubframe     int
	hpEnerAccum      float32
	initialized      bool
	rnnState         [maxNeurons]float32
	downmixState     [3]float32
	info             [detectSize]analysisInfo

	// Scratch buffers of downmix_and_resample (C stack arrays). They do
	// not carry state between calls.
	downmixTmp [960]float32
	tmp3x      [960]float32
}

// tonalityAnalysisInit is tonality_analysis_init.
func tonalityAnalysisInit(tonal *tonalityAnalysisState, fs int32) {
	// Initialize reusable fields.
	tonal.fs = fs
	// Clear remaining fields.
	tonalityAnalysisReset(tonal)
}

// tonalityAnalysisReset is tonality_analysis_reset. Call it when there is
// a discontinuity in the data.
func tonalityAnalysisReset(tonal *tonalityAnalysisState) {
	// Clear non-reusable fields.
	*tonal = tonalityAnalysisState{application: tonal.application, fs: tonal.fs}
}

// Constants that C computes in double precision from M_PI. They are
// variables so that the compiler rounds every double operation as C does,
// instead of evaluating an exact constant expression.
var (
	analysisMPi = 3.14159265358979323846 // M_PI from <math.h>
	// analysisPi4 is pi4 in tonality_analysis.
	analysisPi4 = float32(analysisMPi * analysisMPi * analysisMPi * analysisMPi)
	// analysisHalfOverPi is (float)(.5f/M_PI).
	analysisHalfOverPi = float32(0.5 / analysisMPi)
)

// silkResamplerDown2HP is silk_resampler_down2_hp (float build): it
// downsamples in by two into out and returns the energy of the high band.
// s is the state vector [3].
func silkResamplerDown2HP(s *[3]float32, out, in []float32, inLen int) float32 {
	len2 := inLen / 2
	var hpEner float32
	for k := 0; k < len2; k++ {
		in32 := in[2*k]

		// All-pass section for even input sample.
		y := in32 - s[0]
		x := float32(0.6074371 * y)
		out32 := s[0] + x
		s[0] = in32 + x
		out32HP := out32
		in32 = in[2*k+1]

		// All-pass section for odd input sample, and add to output of
		// previous section.
		y = in32 - s[1]
		x = float32(0.15063 * y)
		out32 = out32 + s[1]
		out32 = out32 + x
		s[1] = in32 + x

		y = -in32 - s[2]
		x = float32(0.15063 * y)
		out32HP = out32HP + s[2]
		out32HP = out32HP + x
		s[2] = -in32 + x

		hpEner += float32(out32HP * out32HP)
		out[k] = float32(.5 * out32)
	}
	return hpEner
}

// downmixAndResample is downmix_and_resample, with S = tonal.downmixState
// and Fs = tonal.fs as at every C call site.
func (tonal *tonalityAnalysisState) downmixAndResample(downmix downmixFunc, x analysisPCM, y []float32, subframe, offset, c1, c2, c int) float32 {
	var ret float32
	if subframe == 0 {
		return 0
	}
	switch tonal.fs {
	case 48000:
		subframe *= 2
		offset *= 2
	case 16000:
		subframe = subframe * 2 / 3
		offset = offset * 2 / 3
	case 24000:
	default:
		panic("opus: analysis sampling rate must be 16, 24 or 48 kHz")
	}
	tmp := tonal.downmixTmp[:subframe]

	downmix(x, tmp, subframe, offset, c1, c2, c)
	if (c2 == -2 && c == 2) || c2 > -1 {
		for j := range tmp {
			tmp[j] = float32(.5 * tmp[j])
		}
	}
	switch tonal.fs {
	case 48000:
		ret = silkResamplerDown2HP(&tonal.downmixState, y, tmp, subframe)
	case 24000:
		copy(y[:subframe], tmp)
	case 16000:
		tmp3x := tonal.tmp3x[:3*subframe]
		// Don't do this at home! This resampler is horrible and it's only
		// (barely) usable for the purpose of the analysis because we don't
		// care about all the aliasing between 8 kHz and 12 kHz.
		for j, v := range tmp {
			tmp3x[3*j] = v
			tmp3x[3*j+1] = v
			tmp3x[3*j+2] = v
		}
		silkResamplerDown2HP(&tonal.downmixState, y, tmp3x, 3*subframe)
	}
	ret = float32(ret * (1.0 / 32768 / 32768))
	return ret
}

// tonalityGetInfo is tonality_get_info.
func tonalityGetInfo(tonal *tonalityAnalysisState, infoOut *analysisInfo, length int) {
	pos := tonal.readPos
	currLookahead := tonal.writePos - tonal.readPos
	if currLookahead < 0 {
		currLookahead += detectSize
	}

	tonal.readSubframe += length / int(tonal.fs/400)
	for tonal.readSubframe >= 8 {
		tonal.readSubframe -= 8
		tonal.readPos++
	}
	if tonal.readPos >= detectSize {
		tonal.readPos -= detectSize
	}

	// On long frames, look at the second analysis window rather than the
	// first.
	if length > int(tonal.fs/50) && pos != tonal.writePos {
		pos++
		if pos == detectSize {
			pos = 0
		}
	}
	if pos == tonal.writePos {
		pos--
	}
	if pos < 0 {
		pos = detectSize - 1
	}
	pos0 := pos
	*infoOut = tonal.info[pos]
	if infoOut.Valid == 0 {
		return
	}
	tonalityMax := infoOut.Tonality
	tonalityAvg := infoOut.Tonality
	tonalityCount := 1
	// Look at the neighbouring frames and pick largest bandwidth found (to
	// be safe).
	bandwidthSpan := 6
	// If possible, look ahead for a tone to compensate for the delay in the
	// tone detector.
	for i := 0; i < 3; i++ {
		pos++
		if pos == detectSize {
			pos = 0
		}
		if pos == tonal.writePos {
			break
		}
		tonalityMax = dsp.Max32(tonalityMax, tonal.info[pos].Tonality)
		tonalityAvg += tonal.info[pos].Tonality
		tonalityCount++
		infoOut.Bandwidth = dsp.IMax(infoOut.Bandwidth, tonal.info[pos].Bandwidth)
		bandwidthSpan--
	}
	pos = pos0
	// Look back in time to see if any has a wider bandwidth than the
	// current frame.
	for i := 0; i < bandwidthSpan; i++ {
		pos--
		if pos < 0 {
			pos = detectSize - 1
		}
		if pos == tonal.writePos {
			break
		}
		infoOut.Bandwidth = dsp.IMax(infoOut.Bandwidth, tonal.info[pos].Bandwidth)
	}
	infoOut.Tonality = dsp.Max32(tonalityAvg/float32(tonalityCount), tonalityMax-.2)

	mpos := pos0
	vpos := pos0
	// If we have enough look-ahead, compensate for the ~5-frame delay in
	// the music prob and ~1 frame delay in the VAD prob.
	if currLookahead > 15 {
		mpos += 5
		if mpos >= detectSize {
			mpos -= detectSize
		}
		vpos++
		if vpos >= detectSize {
			vpos -= detectSize
		}
	}

	// The following calculations attempt to minimize a "badness function"
	// for the transition. When switching from speech to music, the badness
	// of switching at frame k is
	//   b_k = S*v_k + \sum_{i=0}^{k-1} v_i*(p_i - T)
	// where
	//   v_i is the activity probability (VAD) at frame i,
	//   p_i is the music probability at frame i
	//   T is the probability threshold for switching
	//   S is the penalty for switching during active audio rather than silence
	//   the current frame has index i=0
	//
	// Rather than apply badness to directly decide when to switch, what we
	// compute instead is the threshold for which the optimal switching
	// point is now. When considering whether to switch now (frame 0) or at
	// frame k, we have:
	//   S*v_0 = S*v_k + \sum_{i=0}^{k-1} v_i*(p_i - T)
	// which gives us:
	//   T = ( \sum_{i=0}^{k-1} v_i*p_i + S*(v_k-v_0) ) / ( \sum_{i=0}^{k-1} v_i )
	// We take the min threshold across all positive values of k (up to the
	// maximum amount of lookahead we have) to give us the threshold for
	// which the current frame is the optimal switch point.
	//
	// The last step is that we need to consider whether we want to switch
	// at all. For that we use the average of the music probability over the
	// entire window. If the threshold is higher than that average we're not
	// going to switch, so we compute a min with the average as well. The
	// result of all these min operations is music_prob_min, which gives the
	// threshold for switching to music if we're currently encoding for
	// speech.
	//
	// We do the exact opposite to compute music_prob_max which is used for
	// switching from music to speech.
	probMin := float32(1)
	probMax := float32(0)
	vadProb := tonal.info[vpos].ActivityProbability
	probCount := dsp.Max16(.1, vadProb)
	probAvg := float32(dsp.Max16(.1, vadProb) * tonal.info[mpos].MusicProb)
	for {
		mpos++
		if mpos == detectSize {
			mpos = 0
		}
		if mpos == tonal.writePos {
			break
		}
		vpos++
		if vpos == detectSize {
			vpos = 0
		}
		if vpos == tonal.writePos {
			break
		}
		posVad := tonal.info[vpos].ActivityProbability
		probMin = dsp.Min16((probAvg-float32(transitionPenalty*(vadProb-posVad)))/probCount, probMin)
		probMax = dsp.Max16((probAvg+float32(transitionPenalty*(vadProb-posVad)))/probCount, probMax)
		probCount += dsp.Max16(.1, posVad)
		probAvg += float32(dsp.Max16(.1, posVad) * tonal.info[mpos].MusicProb)
	}
	infoOut.MusicProb = probAvg / probCount
	probMin = dsp.Min16(probAvg/probCount, probMin)
	probMax = dsp.Max16(probAvg/probCount, probMax)
	probMin = dsp.Max16(probMin, 0)
	probMax = dsp.Min16(probMax, 1)

	// If we don't have enough look-ahead, do our best to make a decent
	// decision.
	if currLookahead < 10 {
		pmin := probMin
		pmax := probMax
		pos = pos0
		// Look for min/max in the past.
		for i := 0; i < dsp.IMin(tonal.count-1, 15); i++ {
			pos--
			if pos < 0 {
				pos = detectSize - 1
			}
			pmin = dsp.Min16(pmin, tonal.info[pos].MusicProb)
			pmax = dsp.Max16(pmax, tonal.info[pos].MusicProb)
		}
		// Bias against switching on active audio.
		pmin = dsp.Max16(0, pmin-float32(.1*vadProb))
		pmax = dsp.Min16(1, pmax+float32(.1*vadProb))
		w := 1 - float32(.1*float32(currLookahead))
		probMin += float32(w * (pmin - probMin))
		probMax += float32(w * (pmax - probMax))
	}
	infoOut.MusicProbMin = probMin
	infoOut.MusicProbMax = probMax
}

// stdFeatureBias is std_feature_bias.
var stdFeatureBias = [9]float32{
	5.684947, 3.475288, 1.770634, 1.599784, 3.773215,
	2.163313, 1.260756, 1.116868, 1.918795,
}

// binEnergy is the binE expression of tonality_analysis: the energy of
// bin i of the two real spectra packed in out.
func binEnergy(out *[480]dsp.KissFFTCpx, i int) float32 {
	const n = 480
	return float32(out[i].R*out[i].R) + float32(out[n-i].R*out[n-i].R) +
		float32(out[i].I*out[i].I) + float32(out[n-i].I*out[n-i].I)
}

// cLog is (float)log(x) of a float argument.
func cLog(x float32) float32 {
	return float32(math.Log(float64(x)))
}

// cSqrt is (float)sqrt(x) of a float argument.
func cSqrt(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}

// tonalityAnalysis is tonality_analysis.
func tonalityAnalysis(tonal *tonalityAnalysisState, celtMode *celt.Mode, x analysisPCM, length, offset, c1, c2, c, lsbDepth int, downmix downmixFunc) {
	const (
		n  = 480
		n2 = 240
	)
	a := &tonal.angle
	dA := &tonal.dAngle
	d2A := &tonal.d2Angle
	var (
		in, out           [480]dsp.KissFFTCpx
		tonality          [240]float32
		noisiness         [240]float32
		bandTonality      [nbTBands]float32
		logE              [nbTBands]float32
		bfcc              [8]float32
		features          [25]float32
		frameProbs        [2]float32
		isMasked          [nbTBands + 1]bool
		tonality2         [240]float32
		midE              [8]float32
		bandLog2          [nbTBands + 1]float32
		leakageFrom       [nbTBands + 1]float32
		leakageTo         [nbTBands + 1]float32
		layerOut          [maxNeurons]float32
		slope             float32
		specVariability   float32
		maxE              float32
		bandwidth         int
		frameTonality     float32
		maxFrameTonality  float32
		frameNoisiness    float32
		frameStationarity float32
		relativeE         float32
		frameLoudness     float32
		bandwidthMask     float32
		belowMaxPitch     float32
		aboveMaxPitch     float32
	)
	pi4 := analysisPi4

	if !tonal.initialized {
		tonal.memFill = 240
		tonal.initialized = true
	}
	alpha := 1 / float32(dsp.IMin(10, 1+tonal.count))
	alphaE := 1 / float32(dsp.IMin(25, 1+tonal.count))
	// Noise floor related decay for bandwidth detection: -2.2 dB/second.
	alphaE2 := 1 / float32(dsp.IMin(100, 1+tonal.count))
	if tonal.count <= 1 {
		alphaE2 = 1
	}

	if tonal.fs == 48000 {
		// len and offset are now at 24 kHz.
		length /= 2
		offset /= 2
	} else if tonal.fs == 16000 {
		length = 3 * length / 2
		offset = 3 * offset / 2
	}

	kfft := celtMode.MDCT.KFFT[0]
	tonal.hpEnerAccum += tonal.downmixAndResample(downmix, x,
		tonal.inmem[tonal.memFill:], dsp.IMin(length, analysisBufSize-tonal.memFill),
		offset, c1, c2, c)
	if tonal.memFill+length < analysisBufSize {
		tonal.memFill += length
		// Don't have enough to update the analysis.
		return
	}
	hpEner := tonal.hpEnerAccum
	info := &tonal.info[tonal.writePos]
	tonal.writePos++
	if tonal.writePos >= detectSize {
		tonal.writePos -= detectSize
	}

	isSilence := isDigitalSilence(tonal.inmem[:], analysisBufSize, 1, lsbDepth)

	for i := 0; i < n2; i++ {
		w := analysisWindow[i]
		in[i].R = float32(w * tonal.inmem[i])
		in[i].I = float32(w * tonal.inmem[n2+i])
		in[n-i-1].R = float32(w * tonal.inmem[n-i-1])
		in[n-i-1].I = float32(w * tonal.inmem[n+n2-i-1])
	}
	copy(tonal.inmem[:240], tonal.inmem[analysisBufSize-240:])
	remaining := length - (analysisBufSize - tonal.memFill)
	tonal.hpEnerAccum = tonal.downmixAndResample(downmix, x,
		tonal.inmem[240:], remaining,
		offset+analysisBufSize-tonal.memFill, c1, c2, c)
	tonal.memFill = 240 + remaining
	if isSilence {
		// On silence, copy the previous analysis.
		prevPos := tonal.writePos - 2
		if prevPos < 0 {
			prevPos += detectSize
		}
		*info = tonal.info[prevPos]
		return
	}
	dsp.OpusFFT(kfft, in[:], out[:])
	// If there's any NaN on the input, the entire output will be NaN, so
	// we only need to check one value.
	if dsp.CeltIsnan(out[0].R) {
		info.Valid = 0
		return
	}

	for i := 1; i < n2; i++ {
		x1r := out[i].R + out[n-i].R
		x1i := out[i].I - out[n-i].I
		x2r := out[i].I + out[n-i].I
		x2i := out[n-i].R - out[i].R

		angle := float32(analysisHalfOverPi * dsp.FastAtan2f(x1i, x1r))
		dAngle := angle - a[i]
		d2Angle := dAngle - dA[i]

		angle2 := float32(analysisHalfOverPi * dsp.FastAtan2f(x2i, x2r))
		dAngle2 := angle2 - angle
		d2Angle2 := dAngle2 - dAngle

		mod1 := d2Angle - float32(dsp.Float2Int(d2Angle))
		noisiness[i] = dsp.Abs16(mod1)
		mod1 = float32(mod1 * mod1)
		mod1 = float32(mod1 * mod1)

		mod2 := d2Angle2 - float32(dsp.Float2Int(d2Angle2))
		noisiness[i] += dsp.Abs16(mod2)
		mod2 = float32(mod2 * mod2)
		mod2 = float32(mod2 * mod2)

		avgMod := float32(.25 * (d2A[i] + mod1 + float32(2*mod2)))
		// This introduces an extra delay of 2 frames in the detection.
		tonality[i] = 1/(1+float32(float32(float32(40*16)*pi4)*avgMod)) - .015
		// No delay on this detection, but it's less reliable.
		tonality2[i] = 1/(1+float32(float32(float32(40*16)*pi4)*mod2)) - .015

		a[i] = angle2
		dA[i] = dAngle2
		d2A[i] = mod2
	}
	for i := 2; i < n2-1; i++ {
		tt := dsp.Min32(tonality2[i], dsp.Max32(tonality2[i-1], tonality2[i+1]))
		tonality[i] = float32(.9 * dsp.Max32(tonality[i], tt-.1))
	}
	info.Activity = 0
	if tonal.count == 0 {
		for b := 0; b < nbTBands; b++ {
			tonal.lowE[b] = 1e10
			tonal.highE[b] = -1e10
		}
	}
	// The energy of the very first band is special because of DC.
	{
		x1r := float32(2 * out[0].R)
		x2r := float32(2 * out[0].I)
		e := float32(x1r*x1r) + float32(x2r*x2r)
		for i := 1; i < 4; i++ {
			e += binEnergy(&out, i)
		}
		e = float32(scaleEner * e)
		bandLog2[0] = float32(.5 * 1.442695 * cLog(e+1e-10))
	}
	for b := 0; b < nbTBands; b++ {
		var e, tE, nE float32
		for i := tbands[b]; i < tbands[b+1]; i++ {
			binE := binEnergy(&out, i)
			binE = float32(scaleEner * binE)
			e += binE
			tE += float32(binE * dsp.Max32(0, tonality[i]))
			nE += float32(float32(binE*2) * (.5 - noisiness[i]))
		}
		// Check for extreme band energies that could cause NaNs later.
		if !(e < 1e9) || dsp.CeltIsnan(e) {
			info.Valid = 0
			return
		}

		tonal.e[tonal.eCount][b] = e
		frameNoisiness += nE / (1e-15 + e)

		frameLoudness += cSqrt(e + 1e-10)
		logE[b] = cLog(e + 1e-10)
		bandLog2[b+1] = float32(.5 * 1.442695 * cLog(e+1e-10))
		tonal.logE[tonal.eCount][b] = logE[b]
		if tonal.count == 0 {
			tonal.lowE[b] = logE[b]
			tonal.highE[b] = logE[b]
		}
		if float64(tonal.highE[b]) > float64(tonal.lowE[b])+7.5 {
			if tonal.highE[b]-logE[b] > logE[b]-tonal.lowE[b] {
				tonal.highE[b] -= .01
			} else {
				tonal.lowE[b] += .01
			}
		}
		if logE[b] > tonal.highE[b] {
			tonal.highE[b] = logE[b]
			tonal.lowE[b] = dsp.Max32(tonal.highE[b]-15, tonal.lowE[b])
		} else if logE[b] < tonal.lowE[b] {
			tonal.lowE[b] = logE[b]
			tonal.highE[b] = dsp.Min32(tonal.lowE[b]+15, tonal.highE[b])
		}
		relativeE += (logE[b] - tonal.lowE[b]) / (1e-5 + (tonal.highE[b] - tonal.lowE[b]))

		var l1, l2 float32
		for i := 0; i < nbFrames; i++ {
			l1 += cSqrt(tonal.e[i][b])
			l2 += tonal.e[i][b]
		}

		stationarity := dsp.Min16(0.99, l1/float32(math.Sqrt(1e-15+float64(float32(nbFrames*l2)))))
		stationarity = float32(stationarity * stationarity)
		stationarity = float32(stationarity * stationarity)
		frameStationarity += stationarity
		bandTonality[b] = dsp.Max16(tE/(1e-15+e), float32(stationarity*tonal.prevBandTonality[b]))
		frameTonality += bandTonality[b]
		if b >= nbTBands-nbTonalSkipBands {
			frameTonality -= bandTonality[b-nbTBands+nbTonalSkipBands]
		}
		maxFrameTonality = dsp.Max16(maxFrameTonality,
			float32((1+float32(.03*float32(b-nbTBands)))*frameTonality))
		slope += float32(bandTonality[b] * float32(b-8))
		tonal.prevBandTonality[b] = bandTonality[b]
	}

	leakageFrom[0] = bandLog2[0]
	leakageTo[0] = bandLog2[0] - leakageOffset
	for b := 1; b < nbTBands+1; b++ {
		leakSlope := float32(float32(leakageSlope*float32(tbands[b]-tbands[b-1])) / 4)
		leakageFrom[b] = dsp.Min16(leakageFrom[b-1]+leakSlope, bandLog2[b])
		leakageTo[b] = dsp.Max16(leakageTo[b-1]-leakSlope, bandLog2[b]-leakageOffset)
	}
	for b := nbTBands - 2; b >= 0; b-- {
		leakSlope := float32(float32(leakageSlope*float32(tbands[b+1]-tbands[b])) / 4)
		leakageFrom[b] = dsp.Min16(leakageFrom[b+1]+leakSlope, leakageFrom[b])
		leakageTo[b] = dsp.Max16(leakageTo[b+1]-leakSlope, leakageTo[b])
	}
	for b := 0; b < nbTBands+1; b++ {
		// leak_boost[] is made up of two terms. The first, based on
		// leakage_to[], represents the boost needed to overcome the amount
		// of analysis leakage cause in a weaker band b by louder
		// neighbouring bands. The second, based on leakage_from[], applies
		// to a loud band b for which the quantization noise causes
		// synthesis leakage to the weaker neighbouring bands.
		boost := dsp.Max16(0, leakageTo[b]-bandLog2[b]) +
			dsp.Max16(0, bandLog2[b]-(leakageFrom[b]+leakageOffset))
		info.LeakBoost[b] = uint8(dsp.IMin(255, int(math.Floor(.5+float64(float32(64*boost))))))
	}
	for b := nbTBands + 1; b < celt.LeakBands; b++ {
		info.LeakBoost[b] = 0
	}

	for i := 0; i < nbFrames; i++ {
		mindist := float32(1e15)
		for j := 0; j < nbFrames; j++ {
			var dist float32
			for k := 0; k < nbTBands; k++ {
				tmp := tonal.logE[i][k] - tonal.logE[j][k]
				dist += float32(tmp * tmp)
			}
			if j != i {
				mindist = dsp.Min32(mindist, dist)
			}
		}
		specVariability += mindist
	}
	specVariability = cSqrt(specVariability / nbFrames / nbTBands)
	bandwidthMask = 0
	bandwidth = 0
	maxE = 0
	noiseFloor := float32(5.7e-4) / float32(int32(1)<<dsp.IMax(0, lsbDepth-8))
	noiseFloor = float32(noiseFloor * noiseFloor)
	belowMaxPitch = 0
	aboveMaxPitch = 0
	b := 0
	for ; b < nbTBands; b++ {
		var e float32
		// Keep a margin of 300 Hz for aliasing.
		bandStart := tbands[b]
		bandEnd := tbands[b+1]
		for i := bandStart; i < bandEnd; i++ {
			e += binEnergy(&out, i)
		}
		e = float32(scaleEner * e)
		maxE = dsp.Max32(maxE, e)
		if bandStart < 64 {
			belowMaxPitch += e
		} else {
			aboveMaxPitch += e
		}
		tonal.meanE[b] = dsp.Max32(float32((1-alphaE2)*tonal.meanE[b]), e)
		em := dsp.Max32(e, tonal.meanE[b])
		// Consider the band "active" only if all these conditions are met:
		// 1) less than 90 dB below the peak band (maximal masking possible
		//    considering both the ATH and the loudness-dependent slope of
		//    the spreading function)
		// 2) above the PCM quantization noise floor
		// We use b+1 because the first CELT band isn't included in tbands[].
		width := float32(bandEnd - bandStart)
		if float32(e*1e9) > maxE && (em > float32(float32(3*noiseFloor)*width) || e > float32(noiseFloor*width)) {
			bandwidth = b + 1
		}
		// Check if the band is masked (see below).
		maskRatio := float32(.05)
		if tonal.prevBandwidth >= b+1 {
			maskRatio = .01
		}
		isMasked[b] = e < float32(maskRatio*bandwidthMask)
		// Use a simple follower with 13 dB/Bark slope for spreading
		// function.
		bandwidthMask = dsp.Max32(float32(.05*bandwidthMask), e)
	}
	// Special case for the last two bands, for which we don't have
	// spectrum but only the energy above 12 kHz. The difficulty here is
	// that the high-pass we use leaks some LF energy, so we need to increase
	// the threshold without accidentally cutting off the band.
	if tonal.fs == 48000 {
		e := float32(hpEner * (1.0 / (60 * 60)))
		noiseRatio := float32(30)
		if tonal.prevBandwidth == 20 {
			noiseRatio = 10
		}

		aboveMaxPitch += e
		tonal.meanE[b] = dsp.Max32(float32((1-alphaE2)*tonal.meanE[b]), e)
		em := dsp.Max32(e, tonal.meanE[b])
		if em > float32(float32(float32(3*noiseRatio)*noiseFloor)*160) || e > float32(float32(noiseRatio*noiseFloor)*160) {
			bandwidth = 20
		}
		// Check if the band is masked (see below).
		maskRatio := float32(.05)
		if tonal.prevBandwidth == 20 {
			maskRatio = .01
		}
		isMasked[b] = e < float32(maskRatio*bandwidthMask)
	}
	if aboveMaxPitch > belowMaxPitch {
		info.MaxPitchRatio = belowMaxPitch / aboveMaxPitch
	} else {
		info.MaxPitchRatio = 1
	}
	// In some cases, resampling aliasing can create a small amount of
	// energy in the first band being cut. So if the last band is masked,
	// we don't include it.
	if bandwidth == 20 && isMasked[nbTBands] {
		bandwidth -= 2
	} else if bandwidth > 0 && bandwidth <= nbTBands && isMasked[bandwidth-1] {
		bandwidth--
	}
	if tonal.count <= 2 {
		bandwidth = 20
	}
	frameLoudness = float32(20 * float32(math.Log10(float64(frameLoudness))))
	tonal.eTracker = dsp.Max32(tonal.eTracker-.003, frameLoudness)
	tonal.lowECount = float32(tonal.lowECount * (1 - alphaE))
	if frameLoudness < tonal.eTracker-30 {
		tonal.lowECount += alphaE
	}

	for i := 0; i < 8; i++ {
		var sum float32
		for b := 0; b < 16; b++ {
			sum += float32(dctTable[i*16+b] * logE[b])
		}
		bfcc[i] = sum
	}
	for i := 0; i < 8; i++ {
		var sum float32
		for b := 0; b < 16; b++ {
			sum += float32(float32(dctTable[i*16+b]*.5) * (tonal.highE[b] + tonal.lowE[b]))
		}
		midE[i] = sum
	}

	frameStationarity /= nbTBands
	relativeE /= nbTBands
	if tonal.count < 10 {
		relativeE = .5
	}
	frameNoisiness /= nbTBands
	info.Activity = frameNoisiness + float32((1-frameNoisiness)*relativeE)
	frameTonality = maxFrameTonality / (nbTBands - nbTonalSkipBands)
	frameTonality = dsp.Max16(frameTonality, float32(tonal.prevTonality*.8))
	tonal.prevTonality = frameTonality

	slope = float32(slope / (8 * 8))
	info.TonalitySlope = slope

	tonal.eCount = (tonal.eCount + 1) % nbFrames
	tonal.count = dsp.IMin(tonal.count+1, analysisCountMax)
	info.Tonality = frameTonality

	mem := &tonal.mem
	for i := 0; i < 4; i++ {
		features[i] = float32(-0.12299*(bfcc[i]+mem[i+24])) + float32(0.49195*(mem[i]+mem[i+16])) +
			float32(0.69693*mem[i+8]) - float32(1.4349*tonal.cmean[i])
	}

	for i := 0; i < 4; i++ {
		tonal.cmean[i] = float32((1-alpha)*tonal.cmean[i]) + float32(alpha*bfcc[i])
	}

	for i := 0; i < 4; i++ {
		features[4+i] = float32(0.63246*(bfcc[i]-mem[i+24])) + float32(0.31623*(mem[i]-mem[i+16]))
	}
	for i := 0; i < 3; i++ {
		features[8+i] = float32(0.53452*(bfcc[i]+mem[i+24])) - float32(0.26726*(mem[i]+mem[i+16])) -
			float32(0.53452*mem[i+8])
	}

	if tonal.count > 5 {
		for i := 0; i < 9; i++ {
			tonal.std[i] = float32((1-alpha)*tonal.std[i]) + float32(float32(alpha*features[i])*features[i])
		}
	}
	for i := 0; i < 4; i++ {
		features[i] = bfcc[i] - midE[i]
	}

	for i := 0; i < 8; i++ {
		mem[i+24] = mem[i+16]
		mem[i+16] = mem[i+8]
		mem[i+8] = mem[i]
		mem[i] = bfcc[i]
	}
	for i := 0; i < 9; i++ {
		features[11+i] = cSqrt(tonal.std[i]) - stdFeatureBias[i]
	}
	features[18] = specVariability - 0.78
	features[20] = info.Tonality - 0.154723
	features[21] = info.Activity - 0.724643
	features[22] = frameStationarity - 0.743717
	features[23] = info.TonalitySlope + 0.069216
	features[24] = tonal.lowECount - 0.067930

	analysisComputeDense(&layer0, layerOut[:], features[:])
	analysisComputeGRU(&layer1, tonal.rnnState[:], layerOut[:])
	analysisComputeDense(&layer2, frameProbs[:], tonal.rnnState[:])

	// Probability of speech or music vs noise.
	info.ActivityProbability = frameProbs[1]
	info.MusicProb = frameProbs[0]

	info.Bandwidth = bandwidth
	tonal.prevBandwidth = bandwidth
	info.Noisiness = frameNoisiness
	info.Valid = 1
}

// runAnalysis is run_analysis. celtMode supplies the 480-point FFT
// (celt_mode->mdct.kfft[0]).
func runAnalysis(analysis *tonalityAnalysisState, celtMode *celt.Mode, pcm analysisPCM,
	analysisFrameSize, frameSize, c1, c2, c int, fs int32, lsbDepth int,
	downmix downmixFunc, info *analysisInfo) {
	analysisFrameSize -= analysisFrameSize & 1
	if !pcm.isNil() {
		fs50 := int(fs / 50)
		// Avoid overflow/wrap-around of the analysis buffer.
		analysisFrameSize = dsp.IMin((detectSize-5)*int(fs)/50, analysisFrameSize)

		pcmLen := analysisFrameSize - analysis.analysisOffset
		offset := analysis.analysisOffset
		for pcmLen > 0 {
			tonalityAnalysis(analysis, celtMode, pcm, dsp.IMin(fs50, pcmLen), offset, c1, c2, c, lsbDepth, downmix)
			offset += fs50
			pcmLen -= fs50
		}
		analysis.analysisOffset = analysisFrameSize

		analysis.analysisOffset -= frameSize
	}

	tonalityGetInfo(analysis, info, frameSize)
}

// tbands is tbands.
var tbands = [nbTBands + 1]int{
	4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 136, 160, 192, 240,
}

// dctTable is dct_table.
var dctTable = [128]float32{
	0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000,
	0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000, 0.250000,
	0.351851, 0.338330, 0.311806, 0.273300, 0.224292, 0.166664, 0.102631, 0.034654,
	-0.034654, -0.102631, -0.166664, -0.224292, -0.273300, -0.311806, -0.338330, -0.351851,
	0.346760, 0.293969, 0.196424, 0.068975, -0.068975, -0.196424, -0.293969, -0.346760,
	-0.346760, -0.293969, -0.196424, -0.068975, 0.068975, 0.196424, 0.293969, 0.346760,
	0.338330, 0.224292, 0.034654, -0.166664, -0.311806, -0.351851, -0.273300, -0.102631,
	0.102631, 0.273300, 0.351851, 0.311806, 0.166664, -0.034654, -0.224292, -0.338330,
	0.326641, 0.135299, -0.135299, -0.326641, -0.326641, -0.135299, 0.135299, 0.326641,
	0.326641, 0.135299, -0.135299, -0.326641, -0.326641, -0.135299, 0.135299, 0.326641,
	0.311806, 0.034654, -0.273300, -0.338330, -0.102631, 0.224292, 0.351851, 0.166664,
	-0.166664, -0.351851, -0.224292, 0.102631, 0.338330, 0.273300, -0.034654, -0.311806,
	0.293969, -0.068975, -0.346760, -0.196424, 0.196424, 0.346760, 0.068975, -0.293969,
	-0.293969, 0.068975, 0.346760, 0.196424, -0.196424, -0.346760, -0.068975, 0.293969,
	0.273300, -0.166664, -0.338330, 0.034654, 0.351851, 0.102631, -0.311806, -0.224292,
	0.224292, 0.311806, -0.102631, -0.351851, -0.034654, 0.338330, 0.166664, -0.273300,
}

// analysisWindow is analysis_window.
var analysisWindow = [240]float32{
	0.000043, 0.000171, 0.000385, 0.000685, 0.001071, 0.001541, 0.002098, 0.002739,
	0.003466, 0.004278, 0.005174, 0.006156, 0.007222, 0.008373, 0.009607, 0.010926,
	0.012329, 0.013815, 0.015385, 0.017037, 0.018772, 0.020590, 0.022490, 0.024472,
	0.026535, 0.028679, 0.030904, 0.033210, 0.035595, 0.038060, 0.040604, 0.043227,
	0.045928, 0.048707, 0.051564, 0.054497, 0.057506, 0.060591, 0.063752, 0.066987,
	0.070297, 0.073680, 0.077136, 0.080665, 0.084265, 0.087937, 0.091679, 0.095492,
	0.099373, 0.103323, 0.107342, 0.111427, 0.115579, 0.119797, 0.124080, 0.128428,
	0.132839, 0.137313, 0.141849, 0.146447, 0.151105, 0.155823, 0.160600, 0.165435,
	0.170327, 0.175276, 0.180280, 0.185340, 0.190453, 0.195619, 0.200838, 0.206107,
	0.211427, 0.216797, 0.222215, 0.227680, 0.233193, 0.238751, 0.244353, 0.250000,
	0.255689, 0.261421, 0.267193, 0.273005, 0.278856, 0.284744, 0.290670, 0.296632,
	0.302628, 0.308658, 0.314721, 0.320816, 0.326941, 0.333097, 0.339280, 0.345492,
	0.351729, 0.357992, 0.364280, 0.370590, 0.376923, 0.383277, 0.389651, 0.396044,
	0.402455, 0.408882, 0.415325, 0.421783, 0.428254, 0.434737, 0.441231, 0.447736,
	0.454249, 0.460770, 0.467298, 0.473832, 0.480370, 0.486912, 0.493455, 0.500000,
	0.506545, 0.513088, 0.519630, 0.526168, 0.532702, 0.539230, 0.545751, 0.552264,
	0.558769, 0.565263, 0.571746, 0.578217, 0.584675, 0.591118, 0.597545, 0.603956,
	0.610349, 0.616723, 0.623077, 0.629410, 0.635720, 0.642008, 0.648271, 0.654508,
	0.660720, 0.666903, 0.673059, 0.679184, 0.685279, 0.691342, 0.697372, 0.703368,
	0.709330, 0.715256, 0.721144, 0.726995, 0.732807, 0.738579, 0.744311, 0.750000,
	0.755647, 0.761249, 0.766807, 0.772320, 0.777785, 0.783203, 0.788573, 0.793893,
	0.799162, 0.804381, 0.809547, 0.814660, 0.819720, 0.824724, 0.829673, 0.834565,
	0.839400, 0.844177, 0.848895, 0.853553, 0.858151, 0.862687, 0.867161, 0.871572,
	0.875920, 0.880203, 0.884421, 0.888573, 0.892658, 0.896677, 0.900627, 0.904508,
	0.908321, 0.912063, 0.915735, 0.919335, 0.922864, 0.926320, 0.929703, 0.933013,
	0.936248, 0.939409, 0.942494, 0.945503, 0.948436, 0.951293, 0.954072, 0.956773,
	0.959396, 0.961940, 0.964405, 0.966790, 0.969096, 0.971321, 0.973465, 0.975528,
	0.977510, 0.979410, 0.981228, 0.982963, 0.984615, 0.986185, 0.987671, 0.989074,
	0.990393, 0.991627, 0.992778, 0.993844, 0.994826, 0.995722, 0.996534, 0.997261,
	0.997902, 0.998459, 0.998929, 0.999315, 0.999615, 0.999829, 0.999957, 1.000000,
}

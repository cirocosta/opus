// Ported from libopus 1.6.1 silk/resampler.c and silk/resampler_private.h.

package silk

// Matrix of resampling methods used:
//
//	                                Fs_out (kHz)
//	                       8      12     16     24     48
//
//	              8        C      UF     U      UF     UF
//	             12        AF     C      UF     U      UF
//	Fs_in (kHz)  16        D      AF     C      UF     UF
//	             24        AF     D      AF     C      U
//	             48        AF     AF     AF     D      C
//
//	C   -> Copy (no resampling)
//	D   -> Allpass-based 2x downsampling
//	U   -> Allpass-based 2x upsampling
//	UF  -> Allpass-based 2x upsampling followed by FIR interpolation
//	AF  -> AR2 filter followed by FIR interpolation

// resampler_private.h.
const (
	resamplerMaxBatchSizeMs = 10
	resamplerMaxFsKHz       = 48
	resamplerMaxBatchSizeIn = resamplerMaxBatchSizeMs * resamplerMaxFsKHz
)

// Tables with delay compensation values to equalize total delay for
// different modes.
var delayMatrixEnc = [6][3]int8{
	// in \ out  8  12  16
	/*  8 */ {6, 0, 3},
	/* 12 */ {0, 7, 3},
	/* 16 */ {0, 1, 10},
	/* 24 */ {0, 2, 6},
	/* 48 */ {18, 10, 12},
	/* 96 */ {0, 0, 44},
}

var delayMatrixDec = [3][6]int8{
	// in \ out  8  12  16  24  48  96
	/*  8 */ {4, 0, 2, 0, 0, 0},
	/* 12 */ {0, 9, 4, 7, 4, 4},
	/* 16 */ {0, 3, 12, 7, 7, 7},
}

// rateID maps [8000, 12000, 16000, 24000, 48000] to [0, 1, 2, 3, 4].
func rateID(r int32) int {
	b2i := func(b bool) int32 {
		if b {
			return 1
		}
		return 0
	}
	v := (((r >> 12) - b2i(r > 16000)) >> uint(b2i(r > 24000))) - 1
	if v > 5 {
		return 5
	}
	return int(v)
}

const (
	useResamplerCopy           = 0
	useResamplerPrivateUp2HQWr = 1
	useResamplerPrivateIIRFIR  = 2
	useResamplerPrivateDownFIR = 3
)

// resamplerInit is silk_resampler_init: initialize or reset the
// resampler state for a given pair of input/output sampling rates.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func resamplerInit(s *resamplerState, fsHzIn, fsHzOut int32, forEnc bool) int {
	// Clear state.
	*s = resamplerState{}

	// Input checking.
	if forEnc {
		if (fsHzIn != 8000 && fsHzIn != 12000 && fsHzIn != 16000 && fsHzIn != 24000 && fsHzIn != 48000) ||
			(fsHzOut != 8000 && fsHzOut != 12000 && fsHzOut != 16000) {
			return -1
		}
		s.inputDelay = int(delayMatrixEnc[rateID(fsHzIn)][rateID(fsHzOut)])
	} else {
		if (fsHzIn != 8000 && fsHzIn != 12000 && fsHzIn != 16000) ||
			(fsHzOut != 8000 && fsHzOut != 12000 && fsHzOut != 16000 && fsHzOut != 24000 && fsHzOut != 48000) {
			return -1
		}
		s.inputDelay = int(delayMatrixDec[rateID(fsHzIn)][rateID(fsHzOut)])
	}

	s.fsInKHz = int(div32_16(fsHzIn, 1000))
	s.fsOutKHz = int(div32_16(fsHzOut, 1000))

	// Number of samples processed per batch.
	s.batchSize = s.fsInKHz * resamplerMaxBatchSizeMs

	// Find resampler with the right sampling ratio.
	up2x := 0
	if fsHzOut > fsHzIn {
		// Upsample.
		if fsHzOut == mul(fsHzIn, 2) {
			// Special case: directly use 2x upsampler.
			s.resamplerFunction = useResamplerPrivateUp2HQWr
		} else {
			// Default resampler.
			s.resamplerFunction = useResamplerPrivateIIRFIR
			up2x = 1
		}
	} else if fsHzOut < fsHzIn {
		// Downsample.
		s.resamplerFunction = useResamplerPrivateDownFIR
		if mul(fsHzOut, 4) == mul(fsHzIn, 3) { // Fs_out : Fs_in = 3 : 4
			s.firFracs = 3
			s.firOrder = resamplerDownOrderFIR0
			s.coefs = resampler3_4COEFS[:]
		} else if mul(fsHzOut, 3) == mul(fsHzIn, 2) { // Fs_out : Fs_in = 2 : 3
			s.firFracs = 2
			s.firOrder = resamplerDownOrderFIR0
			s.coefs = resampler2_3COEFS[:]
		} else if mul(fsHzOut, 2) == fsHzIn { // Fs_out : Fs_in = 1 : 2
			s.firFracs = 1
			s.firOrder = resamplerDownOrderFIR1
			s.coefs = resampler1_2COEFS[:]
		} else if mul(fsHzOut, 3) == fsHzIn { // Fs_out : Fs_in = 1 : 3
			s.firFracs = 1
			s.firOrder = resamplerDownOrderFIR2
			s.coefs = resampler1_3COEFS[:]
		} else if mul(fsHzOut, 4) == fsHzIn { // Fs_out : Fs_in = 1 : 4
			s.firFracs = 1
			s.firOrder = resamplerDownOrderFIR2
			s.coefs = resampler1_4COEFS[:]
		} else if mul(fsHzOut, 6) == fsHzIn { // Fs_out : Fs_in = 1 : 6
			s.firFracs = 1
			s.firOrder = resamplerDownOrderFIR2
			s.coefs = resampler1_6COEFS[:]
		} else {
			// None available.
			return -1
		}
	} else {
		// Input and output sampling rates are equal: copy.
		s.resamplerFunction = useResamplerCopy
	}

	// Ratio of input/output samples.
	s.invRatioQ16 = lshift(div32(lshift(fsHzIn, 14+up2x), fsHzOut), 2)
	// Make sure the ratio is rounded up.
	for smulww(s.invRatioQ16, fsHzOut) < lshift(fsHzIn, up2x) {
		s.invRatioQ16++
	}
	return 0
}

// resampler is silk_resampler: convert from one sampling rate to
// another. Input and output sampling rates are at most 48000 Hz. in
// holds inLen samples; out must hold the resampled output.
func resampler(s *resamplerState, out []int16, in []int16, inLen int) int {
	// Need at least 1 ms of input data, and the delay can't exceed the
	// 1 ms of buffering.
	nSamples := s.fsInKHz - s.inputDelay

	// Copy to delay buffer.
	copy(s.delayBuf[s.inputDelay:s.inputDelay+nSamples], in[:nSamples])

	switch s.resamplerFunction {
	case useResamplerPrivateUp2HQWr:
		resamplerPrivateUp2HQWrapper(s, out, s.delayBuf[:], s.fsInKHz)
		resamplerPrivateUp2HQWrapper(s, out[s.fsOutKHz:], in[nSamples:], inLen-s.fsInKHz)
	case useResamplerPrivateIIRFIR:
		resamplerPrivateIIRFIR(s, out, s.delayBuf[:], s.fsInKHz)
		resamplerPrivateIIRFIR(s, out[s.fsOutKHz:], in[nSamples:], inLen-s.fsInKHz)
	case useResamplerPrivateDownFIR:
		resamplerPrivateDownFIR(s, out, s.delayBuf[:], s.fsInKHz)
		resamplerPrivateDownFIR(s, out[s.fsOutKHz:], in[nSamples:], inLen-s.fsInKHz)
	default:
		copy(out[:s.fsInKHz], s.delayBuf[:s.fsInKHz])
		copy(out[s.fsOutKHz:s.fsOutKHz+inLen-s.fsInKHz], in[nSamples:nSamples+inLen-s.fsInKHz])
	}

	// Copy to delay buffer.
	copy(s.delayBuf[:s.inputDelay], in[inLen-s.inputDelay:inLen])
	return 0
}

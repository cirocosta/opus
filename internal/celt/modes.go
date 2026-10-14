// Ported from libopus 1.6.1 celt/modes.h and celt/modes.c (no
// CUSTOM_MODES).

package celt

import "github.com/cirocosta/opus/internal/dsp"

// Sizes from celt/modes.h.
const (
	// MaxPeriod is MAX_PERIOD.
	MaxPeriod = 1024
	// decPitchBufSize is DEC_PITCH_BUF_SIZE.
	decPitchBufSize = 2048

	// maxBands is the number of bands of the static mode, which sizes
	// the per-band scratch arrays.
	maxBands = 21
)

// PulseCache is PulseCache from modes.h.
type PulseCache struct {
	Size  int
	Index []int16
	Bits  []uint8
	Caps  []uint8
}

// Mode is OpusCustomMode (CELTMode). Only the static 48 kHz mode exists
// in this build; see [StaticMode].
type Mode struct {
	Fs        int32
	Overlap   int
	NbEBands  int
	EffEBands int
	Preemph   [4]float32
	// EBands is the definition of each "pseudo-critical band".
	EBands []int16

	MaxLM         int
	NbShortMdcts  int
	ShortMdctSize int

	// NbAllocVectors is the number of lines in AllocVectors.
	NbAllocVectors int
	// AllocVectors is the number of bits in each band for several rates.
	AllocVectors []uint8
	LogN         []int16

	Window []float32
	MDCT   *dsp.MDCTLookup
	Cache  PulseCache
}

// eband5ms is eband5ms from modes.c.
var eband5ms = []int16{
	// 0  200 400 600 800  1k 1.2 1.4 1.6  2k 2.4 2.8 3.2  4k 4.8 5.6 6.8  8k 9.6 12k 15.6
	0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 14, 16, 20, 24, 28, 34, 40, 48, 60, 78, 100,
}

// bandAllocation is band_allocation from modes.c: the bit allocation table
// in units of 1/32 bit/sample (0.1875 dB SNR), BITALLOC_SIZE rows.
var bandAllocation = []uint8{
	// 0  200 400 600 800  1k 1.2 1.4 1.6  2k 2.4 2.8 3.2  4k 4.8 5.6 6.8  8k 9.6 12k 15.6
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	90, 80, 75, 69, 63, 56, 49, 40, 34, 29, 20, 18, 10, 0, 0, 0, 0, 0, 0, 0, 0,
	110, 100, 90, 84, 78, 71, 65, 58, 51, 45, 39, 32, 26, 20, 12, 0, 0, 0, 0, 0, 0,
	118, 110, 103, 93, 86, 80, 75, 70, 65, 59, 53, 47, 40, 31, 23, 15, 4, 0, 0, 0, 0,
	126, 119, 112, 104, 95, 89, 83, 78, 72, 66, 60, 54, 47, 39, 32, 25, 17, 12, 1, 0, 0,
	134, 127, 120, 114, 103, 97, 91, 85, 78, 72, 66, 60, 54, 47, 41, 35, 29, 23, 16, 10, 1,
	144, 137, 130, 124, 113, 107, 101, 95, 88, 82, 76, 70, 64, 57, 51, 45, 39, 33, 26, 15, 1,
	152, 145, 138, 132, 123, 117, 111, 105, 98, 92, 86, 80, 74, 67, 61, 55, 49, 43, 36, 20, 1,
	162, 155, 148, 142, 133, 127, 121, 115, 108, 102, 96, 90, 84, 77, 71, 65, 59, 53, 46, 30, 1,
	172, 165, 158, 152, 143, 137, 131, 125, 118, 112, 106, 100, 94, 87, 81, 75, 69, 63, 56, 45, 20,
	200, 200, 200, 200, 200, 200, 200, 200, 198, 193, 188, 183, 178, 173, 168, 163, 158, 153, 148, 129, 104,
}

// mode48000_960_120 is mode48000_960_120 from static_modes_float.h, with
// eBands and allocVectors from modes.c.
var mode48000_960_120 = newStaticMode(&dsp.Mode48000_960_120)

func newStaticMode(s *dsp.StaticMode) *Mode {
	return &Mode{
		Fs:             s.Fs,
		Overlap:        s.Overlap,
		NbEBands:       s.NbEBands,
		EffEBands:      s.EffEBands,
		Preemph:        s.Preemph,
		EBands:         eband5ms,
		MaxLM:          s.MaxLM,
		NbShortMdcts:   s.NbShortMdcts,
		ShortMdctSize:  s.ShortMdctSize,
		NbAllocVectors: s.NbAllocVectors,
		AllocVectors:   bandAllocation,
		LogN:           s.LogN,
		Window:         s.Window,
		MDCT:           s.MDCT,
		Cache: PulseCache{
			Size:  s.CacheSize,
			Index: s.CacheIndex,
			Bits:  s.CacheBits,
			Caps:  s.CacheCaps,
		},
	}
}

// StaticMode returns the static 48 kHz mode with 960-sample frames
// (mode48000_960_120). Callers must not modify it.
func StaticMode() *Mode {
	return mode48000_960_120
}

// CustomModeCreate is opus_custom_mode_create without CUSTOM_MODES: it
// returns the static mode whose sampling rate is fs and whose frame size
// is frameSize, frameSize*2, frameSize*4 or frameSize*8. It returns nil
// and [BadArg] when no static mode matches.
func CustomModeCreate(fs int32, frameSize int) (*Mode, int) {
	m := mode48000_960_120
	for j := 0; j < 4; j++ {
		if fs == m.Fs && frameSize<<uint(j) == m.ShortMdctSize*m.NbShortMdcts {
			return m, OK
		}
	}
	return nil, BadArg
}

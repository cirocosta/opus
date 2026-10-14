// Thin exports for the surround analysis of the multistream encoder
// (surround_analysis in libopus 1.6.1 src/opus_multistream_encoder.c).

package celt

// ComputeBandEnergies is compute_band_energies: it writes the amplitude
// (square root of the energy) of each of the first end bands of the C
// channels of the MDCT spectrum X to bandE (NbEBands values per channel).
func ComputeBandEnergies(m *Mode, X []float32, bandE []float32, end, C, LM int) {
	computeBandEnergies(m, X, bandE, end, C, LM)
}

// Amp2Log2 is amp2Log2: it converts the band amplitudes bandE to the log2
// domain, relative to the mean band energies, in bandLogE.
func Amp2Log2(m *Mode, effEnd, end int, bandE, bandLogE []float32, C int) {
	amp2Log2(m, effEnd, end, bandE, bandLogE, C)
}

// Ported from libopus 1.6.1 celt/quant_bands.h and celt/quant_bands.c
// (float build).

package celt

import (
	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
)

// eMeans is eMeans: the mean energy in each band, quantized in Q4 and
// converted back to float.
var eMeans = [25]float32{
	6.437500, 6.250000, 5.750000, 5.312500, 5.062500,
	4.812500, 4.500000, 4.375000, 4.875000, 4.687500,
	4.562500, 4.437500, 4.875000, 4.625000, 4.312500,
	4.500000, 4.375000, 4.625000, 4.750000, 4.437500,
	3.750000, 3.750000, 3.750000, 3.750000, 3.750000,
}

// Prediction coefficients: 0.9, 0.8, 0.65, 0.5.
var (
	predCoef  = [4]float32{29440 / 32768., 26112 / 32768., 21248 / 32768., 16384 / 32768.}
	betaCoef  = [4]float32{30147 / 32768., 22282 / 32768., 12124 / 32768., 6554 / 32768.}
	betaIntra = float32(4915 / 32768.)
)

// eProbModel is e_prob_model: the parameters of the Laplace-like
// probability models used for the coarse energy. There is one pair of
// parameters for each frame size, prediction type (inter/intra), and band
// number. The first number of each pair is the probability of 0, and the
// second is the decay rate, both in Q8 precision.
var eProbModel = [4][2][42]uint8{
	// 120 sample frames.
	{
		// Inter
		{
			72, 127, 65, 129, 66, 128, 65, 128, 64, 128, 62, 128, 64, 128,
			64, 128, 92, 78, 92, 79, 92, 78, 90, 79, 116, 41, 115, 40,
			114, 40, 132, 26, 132, 26, 145, 17, 161, 12, 176, 10, 177, 11,
		},
		// Intra
		{
			24, 179, 48, 138, 54, 135, 54, 132, 53, 134, 56, 133, 55, 132,
			55, 132, 61, 114, 70, 96, 74, 88, 75, 88, 87, 74, 89, 66,
			91, 67, 100, 59, 108, 50, 120, 40, 122, 37, 97, 43, 78, 50,
		},
	},
	// 240 sample frames.
	{
		// Inter
		{
			83, 78, 84, 81, 88, 75, 86, 74, 87, 71, 90, 73, 93, 74,
			93, 74, 109, 40, 114, 36, 117, 34, 117, 34, 143, 17, 145, 18,
			146, 19, 162, 12, 165, 10, 178, 7, 189, 6, 190, 8, 177, 9,
		},
		// Intra
		{
			23, 178, 54, 115, 63, 102, 66, 98, 69, 99, 74, 89, 71, 91,
			73, 91, 78, 89, 86, 80, 92, 66, 93, 64, 102, 59, 103, 60,
			104, 60, 117, 52, 123, 44, 138, 35, 133, 31, 97, 38, 77, 45,
		},
	},
	// 480 sample frames.
	{
		// Inter
		{
			61, 90, 93, 60, 105, 42, 107, 41, 110, 45, 116, 38, 113, 38,
			112, 38, 124, 26, 132, 27, 136, 19, 140, 20, 155, 14, 159, 16,
			158, 18, 170, 13, 177, 10, 187, 8, 192, 6, 175, 9, 159, 10,
		},
		// Intra
		{
			21, 178, 59, 110, 71, 86, 75, 85, 84, 83, 91, 66, 88, 73,
			87, 72, 92, 75, 98, 72, 105, 58, 107, 54, 115, 52, 114, 55,
			112, 56, 129, 51, 132, 40, 150, 33, 140, 29, 98, 35, 77, 42,
		},
	},
	// 960 sample frames.
	{
		// Inter
		{
			42, 121, 96, 66, 108, 43, 111, 40, 117, 44, 123, 32, 120, 36,
			119, 33, 127, 33, 134, 34, 139, 21, 147, 23, 152, 20, 158, 25,
			154, 26, 166, 21, 173, 16, 184, 13, 184, 10, 150, 13, 139, 15,
		},
		// Intra
		{
			22, 178, 63, 114, 74, 82, 84, 83, 92, 82, 103, 62, 96, 72,
			96, 67, 101, 73, 107, 72, 113, 55, 118, 52, 125, 52, 118, 52,
			117, 55, 135, 49, 137, 39, 157, 32, 145, 29, 97, 33, 77, 40,
		},
	},
}

var smallEnergyIcdf = []uint8{2, 1, 0}

// lossDistortion is loss_distortion.
func lossDistortion(eBands, oldEBands []float32, start, end, length, C int) float32 {
	var dist float32
	for c := 0; c < C; c++ {
		for i := start; i < end; i++ {
			d := eBands[i+c*length] - oldEBands[i+c*length]
			dist = dist + float32(d*d)
		}
	}
	return dsp.Min32(200, dist)
}

// quantCoarseEnergyImpl is quant_coarse_energy_impl.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantCoarseEnergyImpl(m *Mode, start, end int, eBands, oldEBands []float32,
	budget, tell int32, probModel []uint8, errs []float32, enc *entcode.Encoder,
	C, LM int, intra bool, maxDecay float32, lfe bool,
) int {
	badness := 0
	var prev [2]float32
	var coef, beta float32

	if tell+3 <= budget {
		enc.EncBitLogp(b2i(intra), 3)
	}
	if intra {
		coef = 0
		beta = betaIntra
	} else {
		beta = betaCoef[LM]
		coef = predCoef[LM]
	}

	// Encode at a fixed coarse resolution.
	for i := start; i < end; i++ {
		for c := 0; c < C; c++ {
			x := eBands[i+c*m.NbEBands]
			oldE := dsp.Max32(-9, oldEBands[i+c*m.NbEBands])
			f := x - float32(coef*oldE) - prev[c]
			// Rounding to nearest integer here is really important!
			qi := floorInt(.5 + f)
			decayBound := dsp.Max32(-28, oldEBands[i+c*m.NbEBands]) - maxDecay
			// Prevent the energy from going down too quickly (e.g. for
			// bands that have just one bin).
			if qi < 0 && x < decayBound {
				qi += truncInt(float64(decayBound - x))
				if qi > 0 {
					qi = 0
				}
			}
			qi0 := qi
			// If we don't have enough bits to encode all the energy, just
			// assume something safe.
			tell = int32(enc.Tell())
			bitsLeft := int(budget-tell) - 3*C*(end-i)
			if i != start && bitsLeft < 30 {
				if bitsLeft < 24 {
					qi = min(1, qi)
				}
				if bitsLeft < 16 {
					qi = max(-1, qi)
				}
			}
			if lfe && i >= 2 {
				qi = min(qi, 0)
			}
			if budget-tell >= 15 {
				pi := 2 * min(i, 20)
				qi = enc.LaplaceEncode(qi, uint32(probModel[pi])<<7, int(probModel[pi+1])<<6)
			} else if budget-tell >= 2 {
				qi = max(-1, min(qi, 1))
				enc.EncIcdf((2*qi)^-b2i(qi < 0), smallEnergyIcdf, 2)
			} else if budget-tell >= 1 {
				qi = min(0, qi)
				enc.EncBitLogp(-qi, 1)
			} else {
				qi = -1
			}
			errs[i+c*m.NbEBands] = f - float32(qi)
			badness += iabs(qi0 - qi)
			q := float32(qi)

			tmp := float32(coef*oldE) + prev[c] + q
			oldEBands[i+c*m.NbEBands] = tmp
			prev[c] = prev[c] + q - float32(beta*q)
		}
	}
	if lfe {
		return 0
	}
	return badness
}

// quantCoarseEnergy is quant_coarse_energy.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantCoarseEnergy(m *Mode, start, end, effEnd int, eBands, oldEBands []float32, budget uint32,
	errs []float32, enc *entcode.Encoder, C, LM, nbAvailableBytes int,
	forceIntra bool, delayedIntra *float32, twoPass bool, lossRate int, lfe bool,
) {
	var oldEBandsIntra, errorIntra [2 * maxBands]float32
	var intraBits [1275]byte
	badness1 := 0

	intra := forceIntra ||
		(!twoPass && *delayedIntra > float32(2*C*(end-start)) && nbAvailableBytes > (end-start)*C)
	intraBias := int32(
		truncInt(
			float64(
				float32(float32(float32(budget)**delayedIntra)*float32(lossRate)) / float32(C*512),
			),
		),
	)
	newDistortion := lossDistortion(eBands, oldEBands, start, effEnd, m.NbEBands, C)

	tell := uint32(enc.Tell())
	if tell+3 > budget {
		twoPass = false
		intra = false
	}

	maxDecay := float32(16)
	if end-start > 10 {
		maxDecay = dsp.Min32(maxDecay, float32(.125*float32(nbAvailableBytes)))
	}
	if lfe {
		maxDecay = 3
	}
	encStartState := *enc

	n := C * m.NbEBands
	copy(oldEBandsIntra[:n], oldEBands[:n])

	if twoPass || intra {
		badness1 = quantCoarseEnergyImpl(m, start, end, eBands, oldEBandsIntra[:], int32(budget),
			int32(tell), eProbModel[LM][1][:], errorIntra[:], enc, C, LM, true, maxDecay, lfe)
	}

	if !intra {
		tellIntra := int32(enc.TellFrac())

		encIntraState := *enc

		nstartBytes := encStartState.RangeBytes()
		nintraBytes := encIntraState.RangeBytes()
		intraBuf := encIntraState.Buffer()[nstartBytes:]
		saveBytes := nintraBytes - nstartBytes
		// Copy bits from intra bit-stream.
		copy(intraBits[:saveBytes], intraBuf[:saveBytes])

		*enc = encStartState

		badness2 := quantCoarseEnergyImpl(m, start, end, eBands, oldEBands, int32(budget),
			int32(tell), eProbModel[LM][b2i(intra)][:], errs, enc, C, LM, false, maxDecay, lfe)

		if twoPass &&
			(badness1 < badness2 || (badness1 == badness2 && int32(enc.TellFrac())+intraBias > tellIntra)) {
			*enc = encIntraState
			// Copy intra bits to bit-stream.
			copy(intraBuf[:saveBytes], intraBits[:saveBytes])
			copy(oldEBands[:n], oldEBandsIntra[:n])
			copy(errs[:n], errorIntra[:n])
			intra = true
		}
	} else {
		copy(oldEBands[:n], oldEBandsIntra[:n])
		copy(errs[:n], errorIntra[:n])
	}

	if intra {
		*delayedIntra = newDistortion
	} else {
		*delayedIntra = float32(float32(predCoef[LM]*predCoef[LM])**delayedIntra) + newDistortion
	}
}

// quantFineEnergy is quant_fine_energy. prevQuant may be nil.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantFineEnergy(
	m *Mode,
	start, end int,
	oldEBands, errs []float32,
	prevQuant, extraQuant []int,
	enc *entcode.Encoder,
	C int,
) {
	// Encode finer resolution.
	for i := start; i < end; i++ {
		extra := int16(1 << uint(extraQuant[i]))
		if extraQuant[i] <= 0 {
			continue
		}
		if enc.Tell()+C*extraQuant[i] > int(int32(enc.Storage*8)) {
			continue
		}
		var prev int16
		if prevQuant != nil {
			prev = int16(prevQuant[i])
		}
		for c := 0; c < C; c++ {
			q2 := floorInt(
				float32(
					(float32(errs[i+c*m.NbEBands]*float32(int(1)<<uint(prev))) + .5) * float32(
						extra,
					),
				),
			)
			if q2 > int(extra)-1 {
				q2 = int(extra) - 1
			}
			if q2 < 0 {
				q2 = 0
			}
			enc.EncBits(uint32(q2), uint(extraQuant[i]))
			offset := float32(
				float32((float32(q2)+.5)*float32(int(1)<<uint(14-extraQuant[i])))*(1.0/16384),
			) - .5
			offset = float32(offset * float32(float32(int(1)<<uint(14-int(prev)))*(1.0/16384)))
			oldEBands[i+c*m.NbEBands] += offset
			errs[i+c*m.NbEBands] -= offset
		}
	}
}

// quantEnergyFinalise is quant_energy_finalise.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func quantEnergyFinalise(
	m *Mode,
	start, end int,
	oldEBands, errs []float32,
	fineQuant, finePriority []int,
	bitsLeft int,
	enc *entcode.Encoder,
	C int,
) {
	// Use up the remaining bits.
	for prio := 0; prio < 2; prio++ {
		for i := start; i < end && bitsLeft >= C; i++ {
			if fineQuant[i] >= maxFineBits || finePriority[i] != prio {
				continue
			}
			for c := 0; c < C; c++ {
				q2 := 1
				if errs[i+c*m.NbEBands] < 0 {
					q2 = 0
				}
				enc.EncBits(uint32(q2), 1)
				offset := float32(
					float32(
						(float32(q2)-.5)*float32(int(1)<<uint(14-fineQuant[i]-1)),
					) * (1.0 / 16384),
				)
				if oldEBands != nil {
					oldEBands[i+c*m.NbEBands] += offset
				}
				errs[i+c*m.NbEBands] -= offset
				bitsLeft--
			}
		}
	}
}

// unquantCoarseEnergy is unquant_coarse_energy.
func unquantCoarseEnergy(
	m *Mode,
	start, end int,
	oldEBands []float32,
	intra bool,
	dec *entcode.Decoder,
	C, LM int,
) {
	probModel := eProbModel[LM][b2i(intra)][:]
	var prev [2]float32
	var coef, beta float32

	if intra {
		coef = 0
		beta = betaIntra
	} else {
		beta = betaCoef[LM]
		coef = predCoef[LM]
	}

	budget := int32(dec.Storage * 8)

	// Decode at a fixed coarse resolution.
	for i := start; i < end; i++ {
		for c := 0; c < C; c++ {
			var qi int
			tell := int32(dec.Tell())
			if budget-tell >= 15 {
				pi := 2 * min(i, 20)
				qi = dec.LaplaceDecode(uint32(probModel[pi])<<7, int(probModel[pi+1])<<6)
			} else if budget-tell >= 2 {
				qi = dec.DecIcdf(smallEnergyIcdf, 2)
				qi = (qi >> 1) ^ -(qi & 1)
			} else if budget-tell >= 1 {
				qi = -dec.DecBitLogp(1)
			} else {
				qi = -1
			}
			q := float32(qi)

			oldEBands[i+c*m.NbEBands] = dsp.Max32(-9, oldEBands[i+c*m.NbEBands])
			tmp := float32(coef*oldEBands[i+c*m.NbEBands]) + prev[c] + q
			oldEBands[i+c*m.NbEBands] = tmp
			prev[c] = prev[c] + q - float32(beta*q)
		}
	}
}

// unquantFineEnergy is unquant_fine_energy. prevQuant may be nil.
func unquantFineEnergy(
	m *Mode,
	start, end int,
	oldEBands []float32,
	prevQuant, extraQuant []int,
	dec *entcode.Decoder,
	C int,
) {
	// Decode finer resolution.
	for i := start; i < end; i++ {
		extra := int16(extraQuant[i])
		if extraQuant[i] <= 0 {
			continue
		}
		if dec.Tell()+C*extraQuant[i] > int(int32(dec.Storage*8)) {
			continue
		}
		var prev int16
		if prevQuant != nil {
			prev = int16(prevQuant[i])
		}
		for c := 0; c < C; c++ {
			q2 := int(dec.DecBits(uint(extra)))
			offset := float32(
				float32((float32(q2)+.5)*float32(int(1)<<uint(14-int(extra))))*(1.0/16384),
			) - .5
			offset = float32(offset * float32(float32(int(1)<<uint(14-int(prev)))*(1.0/16384)))
			oldEBands[i+c*m.NbEBands] += offset
		}
	}
}

// unquantEnergyFinalise is unquant_energy_finalise. oldEBands may be nil.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func unquantEnergyFinalise(
	m *Mode,
	start, end int,
	oldEBands []float32,
	fineQuant, finePriority []int,
	bitsLeft int,
	dec *entcode.Decoder,
	C int,
) {
	// Use up the remaining bits.
	for prio := 0; prio < 2; prio++ {
		for i := start; i < end && bitsLeft >= C; i++ {
			if fineQuant[i] >= maxFineBits || finePriority[i] != prio {
				continue
			}
			for c := 0; c < C; c++ {
				q2 := int(dec.DecBits(1))
				offset := float32(
					float32(
						(float32(q2)-.5)*float32(int(1)<<uint(14-fineQuant[i]-1)),
					) * (1.0 / 16384),
				)
				if oldEBands != nil {
					oldEBands[i+c*m.NbEBands] += offset
				}
				bitsLeft--
			}
		}
	}
}

// amp2Log2 is amp2Log2.
func amp2Log2(m *Mode, effEnd, end int, bandE, bandLogE []float32, C int) {
	for c := 0; c < C; c++ {
		for i := 0; i < effEnd; i++ {
			bandLogE[i+c*m.NbEBands] = dsp.CeltLog2Db(bandE[i+c*m.NbEBands]) - eMeans[i]
		}
		for i := effEnd; i < end; i++ {
			bandLogE[c*m.NbEBands+i] = -14
		}
	}
}

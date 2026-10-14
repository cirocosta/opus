// Ported from libopus 1.6.1 silk/gain_quant.c.

package silk

const (
	gainQuantOffset      = (minQGainDb*128)/6 + 16*128                                            // OFFSET
	gainQuantScaleQ16    = (65536 * (nLevelsQGain - 1)) / (((maxQGainDb - minQGainDb) * 128) / 6) // SCALE_Q16
	gainQuantInvScaleQ16 = (65536 * (((maxQGainDb - minQGainDb) * 128) / 6)) / (nLevelsQGain - 1) // INV_SCALE_Q16
)

// gainsQuant is silk_gains_quant: gain scalar quantization with
// hysteresis, uniform on log scale. This encoder function is ported with
// the file but is not exercised by the decoder tests.
func gainsQuant(ind *[maxNbSubfr]int8, gainQ16 *[maxNbSubfr]int32, prevInd *int8, conditional bool, nbSubfr int) {
	for k := 0; k < nbSubfr; k++ {
		// Convert to log scale, scale, floor().
		ind[k] = int8(smulwb(gainQuantScaleQ16, lin2log(gainQ16[k])-gainQuantOffset))

		// Round towards previous quantized gain (hysteresis).
		if ind[k] < *prevInd {
			ind[k]++
		}
		ind[k] = int8(limitInt(int(ind[k]), 0, nLevelsQGain-1))

		// Compute delta indices and limit.
		if k == 0 && !conditional {
			// Full index.
			ind[k] = int8(limitInt(int(ind[k]), int(*prevInd)+minDeltaGainQuant, nLevelsQGain-1))
			*prevInd = ind[k]
		} else {
			// Delta index.
			ind[k] = ind[k] - *prevInd

			// Double the quantization step size for large gain increases,
			// so that the max gain level can be reached.
			doubleStepSizeThreshold := 2*maxDeltaGainQuant - nLevelsQGain + int(*prevInd)
			if int(ind[k]) > doubleStepSizeThreshold {
				ind[k] = int8(doubleStepSizeThreshold + ((int(ind[k]) - doubleStepSizeThreshold + 1) >> 1))
			}

			ind[k] = int8(limitInt(int(ind[k]), minDeltaGainQuant, maxDeltaGainQuant))

			// Accumulate deltas.
			if int(ind[k]) > doubleStepSizeThreshold {
				*prevInd = int8(int(*prevInd) + (int(ind[k]) << 1) - doubleStepSizeThreshold)
				*prevInd = int8(minInt(int(*prevInd), nLevelsQGain-1))
			} else {
				*prevInd += ind[k]
			}

			// Shift to make non-negative.
			ind[k] -= minDeltaGainQuant
		}

		// Scale and convert to linear scale.
		gainQ16[k] = log2lin(min32(smulwb(gainQuantInvScaleQ16, int32(*prevInd))+gainQuantOffset, 3967)) // 3967 = 31 in Q7
	}
}

// gainsDequant is silk_gains_dequant: gains scalar dequantization,
// uniform on log scale.
func gainsDequant(gainQ16 *[maxNbSubfr]int32, ind *[maxNbSubfr]int8, prevInd *int8, conditional bool, nbSubfr int) {
	for k := 0; k < nbSubfr; k++ {
		if k == 0 && !conditional {
			// Gain index is not allowed to go down more than 16 steps
			// (~21.8 dB).
			*prevInd = int8(maxInt(int(ind[k]), int(*prevInd)-16))
		} else {
			// Delta index.
			indTmp := int(ind[k]) + minDeltaGainQuant

			// Accumulate deltas.
			doubleStepSizeThreshold := 2*maxDeltaGainQuant - nLevelsQGain + int(*prevInd)
			if indTmp > doubleStepSizeThreshold {
				*prevInd = int8(int(*prevInd) + (indTmp << 1) - doubleStepSizeThreshold)
			} else {
				*prevInd = int8(int(*prevInd) + indTmp)
			}
		}
		*prevInd = int8(limitInt(int(*prevInd), 0, nLevelsQGain-1))

		// Scale and convert to linear scale.
		gainQ16[k] = log2lin(min32(smulwb(gainQuantInvScaleQ16, int32(*prevInd))+gainQuantOffset, 3967)) // 3967 = 31 in Q7
	}
}

// gainsID is silk_gains_ID: compute unique identifier of gain indices
// vector.
func gainsID(ind *[maxNbSubfr]int8, nbSubfr int) int32 {
	gainsID := int32(0)
	for k := 0; k < nbSubfr; k++ {
		gainsID = addLshift32(int32(ind[k]), gainsID, 8)
	}
	return gainsID
}

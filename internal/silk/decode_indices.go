// Ported from libopus 1.6.1 silk/decode_indices.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// decodeIndices is silk_decode_indices: decode side-information
// parameters from payload.
func (psDec *decoderState) decodeIndices(psRangeDec *entcode.Decoder, frameIndex int, decodeLBRR bool, condCoding int) {
	var ecIx [maxLPCOrder]int16
	var predQ8 [maxLPCOrder]uint8
	var ix int

	// Decode signal type and quantizer offset.
	if decodeLBRR || psDec.vadFlags[frameIndex] != 0 {
		ix = psRangeDec.DecIcdf(typeOffsetVADICDF[:], 8) + 2
	} else {
		ix = psRangeDec.DecIcdf(typeOffsetNoVADICDF[:], 8)
	}
	psDec.indices.signalType = int8(ix >> 1)
	psDec.indices.quantOffsetType = int8(ix & 1)

	// Decode gains.
	// First subframe.
	if condCoding == codeConditionally {
		// Conditional coding.
		psDec.indices.gainsIndices[0] = int8(psRangeDec.DecIcdf(deltaGainICDF[:], 8))
	} else {
		// Independent coding, in two stages: MSB bits followed by 3 LSBs.
		psDec.indices.gainsIndices[0] = int8(psRangeDec.DecIcdf(gainICDF[psDec.indices.signalType][:], 8) << 3)
		psDec.indices.gainsIndices[0] += int8(psRangeDec.DecIcdf(uniform8ICDF[:], 8))
	}

	// Remaining subframes.
	for i := 1; i < psDec.nbSubfr; i++ {
		psDec.indices.gainsIndices[i] = int8(psRangeDec.DecIcdf(deltaGainICDF[:], 8))
	}

	// Decode LSF Indices.
	cb := psDec.psNLSFCB
	psDec.indices.nlsfIndices[0] = int8(psRangeDec.DecIcdf(cb.cb1ICDF[int(psDec.indices.signalType>>1)*int(cb.nVectors):], 8))
	nlsfUnpack(ecIx[:], predQ8[:], cb, int(psDec.indices.nlsfIndices[0]))
	for i := 0; i < int(cb.order); i++ {
		ix = psRangeDec.DecIcdf(cb.ecICDF[ecIx[i]:], 8)
		if ix == 0 {
			ix -= psRangeDec.DecIcdf(nlsfEXTICDF[:], 8)
		} else if ix == 2*nlsfQuantMaxAmplitude {
			ix += psRangeDec.DecIcdf(nlsfEXTICDF[:], 8)
		}
		psDec.indices.nlsfIndices[i+1] = int8(ix - nlsfQuantMaxAmplitude)
	}

	// Decode LSF interpolation factor.
	if psDec.nbSubfr == maxNbSubfr {
		psDec.indices.nlsfInterpCoefQ2 = int8(psRangeDec.DecIcdf(nlsfInterpolationFactorICDF[:], 8))
	} else {
		psDec.indices.nlsfInterpCoefQ2 = 4
	}

	if psDec.indices.signalType == typeVoiced {
		// Decode pitch lags.
		// Get lag index.
		decodeAbsoluteLagIndex := true
		if condCoding == codeConditionally && psDec.ecPrevSignalType == typeVoiced {
			// Decode Delta index.
			deltaLagIndex := int(int16(psRangeDec.DecIcdf(pitchDeltaICDF[:], 8)))
			if deltaLagIndex > 0 {
				deltaLagIndex = deltaLagIndex - 9
				psDec.indices.lagIndex = int16(int(psDec.ecPrevLagIndex) + deltaLagIndex)
				decodeAbsoluteLagIndex = false
			}
		}
		if decodeAbsoluteLagIndex {
			// Absolute decoding.
			psDec.indices.lagIndex = int16(psRangeDec.DecIcdf(pitchLagICDF[:], 8)) * int16(psDec.fsKHz>>1)
			psDec.indices.lagIndex += int16(psRangeDec.DecIcdf(psDec.pitchLagLowBitsICDF, 8))
		}
		psDec.ecPrevLagIndex = psDec.indices.lagIndex

		// Get contour index.
		psDec.indices.contourIndex = int8(psRangeDec.DecIcdf(psDec.pitchContourICDF, 8))

		// Decode LTP gains.
		// Decode PERIndex value.
		psDec.indices.perIndex = int8(psRangeDec.DecIcdf(ltpPerIndexICDF[:], 8))

		for k := 0; k < psDec.nbSubfr; k++ {
			psDec.indices.ltpIndex[k] = int8(psRangeDec.DecIcdf(ltpGainICDFPtrs[psDec.indices.perIndex], 8))
		}

		// Decode LTP scaling.
		if condCoding == codeIndependently {
			psDec.indices.ltpScaleIndex = int8(psRangeDec.DecIcdf(ltpScaleICDF[:], 8))
		} else {
			psDec.indices.ltpScaleIndex = 0
		}
	}
	psDec.ecPrevSignalType = int(psDec.indices.signalType)

	// Decode seed.
	psDec.indices.seed = int8(psRangeDec.DecIcdf(uniform4ICDF[:], 8))
}

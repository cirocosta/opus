// Ported from libopus 1.6.1 silk/encode_indices.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// encodeIndices is silk_encode_indices: encode side-information
// parameters to payload.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psEncC *encoderState) encodeIndices(
	psRangeEnc *entcode.Encoder,
	frameIndex int,
	encodeLBRR bool,
	condCoding int,
) {
	var (
		ecIx   [maxLPCOrder]int16
		predQ8 [maxLPCOrder]uint8
	)
	var psIndices *sideInfoIndices
	if encodeLBRR {
		psIndices = &psEncC.indicesLBRR[frameIndex]
	} else {
		psIndices = &psEncC.indices
	}

	// Encode signal type and quantizer offset.
	typeOffset := 2*int(psIndices.signalType) + int(psIndices.quantOffsetType)
	if encodeLBRR || typeOffset >= 2 {
		psRangeEnc.EncIcdf(typeOffset-2, typeOffsetVADICDF[:], 8)
	} else {
		psRangeEnc.EncIcdf(typeOffset, typeOffsetNoVADICDF[:], 8)
	}

	// Encode gains.
	// First subframe.
	if condCoding == codeConditionally {
		// Conditional coding.
		psRangeEnc.EncIcdf(int(psIndices.gainsIndices[0]), deltaGainICDF[:], 8)
	} else {
		// Independent coding, in two stages: MSB bits followed by 3 LSBs.
		psRangeEnc.EncIcdf(int(psIndices.gainsIndices[0]>>3), gainICDF[psIndices.signalType][:], 8)
		psRangeEnc.EncIcdf(int(psIndices.gainsIndices[0]&7), uniform8ICDF[:], 8)
	}

	// Remaining subframes.
	for i := 1; i < psEncC.nbSubfr; i++ {
		psRangeEnc.EncIcdf(int(psIndices.gainsIndices[i]), deltaGainICDF[:], 8)
	}

	// Encode NLSFs.
	cb := psEncC.psNLSFCB
	psRangeEnc.EncIcdf(
		int(psIndices.nlsfIndices[0]),
		cb.cb1ICDF[int(psIndices.signalType>>1)*int(cb.nVectors):],
		8,
	)
	nlsfUnpack(ecIx[:], predQ8[:], cb, int(psIndices.nlsfIndices[0]))
	for i := 0; i < int(cb.order); i++ {
		if psIndices.nlsfIndices[i+1] >= nlsfQuantMaxAmplitude {
			psRangeEnc.EncIcdf(2*nlsfQuantMaxAmplitude, cb.ecICDF[ecIx[i]:], 8)
			psRangeEnc.EncIcdf(
				int(psIndices.nlsfIndices[i+1])-nlsfQuantMaxAmplitude,
				nlsfEXTICDF[:],
				8,
			)
		} else if psIndices.nlsfIndices[i+1] <= -nlsfQuantMaxAmplitude {
			psRangeEnc.EncIcdf(0, cb.ecICDF[ecIx[i]:], 8)
			psRangeEnc.EncIcdf(
				-int(psIndices.nlsfIndices[i+1])-nlsfQuantMaxAmplitude,
				nlsfEXTICDF[:],
				8,
			)
		} else {
			psRangeEnc.EncIcdf(
				int(psIndices.nlsfIndices[i+1])+nlsfQuantMaxAmplitude,
				cb.ecICDF[ecIx[i]:],
				8,
			)
		}
	}

	// Encode NLSF interpolation factor.
	if psEncC.nbSubfr == maxNbSubfr {
		psRangeEnc.EncIcdf(int(psIndices.nlsfInterpCoefQ2), nlsfInterpolationFactorICDF[:], 8)
	}

	if psIndices.signalType == typeVoiced {
		// Encode pitch lags.
		// Lag index.
		encodeAbsoluteLagIndex := true
		if condCoding == codeConditionally && psEncC.ecPrevSignalType == typeVoiced {
			// Delta Encoding.
			deltaLagIndex := int(psIndices.lagIndex) - int(psEncC.ecPrevLagIndex)
			if deltaLagIndex < -8 || deltaLagIndex > 11 {
				deltaLagIndex = 0
			} else {
				deltaLagIndex = deltaLagIndex + 9
				encodeAbsoluteLagIndex = false // Only use delta
			}
			psRangeEnc.EncIcdf(deltaLagIndex, pitchDeltaICDF[:], 8)
		}
		if encodeAbsoluteLagIndex {
			// Absolute encoding.
			pitchHighBits := div32_16(int32(psIndices.lagIndex), int32(psEncC.fsKHz>>1))
			pitchLowBits := int32(
				psIndices.lagIndex,
			) - smulbb(
				pitchHighBits,
				int32(psEncC.fsKHz>>1),
			)
			psRangeEnc.EncIcdf(int(pitchHighBits), pitchLagICDF[:], 8)
			psRangeEnc.EncIcdf(int(pitchLowBits), psEncC.pitchLagLowBitsICDF, 8)
		}
		psEncC.ecPrevLagIndex = psIndices.lagIndex

		// Contour index.
		psRangeEnc.EncIcdf(int(psIndices.contourIndex), psEncC.pitchContourICDF, 8)

		// Encode LTP gains.
		// PERIndex value.
		psRangeEnc.EncIcdf(int(psIndices.perIndex), ltpPerIndexICDF[:], 8)

		// Codebook Indices.
		for k := 0; k < psEncC.nbSubfr; k++ {
			psRangeEnc.EncIcdf(int(psIndices.ltpIndex[k]), ltpGainICDFPtrs[psIndices.perIndex], 8)
		}

		// Encode LTP scaling.
		if condCoding == codeIndependently {
			psRangeEnc.EncIcdf(int(psIndices.ltpScaleIndex), ltpScaleICDF[:], 8)
		}
	}

	psEncC.ecPrevSignalType = int(psIndices.signalType)

	// Encode seed.
	psRangeEnc.EncIcdf(int(psIndices.seed), uniform4ICDF[:], 8)
}

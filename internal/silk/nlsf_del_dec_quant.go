// Ported from libopus 1.6.1 silk/NLSF_del_dec_quant.c.

package silk

// nlsfQuantLevelAdjQ10 is SILK_FIX_CONST( NLSF_QUANT_LEVEL_ADJ, 10 ).
var nlsfQuantLevelAdjQ10 = int16(fixConst(nlsfQuantLevelAdj, 10))

// nlsfDelDecQuant is silk_NLSF_del_dec_quant: delayed-decision quantizer
// for NLSF residuals. It returns the RD value in Q25.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func nlsfDelDecQuant(indices []int8, xQ10 []int16, wQ5 []int16, predCoefQ8 []uint8, ecIx []int16,
	ecRatesQ5 []uint8, quantStepSizeQ16 int, invQuantStepSizeQ6 int16, muQ20 int32, order int16,
) int32 {
	var (
		indSort    [nlsfQuantDelDecStates]int
		ind        [nlsfQuantDelDecStates][maxLPCOrder]int8
		prevOutQ10 [2 * nlsfQuantDelDecStates]int16
		rdQ25      [2 * nlsfQuantDelDecStates]int32
		rdMinQ25   [nlsfQuantDelDecStates]int32
		rdMaxQ25   [nlsfQuantDelDecStates]int32

		out0Q10Table [2 * nlsfQuantMaxAmplitudeExt]int
		out1Q10Table [2 * nlsfQuantMaxAmplitudeExt]int
	)

	for i := -nlsfQuantMaxAmplitudeExt; i <= nlsfQuantMaxAmplitudeExt-1; i++ {
		out0Q10 := int16(lshift(int32(i), 10))
		out1Q10 := out0Q10 + 1024
		if i > 0 {
			out0Q10 = out0Q10 - nlsfQuantLevelAdjQ10
			out1Q10 = out1Q10 - nlsfQuantLevelAdjQ10
		} else if i == 0 {
			out1Q10 = out1Q10 - nlsfQuantLevelAdjQ10
		} else if i == -1 {
			out0Q10 = out0Q10 + nlsfQuantLevelAdjQ10
		} else {
			out0Q10 = out0Q10 + nlsfQuantLevelAdjQ10
			out1Q10 = out1Q10 + nlsfQuantLevelAdjQ10
		}
		out0Q10Table[i+nlsfQuantMaxAmplitudeExt] = int(
			smulbb(int32(out0Q10), int32(quantStepSizeQ16)) >> 16,
		)
		out1Q10Table[i+nlsfQuantMaxAmplitudeExt] = int(
			smulbb(int32(out1Q10), int32(quantStepSizeQ16)) >> 16,
		)
	}

	nStates := 1
	rdQ25[0] = 0
	prevOutQ10[0] = 0
	for i := int(order) - 1; i >= 0; i-- {
		ratesQ5 := ecRatesQ5[ecIx[i]:]
		inQ10 := int(xQ10[i])
		for j := 0; j < nStates; j++ {
			predQ10 := int(smulbb(int32(int16(predCoefQ8[i])), int32(prevOutQ10[j])) >> 8)
			resQ10 := inQ10 - predQ10
			indTmp := int(smulbb(int32(invQuantStepSizeQ6), int32(resQ10)) >> 16)
			indTmp = limitInt(indTmp, -nlsfQuantMaxAmplitudeExt, nlsfQuantMaxAmplitudeExt-1)
			ind[j][i] = int8(indTmp)

			// Compute outputs for ind_tmp and ind_tmp + 1.
			out0Q10 := int16(out0Q10Table[indTmp+nlsfQuantMaxAmplitudeExt])
			out1Q10 := int16(out1Q10Table[indTmp+nlsfQuantMaxAmplitudeExt])

			out0Q10 = out0Q10 + int16(predQ10)
			out1Q10 = out1Q10 + int16(predQ10)
			prevOutQ10[j] = out0Q10
			prevOutQ10[j+nStates] = out1Q10

			// Compute RD for ind_tmp and ind_tmp + 1.
			var rate0Q5, rate1Q5 int
			if indTmp+1 >= nlsfQuantMaxAmplitude {
				if indTmp+1 == nlsfQuantMaxAmplitude {
					rate0Q5 = int(ratesQ5[indTmp+nlsfQuantMaxAmplitude])
					rate1Q5 = 280
				} else {
					rate0Q5 = int(smlabb(280-43*nlsfQuantMaxAmplitude, 43, int32(indTmp)))
					rate1Q5 = rate0Q5 + 43
				}
			} else if indTmp <= -nlsfQuantMaxAmplitude {
				if indTmp == -nlsfQuantMaxAmplitude {
					rate0Q5 = 280
					rate1Q5 = int(ratesQ5[indTmp+1+nlsfQuantMaxAmplitude])
				} else {
					rate0Q5 = int(smlabb(280-43*nlsfQuantMaxAmplitude, -43, int32(indTmp)))
					rate1Q5 = rate0Q5 - 43
				}
			} else {
				rate0Q5 = int(ratesQ5[indTmp+nlsfQuantMaxAmplitude])
				rate1Q5 = int(ratesQ5[indTmp+1+nlsfQuantMaxAmplitude])
			}
			rdTmpQ25 := rdQ25[j]
			diffQ10 := int32(inQ10 - int(out0Q10))
			rdQ25[j] = smlabb(
				mla(rdTmpQ25, smulbb(diffQ10, diffQ10), int32(wQ5[i])),
				muQ20,
				int32(rate0Q5),
			)
			diffQ10 = int32(inQ10 - int(out1Q10))
			rdQ25[j+nStates] = smlabb(
				mla(rdTmpQ25, smulbb(diffQ10, diffQ10), int32(wQ5[i])),
				muQ20,
				int32(rate1Q5),
			)
		}

		if nStates <= nlsfQuantDelDecStates/2 {
			// Double number of states and copy.
			for j := 0; j < nStates; j++ {
				ind[j+nStates][i] = ind[j][i] + 1
			}
			nStates = nStates << 1
			for j := nStates; j < nlsfQuantDelDecStates; j++ {
				ind[j][i] = ind[j-nStates][i]
			}
		} else {
			// Sort lower and upper half of RD_Q25, pairwise.
			for j := 0; j < nlsfQuantDelDecStates; j++ {
				if rdQ25[j] > rdQ25[j+nlsfQuantDelDecStates] {
					rdMaxQ25[j] = rdQ25[j]
					rdMinQ25[j] = rdQ25[j+nlsfQuantDelDecStates]
					rdQ25[j] = rdMinQ25[j]
					rdQ25[j+nlsfQuantDelDecStates] = rdMaxQ25[j]
					// Swap prev_out values.
					out0Q10 := prevOutQ10[j]
					prevOutQ10[j] = prevOutQ10[j+nlsfQuantDelDecStates]
					prevOutQ10[j+nlsfQuantDelDecStates] = out0Q10
					indSort[j] = j + nlsfQuantDelDecStates
				} else {
					rdMinQ25[j] = rdQ25[j]
					rdMaxQ25[j] = rdQ25[j+nlsfQuantDelDecStates]
					indSort[j] = j
				}
			}
			// Compare the highest RD values of the winning half with the
			// lowest one in the losing half, and copy if necessary.
			// Afterwards ind_sort[] will contain the indices of the
			// NLSF_QUANT_DEL_DEC_STATES winning RD values.
			for {
				minMaxQ25 := int32MAX
				maxMinQ25 := int32(0)
				indMinMax := 0
				indMaxMin := 0
				for j := 0; j < nlsfQuantDelDecStates; j++ {
					if minMaxQ25 > rdMaxQ25[j] {
						minMaxQ25 = rdMaxQ25[j]
						indMinMax = j
					}
					if maxMinQ25 < rdMinQ25[j] {
						maxMinQ25 = rdMinQ25[j]
						indMaxMin = j
					}
				}
				if minMaxQ25 >= maxMinQ25 {
					break
				}
				// Copy ind_min_max to ind_max_min.
				indSort[indMaxMin] = indSort[indMinMax] ^ nlsfQuantDelDecStates
				rdQ25[indMaxMin] = rdQ25[indMinMax+nlsfQuantDelDecStates]
				prevOutQ10[indMaxMin] = prevOutQ10[indMinMax+nlsfQuantDelDecStates]
				rdMinQ25[indMaxMin] = 0
				rdMaxQ25[indMinMax] = int32MAX
				ind[indMaxMin] = ind[indMinMax]
			}
			// Increment index if it comes from the upper half.
			for j := 0; j < nlsfQuantDelDecStates; j++ {
				ind[j][i] += int8(indSort[j] >> nlsfQuantDelDecStatesLog2)
			}
		}
	}

	// Last sample: find winner, copy indices and return RD value.
	indTmp := 0
	minQ25 := int32MAX
	for j := 0; j < 2*nlsfQuantDelDecStates; j++ {
		if minQ25 > rdQ25[j] {
			minQ25 = rdQ25[j]
			indTmp = j
		}
	}
	for j := 0; j < int(order); j++ {
		indices[j] = ind[indTmp&(nlsfQuantDelDecStates-1)][j]
	}
	indices[0] += int8(indTmp >> nlsfQuantDelDecStatesLog2)
	return minQ25
}

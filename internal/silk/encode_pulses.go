// Ported from libopus 1.6.1 silk/encode_pulses.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// combineAndCheck is combine_and_check. It returns 1 if a sum exceeds
// maxPulses.
func combineAndCheck(pulsesComb []int, pulsesIn []int, maxPulses, length int) int {
	for k := 0; k < length; k++ {
		sum := pulsesIn[2*k] + pulsesIn[2*k+1]
		if sum > maxPulses {
			return 1
		}
		pulsesComb[k] = sum
	}
	return 0
}

// encodePulses is silk_encode_pulses: encode quantization indices of
// excitation. pulses must have room for frameLength rounded up to a
// multiple of SHELL_CODEC_FRAME_LENGTH.
func encodePulses(psRangeEnc *entcode.Encoder, signalType, quantOffsetType int, pulses []int8, frameLength int) {
	var (
		absPulses  [maxNbShellBlocks * shellCodecFrameLength]int
		sumPulses  [maxNbShellBlocks]int
		nRshifts   [maxNbShellBlocks]int
		pulsesComb [8]int
	)
	rateLevelIndex := 0

	// Prepare for shell coding.
	// Calculate number of shell blocks.
	iter := frameLength >> log2ShellCodecFrameLength
	if iter*shellCodecFrameLength < frameLength {
		iter++
		for i := frameLength; i < frameLength+shellCodecFrameLength; i++ {
			pulses[i] = 0
		}
	}

	// Take the absolute value of the pulses.
	for i := 0; i < iter*shellCodecFrameLength; i += 4 {
		absPulses[i+0] = absInt(int(pulses[i+0]))
		absPulses[i+1] = absInt(int(pulses[i+1]))
		absPulses[i+2] = absInt(int(pulses[i+2]))
		absPulses[i+3] = absInt(int(pulses[i+3]))
	}

	// Calc sum pulses per shell code frame.
	absPulsesPtr := 0
	for i := 0; i < iter; i++ {
		nRshifts[i] = 0
		ap := absPulses[absPulsesPtr : absPulsesPtr+shellCodecFrameLength]

		for {
			// 1+1 -> 2.
			scaleDown := combineAndCheck(pulsesComb[:], ap, int(maxPulsesTable[0]), 8)
			// 2+2 -> 4.
			scaleDown += combineAndCheck(pulsesComb[:], pulsesComb[:], int(maxPulsesTable[1]), 4)
			// 4+4 -> 8.
			scaleDown += combineAndCheck(pulsesComb[:], pulsesComb[:], int(maxPulsesTable[2]), 2)
			// 8+8 -> 16.
			scaleDown += combineAndCheck(sumPulses[i:], pulsesComb[:], int(maxPulsesTable[3]), 1)

			if scaleDown != 0 {
				// We need to downscale the quantization signal.
				nRshifts[i]++
				for k := 0; k < shellCodecFrameLength; k++ {
					ap[k] = ap[k] >> 1
				}
			} else {
				// Jump out of while(1) loop and go to next shell coding
				// frame.
				break
			}
		}
		absPulsesPtr += shellCodecFrameLength
	}

	// Rate level.
	// Find rate level that leads to fewest bits for coding of pulses per
	// block info.
	minSumBitsQ5 := int32MAX
	for k := 0; k < nRateLevels-1; k++ {
		nBitsPtr := &pulsesPerBlockBITSQ5[k]
		sumBitsQ5 := int32(rateLevelsBITSQ5[signalType>>1][k])
		for i := 0; i < iter; i++ {
			if nRshifts[i] > 0 {
				sumBitsQ5 += int32(nBitsPtr[silkMaxPulses+1])
			} else {
				sumBitsQ5 += int32(nBitsPtr[sumPulses[i]])
			}
		}
		if sumBitsQ5 < minSumBitsQ5 {
			minSumBitsQ5 = sumBitsQ5
			rateLevelIndex = k
		}
	}
	psRangeEnc.EncIcdf(rateLevelIndex, rateLevelsICDF[signalType>>1][:], 8)

	// Sum-Weighted-Pulses Encoding.
	cdfPtr := pulsesPerBlockICDF[rateLevelIndex][:]
	for i := 0; i < iter; i++ {
		if nRshifts[i] == 0 {
			psRangeEnc.EncIcdf(sumPulses[i], cdfPtr, 8)
		} else {
			psRangeEnc.EncIcdf(silkMaxPulses+1, cdfPtr, 8)
			for k := 0; k < nRshifts[i]-1; k++ {
				psRangeEnc.EncIcdf(silkMaxPulses+1, pulsesPerBlockICDF[nRateLevels-1][:], 8)
			}
			psRangeEnc.EncIcdf(sumPulses[i], pulsesPerBlockICDF[nRateLevels-1][:], 8)
		}
	}

	// Shell Encoding.
	for i := 0; i < iter; i++ {
		if sumPulses[i] > 0 {
			shellEncoder(psRangeEnc, absPulses[i*shellCodecFrameLength:])
		}
	}

	// LSB Encoding.
	for i := 0; i < iter; i++ {
		if nRshifts[i] > 0 {
			pulsesPtr := pulses[i*shellCodecFrameLength:]
			nLS := nRshifts[i] - 1
			for k := 0; k < shellCodecFrameLength; k++ {
				absQ := int32(int8(absInt(int(pulsesPtr[k]))))
				for j := nLS; j > 0; j-- {
					bit := (absQ >> uint(j)) & 1
					psRangeEnc.EncIcdf(int(bit), lsbICDF[:], 8)
				}
				bit := absQ & 1
				psRangeEnc.EncIcdf(int(bit), lsbICDF[:], 8)
			}
		}
	}

	// Encode signs.
	encodeSigns(psRangeEnc, pulses, frameLength, signalType, quantOffsetType, &sumPulses)
}

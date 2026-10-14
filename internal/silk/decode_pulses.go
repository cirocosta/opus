// Ported from libopus 1.6.1 silk/decode_pulses.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// decodePulses is silk_decode_pulses: decode quantization indices of
// excitation. pulses must hold frameLength rounded up to a multiple of
// SHELL_CODEC_FRAME_LENGTH samples.
func decodePulses(psRangeDec *entcode.Decoder, pulses []int16, signalType, quantOffsetType, frameLength int) {
	var sumPulses, nLshifts [maxNbShellBlocks]int

	// Decode rate level.
	rateLevelIndex := psRangeDec.DecIcdf(rateLevelsICDF[signalType>>1][:], 8)

	// Calculate number of shell blocks.
	iter := frameLength >> log2ShellCodecFrameLength
	if iter*shellCodecFrameLength < frameLength {
		// Make sure only happens for 10 ms @ 12 kHz.
		iter++
	}

	// Sum-Weighted-Pulses Decoding.
	cdfPtr := pulsesPerBlockICDF[rateLevelIndex][:]
	for i := 0; i < iter; i++ {
		nLshifts[i] = 0
		sumPulses[i] = psRangeDec.DecIcdf(cdfPtr, 8)

		// LSB indication.
		for sumPulses[i] == silkMaxPulses+1 {
			nLshifts[i]++
			// When we've already got 10 LSBs, we shift the table to not
			// allow (SILK_MAX_PULSES + 1).
			off := 0
			if nLshifts[i] == 10 {
				off = 1
			}
			sumPulses[i] = psRangeDec.DecIcdf(pulsesPerBlockICDF[nRateLevels-1][off:], 8)
		}
	}

	// Shell decoding.
	for i := 0; i < iter; i++ {
		blk := pulses[i*shellCodecFrameLength : (i+1)*shellCodecFrameLength]
		if sumPulses[i] > 0 {
			shellDecoder(blk, psRangeDec, sumPulses[i])
		} else {
			clear(blk)
		}
	}

	// LSB Decoding.
	for i := 0; i < iter; i++ {
		if nLshifts[i] > 0 {
			nLS := nLshifts[i]
			pulsesPtr := pulses[i*shellCodecFrameLength:]
			for k := 0; k < shellCodecFrameLength; k++ {
				absQ := int32(pulsesPtr[k])
				for j := 0; j < nLS; j++ {
					absQ = lshift(absQ, 1)
					absQ += int32(psRangeDec.DecIcdf(lsbICDF[:], 8))
				}
				pulsesPtr[k] = int16(absQ)
			}
			// Mark the number of pulses non-zero for sign decoding.
			sumPulses[i] |= nLS << 5
		}
	}

	// Decode and add signs to pulse signal.
	decodeSigns(psRangeDec, pulses, frameLength, signalType, quantOffsetType, &sumPulses)
}

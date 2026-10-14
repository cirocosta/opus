// Ported from libopus 1.6.1 silk/code_signs.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// silkEncMap is silk_enc_map (shifting avoids if-statement).
func silkEncMap(a int32) int { return int((a >> 15) + 1) }

// silkDecMap is silk_dec_map.
func silkDecMap(a int) int16 { return int16((a << 1) - 1) }

// encodeSigns is silk_encode_signs: encodes signs of excitation. This
// encoder function is ported with the file but is not exercised by the
// decoder tests.
func encodeSigns(psRangeEnc *entcode.Encoder, pulses []int8, length, signalType, quantOffsetType int,
	sumPulses *[maxNbShellBlocks]int) {
	var icdf [2]uint8
	icdf[1] = 0
	qPtr := pulses
	i := int(smulbb(7, int32(quantOffsetType+(signalType<<1))))
	icdfPtr := signICDF[i:]
	length = (length + shellCodecFrameLength/2) >> log2ShellCodecFrameLength
	for i = 0; i < length; i++ {
		p := sumPulses[i]
		if p > 0 {
			icdf[0] = icdfPtr[minInt(p&0x1F, 6)]
			for j := 0; j < shellCodecFrameLength; j++ {
				if qPtr[j] != 0 {
					psRangeEnc.EncIcdf(silkEncMap(int32(qPtr[j])), icdf[:], 8)
				}
			}
		}
		qPtr = qPtr[shellCodecFrameLength:]
	}
}

// decodeSigns is silk_decode_signs: decodes signs of excitation.
func decodeSigns(psRangeDec *entcode.Decoder, pulses []int16, length, signalType, quantOffsetType int,
	sumPulses *[maxNbShellBlocks]int) {
	var icdf [2]uint8
	icdf[1] = 0
	qPtr := pulses
	i := int(smulbb(7, int32(quantOffsetType+(signalType<<1))))
	icdfPtr := signICDF[i:]
	length = (length + shellCodecFrameLength/2) >> log2ShellCodecFrameLength
	for i = 0; i < length; i++ {
		p := sumPulses[i]
		if p > 0 {
			icdf[0] = icdfPtr[minInt(p&0x1F, 6)]
			for j := 0; j < shellCodecFrameLength; j++ {
				if qPtr[j] > 0 {
					// Attach sign: implementation with shift, subtraction,
					// multiplication.
					qPtr[j] *= silkDecMap(psRangeDec.DecIcdf(icdf[:], 8))
				}
			}
		}
		qPtr = qPtr[shellCodecFrameLength:]
	}
}

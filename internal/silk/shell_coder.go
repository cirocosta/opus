// Ported from libopus 1.6.1 silk/shell_coder.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// Shell coder; pulse-subframe length is hardcoded.

// combinePulses is combine_pulses.
func combinePulses(out []int, in []int, len int) {
	for k := 0; k < len; k++ {
		out[k] = in[2*k] + in[2*k+1]
	}
}

// encodeSplit is encode_split.
func encodeSplit(psRangeEnc *entcode.Encoder, pChild1, p int, shellTable []uint8) {
	if p > 0 {
		psRangeEnc.EncIcdf(pChild1, shellTable[shellCodeTableOffsets[p]:], 8)
	}
}

// decodeSplit is decode_split.
func decodeSplit(pChild1, pChild2 *int16, psRangeDec *entcode.Decoder, p int, shellTable []uint8) {
	if p > 0 {
		*pChild1 = int16(psRangeDec.DecIcdf(shellTable[shellCodeTableOffsets[p]:], 8))
		*pChild2 = int16(p) - *pChild1
	} else {
		*pChild1 = 0
		*pChild2 = 0
	}
}

// shellEncoder is silk_shell_encoder: shell encoder, operates on one
// shell code frame of 16 pulses. This encoder function is ported with
// the file but is not exercised by the decoder tests.
func shellEncoder(psRangeEnc *entcode.Encoder, pulses0 []int) {
	var pulses1 [8]int
	var pulses2 [4]int
	var pulses3 [2]int
	var pulses4 [1]int

	// Tree representation per pulse-subframe.
	combinePulses(pulses1[:], pulses0, 8)
	combinePulses(pulses2[:], pulses1[:], 4)
	combinePulses(pulses3[:], pulses2[:], 2)
	combinePulses(pulses4[:], pulses3[:], 1)

	t0, t1, t2, t3 := shellCodeTable0[:], shellCodeTable1[:], shellCodeTable2[:], shellCodeTable3[:]
	encodeSplit(psRangeEnc, pulses3[0], pulses4[0], t3)

	encodeSplit(psRangeEnc, pulses2[0], pulses3[0], t2)

	encodeSplit(psRangeEnc, pulses1[0], pulses2[0], t1)
	encodeSplit(psRangeEnc, pulses0[0], pulses1[0], t0)
	encodeSplit(psRangeEnc, pulses0[2], pulses1[1], t0)

	encodeSplit(psRangeEnc, pulses1[2], pulses2[1], t1)
	encodeSplit(psRangeEnc, pulses0[4], pulses1[2], t0)
	encodeSplit(psRangeEnc, pulses0[6], pulses1[3], t0)

	encodeSplit(psRangeEnc, pulses2[2], pulses3[1], t2)

	encodeSplit(psRangeEnc, pulses1[4], pulses2[2], t1)
	encodeSplit(psRangeEnc, pulses0[8], pulses1[4], t0)
	encodeSplit(psRangeEnc, pulses0[10], pulses1[5], t0)

	encodeSplit(psRangeEnc, pulses1[6], pulses2[3], t1)
	encodeSplit(psRangeEnc, pulses0[12], pulses1[6], t0)
	encodeSplit(psRangeEnc, pulses0[14], pulses1[7], t0)
}

// shellDecoder is silk_shell_decoder: shell decoder, operates on one
// shell code frame of 16 pulses.
func shellDecoder(pulses0 []int16, psRangeDec *entcode.Decoder, pulses4 int) {
	var pulses3 [2]int16
	var pulses2 [4]int16
	var pulses1 [8]int16

	t0, t1, t2, t3 := shellCodeTable0[:], shellCodeTable1[:], shellCodeTable2[:], shellCodeTable3[:]
	decodeSplit(&pulses3[0], &pulses3[1], psRangeDec, pulses4, t3)

	decodeSplit(&pulses2[0], &pulses2[1], psRangeDec, int(pulses3[0]), t2)

	decodeSplit(&pulses1[0], &pulses1[1], psRangeDec, int(pulses2[0]), t1)
	decodeSplit(&pulses0[0], &pulses0[1], psRangeDec, int(pulses1[0]), t0)
	decodeSplit(&pulses0[2], &pulses0[3], psRangeDec, int(pulses1[1]), t0)

	decodeSplit(&pulses1[2], &pulses1[3], psRangeDec, int(pulses2[1]), t1)
	decodeSplit(&pulses0[4], &pulses0[5], psRangeDec, int(pulses1[2]), t0)
	decodeSplit(&pulses0[6], &pulses0[7], psRangeDec, int(pulses1[3]), t0)

	decodeSplit(&pulses2[2], &pulses2[3], psRangeDec, int(pulses3[1]), t2)

	decodeSplit(&pulses1[4], &pulses1[5], psRangeDec, int(pulses2[2]), t1)
	decodeSplit(&pulses0[8], &pulses0[9], psRangeDec, int(pulses1[4]), t0)
	decodeSplit(&pulses0[10], &pulses0[11], psRangeDec, int(pulses1[5]), t0)

	decodeSplit(&pulses1[6], &pulses1[7], psRangeDec, int(pulses2[3]), t1)
	decodeSplit(&pulses0[12], &pulses0[13], psRangeDec, int(pulses1[6]), t0)
	decodeSplit(&pulses0[14], &pulses0[15], psRangeDec, int(pulses1[7]), t0)
}

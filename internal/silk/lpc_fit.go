// Ported from libopus 1.6.1 silk/LPC_fit.c.

package silk

// lpcFit is silk_LPC_fit: convert int32 coefficients to int16 coefs and
// make sure there's no wrap-around.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func lpcFit(aQOUT []int16, aQIN []int32, qOUT, qIN, d int) {
	var i int
	idx := 0

	// Limit the maximum absolute value of the prediction coefficients, so
	// that they'll fit in int16.
	for i = 0; i < 10; i++ {
		// Find maximum absolute value and its index.
		maxabs := int32(0)
		for k := 0; k < d; k++ {
			absval := abs32(aQIN[k])
			if absval > maxabs {
				maxabs = absval
				idx = k
			}
		}
		maxabs = rshiftRound(maxabs, qIN-qOUT)

		if maxabs > int16MAX {
			// Reduce magnitude of prediction coefficients.
			maxabs = min32(
				maxabs,
				163838,
			) // ( silk_int32_MAX >> 14 ) + silk_int16_MAX = 163838
			chirpQ16 := 65470 - div32(
				lshift(maxabs-int16MAX, 14),
				(maxabs*int32(idx+1))>>2,
			) // SILK_FIX_CONST( 0.999, 16 ) = 65470
			bwexpander32(aQIN, d, chirpQ16)
		} else {
			break
		}
	}

	if i == 10 {
		// Reached the last iteration, clip the coefficients.
		for k := 0; k < d; k++ {
			aQOUT[k] = int16(sat16(rshiftRound(aQIN[k], qIN-qOUT)))
			aQIN[k] = lshift(int32(aQOUT[k]), qIN-qOUT)
		}
	} else {
		for k := 0; k < d; k++ {
			aQOUT[k] = int16(rshiftRound(aQIN[k], qIN-qOUT))
		}
	}
}

// Ported from libopus 1.6.1 silk/LPC_inv_pred_gain.c.

package silk

const (
	lpcInvPredGainQA = 24             // QA
	lpcInvAlimit     = 16773022       // A_LIMIT = SILK_FIX_CONST( 0.99975, QA )
	lpcInvMinGainQ30 = 107374         // SILK_FIX_CONST( 1.0f / MAX_PREDICTION_POWER_GAIN, 30 )
	lpcInvOneQ30     = int32(1 << 30) // SILK_FIX_CONST( 1, 30 )
)

// mul32FracQ is MUL32_FRAC_Q.
func mul32FracQ(a32, b32 int32, q int) int32 {
	return int32(rshiftRound64(smull(a32, b32), q))
}

// lpcInversePredGainQA is LPC_inverse_pred_gain_QA_c: compute inverse of
// LPC prediction gain, and test if LPC coefficients are stable (all
// poles within unit circle). It returns the inverse prediction gain in
// energy domain, Q30.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func lpcInversePredGainQA(aQA *[silkMaxOrderLPC]int32, order int) int32 {
	var k int
	invGainQ30 := lpcInvOneQ30
	for k = order - 1; k > 0; k-- {
		// Check for stability.
		if aQA[k] > lpcInvAlimit || aQA[k] < -lpcInvAlimit {
			return 0
		}

		// Set RC equal to negated AR coef.
		rcQ31 := -lshift(aQA[k], 31-lpcInvPredGainQA)

		// rcMult1Q30 range: [ 1 : 2^30 ].
		rcMult1Q30 := lpcInvOneQ30 - smmul(rcQ31, rcQ31)

		// Update inverse gain.
		// invGainQ30 range: [ 0 : 2^30 ].
		invGainQ30 = lshift(smmul(invGainQ30, rcMult1Q30), 2)
		if invGainQ30 < lpcInvMinGainQ30 {
			return 0
		}

		// rcMult2 range: [ 2^30 : silk_int32_MAX ].
		mult2Q := int(32 - clz32(abs32(rcMult1Q30)))
		rcMult2 := inverse32VarQ(rcMult1Q30, mult2Q+30)

		// Update AR coefficient.
		for n := 0; n < (k+1)>>1; n++ {
			tmp1 := aQA[n]
			tmp2 := aQA[k-n-1]
			tmp64 := rshiftRound64(
				smull(subSat32(tmp1, mul32FracQ(tmp2, rcQ31, 31)), rcMult2),
				mult2Q,
			)
			if tmp64 > int64(int32MAX) || tmp64 < int64(int32MIN) {
				return 0
			}
			aQA[n] = int32(tmp64)
			tmp64 = rshiftRound64(
				smull(subSat32(tmp2, mul32FracQ(tmp1, rcQ31, 31)), rcMult2),
				mult2Q,
			)
			if tmp64 > int64(int32MAX) || tmp64 < int64(int32MIN) {
				return 0
			}
			aQA[k-n-1] = int32(tmp64)
		}
	}

	// Check for stability.
	if aQA[k] > lpcInvAlimit || aQA[k] < -lpcInvAlimit {
		return 0
	}

	// Set RC equal to negated AR coef.
	rcQ31 := -lshift(aQA[0], 31-lpcInvPredGainQA)

	// Range: [ 1 : 2^30 ].
	rcMult1Q30 := lpcInvOneQ30 - smmul(rcQ31, rcQ31)

	// Update inverse gain.
	// Range: [ 0 : 2^30 ].
	invGainQ30 = lshift(smmul(invGainQ30, rcMult1Q30), 2)
	if invGainQ30 < lpcInvMinGainQ30 {
		return 0
	}

	return invGainQ30
}

// lpcInversePredGain is silk_LPC_inverse_pred_gain_c, for input in Q12
// domain. It returns the inverse prediction gain in energy domain, Q30.
func lpcInversePredGain(aQ12 []int16, order int) int32 {
	var atmpQA [silkMaxOrderLPC]int32
	dcResp := int32(0)

	// Increase Q domain of the AR coefficients.
	for k := 0; k < order; k++ {
		dcResp += int32(aQ12[k])
		atmpQA[k] = lshift(int32(aQ12[k]), lpcInvPredGainQA-12)
	}
	// If the DC is unstable, we don't even need to do the full
	// calculations.
	if dcResp >= 4096 {
		return 0
	}
	return lpcInversePredGainQA(&atmpQA, order)
}

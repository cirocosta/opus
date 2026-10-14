// Ported from libopus 1.6.1 silk/NLSF_stabilize.c.

package silk

// NLSF stabilizer:
//
//   - Moves NLSFs further apart if they are too close
//   - Moves NLSFs away from borders if they are too close
//   - High effort to achieve a modification with minimum
//     Euclidean distance to input vector
//   - Output are sorted NLSF coefficients

const nlsfStabilizeMaxLoops = 20 // MAX_LOOPS

// nlsfStabilize is silk_NLSF_stabilize, for a single input data vector.
// nDeltaMinQ15 has L+1 entries, and nDeltaMinQ15[L] must be >= 1.
func nlsfStabilize(nlsfQ15 []int16, nDeltaMinQ15 []int16, l int) {
	var loops int
	for loops = 0; loops < nlsfStabilizeMaxLoops; loops++ {
		// Find smallest distance.
		// First element.
		minDiffQ15 := int32(nlsfQ15[0]) - int32(nDeltaMinQ15[0])
		idx := 0
		// Middle elements.
		for i := 1; i <= l-1; i++ {
			diffQ15 := int32(nlsfQ15[i]) - (int32(nlsfQ15[i-1]) + int32(nDeltaMinQ15[i]))
			if diffQ15 < minDiffQ15 {
				minDiffQ15 = diffQ15
				idx = i
			}
		}
		// Last element.
		diffQ15 := int32(1<<15) - (int32(nlsfQ15[l-1]) + int32(nDeltaMinQ15[l]))
		if diffQ15 < minDiffQ15 {
			minDiffQ15 = diffQ15
			idx = l
		}

		// Now check if the smallest distance non-negative.
		if minDiffQ15 >= 0 {
			return
		}

		if idx == 0 {
			// Move away from lower limit.
			nlsfQ15[0] = nDeltaMinQ15[0]
		} else if idx == l {
			// Move away from higher limit.
			nlsfQ15[l-1] = int16(int32(1<<15) - int32(nDeltaMinQ15[l]))
		} else {
			// Find the lower extreme for the location of the current
			// center frequency.
			minCenterQ15 := int32(0)
			for k := 0; k < idx; k++ {
				minCenterQ15 += int32(nDeltaMinQ15[k])
			}
			minCenterQ15 += int32(nDeltaMinQ15[idx]) >> 1

			// Find the upper extreme for the location of the current
			// center frequency.
			maxCenterQ15 := int32(1 << 15)
			for k := l; k > idx; k-- {
				maxCenterQ15 -= int32(nDeltaMinQ15[k])
			}
			maxCenterQ15 -= int32(nDeltaMinQ15[idx]) >> 1

			// Move apart, sorted by value, keeping the same center
			// frequency.
			centerFreqQ15 := int16(limit32(rshiftRound(int32(nlsfQ15[idx-1])+int32(nlsfQ15[idx]), 1),
				minCenterQ15, maxCenterQ15))
			nlsfQ15[idx-1] = int16(int32(centerFreqQ15) - int32(nDeltaMinQ15[idx])>>1)
			nlsfQ15[idx] = int16(int32(nlsfQ15[idx-1]) + int32(nDeltaMinQ15[idx]))
		}
	}

	// Safe and simple fall back method, which is less ideal than the above.
	if loops == nlsfStabilizeMaxLoops {
		// Insertion sort (fast for already almost sorted arrays).
		insertionSortIncreasingAllValuesInt16(nlsfQ15, l)

		// First NLSF should be no less than NDeltaMin[0].
		nlsfQ15[0] = int16(maxInt(int(nlsfQ15[0]), int(nDeltaMinQ15[0])))

		// Keep delta_min distance between the NLSFs.
		for i := 1; i < l; i++ {
			nlsfQ15[i] = int16(maxInt(int(nlsfQ15[i]), int(addSat16(nlsfQ15[i-1], nDeltaMinQ15[i]))))
		}

		// Last NLSF should be no higher than 1 - NDeltaMin[L].
		nlsfQ15[l-1] = int16(minInt(int(nlsfQ15[l-1]), (1<<15)-int(nDeltaMinQ15[l])))

		// Keep NDeltaMin distance between the NLSFs.
		for i := l - 2; i >= 0; i-- {
			nlsfQ15[i] = int16(minInt(int(nlsfQ15[i]), int(nlsfQ15[i+1])-int(nDeltaMinQ15[i+1])))
		}
	}
}

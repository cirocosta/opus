// Ported from libopus 1.6.1 silk/A2NLSF.c.

package silk

// Conversion between prediction filter coefficients and NLSFs. Requires
// the order to be an even number. A piecewise linear approximation maps
// LSF <-> cos(LSF), so the result is not accurate NLSFs, but the two
// functions are accurate inverses of each other.

const (
	// Number of binary divisions, when not in low complexity mode.
	binDivStepsA2NLSFFIX   = 3 // must be no higher than 16 - log2( LSF_COS_TAB_SZ_FIX )
	maxIterationsA2NLSFFIX = 16
)

// a2nlsfTransPoly is silk_A2NLSF_trans_poly: transforms polynomials
// from cos(n*f) to cos(f)^n.
func a2nlsfTransPoly(p []int32, dd int) {
	for k := 2; k <= dd; k++ {
		for n := dd; n > k; n-- {
			p[n-2] -= p[n]
		}
		p[k-2] -= lshift(p[k], 1)
	}
}

// a2nlsfEvalPoly is silk_A2NLSF_eval_poly: polynomial evaluation. It
// returns the polynomial evaluation, in Q16.
func a2nlsfEvalPoly(p []int32, x int32, dd int) int32 {
	y32 := p[dd] // Q16
	xQ16 := lshift(x, 4)

	if dd == 8 {
		y32 = smlaww(p[7], y32, xQ16)
		y32 = smlaww(p[6], y32, xQ16)
		y32 = smlaww(p[5], y32, xQ16)
		y32 = smlaww(p[4], y32, xQ16)
		y32 = smlaww(p[3], y32, xQ16)
		y32 = smlaww(p[2], y32, xQ16)
		y32 = smlaww(p[1], y32, xQ16)
		y32 = smlaww(p[0], y32, xQ16)
	} else {
		for n := dd - 1; n >= 0; n-- {
			y32 = smlaww(p[n], y32, xQ16) // Q16
		}
	}
	return y32
}

// a2nlsfInit is silk_A2NLSF_init.
func a2nlsfInit(aQ16 []int32, p, q []int32, dd int) {
	// Convert filter coefs to even and odd polynomials.
	p[dd] = lshift(1, 16)
	q[dd] = lshift(1, 16)
	for k := 0; k < dd; k++ {
		p[k] = -aQ16[dd-k-1] - aQ16[dd+k] // Q16
		q[k] = -aQ16[dd-k-1] + aQ16[dd+k] // Q16
	}

	// Divide out zeros as we have that for even filter orders, z = 1 is
	// always a root in Q, and z = -1 is always a root in P.
	for k := dd; k > 0; k-- {
		p[k-1] -= p[k]
		q[k-1] += q[k]
	}

	// Transform polynomials from cos(n*f) to cos(f)^n.
	a2nlsfTransPoly(p, dd)
	a2nlsfTransPoly(q, dd)
}

// a2nlsf is silk_A2NLSF: compute Normalized Line Spectral Frequencies
// (NLSFs) from whitening filter coefficients. If not all roots are
// found, the a_Q16 coefficients are bandwidth expanded until
// convergence.
func a2nlsf(nlsf []int16, aQ16 []int32, d int) {
	var (
		pPoly [silkMaxOrderLPC/2 + 1]int32
		qPoly [silkMaxOrderLPC/2 + 1]int32
	)
	pq := [2][]int32{pPoly[:], qPoly[:]}

	dd := d >> 1

	a2nlsfInit(aQ16, pPoly[:], qPoly[:], dd)

	// Find roots, alternating between P and Q.
	p := pPoly[:] // Pointer to polynomial

	xlo := int32(lsfCosTabFIXQ12[0]) // Q12
	ylo := a2nlsfEvalPoly(p, xlo, dd)

	var rootIx int
	if ylo < 0 {
		// Set the first NLSF to zero and move on to the next.
		nlsf[0] = 0
		p = qPoly[:] // Pointer to polynomial
		ylo = a2nlsfEvalPoly(p, xlo, dd)
		rootIx = 1 // Index of current root
	} else {
		rootIx = 0 // Index of current root
	}
	k := 1 // Loop counter
	i := 0 // Counter for bandwidth expansions applied
	thr := int32(0)
	for {
		// Evaluate polynomial.
		xhi := int32(lsfCosTabFIXQ12[k]) // Q12
		yhi := a2nlsfEvalPoly(p, xhi, dd)

		// Detect zero crossing.
		if (ylo <= 0 && yhi >= thr) || (ylo >= 0 && yhi <= -thr) {
			if yhi == 0 {
				// If the root lies exactly at the end of the current
				// interval, look for the next root in the next interval.
				thr = 1
			} else {
				thr = 0
			}
			// Binary division.
			ffrac := int32(-256)
			for m := 0; m < binDivStepsA2NLSFFIX; m++ {
				// Evaluate polynomial.
				xmid := rshiftRound(xlo+xhi, 1)
				ymid := a2nlsfEvalPoly(p, xmid, dd)

				// Detect zero crossing.
				if (ylo <= 0 && ymid >= 0) || (ylo >= 0 && ymid <= 0) {
					// Reduce frequency.
					xhi = xmid
					yhi = ymid
				} else {
					// Increase frequency.
					xlo = xmid
					ylo = ymid
					ffrac = addRshift32(ffrac, 128, m)
				}
			}

			// Interpolate.
			if abs32(ylo) < 65536 {
				// Avoid dividing by zero.
				den := ylo - yhi
				nom := lshift(ylo, 8-binDivStepsA2NLSFFIX) + (den >> 1)
				if den != 0 {
					ffrac += div32(nom, den)
				}
			} else {
				// No risk of dividing by zero because abs(ylo - yhi) >=
				// abs(ylo) >= 65536.
				ffrac += div32(ylo, (ylo-yhi)>>(8-binDivStepsA2NLSFFIX))
			}
			nlsf[rootIx] = int16(min32(lshift(int32(k), 8)+ffrac, int16MAX))

			rootIx++ // Next root
			if rootIx >= d {
				// Found all roots.
				break
			}
			// Alternate pointer to polynomial.
			p = pq[rootIx&1]

			// Evaluate polynomial.
			xlo = int32(lsfCosTabFIXQ12[k-1]) // Q12
			ylo = lshift(int32(1-(rootIx&2)), 12)
		} else {
			// Increment loop counter.
			k++
			xlo = xhi
			ylo = yhi
			thr = 0

			if k > lsfCosTabSzFIX {
				i++
				if i > maxIterationsA2NLSFFIX {
					// Set NLSFs to white spectrum and exit.
					nlsf[0] = int16(div32_16(1<<15, int32(d+1)))
					for k = 1; k < d; k++ {
						nlsf[k] = nlsf[k-1] + nlsf[0]
					}
					return
				}

				// Error: Apply progressively more bandwidth expansion and
				// run again.
				bwexpander32(aQ16, d, 65536-lshift(1, i))

				a2nlsfInit(aQ16, pPoly[:], qPoly[:], dd)
				p = pPoly[:]                    // Pointer to polynomial
				xlo = int32(lsfCosTabFIXQ12[0]) // Q12
				ylo = a2nlsfEvalPoly(p, xlo, dd)
				if ylo < 0 {
					// Set the first NLSF to zero and move on to the next.
					nlsf[0] = 0
					p = qPoly[:] // Pointer to polynomial
					ylo = a2nlsfEvalPoly(p, xlo, dd)
					rootIx = 1 // Index of current root
				} else {
					rootIx = 0 // Index of current root
				}
				k = 1 // Reset loop counter
			}
		}
	}
}

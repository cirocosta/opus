// Ported from libopus 1.6.1 celt/rate.h and celt/rate.c (no CUSTOM_MODES,
// no ENABLE_QEXT, no FUZZING).

package celt

import "github.com/cirocosta/opus/internal/entcode"

var log2FracTable = [24]uint8{
	0,
	8, 13,
	16, 19, 21, 23,
	24, 26, 27, 28, 29, 30, 31, 32,
	32, 33, 34, 34, 35, 36, 36, 37, 37,
}

const allocSteps = 6

// getPulses is get_pulses.
func getPulses(i int) int {
	if i < 8 {
		return i
	}
	return (8 + (i & 7)) << uint((i>>3)-1)
}

// bits2pulses is bits2pulses.
func bits2pulses(m *Mode, band, LM, bits int) int {
	LM++
	cache := m.Cache.Bits[m.Cache.Index[LM*m.NbEBands+band]:]
	lo := 0
	hi := int(cache[0])
	bits--
	for i := 0; i < logMaxPseudo; i++ {
		mid := (lo + hi + 1) >> 1
		if int(cache[mid]) >= bits {
			hi = mid
		} else {
			lo = mid
		}
	}
	loBits := -1
	if lo != 0 {
		loBits = int(cache[lo])
	}
	if bits-loBits <= int(cache[hi])-bits {
		return lo
	}
	return hi
}

// pulses2bits is pulses2bits.
func pulses2bits(m *Mode, band, LM, pulses int) int {
	LM++
	cache := m.Cache.Bits[m.Cache.Index[LM*m.NbEBands+band]:]
	if pulses == 0 {
		return 0
	}
	return int(cache[pulses]) + 1
}

// rangeCoder is the ec_ctx argument of the functions that both encode and
// decode. Exactly one of enc and dec is set.
type rangeCoder struct {
	enc *entcode.Encoder
	dec *entcode.Decoder
}

func (rc rangeCoder) tellFrac() int {
	if rc.enc != nil {
		return int(rc.enc.TellFrac())
	}
	return int(rc.dec.TellFrac())
}

// interpBits2pulses is interp_bits2pulses.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func interpBits2pulses(
	m *Mode,
	start, end, skipStart int,
	bits1, bits2, thresh, caps []int,
	total int32,
	outBalance *int32,
	skipRsv int,
	intensity *int,
	intensityRsv int,
	dualStereo *int,
	dualStereoRsv int,
	bits, ebits, finePriority []int,
	C, LM int,
	ec rangeCoder,
	encode bool,
	prev, signalBandwidth int,
) int {
	var psum int32
	allocFloor := C << bitRes
	stereo := 0
	if C > 1 {
		stereo = 1
	}
	logM := LM << bitRes
	lo := 0
	hi := 1 << allocSteps
	for i := 0; i < allocSteps; i++ {
		mid := (lo + hi) >> 1
		psum = 0
		done := false
		for j := end - 1; j >= start; j-- {
			tmp := bits1[j] + int(int32(mid)*int32(bits2[j])>>allocSteps)
			if tmp >= thresh[j] || done {
				done = true
				// Don't allocate more than we can actually use.
				psum += int32(min(tmp, caps[j]))
			} else if tmp >= allocFloor {
				psum += int32(allocFloor)
			}
		}
		if psum > total {
			hi = mid
		} else {
			lo = mid
		}
	}
	psum = 0
	done := false
	for j := end - 1; j >= start; j-- {
		tmp := bits1[j] + int(int32(lo)*int32(bits2[j])>>allocSteps)
		if tmp < thresh[j] && !done {
			if tmp >= allocFloor {
				tmp = allocFloor
			} else {
				tmp = 0
			}
		} else {
			done = true
		}
		// Don't allocate more than we can actually use.
		tmp = min(tmp, caps[j])
		bits[j] = tmp
		psum += int32(tmp)
	}

	// Decide which bands to skip, working backwards from the end.
	codedBands := end
	for ; ; codedBands-- {
		j := codedBands - 1
		// Never skip the first band, nor a band that has been boosted by
		// dynalloc. In the first case, we'd be coding a bit to signal
		// we're going to waste all the other bits. In the second case,
		// we'd be coding a bit to redistribute all the bits we just
		// signaled should be concentrated in this band.
		if j <= skipStart {
			// Give the bit we reserved to end skipping back.
			total += int32(skipRsv)
			break
		}
		// Figure out how many left-over bits we would be adding to this
		// band. This can include bits we've stolen back from higher,
		// skipped bands.
		left := total - psum
		percoeff := int32(entcode.Udiv(uint32(left), uint32(m.EBands[codedBands]-m.EBands[start])))
		left -= int32(m.EBands[codedBands]-m.EBands[start]) * percoeff
		rem := max(int(left)-int(m.EBands[j]-m.EBands[start]), 0)
		bandWidth := int(m.EBands[codedBands] - m.EBands[j])
		bandBits := int(int32(bits[j]) + percoeff*int32(bandWidth) + int32(rem))
		// Only code a skip decision if we're above the threshold for this
		// band. Otherwise it is force-skipped. This ensures that we have
		// enough bits to code the skip flag.
		if bandBits >= max(thresh[j], allocFloor+(1<<bitRes)) {
			if encode {
				// This if() block is the only part of the allocation
				// function that is not a mandatory part of the bitstream:
				// any bands we choose to skip here must be explicitly
				// signaled.
				depthThreshold := 0
				// We choose a threshold with some hysteresis to keep bands
				// from fluctuating in and out, but we try not to fold
				// below a certain point.
				if codedBands > 17 {
					if j < prev {
						depthThreshold = 7
					} else {
						depthThreshold = 9
					}
				}
				if codedBands <= start+2 ||
					(bandBits > (depthThreshold*bandWidth<<uint(LM)<<bitRes)>>4 && j <= signalBandwidth) {
					ec.enc.EncBitLogp(1, 1)
					break
				}
				ec.enc.EncBitLogp(0, 1)
			} else if ec.dec.DecBitLogp(1) != 0 {
				break
			}
			// We used a bit to skip this band.
			psum += 1 << bitRes
			bandBits -= 1 << bitRes
		}
		// Reclaim the bits originally allocated to this band.
		psum -= int32(bits[j] + intensityRsv)
		if intensityRsv > 0 {
			intensityRsv = int(log2FracTable[j-start])
		}
		psum += int32(intensityRsv)
		if bandBits >= allocFloor {
			// If we have enough for a fine energy bit per channel, use it.
			psum += int32(allocFloor)
			bits[j] = allocFloor
		} else {
			// Otherwise this band gets nothing at all.
			bits[j] = 0
		}
	}

	// Code the intensity and dual stereo parameters.
	if intensityRsv > 0 {
		if encode {
			*intensity = min(*intensity, codedBands)
			ec.enc.EncUint(uint32(*intensity-start), uint32(codedBands+1-start))
		} else {
			*intensity = start + int(ec.dec.DecUint(uint32(codedBands+1-start)))
		}
	} else {
		*intensity = 0
	}
	if *intensity <= start {
		total += int32(dualStereoRsv)
		dualStereoRsv = 0
	}
	if dualStereoRsv > 0 {
		if encode {
			ec.enc.EncBitLogp(*dualStereo, 1)
		} else {
			*dualStereo = ec.dec.DecBitLogp(1)
		}
	} else {
		*dualStereo = 0
	}

	// Allocate the remaining bits.
	left := total - psum
	percoeff := int32(entcode.Udiv(uint32(left), uint32(m.EBands[codedBands]-m.EBands[start])))
	left -= int32(m.EBands[codedBands]-m.EBands[start]) * percoeff
	for j := start; j < codedBands; j++ {
		bits[j] += int(percoeff) * int(m.EBands[j+1]-m.EBands[j])
	}
	for j := start; j < codedBands; j++ {
		tmp := int(min(left, int32(m.EBands[j+1]-m.EBands[j])))
		bits[j] += tmp
		left -= int32(tmp)
	}

	var balance int32
	j := start
	for ; j < codedBands; j++ {
		N0 := int(m.EBands[j+1] - m.EBands[j])
		N := N0 << uint(LM)
		bit := int32(bits[j]) + balance
		var excess int32
		if N > 1 {
			excess = max(bit-int32(caps[j]), 0)
			bits[j] = int(bit - excess)

			// Compensate for the extra DoF in stereo.
			den := C * N
			if C == 2 && N > 2 && *dualStereo == 0 && j < *intensity {
				den++
			}

			NClogN := den * (int(m.LogN[j]) + logM)

			// Offset for the number of fine bits by log2(N)/2 +
			// FINE_OFFSET compared to their "fair share" of total/N.
			offset := (NClogN >> 1) - den*fineOffset

			// N=2 is the only point that doesn't match the curve.
			if N == 2 {
				offset += den << bitRes >> 2
			}

			// Changing the offset for allocating the second and third fine
			// energy bit.
			if bits[j]+offset < den*2<<bitRes {
				offset += NClogN >> 2
			} else if bits[j]+offset < den*3<<bitRes {
				offset += NClogN >> 3
			}

			// Divide with rounding.
			ebits[j] = max(0, bits[j]+offset+(den<<(bitRes-1)))
			ebits[j] = int(entcode.Udiv(uint32(ebits[j]), uint32(den))) >> bitRes

			// Make sure not to bust.
			if C*ebits[j] > (bits[j] >> bitRes) {
				ebits[j] = bits[j] >> uint(stereo) >> bitRes
			}

			// More than that is useless because that's about as far as PVQ
			// can go.
			ebits[j] = min(ebits[j], maxFineBits)

			// If we rounded down or capped this band, make it a candidate
			// for the final fine energy pass.
			finePriority[j] = b2i(ebits[j]*(den<<bitRes) >= bits[j]+offset)

			// Remove the allocated fine bits; the rest are assigned to PVQ.
			bits[j] -= C * ebits[j] << bitRes
		} else {
			// For N=1, all bits go to fine energy except for a single sign
			// bit.
			excess = max(0, bit-int32(C<<bitRes))
			bits[j] = int(bit - excess)
			ebits[j] = 0
			finePriority[j] = 1
		}

		// Fine energy can't take advantage of the re-balancing in
		// quant_all_bands(). Instead, do the re-balancing here.
		if excess > 0 {
			extraFine := min(int(excess>>uint(stereo+bitRes)), maxFineBits-ebits[j])
			ebits[j] += extraFine
			extraBits := extraFine * C << bitRes
			finePriority[j] = b2i(int32(extraBits) >= excess-balance)
			excess -= int32(extraBits)
		}
		balance = excess
	}
	// Save any remaining bits over the cap for the rebalancing in
	// quant_all_bands().
	*outBalance = balance

	// The skipped bands use all their bits for fine energy.
	for ; j < end; j++ {
		ebits[j] = bits[j] >> uint(stereo) >> bitRes
		bits[j] = 0
		finePriority[j] = b2i(ebits[j] < 1)
	}
	return codedBands
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// cltComputeAllocation is clt_compute_allocation. ec is the encoder when
// encode is true and the decoder otherwise.
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func cltComputeAllocation(
	m *Mode,
	start, end int,
	offsets, caps []int,
	allocTrim int,
	intensity, dualStereo *int,
	total int32,
	balance *int32,
	pulses, ebits, finePriority []int,
	C, LM int,
	ec rangeCoder,
	encode bool,
	prev, signalBandwidth int,
) int {
	var bits1, bits2, thresh, trimOffset [maxBands]int

	total = max(total, 0)
	length := m.NbEBands
	skipStart := start
	// Reserve a bit to signal the end of manually skipped bands.
	skipRsv := 0
	if total >= 1<<bitRes {
		skipRsv = 1 << bitRes
	}
	total -= int32(skipRsv)
	// Reserve bits for the intensity and dual stereo parameters.
	intensityRsv, dualStereoRsv := 0, 0
	if C == 2 {
		intensityRsv = int(log2FracTable[end-start])
		if int32(intensityRsv) > total {
			intensityRsv = 0
		} else {
			total -= int32(intensityRsv)
			if total >= 1<<bitRes {
				dualStereoRsv = 1 << bitRes
			}
			total -= int32(dualStereoRsv)
		}
	}

	for j := start; j < end; j++ {
		w := int(m.EBands[j+1] - m.EBands[j])
		// Below this threshold, we're sure not to allocate any PVQ bits.
		thresh[j] = max(C<<bitRes, (3*w<<uint(LM)<<bitRes)>>4)
		// Tilt of the allocation curve.
		trimOffset[j] = C * w * (allocTrim - 5 - LM) * (end - j - 1) * (1 << uint(LM+bitRes)) >> 6
		// Giving less resolution to single-coefficient bands because they
		// get more benefit from having one coarse value per coefficient.
		if w<<uint(LM) == 1 {
			trimOffset[j] -= C << bitRes
		}
	}
	lo := 1
	hi := m.NbAllocVectors - 1
	for {
		done := false
		psum := 0
		mid := (lo + hi) >> 1
		for j := end - 1; j >= start; j-- {
			N := int(m.EBands[j+1] - m.EBands[j])
			bitsj := C * N * int(m.AllocVectors[mid*length+j]) << uint(LM) >> 2
			if bitsj > 0 {
				bitsj = max(0, bitsj+trimOffset[j])
			}
			bitsj += offsets[j]
			if bitsj >= thresh[j] || done {
				done = true
				// Don't allocate more than we can actually use.
				psum += min(bitsj, caps[j])
			} else if bitsj >= C<<bitRes {
				psum += C << bitRes
			}
		}
		if int32(psum) > total {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
		if lo > hi {
			break
		}
	}
	hi = lo
	lo--
	for j := start; j < end; j++ {
		N := int(m.EBands[j+1] - m.EBands[j])
		bits1j := C * N * int(m.AllocVectors[lo*length+j]) << uint(LM) >> 2
		var bits2j int
		if hi >= m.NbAllocVectors {
			bits2j = caps[j]
		} else {
			bits2j = C * N * int(m.AllocVectors[hi*length+j]) << uint(LM) >> 2
		}
		if bits1j > 0 {
			bits1j = max(0, bits1j+trimOffset[j])
		}
		if bits2j > 0 {
			bits2j = max(0, bits2j+trimOffset[j])
		}
		if lo > 0 {
			bits1j += offsets[j]
		}
		bits2j += offsets[j]
		if offsets[j] > 0 {
			skipStart = j
		}
		bits2j = max(0, bits2j-bits1j)
		bits1[j] = bits1j
		bits2[j] = bits2j
	}
	return interpBits2pulses(m, start, end, skipStart, bits1[:], bits2[:], thresh[:], caps,
		total, balance, skipRsv, intensity, intensityRsv, dualStereo, dualStereoRsv,
		pulses, ebits, finePriority, C, LM, ec, encode, prev, signalBandwidth)
}

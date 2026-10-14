// Ported from libopus 1.6.1 celt/cwrs.h and celt/cwrs.c (the table-based
// version, without SMALL_FOOTPRINT, CUSTOM_MODES or ENABLE_QEXT).

package celt

import "github.com/cirocosta/opus/internal/entcode"

// celtPVQURow is CELT_PVQ_U_ROW: row i starts at U(i, i).
var celtPVQURow [15][]uint32

func init() {
	for i, off := range celtPVQURowOffset {
		celtPVQURow[i] = celtPVQUData[off:]
	}
}

// pvqU is CELT_PVQ_U(n, k) = U(n, k) = U(k, n).
func pvqU(n, k int) uint32 {
	return celtPVQURow[min(n, k)][max(n, k)]
}

// pvqV is CELT_PVQ_V(n, k) = U(n, k) + U(n, k+1): the number of PVQ
// codewords for a band of size n with k pulses.
func pvqV(n, k int) uint32 {
	return pvqU(n, k) + pvqU(n, k+1)
}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// icwrs is icwrs: the index of the pulse vector y (n >= 2).
func icwrs(n int, y []int) uint32 {
	j := n - 1
	var i uint32
	if y[j] < 0 {
		i = 1
	}
	k := iabs(y[j])
	for {
		j--
		i += pvqU(n-j, k)
		k += iabs(y[j])
		if y[j] < 0 {
			i += pvqU(n-j, k+1)
		}
		if j <= 0 {
			break
		}
	}
	return i
}

// encodePulses is encode_pulses.
func encodePulses(y []int, n, k int, enc *entcode.Encoder) {
	enc.EncUint(icwrs(n, y), pvqV(n, k))
}

// cwrsi is cwrsi: it decodes index i into the pulse vector y and returns
// the squared norm of y (as opus_val32, a float in this build).
// preserve coupled reference band, pulse and rounding decisions
//
//revive:disable-next-line:cognitive-complexity
func cwrsi(n, k int, i uint32, y []int) float32 {
	var yy float32
	yi := 0
	for n > 2 {
		var p, q uint32
		var s, k0 int
		// Lots of pulses case:
		if k >= n {
			row := celtPVQURow[n]
			// Are the pulses in this dimension negative?
			p = row[k+1]
			s = 0
			if i >= p {
				s = -1
			}
			i -= p & uint32(s)
			// Count how many pulses were placed in this dimension.
			k0 = k
			q = row[n]
			if q > i {
				k = n
				for {
					k--
					p = celtPVQURow[k][n]
					if p <= i {
						break
					}
				}
			} else {
				for p = row[k]; p > i; p = row[k] {
					k--
				}
			}
			i -= p
			val := int(int16((k0 - k + s) ^ s))
			y[yi] = val
			yi++
			yy = yy + float32(float32(val)*float32(val))
		} else {
			// Lots of dimensions case:
			// Are there any pulses in this dimension at all?
			p = celtPVQURow[k][n]
			q = celtPVQURow[k+1][n]
			if p <= i && i < q {
				i -= p
				y[yi] = 0
				yi++
			} else {
				// Are the pulses in this dimension negative?
				s = 0
				if i >= q {
					s = -1
				}
				i -= q & uint32(s)
				// Count how many pulses were placed in this dimension.
				k0 = k
				for {
					k--
					p = celtPVQURow[k][n]
					if p <= i {
						break
					}
				}
				i -= p
				val := int(int16((k0 - k + s) ^ s))
				y[yi] = val
				yi++
				yy = yy + float32(float32(val)*float32(val))
			}
		}
		n--
	}
	// n == 2
	p := uint32(2*k + 1)
	s := 0
	if i >= p {
		s = -1
	}
	i -= p & uint32(s)
	k0 := k
	k = int((i + 1) >> 1)
	if k != 0 {
		i -= uint32(2*k - 1)
	}
	val := int(int16((k0 - k + s) ^ s))
	y[yi] = val
	yi++
	yy = yy + float32(float32(val)*float32(val))
	// n == 1
	s = -int(i)
	val = int(int16((k + s) ^ s))
	y[yi] = val
	yy = yy + float32(float32(val)*float32(val))
	return yy
}

// decodePulses is decode_pulses.
func decodePulses(y []int, n, k int, dec *entcode.Decoder) float32 {
	return cwrsi(n, k, dec.DecUint(pvqV(n, k)), y)
}

// Ported from libopus 1.6.1 celt/laplace.h and celt/laplace.c.

package entcode

const (
	// laplaceLogMinp is the log2 of the minimum probability of an energy
	// delta, out of 32768 (LAPLACE_LOG_MINP).
	laplaceLogMinp = 0
	// laplaceMinp is the minimum probability of an energy delta, out of
	// 32768 (LAPLACE_MINP).
	laplaceMinp = 1 << laplaceLogMinp
	// laplaceNmin is the minimum number of guaranteed representable
	// energy deltas, in one direction (LAPLACE_NMIN).
	laplaceNmin = 16
)

// laplaceGetFreq1 ports ec_laplace_get_freq1. decay is positive and at
// most 11456.
func laplaceGetFreq1(fs0 uint32, decay int) uint32 {
	ft := 32768 - laplaceMinp*(2*laplaceNmin) - fs0
	return ft * uint32(int32(16384-decay)) >> 15
}

// LaplaceEncode encodes a value that is assumed to be the realization of
// a Laplace-distributed random process (ec_laplace_encode). fs is the
// probability of 0, multiplied by 32768, and decay is the probability of
// a value of +/-1, multiplied by 16384. It returns the value actually
// coded, which can be smaller in magnitude than value when value is out
// of range. The C version returns it through the value pointer.
// preserve reference entropy intervals, tail clipping and finalization order
//
//revive:disable-next-line:cognitive-complexity
func (e *Encoder) LaplaceEncode(value int, fs uint32, decay int) int {
	var fl uint32
	val := value
	if val != 0 {
		s := 0
		if val < 0 {
			s = -1
		}
		val = (val + s) ^ s
		fl = fs
		fs = laplaceGetFreq1(fs, decay)
		// Search the decaying part of the PDF.
		i := 1
		for ; fs > 0 && i < val; i++ {
			fs *= 2
			fl += fs + 2*laplaceMinp
			fs = (fs * uint32(int32(decay))) >> 15
		}
		// Everything beyond that has probability laplaceMinp.
		if fs == 0 {
			ndiMax := int(int32((32768 - fl + laplaceMinp - 1) >> laplaceLogMinp))
			ndiMax = (ndiMax - s) >> 1
			di := min(val-i, ndiMax-1)
			fl += uint32((2*di + 1 + s) * laplaceMinp)
			if laplaceMinp < 32768-fl {
				fs = laplaceMinp
			} else {
				fs = 32768 - fl
			}
			value = (i + di + s) ^ s
		} else {
			fs += laplaceMinp
			fl += fs & uint32(^s)
		}
	}
	e.EncodeBin(fl, fl+fs, 15)
	return value
}

// LaplaceDecode decodes a value coded with LaplaceEncode
// (ec_laplace_decode).
func (d *Decoder) LaplaceDecode(fs uint32, decay int) int {
	val := 0
	fm := d.DecodeBin(15)
	var fl uint32
	if fm >= fs {
		val++
		fl = fs
		fs = laplaceGetFreq1(fs, decay) + laplaceMinp
		// Search the decaying part of the PDF.
		for fs > laplaceMinp && fm >= fl+2*fs {
			fs *= 2
			fl += fs
			fs = ((fs - 2*laplaceMinp) * uint32(int32(decay))) >> 15
			fs += laplaceMinp
			val++
		}
		// Everything beyond that has probability laplaceMinp.
		if fs <= laplaceMinp {
			di := int((fm - fl) >> (laplaceLogMinp + 1))
			val += di
			fl += uint32(2 * di * laplaceMinp)
		}
		if fm < fl+fs {
			val = -val
		} else {
			fl += fs
		}
	}
	d.Update(fl, min(fl+fs, 32768), 32768)
	return val
}

// laplaceP0Icdf builds the magnitude table shared by LaplaceEncodeP0 and
// LaplaceDecodeP0.
func laplaceP0Icdf(icdf *[8]uint16, decay uint16) {
	icdf[0] = uint16(max(7, int(decay)))
	for i := 1; i < 7; i++ {
		icdf[i] = uint16(max(7-i, int((int32(icdf[i-1])*int32(decay))>>15)))
	}
	icdf[7] = 0
}

// LaplaceEncodeP0 encodes value with an explicit probability of zero p0
// and a geometric decay, both out of 32768 (ec_laplace_encode_p0).
func (e *Encoder) LaplaceEncodeP0(value int, p0, decay uint16) {
	var signIcdf [3]uint16
	signIcdf[0] = 32768 - p0
	signIcdf[1] = signIcdf[0] / 2
	signIcdf[2] = 0
	s := 0
	if value > 0 {
		s = 1
	} else if value < 0 {
		s = 2
	}
	e.EncIcdf16(s, signIcdf[:], 15)
	if value < 0 {
		value = -value
	}
	if value != 0 {
		var icdf [8]uint16
		laplaceP0Icdf(&icdf, decay)
		value--
		for {
			e.EncIcdf16(min(value, 7), icdf[:], 15)
			value -= 7
			if value < 0 {
				break
			}
		}
	}
}

// LaplaceDecodeP0 decodes a value coded with LaplaceEncodeP0
// (ec_laplace_decode_p0).
func (d *Decoder) LaplaceDecodeP0(p0, decay uint16) int {
	var signIcdf [3]uint16
	signIcdf[0] = 32768 - p0
	signIcdf[1] = signIcdf[0] / 2
	signIcdf[2] = 0
	s := d.DecIcdf16(signIcdf[:], 15)
	if s == 2 {
		s = -1
	}
	if s == 0 {
		return 0
	}
	var icdf [8]uint16
	laplaceP0Icdf(&icdf, decay)
	value := 1
	for {
		v := d.DecIcdf16(icdf[:], 15)
		value += v
		if v != 7 {
			break
		}
	}
	return s * value
}

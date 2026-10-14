// Ported from libopus 1.6.1 celt/entdec.h and celt/entdec.c.

package entcode

// Decoder is a range decoder (ec_dec). It is based on Martin (1979),
// which rediscovers the FIFO arithmetic code of Pasco (1976). The end of
// the stream is handled by writing out the smallest number of bits that
// ensures that the stream decodes correctly regardless of the value of
// any following bits. Tell reports how many bits were needed to decode
// all the symbols so far; other data can be packed in the rest of the
// buffer.
type Decoder struct {
	Ctx
}

// readByte ports ec_read_byte.
func (d *Decoder) readByte() int {
	if d.Offs < d.Storage {
		b := d.Buf[d.Offs]
		d.Offs++
		return int(b)
	}
	return 0
}

// readByteFromEnd ports ec_read_byte_from_end.
func (d *Decoder) readByteFromEnd() int {
	if d.EndOffs < d.Storage {
		d.EndOffs++
		return int(d.Buf[d.Storage-d.EndOffs])
	}
	return 0
}

// normalize normalizes the contents of Val and Rng so that Rng lies
// entirely in the high-order symbol (ec_dec_normalize).
func (d *Decoder) normalize() {
	// If the range is too small, rescale it and input some bits.
	for d.Rng <= codeBot {
		d.NbitsTotal += SymBits
		d.Rng <<= SymBits
		// Use up the remaining bits from our last symbol.
		sym := d.Rem
		// Read the next value from the input.
		d.Rem = d.readByte()
		// Take the rest of the bits we need from this new symbol.
		sym = (sym<<SymBits | d.Rem) >> (SymBits - codeExtra)
		// And subtract them from val, capped to be less than codeTop.
		d.Val = ((d.Val << SymBits) + (symMax &^ uint32(sym))) & (codeTop - 1)
	}
}

// Init initializes the decoder to read from buf (ec_dec_init). Storage
// is set to len(buf).
func (d *Decoder) Init(buf []byte) {
	d.Buf = buf
	d.Storage = uint32(len(buf))
	d.EndOffs = 0
	d.EndWindow = 0
	d.NendBits = 0
	// This is the offset from which Tell will subtract partial bits. The
	// final value after the normalize call will be the same as in the
	// encoder, but we have to compensate for the bits that are added
	// there.
	d.NbitsTotal = CodeBits + 1 -
		((CodeBits-codeExtra)/SymBits)*SymBits
	d.Offs = 0
	d.Rng = 1 << codeExtra
	d.Rem = d.readByte()
	d.Val = d.Rng - 1 - uint32(d.Rem>>(SymBits-codeExtra))
	d.Error = 0
	// Normalize the interval.
	d.normalize()
}

// Decode calculates the cumulative frequency for the next symbol
// (ec_decode). The caller must then call Update with the frequency
// range of the symbol that contains the returned value.
func (d *Decoder) Decode(ft uint32) uint32 {
	d.Ext = Udiv(d.Rng, ft)
	s := d.Val / d.Ext
	return ft - min(s+1, ft)
}

// DecodeBin is Decode with ft equal to 1<<bits (ec_decode_bin).
func (d *Decoder) DecodeBin(bits uint) uint32 {
	d.Ext = d.Rng >> bits
	s := d.Val / d.Ext
	return (1 << bits) - min(s+1, 1<<bits)
}

// Update advances the decoder past the next symbol, which occupies
// [fl, fh) out of a total of ft (ec_dec_update).
func (d *Decoder) Update(fl, fh, ft uint32) {
	s := d.Ext * (ft - fh)
	d.Val -= s
	if fl > 0 {
		d.Rng = d.Ext * (fh - fl)
	} else {
		d.Rng -= s
	}
	d.normalize()
}

// DecBitLogp decodes a bit that has a 1/(1<<logp) probability of being
// one (ec_dec_bit_logp).
func (d *Decoder) DecBitLogp(logp uint) int {
	r := d.Rng
	v := d.Val
	s := r >> logp
	ret := 0
	if v < s {
		ret = 1
		d.Rng = s
	} else {
		d.Val = v - s
		d.Rng = r - s
	}
	d.normalize()
	return ret
}

// DecIcdf decodes a symbol given an "inverse" CDF table with 8-bit
// entries (ec_dec_icdf). See Encoder.EncIcdf.
func (d *Decoder) DecIcdf(icdf []uint8, ftb uint) int {
	s := d.Rng
	v := d.Val
	r := s >> ftb
	ret := -1
	var t uint32
	for {
		t = s
		ret++
		s = r * uint32(icdf[ret])
		if v >= s {
			break
		}
	}
	d.Val = v - s
	d.Rng = t - s
	d.normalize()
	return ret
}

// DecIcdf16 is DecIcdf with 16-bit table entries (ec_dec_icdf16).
func (d *Decoder) DecIcdf16(icdf []uint16, ftb uint) int {
	s := d.Rng
	v := d.Val
	r := s >> ftb
	ret := -1
	var t uint32
	for {
		t = s
		ret++
		s = r * uint32(icdf[ret])
		if v >= s {
			break
		}
	}
	d.Val = v - s
	d.Rng = t - s
	d.normalize()
	return ret
}

// DecUint decodes a raw unsigned integer in the range [0, ft)
// (ec_dec_uint). ft must be at least 2. If the decoded value is out of
// range, it sets Error to 1 and returns ft-1.
func (d *Decoder) DecUint(ft uint32) uint32 {
	ft--
	ftb := Ilog(ft)
	if ftb > UintBits {
		ftb -= UintBits
		f := (ft >> uint(ftb)) + 1
		s := d.Decode(f)
		d.Update(s, s+1, f)
		t := s<<uint(ftb) | d.DecBits(uint(ftb))
		if t <= ft {
			return t
		}
		d.Error = 1
		return ft
	}
	ft++
	s := d.Decode(ft)
	d.Update(s, s+1, ft)
	return s
}

// DecBits extracts a sequence of raw bits from the end of the stream
// (ec_dec_bits). bits must be between 1 and 25.
func (d *Decoder) DecBits(bits uint) uint32 {
	window := d.EndWindow
	available := d.NendBits
	if uint(available) < bits {
		for {
			window |= uint32(d.readByteFromEnd()) << uint(available)
			available += SymBits
			if available > WindowSize-SymBits {
				break
			}
		}
	}
	ret := window & ((uint32(1) << bits) - 1)
	window >>= bits
	available -= int(bits)
	d.EndWindow = window
	d.NendBits = available
	d.NbitsTotal += int(bits)
	return ret
}

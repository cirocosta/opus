// Ported from libopus 1.6.1 celt/entenc.h and celt/entenc.c.

package entcode

// Encoder is a range encoder (ec_enc). See Martin, "Range encoding: an
// algorithm for removing redundancy from a digitised message" (1979),
// and Moffat, Neal, Witten, "Arithmetic Coding Revisited" (1998).
type Encoder struct {
	Ctx
}

// writeByte ports ec_write_byte.
func (e *Encoder) writeByte(value uint32) int {
	if e.Offs+e.EndOffs >= e.Storage {
		return -1
	}
	e.Buf[e.Offs] = byte(value)
	e.Offs++
	return 0
}

// writeByteAtEnd ports ec_write_byte_at_end.
func (e *Encoder) writeByteAtEnd(value uint32) int {
	if e.Offs+e.EndOffs >= e.Storage {
		return -1
	}
	e.EndOffs++
	e.Buf[e.Storage-e.EndOffs] = byte(value)
	return 0
}

// carryOut outputs a symbol, with a carry bit (ec_enc_carry_out). If
// there is a potential to propagate a carry over several symbols, they
// are buffered until it can be determined whether or not an actual carry
// will occur.
func (e *Encoder) carryOut(c int) {
	if c != symMax {
		// No further carry propagation possible, flush buffer.
		carry := c >> SymBits
		// Do not output a byte on the first write.
		if e.Rem >= 0 {
			e.Error |= e.writeByte(uint32(e.Rem + carry))
		}
		if e.Ext > 0 {
			sym := uint32(symMax+carry) & symMax
			for {
				e.Error |= e.writeByte(sym)
				e.Ext--
				if e.Ext == 0 {
					break
				}
			}
		}
		e.Rem = c & symMax
	} else {
		e.Ext++
	}
}

// normalize ports ec_enc_normalize.
func (e *Encoder) normalize() {
	// If the range is too small, output some bits and rescale it.
	for e.Rng <= codeBot {
		e.carryOut(int(e.Val >> codeShift))
		// Move the next-to-high-order symbol into the high-order position.
		e.Val = (e.Val << SymBits) & (codeTop - 1)
		e.Rng <<= SymBits
		e.NbitsTotal += SymBits
	}
}

// Init initializes the encoder to write into buf (ec_enc_init). Storage
// is set to len(buf).
func (e *Encoder) Init(buf []byte) {
	e.Buf = buf
	e.EndOffs = 0
	e.EndWindow = 0
	e.NendBits = 0
	// This is the offset from which Tell will subtract partial bits.
	e.NbitsTotal = CodeBits + 1
	e.Offs = 0
	e.Rng = codeTop
	e.Rem = -1
	e.Val = 0
	e.Ext = 0
	e.Storage = uint32(len(buf))
	e.Error = 0
}

// Encode encodes a symbol given its frequency information
// (ec_encode). The symbol occupies [fl, fh) out of a total of ft.
func (e *Encoder) Encode(fl, fh, ft uint32) {
	r := Udiv(e.Rng, ft)
	if fl > 0 {
		e.Val += e.Rng - r*(ft-fl)
		e.Rng = r * (fh - fl)
	} else {
		e.Rng -= r * (ft - fh)
	}
	e.normalize()
}

// EncodeBin is Encode with ft equal to 1<<bits (ec_encode_bin).
func (e *Encoder) EncodeBin(fl, fh uint32, bits uint) {
	r := e.Rng >> bits
	if fl > 0 {
		e.Val += e.Rng - r*((1<<bits)-fl)
		e.Rng = r * (fh - fl)
	} else {
		e.Rng -= r * ((1 << bits) - fh)
	}
	e.normalize()
}

// EncBitLogp encodes a bit that has a 1/(1<<logp) probability of being
// one (ec_enc_bit_logp). Any nonzero val encodes a one.
func (e *Encoder) EncBitLogp(val int, logp uint) {
	r := e.Rng
	l := e.Val
	s := r >> logp
	r -= s
	if val != 0 {
		e.Val = l + r
		e.Rng = s
	} else {
		e.Rng = r
	}
	e.normalize()
}

// EncIcdf encodes symbol s using an "inverse" CDF table with 8-bit
// entries (ec_enc_icdf). icdf[i] is (1<<ftb) minus the cumulative
// frequency of symbols 0..i, and the table ends with 0.
func (e *Encoder) EncIcdf(s int, icdf []uint8, ftb uint) {
	r := e.Rng >> ftb
	if s > 0 {
		e.Val += e.Rng - r*uint32(icdf[s-1])
		e.Rng = r * (uint32(icdf[s-1]) - uint32(icdf[s]))
	} else {
		e.Rng -= r * uint32(icdf[s])
	}
	e.normalize()
}

// EncIcdf16 is EncIcdf with 16-bit table entries (ec_enc_icdf16).
func (e *Encoder) EncIcdf16(s int, icdf []uint16, ftb uint) {
	r := e.Rng >> ftb
	if s > 0 {
		e.Val += e.Rng - r*uint32(icdf[s-1])
		e.Rng = r * (uint32(icdf[s-1]) - uint32(icdf[s]))
	} else {
		e.Rng -= r * uint32(icdf[s])
	}
	e.normalize()
}

// EncUint encodes a raw unsigned integer fl in the range [0, ft)
// (ec_enc_uint). ft must be at least 2 and may be as large as 1<<32-1.
func (e *Encoder) EncUint(fl, ft uint32) {
	ft--
	ftb := Ilog(ft)
	if ftb > UintBits {
		ftb -= UintBits
		t := (ft >> uint(ftb)) + 1
		f := fl >> uint(ftb)
		e.Encode(f, f+1, t)
		e.EncBits(fl&((uint32(1)<<uint(ftb))-1), uint(ftb))
	} else {
		e.Encode(fl, fl+1, ft+1)
	}
}

// EncBits encodes a sequence of raw bits at the end of the stream
// (ec_enc_bits). bits must be between 1 and 25.
func (e *Encoder) EncBits(fl uint32, bits uint) {
	window := e.EndWindow
	used := e.NendBits
	if uint(used)+bits > WindowSize {
		for {
			e.Error |= e.writeByteAtEnd(window & symMax)
			window >>= SymBits
			used -= SymBits
			if used < SymBits {
				break
			}
		}
	}
	window |= fl << uint(used)
	used += int(bits)
	e.EndWindow = window
	e.NendBits = used
	e.NbitsTotal += int(bits)
}

// PatchInitialBits overwrites a few bits at the very start of an
// existing stream, without changing any of the other bits
// (ec_enc_patch_initial_bits). nbits must be at most 8. If not enough
// data has been encoded yet, it sets Error to -1.
func (e *Encoder) PatchInitialBits(val, nbits uint32) {
	shift := SymBits - nbits
	mask := ((uint32(1) << nbits) - 1) << shift
	if e.Offs > 0 {
		// The first byte has been finalized.
		e.Buf[0] = byte((uint32(e.Buf[0]) &^ mask) | val<<shift)
	} else if e.Rem >= 0 {
		// The first byte is still awaiting carry propagation.
		e.Rem = int((uint32(e.Rem) &^ mask) | val<<shift)
	} else if e.Rng <= codeTop>>nbits {
		// The renormalization loop has never been run.
		e.Val = (e.Val &^ (mask << codeShift)) | val<<(codeShift+shift)
	} else {
		// The encoder has not even encoded nbits of data yet.
		e.Error = -1
	}
}

// Shrink compacts the data to fit in a buffer of size bytes
// (ec_enc_shrink). The raw bits at the end move down. size must be at
// least Offs+EndOffs.
func (e *Encoder) Shrink(size uint32) {
	copy(e.Buf[size-e.EndOffs:size], e.Buf[e.Storage-e.EndOffs:e.Storage])
	e.Storage = size
}

// Done finalizes the stream (ec_enc_done). It outputs the minimum
// number of bits that ensures that the symbols encoded so far decode
// correctly regardless of the bits that follow, flushes the raw bits,
// and clears the unused space between them.
func (e *Encoder) Done() {
	l := CodeBits - Ilog(e.Rng)
	msk := (codeTop - 1) >> uint(l)
	end := (e.Val + msk) &^ msk
	if (end | msk) >= e.Val+e.Rng {
		l++
		msk >>= 1
		end = (e.Val + msk) &^ msk
	}
	for l > 0 {
		e.carryOut(int(end >> codeShift))
		end = (end << SymBits) & (codeTop - 1)
		l -= SymBits
	}
	// If we have a buffered byte flush it into the output buffer.
	if e.Rem >= 0 || e.Ext > 0 {
		e.carryOut(0)
	}
	// If we have buffered extra bits, flush them as well.
	window := e.EndWindow
	used := e.NendBits
	for used >= SymBits {
		e.Error |= e.writeByteAtEnd(window & symMax)
		window >>= SymBits
		used -= SymBits
	}
	// Clear any excess space and add any remaining extra bits to the last
	// byte.
	if e.Error == 0 {
		if e.Buf != nil {
			clear(e.Buf[e.Offs : e.Storage-e.EndOffs])
		}
		if used > 0 {
			// If there is no range coder data at all, give up.
			if e.EndOffs >= e.Storage {
				e.Error = -1
			} else {
				l = -l
				// If we have busted, do not add too many extra bits to the
				// last byte; it would corrupt the range coder data, and that
				// is more important.
				if e.Offs+e.EndOffs >= e.Storage && l < used {
					window &= (1 << uint(l)) - 1
					e.Error = -1
				}
				e.Buf[e.Storage-e.EndOffs-1] |= byte(window)
			}
		}
	}
}

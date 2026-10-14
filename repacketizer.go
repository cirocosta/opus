// Ported from libopus 1.6.1 src/repacketizer.c and the OpusRepacketizer
// type of src/opus_private.h.

package opus

// Repacketizer merges multiple Opus packets into one packet, or splits one
// packet into several. It ports OpusRepacketizer.
//
// The repacketizer keeps references to the packets passed to Cat, so their
// contents must not change until the next Reset. The zero value is ready
// to use.
type Repacketizer struct {
	toc             byte
	nbFrames        int
	frames          [48][]byte
	len             [48]int16
	framesize       int32
	paddings        [48][]byte
	paddingNbFrames [48]byte

	// exts is scratch space for the extensions of outRangeImpl (the C
	// code uses a stack array). It does not carry state.
	exts []ExtensionData
}

// NewRepacketizer returns a new, empty Repacketizer. It ports
// opus_repacketizer_create.
func NewRepacketizer() *Repacketizer {
	rp := &Repacketizer{}
	return rp.Reset()
}

// Reset discards all the frames and returns rp. It ports
// opus_repacketizer_init.
func (rp *Repacketizer) Reset() *Repacketizer {
	rp.nbFrames = 0
	return rp
}

// catImpl ports opus_repacketizer_cat_impl. It returns OK or a negative
// error code.
func (rp *Repacketizer) catImpl(data []byte, selfDelimited bool) int {
	var tmpTOC byte
	// Set of check ToC.
	if len(data) < 1 {
		return int(ErrInvalidPacket)
	}
	if rp.nbFrames == 0 {
		rp.toc = data[0]
		rp.framesize = opusPacketGetSamplesPerFrame(data[0], 8000)
	} else if rp.toc&0xFC != data[0]&0xFC {
		return int(ErrInvalidPacket)
	}
	currNbFrames := opusPacketGetNbFrames(data)
	if currNbFrames < 1 {
		return int(ErrInvalidPacket)
	}

	// Check the 120 ms maximum packet size.
	if int32(currNbFrames+rp.nbFrames)*rp.framesize > 960 {
		return int(ErrInvalidPacket)
	}

	ret := opusPacketParseImpl(data, selfDelimited, &tmpTOC,
		rp.frames[rp.nbFrames:], rp.len[rp.nbFrames:], nil, nil,
		&rp.paddings[rp.nbFrames])
	if ret < 1 {
		return ret
	}
	rp.paddingNbFrames[rp.nbFrames] = byte(ret)

	// Set padding length to zero for all but the first frame.
	for currNbFrames > 1 {
		rp.nbFrames++
		rp.paddingNbFrames[rp.nbFrames] = 0
		rp.paddings[rp.nbFrames] = nil
		currNbFrames--
	}
	rp.nbFrames++
	return OK
}

// Cat adds a packet to the current state. All packets must have the same
// configuration (the first six bits of the TOC byte), and the total
// duration must not exceed 120 ms. It ports opus_repacketizer_cat.
func (rp *Repacketizer) Cat(data []byte) error {
	return errorFromCode(rp.catImpl(data, false))
}

// NbFrames returns the total number of frames added with Cat since the last
// Reset. It ports opus_repacketizer_get_nb_frames.
func (rp *Repacketizer) NbFrames() int {
	return rp.nbFrames
}

// outRangeImpl ports opus_repacketizer_out_range_impl. The output buffer is
// data and maxlen is len(data). It returns the packet length or a negative
// error code.
// preserve the reference packet header, padding and frame-offset grammar
//
//revive:disable-next-line:cognitive-complexity
func (rp *Repacketizer) outRangeImpl(begin, end int, data []byte,
	selfDelimited, pad bool, extensions []ExtensionData,
) int32 {
	var totSize int32
	maxlen := int32(len(data))
	var onesBegin, onesEnd int32
	var extBegin, extLen int32

	if begin < 0 || begin >= end || end > rp.nbFrames {
		return int32(ErrBadArg)
	}
	count := end - begin

	length := rp.len[begin:end]
	frames := rp.frames[begin:end]
	if selfDelimited {
		totSize = 1
		if length[count-1] >= 252 {
			totSize++
		}
	}

	// Figure out the total number of extensions.
	totalExtCount := len(extensions)
	for i := begin; i < end; i++ {
		n := PacketExtensionsCount(rp.paddings[i], int(rp.paddingNbFrames[i]))
		if n > 0 {
			totalExtCount += n
		}
	}
	var allExtensions []ExtensionData
	if totalExtCount > 0 {
		if cap(rp.exts) < totalExtCount {
			rp.exts = make([]ExtensionData, totalExtCount)
		}
		allExtensions = rp.exts[:totalExtCount]
	}
	// Copy over any extensions that were passed in.
	extCount := copy(allExtensions, extensions)

	// Incorporate any extensions from the repacketizer padding.
	for i := begin; i < end; i++ {
		frameExtCount := int32(totalExtCount - extCount)
		ret := opusPacketExtensionsParse(rp.paddings[i], allExtensions[extCount:],
			&frameExtCount, int(rp.paddingNbFrames[i]))
		if ret < 0 {
			return int32(ErrInternalError)
		}
		// Renumber the extension frame numbers.
		for j := 0; j < int(frameExtCount); j++ {
			allExtensions[extCount+j].Frame += i - begin
		}
		extCount += int(frameExtCount)
	}

	ptr := 0
	switch count {
	case 1:
		// Code 0.
		totSize += int32(length[0]) + 1
		if totSize > maxlen {
			return int32(ErrBufferTooSmall)
		}
		data[ptr] = rp.toc & 0xFC
		ptr++
	case 2:
		if length[1] == length[0] {
			// Code 1.
			totSize += 2*int32(length[0]) + 1
			if totSize > maxlen {
				return int32(ErrBufferTooSmall)
			}
			data[ptr] = (rp.toc & 0xFC) | 0x1
			ptr++
		} else {
			// Code 2.
			totSize += int32(length[0]) + int32(length[1]) + 2
			if length[0] >= 252 {
				totSize++
			}
			if totSize > maxlen {
				return int32(ErrBufferTooSmall)
			}
			data[ptr] = (rp.toc & 0xFC) | 0x2
			ptr++
			ptr += encodeSize(int(length[0]), data[ptr:])
		}
	}
	vbr := false
	if count > 2 || (pad && totSize < maxlen) || extCount > 0 {
		// Code 3.
		var padAmount int32

		// Restart the process for the padding case.
		ptr = 0
		if selfDelimited {
			totSize = 1
			if length[count-1] >= 252 {
				totSize++
			}
		} else {
			totSize = 0
		}
		for i := 1; i < count; i++ {
			if length[i] != length[0] {
				vbr = true
				break
			}
		}
		if vbr {
			totSize += 2
			for i := 0; i < count-1; i++ {
				totSize += 1 + int32(length[i])
				if length[i] >= 252 {
					totSize++
				}
			}
			totSize += int32(length[count-1])

			if totSize > maxlen {
				return int32(ErrBufferTooSmall)
			}
			data[ptr] = (rp.toc & 0xFC) | 0x3
			ptr++
			data[ptr] = byte(count) | 0x80
			ptr++
		} else {
			totSize += int32(count)*int32(length[0]) + 2
			if totSize > maxlen {
				return int32(ErrBufferTooSmall)
			}
			data[ptr] = (rp.toc & 0xFC) | 0x3
			ptr++
			data[ptr] = byte(count)
			ptr++
		}
		if pad {
			padAmount = maxlen - totSize
		}
		if extCount > 0 {
			// Figure out how much space we need for the extensions.
			extLen = opusPacketExtensionsGenerate(nil, maxlen-totSize,
				allExtensions[:extCount], count, false)
			if extLen < 0 {
				return extLen
			}
			if !pad {
				padAmount = extLen + 1
				if extLen != 0 {
					padAmount = extLen + (extLen+253)/254
				}
			}
		}
		if padAmount != 0 {
			data[1] |= 0x40
			nb255s := (padAmount - 1) / 255
			if totSize+extLen+nb255s+1 > maxlen {
				return int32(ErrBufferTooSmall)
			}
			extBegin = totSize + padAmount - extLen
			// Prepend 0x01 padding.
			onesBegin = totSize + nb255s + 1
			onesEnd = totSize + padAmount - extLen
			for i := int32(0); i < nb255s; i++ {
				data[ptr] = 255
				ptr++
			}
			data[ptr] = byte(padAmount - 255*nb255s - 1)
			ptr++
			totSize += padAmount
		}
		if vbr {
			for i := 0; i < count-1; i++ {
				ptr += encodeSize(int(length[i]), data[ptr:])
			}
		}
	}
	if selfDelimited {
		ptr += encodeSize(int(length[count-1]), data[ptr:])
	}
	// Copy the actual data. Go's copy has memmove semantics, like the
	// OPUS_MOVE() that libopus uses for in-place padding and unpadding.
	for i := 0; i < count; i++ {
		ptr += copy(data[ptr:ptr+int(length[i])], frames[i])
	}
	if extLen > 0 {
		opusPacketExtensionsGenerate(data[extBegin:extBegin+extLen], extLen,
			allExtensions[:extCount], count, false)
	}
	for i := onesBegin; i < onesEnd; i++ {
		data[i] = 0x01
	}
	if pad && extCount == 0 {
		// Fill padding with zeros.
		clear(data[ptr:maxlen])
	}
	return totSize
}

// OutRange constructs a packet from frames [begin, end) of the current
// state into data and returns the packet length. The packet is at most
// len(data) bytes. It ports opus_repacketizer_out_range.
func (rp *Repacketizer) OutRange(begin, end int, data []byte) (int, error) {
	ret := rp.outRangeImpl(begin, end, data, false, false, nil)
	if ret < 0 {
		return 0, Error(ret)
	}
	return int(ret), nil
}

// Out constructs a packet from all the frames of the current state into
// data and returns the packet length. It ports opus_repacketizer_out.
func (rp *Repacketizer) Out(data []byte) (int, error) {
	return rp.OutRange(0, rp.nbFrames, data)
}

// opusPacketPadImpl ports opus_packet_pad_impl. The packet is data[:length]
// and the padded packet fills data[:newLen]. It returns the new length, OK
// when length == newLen, or a negative error code.
func opusPacketPadImpl(data []byte, length, newLen int32, pad bool,
	extensions []ExtensionData,
) int32 {
	var rp Repacketizer
	return packetPadScratch(data, length, newLen, pad, extensions, &rp, nil)
}

// packetPadScratch is opusPacketPadImpl with caller-owned scratch space:
// rp is the repacketizer to use and cp a buffer for the copy of the
// packet, reallocated when shorter than length. It lets the encoder pad
// packets without allocating.
func packetPadScratch(data []byte, length, newLen int32, pad bool,
	extensions []ExtensionData, rp *Repacketizer, cp []byte,
) int32 {
	if length < 1 {
		return int32(ErrBadArg)
	}
	if length == newLen {
		return OK
	} else if length > newLen {
		return int32(ErrBadArg)
	}
	rp.Reset()
	// Moving payload to the end of the packet so we can do in-place
	// padding.
	if int32(len(cp)) < length {
		cp = make([]byte, length)
	}
	cp = cp[:length]
	copy(cp, data[:length])
	if ret := rp.catImpl(cp, false); ret != OK {
		return int32(ret)
	}
	return rp.outRangeImpl(0, rp.nbFrames, data[:newLen], false, pad, extensions)
}

// opusPacketPad ports opus_packet_pad.
func opusPacketPad(data []byte, length, newLen int32) int {
	ret := opusPacketPadImpl(data, length, newLen, true, nil)
	if ret > 0 {
		return OK
	}
	return int(ret)
}

// PacketPad pads the packet data[:length] in place so that it fills all of
// data. It ports opus_packet_pad with new_len equal to len(data).
func PacketPad(data []byte, length int) error {
	if length > len(data) {
		return ErrBadArg
	}
	return errorFromCode(opusPacketPad(data, int32(length), int32(len(data))))
}

// opusPacketUnpad ports opus_packet_unpad. It returns the new length or a
// negative error code.
func opusPacketUnpad(data []byte) int32 {
	var rp Repacketizer
	if len(data) < 1 {
		return int32(ErrBadArg)
	}
	rp.Reset()
	if ret := rp.catImpl(data, false); ret < 0 {
		return int32(ret)
	}
	// Discard all padding and extensions.
	for i := 0; i < rp.nbFrames; i++ {
		rp.paddings[i] = nil
	}
	return rp.outRangeImpl(0, rp.nbFrames, data, false, false, nil)
}

// PacketUnpad removes all padding (and extensions) from the packet in data,
// in place, and returns the new length. It ports opus_packet_unpad.
func PacketUnpad(data []byte) (int, error) {
	ret := opusPacketUnpad(data)
	if ret < 0 {
		return 0, Error(ret)
	}
	return int(ret), nil
}

// opusMultistreamPacketPad ports opus_multistream_packet_pad.
func opusMultistreamPacketPad(data []byte, length, newLen int32, nbStreams int) int {
	var toc byte
	var size [48]int16
	var packetOffset int32

	if length < 1 {
		return int(ErrBadArg)
	}
	if length == newLen {
		return OK
	} else if length > newLen {
		return int(ErrBadArg)
	}
	amount := newLen - length
	// Seek to last stream.
	pos := int32(0)
	for s := 0; s < nbStreams-1; s++ {
		if length <= 0 {
			return int(ErrInvalidPacket)
		}
		count := opusPacketParseImpl(data[pos:pos+length], true, &toc, nil,
			size[:], nil, &packetOffset, nil)
		if count < 0 {
			return count
		}
		pos += packetOffset
		length -= packetOffset
	}
	return opusPacketPad(data[pos:], length, length+amount)
}

// MultistreamPacketPad pads the multistream packet data[:length] in place
// so that it fills all of data. It ports opus_multistream_packet_pad with
// new_len equal to len(data).
func MultistreamPacketPad(data []byte, length, nbStreams int) error {
	if length > len(data) {
		return ErrBadArg
	}
	return errorFromCode(opusMultistreamPacketPad(data, int32(length),
		int32(len(data)), nbStreams))
}

// opusMultistreamPacketUnpad ports opus_multistream_packet_unpad. It
// returns the new length or a negative error code.
// preserve the reference packet header, padding and frame-offset grammar
//
//revive:disable-next-line:cognitive-complexity
func opusMultistreamPacketUnpad(data []byte, nbStreams int) int32 {
	var toc byte
	var size [48]int16
	var packetOffset int32
	var rp Repacketizer

	length := int32(len(data))
	if length < 1 {
		return int32(ErrBadArg)
	}
	dst := int32(0)
	pos := int32(0)
	dstLen := int32(0)
	// Unpad all frames.
	for s := 0; s < nbStreams; s++ {
		selfDelimited := s != nbStreams-1
		if length <= 0 {
			return int32(ErrInvalidPacket)
		}
		rp.Reset()
		ret := int32(opusPacketParseImpl(data[pos:pos+length], selfDelimited,
			&toc, nil, size[:], nil, &packetOffset, nil))
		if ret < 0 {
			return ret
		}
		ret = int32(rp.catImpl(data[pos:pos+packetOffset], selfDelimited))
		if ret < 0 {
			return ret
		}
		// Discard all padding and extensions.
		for i := 0; i < rp.nbFrames; i++ {
			rp.paddings[i] = nil
		}
		ret = rp.outRangeImpl(0, rp.nbFrames, data[dst:dst+length],
			selfDelimited, false, nil)
		if ret < 0 {
			return ret
		}
		dstLen += ret
		dst += ret
		pos += packetOffset
		length -= packetOffset
	}
	return dstLen
}

// MultistreamPacketUnpad removes all padding from the multistream packet in
// data (nbStreams streams), in place, and returns the new length. It ports
// opus_multistream_packet_unpad.
func MultistreamPacketUnpad(data []byte, nbStreams int) (int, error) {
	ret := opusMultistreamPacketUnpad(data, nbStreams)
	if ret < 0 {
		return 0, Error(ret)
	}
	return int(ret), nil
}

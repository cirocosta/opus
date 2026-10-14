// Ported from libopus 1.6.1 src/extensions.c and the extension types of
// src/opus_private.h.

package opus

// ExtensionData is one Opus padding extension. It ports
// opus_extension_data. Data holds the extension payload; for parsed
// extensions it shares the packet padding's backing array. Mutating or
// reusing that array also changes the parsed payload.
type ExtensionData struct {
	ID    int
	Frame int
	Data  []byte
}

// skipExtensionPayload ports skip_extension_payload. Given an extension
// payload (excluding the initial ID byte) at data[*pos:], it advances *pos
// to the next extension and returns the length of the remaining
// extensions, or -1 on error. A "Repeat These Extensions" extension (ID 2)
// does not advance past the repeated extension payloads.
// keep the reference extension grammar and repeat-frame cursor transitions together
//
//revive:disable-next-line:cognitive-complexity
func skipExtensionPayload(data []byte, pos *int, length int32,
	headerSize *int32, idByte int, trailingShortLen int32,
) int32 {
	p := *pos
	var hs int32
	id := idByte >> 1
	l := int32(idByte & 1)
	if (id == 0 && l == 1) || id == 2 {
		*headerSize = 0
		return length
	}
	if id > 0 && id < 32 {
		if length < l {
			return -1
		}
		p += int(l)
		length -= l
	} else {
		if l == 0 {
			if length < trailingShortLen {
				return -1
			}
			p += int(length - trailingShortLen)
			length = trailingShortLen
		} else {
			var bytes int32
			for {
				if length < 1 {
					return -1
				}
				lacing := int32(data[p])
				p++
				bytes += lacing
				hs++
				length -= lacing + 1
				if lacing != 255 {
					break
				}
			}
			if length < 0 {
				return -1
			}
			p += int(bytes)
		}
	}
	*pos = p
	*headerSize = hs
	return length
}

// skipExtension ports skip_extension. Given an extension at data[*pos:], it
// advances *pos to the next extension and returns the length of the
// remaining extensions, or -1 on error. A "Repeat These Extensions"
// extension (ID 2) only advances past the extension ID byte.
func skipExtension(data []byte, pos *int, length int32, headerSize *int32) int32 {
	if length == 0 {
		*headerSize = 0
		return 0
	}
	if length < 1 {
		return -1
	}
	p := *pos
	idByte := int(data[p])
	p++
	length--
	length = skipExtensionPayload(data, &p, length, headerSize, idByte, 0)
	if length >= 0 {
		*pos = p
		*headerSize++
	}
	return length
}

// ExtensionIterator walks the extensions stored in the padding of an Opus
// packet. It ports OpusExtensionIterator. The C pointers into the padding
// are kept as offsets into data; lastLong is -1 for the C NULL.
type ExtensionIterator struct {
	data             []byte
	currData         int
	repeatData       int
	lastLong         int
	srcData          int
	len              int32
	currLen          int32
	repeatLen        int32
	srcLen           int32
	trailingShortLen int32
	nbFrames         int
	frameMax         int
	currFrame        int
	repeatFrame      int
	repeatL          byte
}

// NewExtensionIterator returns an iterator over the extensions in the
// padding data of a packet with nbFrames frames (0 to 48).
func NewExtensionIterator(data []byte, nbFrames int) *ExtensionIterator {
	it := &ExtensionIterator{}
	it.Init(data, nbFrames)
	return it
}

// Init sets up the iterator for the padding data of a packet with nbFrames
// frames (0 to 48). It ports opus_extension_iterator_init.
func (it *ExtensionIterator) Init(data []byte, nbFrames int) {
	it.data = data
	it.repeatData, it.currData = 0, 0
	it.lastLong, it.srcData = -1, -1
	it.len = int32(len(data))
	it.currLen = it.len
	it.repeatLen, it.srcLen = 0, 0
	it.trailingShortLen = 0
	it.frameMax, it.nbFrames = nbFrames, nbFrames
	it.repeatFrame, it.currFrame = 0, 0
	it.repeatL = 0
}

// Reset restarts the iteration from the first extension. It ports
// opus_extension_iterator_reset.
func (it *ExtensionIterator) Reset() {
	it.repeatData, it.currData = 0, 0
	it.lastLong = -1
	it.currLen = it.len
	it.repeatFrame, it.currFrame = 0, 0
	it.trailingShortLen = 0
}

// SetFrameMax tells the iterator not to return extensions for frames of
// index frameMax or larger. It ports opus_extension_iterator_set_frame_max.
func (it *ExtensionIterator) SetFrameMax(frameMax int) {
	it.frameMax = frameMax
}

// nextRepeat ports opus_extension_iterator_next_repeat. It returns 1 if a
// repeated extension is found, a negative error code, or 0 when the
// repetition is done.
// keep the reference extension grammar and repeat-frame cursor transitions together
//
//revive:disable-next-line:cognitive-complexity
func (it *ExtensionIterator) nextRepeat(ext *ExtensionData) int {
	var headerSize int32
	for ; it.repeatFrame < it.nbFrames; it.repeatFrame++ {
		for it.srcLen > 0 {
			repeatIDByte := int(it.data[it.srcData])
			it.srcLen = skipExtension(it.data, &it.srcData, it.srcLen, &headerSize)
			// Don't repeat padding or frame separators with a 0 increment.
			if repeatIDByte <= 3 {
				continue
			}
			// If the "Repeat These Extensions" extension had L == 0 and
			// this is the last repeated long extension, then force
			// decoding the payload with L = 0.
			if it.repeatL == 0 && it.repeatFrame+1 >= it.nbFrames &&
				it.srcData == it.lastLong {
				repeatIDByte &^= 1
			}
			currData0 := it.currData
			it.currLen = skipExtensionPayload(it.data, &it.currData, it.currLen,
				&headerSize, repeatIDByte, it.trailingShortLen)
			if it.currLen < 0 {
				return int(ErrInvalidPacket)
			}
			// If we were asked to stop at frameMax, skip extensions for
			// later frames.
			if it.repeatFrame >= it.frameMax {
				continue
			}
			if ext != nil {
				start := currData0 + int(headerSize)
				ext.ID = repeatIDByte >> 1
				ext.Frame = it.repeatFrame
				ext.Data = it.data[start:it.currData]
			}
			return 1
		}
		// We finished repeating the extensions for this frame.
		it.srcData = it.repeatData
		it.srcLen = it.repeatLen
	}
	// We finished repeating extensions.
	it.repeatData = it.currData
	it.lastLong = -1
	// If L == 0, advance the frame number to handle the case where we did
	// not consume all of the data with an L == 0 long extension.
	if it.repeatL == 0 {
		it.currFrame++
		// Ignore additional padding if this was already the last frame.
		if it.currFrame >= it.nbFrames {
			it.currLen = 0
		}
	}
	it.repeatFrame = 0
	return 0
}

// next ports opus_extension_iterator_next. It returns 1 if an extension is
// found, 0 when there are no more, or a negative error code.
// keep the reference extension grammar and repeat-frame cursor transitions together
//
//revive:disable-next-line:cognitive-complexity
func (it *ExtensionIterator) next(ext *ExtensionData) int {
	var headerSize int32
	if it.currLen < 0 {
		return int(ErrInvalidPacket)
	}
	if it.repeatFrame > 0 {
		// We are in the process of repeating some extensions.
		if ret := it.nextRepeat(ext); ret != 0 {
			return ret
		}
	}
	// Checking this here allows SetFrameMax to be called at any point.
	if it.currFrame >= it.frameMax {
		return 0
	}
	for it.currLen > 0 {
		currData0 := it.currData
		id := int(it.data[currData0] >> 1)
		l := it.data[currData0] & 1
		it.currLen = skipExtension(it.data, &it.currData, it.currLen, &headerSize)
		if it.currLen < 0 {
			return int(ErrInvalidPacket)
		}
		if id == 1 {
			if l == 0 {
				it.currFrame++
			} else {
				// A frame increment of 0 is a no-op.
				if it.data[currData0+1] == 0 {
					continue
				}
				it.currFrame += int(it.data[currData0+1])
			}
			if it.currFrame >= it.nbFrames {
				it.currLen = -1
				return int(ErrInvalidPacket)
			}
			// If we were asked to stop at frameMax, skip extensions for
			// later frames.
			if it.currFrame >= it.frameMax {
				it.currLen = 0
			}
			it.repeatData = it.currData
			it.lastLong = -1
			it.trailingShortLen = 0
		} else if id == 2 {
			it.repeatL = l
			it.repeatFrame = it.currFrame + 1
			it.repeatLen = int32(currData0 - it.repeatData)
			it.srcData = it.repeatData
			it.srcLen = it.repeatLen
			if ret := it.nextRepeat(ext); ret != 0 {
				return ret
			}
		} else if id > 2 {
			// Update the location of the last long extension. This lets us
			// know when we need to modify the last L flag if we repeat
			// these extensions with L=0.
			if id >= 32 {
				it.lastLong = it.currData
				it.trailingShortLen = 0
			} else {
				// Otherwise, keep track of how many payload bytes follow
				// the last long extension.
				it.trailingShortLen += int32(l)
			}
			if ext != nil {
				start := currData0 + int(headerSize)
				ext.ID = id
				ext.Frame = it.currFrame
				ext.Data = it.data[start:it.currData]
			}
			return 1
		}
	}
	return 0
}

// find ports opus_extension_iterator_find.
func (it *ExtensionIterator) find(ext *ExtensionData, id int) int {
	var currExt ExtensionData
	for {
		ret := it.next(&currExt)
		if ret <= 0 {
			return ret
		}
		if currExt.ID == id {
			*ext = currExt
			return ret
		}
	}
}

// Next returns the next extension (excluding real padding, separators, and
// repeat indicators, but including the repeated extensions) in bitstream
// order. Because of the repetition mechanism, extensions are not always in
// frame order. ok is false when there are no more extensions. It ports
// opus_extension_iterator_next.
func (it *ExtensionIterator) Next() (ext ExtensionData, ok bool, err error) {
	ret := it.next(&ext)
	if ret <= 0 {
		return ExtensionData{}, false, errorFromCode(ret)
	}
	return ext, true, nil
}

// Find returns the next extension with the given ID. ok is false when there
// are no more such extensions. It ports opus_extension_iterator_find.
func (it *ExtensionIterator) Find(id int) (ext ExtensionData, ok bool, err error) {
	ret := it.find(&ext, id)
	if ret <= 0 {
		return ExtensionData{}, false, errorFromCode(ret)
	}
	return ext, true, nil
}

// PacketExtensionsCount returns the number of extensions in the padding
// data of a packet with nbFrames frames, excluding real padding,
// separators, and repeat indicators, but including the repeated
// extensions. Counting stops at the first malformed extension. It ports
// opus_packet_extensions_count.
func PacketExtensionsCount(data []byte, nbFrames int) int {
	var it ExtensionIterator
	it.Init(data, nbFrames)
	count := 0
	for it.next(nil) > 0 {
		count++
	}
	return count
}

// PacketExtensionsCountExt is like PacketExtensionsCount, and also stores
// the number of extensions of each frame in nbFrameExts[:nbFrames]. It
// ports opus_packet_extensions_count_ext.
func PacketExtensionsCountExt(data []byte, nbFrameExts []int, nbFrames int) int {
	var it ExtensionIterator
	var ext ExtensionData
	it.Init(data, nbFrames)
	clear(nbFrameExts[:nbFrames])
	count := 0
	for it.next(&ext) > 0 {
		nbFrameExts[ext.Frame]++
		count++
	}
	return count
}

// opusPacketExtensionsParse ports opus_packet_extensions_parse. On input
// *nbExtensions is the capacity of extensions; on success it is the number
// of extensions found. It returns 0 or a negative error code.
func opusPacketExtensionsParse(data []byte, extensions []ExtensionData,
	nbExtensions *int32, nbFrames int,
) int {
	var it ExtensionIterator
	var ret int
	it.Init(data, nbFrames)
	var count int32
	for count = 0; ; count++ {
		var ext ExtensionData
		ret = it.next(&ext)
		if ret <= 0 {
			break
		}
		if count == *nbExtensions {
			return int(ErrBufferTooSmall)
		}
		extensions[count] = ext
	}
	*nbExtensions = count
	return ret
}

// PacketExtensionsParse extracts the extensions from the padding data of a
// packet with nbFrames frames into extensions, in bitstream order, and
// returns how many it stored. It returns ErrBufferTooSmall if extensions is
// too short. It ports opus_packet_extensions_parse.
func PacketExtensionsParse(data []byte, extensions []ExtensionData, nbFrames int) (int, error) {
	n := int32(len(extensions))
	if ret := opusPacketExtensionsParse(data, extensions, &n, nbFrames); ret < 0 {
		return 0, Error(ret)
	}
	return int(n), nil
}

// opusPacketExtensionsParseExt ports opus_packet_extensions_parse_ext.
func opusPacketExtensionsParseExt(data []byte, extensions []ExtensionData,
	nbExtensions *int32, nbFrameExts []int, nbFrames int,
) int {
	var it ExtensionIterator
	var ext ExtensionData
	var nbFramesCum [49]int32
	var ret int
	// Convert the frame extension count array to a cumulative sum.
	var prevTotal int32
	var count int
	for count = 0; count < nbFrames; count++ {
		total := int32(nbFrameExts[count]) + prevTotal
		nbFramesCum[count] = prevTotal
		prevTotal = total
	}
	nbFramesCum[count] = prevTotal
	it.Init(data, nbFrames)
	var n int32
	for n = 0; ; n++ {
		ret = it.next(&ext)
		if ret <= 0 {
			break
		}
		idx := nbFramesCum[ext.Frame]
		nbFramesCum[ext.Frame]++
		if idx >= *nbExtensions {
			return int(ErrBufferTooSmall)
		}
		extensions[idx] = ext
	}
	*nbExtensions = n
	return ret
}

// PacketExtensionsParseExt extracts the extensions like
// PacketExtensionsParse, but in frame order. nbFrameExts must hold the
// output of PacketExtensionsCountExt. It ports
// opus_packet_extensions_parse_ext.
func PacketExtensionsParseExt(data []byte, extensions []ExtensionData,
	nbFrameExts []int, nbFrames int,
) (int, error) {
	n := int32(len(extensions))
	if ret := opusPacketExtensionsParseExt(data, extensions, &n, nbFrameExts, nbFrames); ret < 0 {
		return 0, Error(ret)
	}
	return int(n), nil
}

// writeExtensionPayload ports write_extension_payload. data may be nil to
// only compute the size.
// keep the reference extension grammar and repeat-frame cursor transitions together
//
//revive:disable-next-line:cognitive-complexity
func writeExtensionPayload(data []byte, length, pos int32, ext *ExtensionData, last bool) int32 {
	extLen := int32(len(ext.Data))
	if ext.ID < 32 {
		if extLen > 1 {
			return int32(ErrBadArg)
		}
		if extLen > 0 {
			if length-pos < extLen {
				return int32(ErrBufferTooSmall)
			}
			if data != nil {
				data[pos] = ext.Data[0]
			}
			pos++
		}
	} else {
		lengthBytes := 1 + extLen/255
		if last {
			lengthBytes = 0
		}
		if length-pos < lengthBytes+extLen {
			return int32(ErrBufferTooSmall)
		}
		if !last {
			for j := int32(0); j < extLen/255; j++ {
				if data != nil {
					data[pos] = 255
				}
				pos++
			}
			if data != nil {
				data[pos] = byte(extLen % 255)
			}
			pos++
		}
		if data != nil {
			copy(data[pos:pos+extLen], ext.Data)
		}
		pos += extLen
	}
	return pos
}

// writeExtension ports write_extension.
func writeExtension(data []byte, length, pos int32, ext *ExtensionData, last bool) int32 {
	if length-pos < 1 {
		return int32(ErrBufferTooSmall)
	}
	if data != nil {
		v := ext.ID << 1
		if ext.ID < 32 {
			v += len(ext.Data)
		} else if !last {
			v++
		}
		data[pos] = byte(v)
	}
	pos++
	return writeExtensionPayload(data, length, pos, ext, last)
}

// opusPacketExtensionsGenerate ports opus_packet_extensions_generate. data
// may be nil to only compute the size; otherwise it must have room for
// length bytes. It returns the number of bytes written or a negative error
// code.
// keep the reference extension grammar and repeat-frame cursor transitions together
//
//revive:disable-next-line:cognitive-complexity
func opusPacketExtensionsGenerate(data []byte, length int32,
	extensions []ExtensionData, nbFrames int, pad bool,
) int32 {
	var frameMinIdx, frameMaxIdx, frameRepeatIdx [48]int32
	nbExtensions := int32(len(extensions))
	currFrame := 0
	var pos, written int32

	if nbFrames > 48 {
		return int32(ErrBadArg)
	}

	// Do a little work up-front to make this O(nbExtensions) instead of
	// O(nbExtensions*nbFrames) so long as the extensions are in frame
	// order (without requiring that they be in frame order).
	for f := 0; f < nbFrames; f++ {
		frameMinIdx[f] = nbExtensions
	}
	for i := int32(0); i < nbExtensions; i++ {
		f := extensions[i].Frame
		if f < 0 || f >= nbFrames {
			return int32(ErrBadArg)
		}
		if extensions[i].ID < 3 || extensions[i].ID > 127 {
			return int32(ErrBadArg)
		}
		frameMinIdx[f] = min(frameMinIdx[f], i)
		frameMaxIdx[f] = max(frameMaxIdx[f], i+1)
	}
	for f := 0; f < nbFrames; f++ {
		frameRepeatIdx[f] = frameMinIdx[f]
	}
	for f := 0; f < nbFrames; f++ {
		repeatCount := 0
		lastLongIdx := int32(-1)
		if f+1 < nbFrames {
			for i := frameMinIdx[f]; i < frameMaxIdx[f]; i++ {
				if extensions[i].Frame != f {
					continue
				}
				// Test if we can repeat this extension in future frames.
				g := f + 1
				for ; g < nbFrames; g++ {
					if frameRepeatIdx[g] >= frameMaxIdx[g] {
						break
					}
					r := &extensions[frameRepeatIdx[g]]
					if r.ID != extensions[i].ID {
						break
					}
					if r.ID < 32 && len(r.Data) != len(extensions[i].Data) {
						break
					}
				}
				if g < nbFrames {
					break
				}
				// We can! If this is a long extension, save the index of
				// the last instance, so we can modify its L flag.
				if extensions[i].ID >= 32 {
					lastLongIdx = frameRepeatIdx[nbFrames-1]
				}
				// Advance the repeat pointers.
				for g = f + 1; g < nbFrames; g++ {
					j := frameRepeatIdx[g] + 1
					for j < frameMaxIdx[g] && extensions[j].Frame != g {
						j++
					}
					frameRepeatIdx[g] = j
				}
				repeatCount++
				// Point the repeat pointer for this frame to the current
				// extension, so we know when to trigger the repeats.
				frameRepeatIdx[f] = i
			}
		}
		for i := frameMinIdx[f]; i < frameMaxIdx[f]; i++ {
			if extensions[i].Frame != f {
				continue
			}
			// Insert separator when needed.
			if f != currFrame {
				diff := f - currFrame
				if length-pos < 2 {
					return int32(ErrBufferTooSmall)
				}
				if diff == 1 {
					if data != nil {
						data[pos] = 0x02
					}
					pos++
				} else {
					if data != nil {
						data[pos] = 0x03
					}
					pos++
					if data != nil {
						data[pos] = byte(diff)
					}
					pos++
				}
				currFrame = f
			}

			pos = writeExtension(data, length, pos, &extensions[i],
				written == nbExtensions-1)
			if pos < 0 {
				return pos
			}
			written++

			if repeatCount > 0 && frameRepeatIdx[f] == i {
				// Add the repeat indicator.
				nbRepeated := int32(repeatCount * (nbFrames - (f + 1)))
				last := written+nbRepeated == nbExtensions ||
					(lastLongIdx < 0 && i+1 >= frameMaxIdx[f])
				if length-pos < 1 {
					return int32(ErrBufferTooSmall)
				}
				if data != nil {
					if last {
						data[pos] = 0x04
					} else {
						data[pos] = 0x05
					}
				}
				pos++
				for g := f + 1; g < nbFrames; g++ {
					j := frameMinIdx[g]
					for ; j < frameRepeatIdx[g]; j++ {
						if extensions[j].Frame == g {
							pos = writeExtensionPayload(data, length, pos,
								&extensions[j], last && j == lastLongIdx)
							if pos < 0 {
								return pos
							}
							written++
						}
					}
					frameMinIdx[g] = j
				}
				if last {
					currFrame++
				}
			}
		}
	}
	// If we need to pad, just prepend 0x01 bytes. Even better would be to
	// fill the end with zeros, but that requires checking that turning the
	// last extension into an L=1 case still fits.
	if pad && pos < length {
		padding := length - pos
		if data != nil {
			copy(data[padding:padding+pos], data[:pos])
			for i := int32(0); i < padding; i++ {
				data[i] = 0x01
			}
		}
		pos += padding
	}
	return pos
}

// PacketExtensionsGenerate writes the extensions for a packet with
// nbFrames frames (at most 48) into data and returns the number of bytes
// written. If pad is true, it fills all of data, prepending padding bytes.
// It ports opus_packet_extensions_generate.
func PacketExtensionsGenerate(
	data []byte,
	extensions []ExtensionData,
	nbFrames int,
	pad bool,
) (int, error) {
	ret := opusPacketExtensionsGenerate(data, int32(len(data)), extensions, nbFrames, pad)
	if ret < 0 {
		return 0, Error(ret)
	}
	return int(ret), nil
}

// PacketExtensionsSize returns the number of bytes that
// PacketExtensionsGenerate needs to write the extensions without padding.
func PacketExtensionsSize(extensions []ExtensionData, nbFrames int) (int, error) {
	ret := opusPacketExtensionsGenerate(nil, 1<<30, extensions, nbFrames, false)
	if ret < 0 {
		return 0, Error(ret)
	}
	return int(ret), nil
}

// Ported from libopus 1.6.1 src/opus.c, plus the packet inspection
// functions of src/opus_decoder.c (opus_packet_get_bandwidth,
// opus_packet_get_nb_channels, opus_packet_get_nb_frames,
// opus_packet_get_nb_samples, opus_packet_has_lbrr, opus_packet_get_mode).

package opus

import "math"

// Codec modes from opus_private.h.
const (
	modeSilkOnly = 1000 // MODE_SILK_ONLY
	modeHybrid   = 1001 // MODE_HYBRID
	modeCeltOnly = 1002 // MODE_CELT_ONLY
)

func abs32(x float32) float32 { return float32(math.Abs(float64(x))) }

// opusLimit2Checkwithin1 ports opus_limit2_checkwithin1_c from
// celt/mathops.c: clamp samples to [-2, 2]. The generic C version always
// returns 0.
func opusLimit2Checkwithin1(samples []float32, cnt int) int {
	if cnt <= 0 {
		return 1
	}
	for i := 0; i < cnt; i++ {
		v := samples[i]
		// FMAX(-2.0f, v) and FMIN(2.0f, v).
		if -2.0 > v {
			v = -2.0
		}
		if 2.0 < v {
			v = 2.0
		}
		samples[i] = v
	}
	return 0
}

// opusPCMSoftClipImpl ports opus_pcm_soft_clip_impl. x holds n interleaved
// samples for each of c channels; declipMem holds c values.
// preserve the reference packet grammar and sample-boundary decisions
//
//revive:disable-next-line:cognitive-complexity
func opusPCMSoftClipImpl(x []float32, n, c int, declipMem []float32) {
	if c < 1 || n < 1 || x == nil || declipMem == nil {
		return
	}
	allWithinNeg1Pos1 := opusLimit2Checkwithin1(x, n*c)

	for ch := 0; ch < c; ch++ {
		xc := x[ch:]
		a := declipMem[ch]
		var i int
		// Continue applying the non-linearity from the previous frame to
		// avoid any discontinuity.
		for i = 0; i < n; i++ {
			if xc[i*c]*a >= 0 {
				break
			}
			xc[i*c] = xc[i*c] + float32(a*xc[i*c]*xc[i*c])
		}

		curr := 0
		x0 := xc[0]
		for {
			if allWithinNeg1Pos1 != 0 {
				i = n
			} else {
				for i = curr; i < n; i++ {
					if xc[i*c] > 1 || xc[i*c] < -1 {
						break
					}
				}
			}
			if i == n {
				a = 0
				break
			}
			peakPos := i
			start, end := i, i
			maxval := abs32(xc[i*c])
			// Look for first zero crossing before clipping.
			for start > 0 && xc[i*c]*xc[(start-1)*c] >= 0 {
				start--
			}
			// Look for first zero crossing after clipping.
			for end < n && xc[i*c]*xc[end*c] >= 0 {
				// Look for other peaks until the next zero-crossing.
				if abs32(xc[end*c]) > maxval {
					maxval = abs32(xc[end*c])
					peakPos = end
				}
				end++
			}
			// Detect the special case where we clip before the first zero
			// crossing.
			special := start == 0 && xc[i*c]*xc[0] >= 0

			// Compute a such that maxval + a*maxval^2 = 1.
			a = (maxval - 1) / float32(maxval*maxval)
			// Slightly boost "a" by 2^-22.
			a += float32(a * 2.4e-7)
			if xc[i*c] > 0 {
				a = -a
			}
			// Apply soft clipping.
			for i = start; i < end; i++ {
				xc[i*c] = xc[i*c] + float32(a*xc[i*c]*xc[i*c])
			}

			if special && peakPos >= 2 {
				// Add a linear ramp from the first sample to the signal
				// peak. This avoids a discontinuity at the beginning of
				// the frame.
				offset := x0 - xc[0]
				delta := offset / float32(peakPos)
				for i = curr; i < peakPos; i++ {
					offset -= delta
					xc[i*c] += offset
					// MAX16(-1.f, MIN16(1.f, x)).
					v := xc[i*c]
					if 1.0 < v {
						v = 1.0
					}
					if -1.0 > v {
						v = -1.0
					}
					xc[i*c] = v
				}
			}
			curr = end
			if curr == n {
				break
			}
		}
		declipMem[ch] = a
	}
}

// PCMSoftClip applies soft-clipping to bring a float signal within the
// [-1,1] range. pcm holds frameSize interleaved samples per channel. If the
// signal is already in that range, nothing is done. softclipMem holds one
// value per channel and must be zeroed before the first call on a stream. It
// ports opus_pcm_soft_clip.
func PCMSoftClip(pcm []float32, frameSize, channels int, softclipMem []float32) {
	opusPCMSoftClipImpl(pcm, frameSize, channels, softclipMem)
}

// encodeSize ports encode_size: write a frame length with the 1 or 2 byte
// encoding and return the number of bytes written.
func encodeSize(size int, data []byte) int {
	if size < 252 {
		data[0] = byte(size)
		return 1
	}
	data[0] = byte(252 + (size & 0x3))
	data[1] = byte((size - int(data[0])) >> 2)
	return 2
}

// parseSize ports parse_size. len is the number of readable bytes of data.
func parseSize(data []byte, length int32, size *int16) int {
	if length < 1 {
		*size = -1
		return -1
	} else if data[0] < 252 {
		*size = int16(data[0])
		return 1
	} else if length < 2 {
		*size = -1
		return -1
	}
	*size = 4*int16(data[1]) + int16(data[0])
	return 2
}

// opusPacketGetSamplesPerFrame ports opus_packet_get_samples_per_frame for
// a TOC byte.
func opusPacketGetSamplesPerFrame(toc byte, fs int32) int32 {
	var audiosize int32
	if toc&0x80 != 0 {
		audiosize = int32((toc >> 3) & 0x3)
		audiosize = (fs << audiosize) / 400
	} else if toc&0x60 == 0x60 {
		if toc&0x08 != 0 {
			audiosize = fs / 50
		} else {
			audiosize = fs / 100
		}
	} else {
		audiosize = int32((toc >> 3) & 0x3)
		if audiosize == 3 {
			audiosize = fs * 60 / 1000
		} else {
			audiosize = (fs << audiosize) / 100
		}
	}
	return audiosize
}

// opusPacketGetBandwidth ports opus_packet_get_bandwidth for a TOC byte.
func opusPacketGetBandwidth(toc byte) int {
	var bandwidth int
	if toc&0x80 != 0 {
		bandwidth = BandwidthMediumband + int((toc>>5)&0x3)
		if bandwidth == BandwidthMediumband {
			bandwidth = BandwidthNarrowband
		}
	} else if toc&0x60 == 0x60 {
		if toc&0x10 != 0 {
			bandwidth = BandwidthFullband
		} else {
			bandwidth = BandwidthSuperwideband
		}
	} else {
		bandwidth = BandwidthNarrowband + int((toc>>5)&0x3)
	}
	return bandwidth
}

// opusPacketGetMode ports opus_packet_get_mode for a TOC byte.
func opusPacketGetMode(toc byte) int {
	if toc&0x80 != 0 {
		return modeCeltOnly
	} else if toc&0x60 == 0x60 {
		return modeHybrid
	}
	return modeSilkOnly
}

// opusPacketGetNbChannels ports opus_packet_get_nb_channels for a TOC byte.
func opusPacketGetNbChannels(toc byte) int {
	if toc&0x4 != 0 {
		return 2
	}
	return 1
}

// opusPacketGetNbFrames ports opus_packet_get_nb_frames. It returns the
// frame count or a negative error code.
func opusPacketGetNbFrames(packet []byte) int {
	if len(packet) < 1 {
		return int(ErrBadArg)
	}
	count := packet[0] & 0x3
	if count == 0 {
		return 1
	} else if count != 3 {
		return 2
	} else if len(packet) < 2 {
		return int(ErrInvalidPacket)
	}
	return int(packet[1] & 0x3F)
}

// opusPacketGetNbSamples ports opus_packet_get_nb_samples. It returns the
// sample count or a negative error code.
func opusPacketGetNbSamples(packet []byte, fs int32) int {
	count := opusPacketGetNbFrames(packet)
	if count < 0 {
		return count
	}
	samples := int32(count) * opusPacketGetSamplesPerFrame(packet[0], fs)
	// Can't have more than 120 ms.
	if samples*25 > fs*3 {
		return int(ErrInvalidPacket)
	}
	return int(samples)
}

// opusPacketHasLBRR ports opus_packet_has_lbrr. It returns 1 if the packet
// has LBRR data, 0 if not, or a negative error code.
func opusPacketHasLBRR(packet []byte) int {
	var frames [48][]byte
	var size [48]int16
	nbFrames := int32(1)

	if len(packet) < 1 {
		// opus_packet_parse rejects empty packets.
		return int(ErrInvalidPacket)
	}
	packetMode := opusPacketGetMode(packet[0])
	if packetMode == modeCeltOnly {
		return 0
	}
	packetFrameSize := opusPacketGetSamplesPerFrame(packet[0], 48000)
	if packetFrameSize > 960 {
		nbFrames = packetFrameSize / 960
	}
	packetStreamChannels := opusPacketGetNbChannels(packet[0])
	ret := opusPacketParse(packet, nil, frames[:], size[:], nil)
	if ret <= 0 {
		return ret
	}
	if size[0] == 0 {
		return 0
	}
	lbrr := int(frames[0][0]>>(7-nbFrames)) & 0x1
	if packetStreamChannels == 2 {
		if lbrr != 0 || (int(frames[0][0]>>(6-2*nbFrames))&0x1) != 0 {
			lbrr = 1
		} else {
			lbrr = 0
		}
	}
	return lbrr
}

// opusPacketParseImpl ports opus_packet_parse_impl.
//
// data is the whole packet (len(data) is the C len). The optional outputs
// may be nil, like the NULL pointers of the C code: outTOC receives the TOC
// byte, frames receives a slice of data for each frame, payloadOffset the
// offset of the first frame, packetOffset the total number of bytes used by
// this packet (including padding), and padding the padding bytes (its
// length is the C padding_len). size is required and must have room for 48
// values. It returns the number of frames or a negative error code.
// preserve the reference packet grammar and sample-boundary decisions
//
//revive:disable-next-line:cognitive-complexity
func opusPacketParseImpl(data []byte, selfDelimited bool, outTOC *byte,
	frames [][]byte, size []int16, payloadOffset *int, packetOffset *int32,
	padding *[]byte,
) int {
	var count int
	cbr := false
	var pad int32
	pos := 0 // offset of the C data pointer into data

	// Make sure we return NULL/0 on error.
	if padding != nil {
		*padding = nil
	}

	if size == nil {
		return int(ErrBadArg)
	}
	length := int32(len(data))
	if length == 0 {
		return int(ErrInvalidPacket)
	}

	framesize := opusPacketGetSamplesPerFrame(data[0], 48000)

	toc := data[pos]
	pos++
	length--
	lastSize := length
	switch toc & 0x3 {
	case 0:
		// One frame.
		count = 1
	case 1:
		// Two CBR frames.
		count = 2
		cbr = true
		if !selfDelimited {
			if length&0x1 != 0 {
				return int(ErrInvalidPacket)
			}
			lastSize = length / 2
			// If lastSize doesn't fit in size[0], we'll catch it later.
			size[0] = int16(lastSize)
		}
	case 2:
		// Two VBR frames.
		count = 2
		bytes := parseSize(data[pos:], length, &size[0])
		length -= int32(bytes)
		if size[0] < 0 || int32(size[0]) > length {
			return int(ErrInvalidPacket)
		}
		pos += bytes
		lastSize = length - int32(size[0])
	default:
		// Multiple CBR/VBR frames (from 0 to 120 ms).
		if length < 1 {
			return int(ErrInvalidPacket)
		}
		// Number of frames encoded in bits 0 to 5.
		ch := data[pos]
		pos++
		count = int(ch & 0x3F)
		if count <= 0 || framesize*int32(count) > 5760 {
			return int(ErrInvalidPacket)
		}
		length--
		// Padding flag is bit 6.
		if ch&0x40 != 0 {
			for {
				if length <= 0 {
					return int(ErrInvalidPacket)
				}
				p := int32(data[pos])
				pos++
				length--
				tmp := p
				if p == 255 {
					tmp = 254
				}
				length -= tmp
				pad += tmp
				if p != 255 {
					break
				}
			}
		}
		if length < 0 {
			return int(ErrInvalidPacket)
		}
		// VBR flag is bit 7.
		cbr = ch&0x80 == 0
		if !cbr {
			// VBR case.
			lastSize = length
			for i := 0; i < count-1; i++ {
				bytes := parseSize(data[pos:], length, &size[i])
				length -= int32(bytes)
				if size[i] < 0 || int32(size[i]) > length {
					return int(ErrInvalidPacket)
				}
				pos += bytes
				lastSize -= int32(bytes) + int32(size[i])
			}
			if lastSize < 0 {
				return int(ErrInvalidPacket)
			}
		} else if !selfDelimited {
			// CBR case.
			lastSize = length / int32(count)
			if lastSize*int32(count) != length {
				return int(ErrInvalidPacket)
			}
			for i := 0; i < count-1; i++ {
				size[i] = int16(lastSize)
			}
		}
	}
	// Self-delimited framing has an extra size for the last frame.
	if selfDelimited {
		bytes := parseSize(data[pos:], length, &size[count-1])
		length -= int32(bytes)
		if size[count-1] < 0 || int32(size[count-1]) > length {
			return int(ErrInvalidPacket)
		}
		pos += bytes
		// For CBR packets, apply the size to all the frames.
		if cbr {
			if int32(size[count-1])*int32(count) > length {
				return int(ErrInvalidPacket)
			}
			for i := 0; i < count-1; i++ {
				size[i] = size[count-1]
			}
		} else if int32(bytes)+int32(size[count-1]) > lastSize {
			return int(ErrInvalidPacket)
		}
	} else {
		// Because it's not encoded explicitly, it's possible the size of
		// the last packet (or all the packets, for the CBR case) is larger
		// than 1275. Reject them here.
		if lastSize > 1275 {
			return int(ErrInvalidPacket)
		}
		size[count-1] = int16(lastSize)
	}

	if payloadOffset != nil {
		*payloadOffset = pos
	}

	for i := 0; i < count; i++ {
		end := pos + int(size[i])
		if frames != nil {
			frames[i] = data[pos:end]
		}
		pos = end
	}

	if padding != nil {
		end := pos + int(pad)
		*padding = data[pos:end]
	}
	if packetOffset != nil {
		*packetOffset = pad + int32(pos)
	}

	if outTOC != nil {
		*outTOC = toc
	}

	return count
}

// opusPacketParse ports opus_packet_parse.
func opusPacketParse(data []byte, outTOC *byte, frames [][]byte, size []int16,
	payloadOffset *int,
) int {
	return opusPacketParseImpl(data, false, outTOC, frames, size,
		payloadOffset, nil, nil)
}

// PacketParse parses an Opus packet into one or more frames. It returns the
// TOC byte, the frames (slices of data), and the offset of the first frame
// within data. It ports opus_packet_parse.
func PacketParse(data []byte) (toc byte, frames [][]byte, payloadOffset int, err error) {
	var f [48][]byte
	var size [48]int16
	ret := opusPacketParse(data, &toc, f[:], size[:], &payloadOffset)
	if ret < 0 {
		return 0, nil, 0, Error(ret)
	}
	frames = make([][]byte, ret)
	copy(frames, f[:ret])
	return toc, frames, payloadOffset, nil
}

// PacketGetBandwidth returns the bandwidth (one of the Bandwidth constants)
// of an Opus packet. It ports opus_packet_get_bandwidth.
func PacketGetBandwidth(data []byte) (int, error) {
	if len(data) < 1 {
		return 0, ErrBadArg
	}
	return opusPacketGetBandwidth(data[0]), nil
}

// PacketGetSamplesPerFrame returns the number of samples per frame of an
// Opus packet at sampling rate fs. It ports
// opus_packet_get_samples_per_frame.
func PacketGetSamplesPerFrame(data []byte, fs int) (int, error) {
	if len(data) < 1 {
		return 0, ErrBadArg
	}
	return int(opusPacketGetSamplesPerFrame(data[0], int32(fs))), nil
}

// PacketGetNbChannels returns the number of channels coded in an Opus
// packet. It ports opus_packet_get_nb_channels.
func PacketGetNbChannels(data []byte) (int, error) {
	if len(data) < 1 {
		return 0, ErrBadArg
	}
	return opusPacketGetNbChannels(data[0]), nil
}

// PacketGetNbFrames returns the number of frames in an Opus packet. It
// ports opus_packet_get_nb_frames.
func PacketGetNbFrames(packet []byte) (int, error) {
	ret := opusPacketGetNbFrames(packet)
	return max(ret, 0), errorFromCode(ret)
}

// PacketGetNbSamples returns the number of samples of an Opus packet at
// sampling rate fs. It ports opus_packet_get_nb_samples.
func PacketGetNbSamples(packet []byte, fs int) (int, error) {
	ret := opusPacketGetNbSamples(packet, int32(fs))
	return max(ret, 0), errorFromCode(ret)
}

// PacketHasLBRR reports whether an Opus packet carries SILK LBRR
// (in-band FEC) data. It ports opus_packet_has_lbrr.
func PacketHasLBRR(packet []byte) (bool, error) {
	ret := opusPacketHasLBRR(packet)
	return ret > 0, errorFromCode(ret)
}

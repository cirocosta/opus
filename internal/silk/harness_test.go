package silk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/cirocosta/opus/internal/entcode"
)

// This file holds the test harness that mirrors the C golden driver
// (tools/oracle/silk/dec.c, see testdata/README). Both decode the SILK
// part of every SILK-only and hybrid Opus frame of a stream with
// silk_Decode, optionally simulating packet loss, and print one line per
// silk_Decode call.

// opusFrame is one Opus frame and the TOC byte of its packet.
type opusFrame struct {
	toc  byte
	data []byte
}

// parseSize reads a frame size (RFC 6716 section 3.2.1).
func parseSize(data []byte) (size, n int, err error) {
	if len(data) < 1 {
		return 0, 0, errors.New("short size")
	}
	if data[0] < 252 {
		return int(data[0]), 1, nil
	}
	if len(data) < 2 {
		return 0, 0, errors.New("short size")
	}
	return 4*int(data[1]) + int(data[0]), 2, nil
}

// parsePacket splits an Opus packet into frames like opus_packet_parse
// (not self-delimited).
func parsePacket(data []byte) (toc byte, frames [][]byte, err error) {
	if len(data) < 1 {
		return 0, nil, errors.New("empty packet")
	}
	toc = data[0]
	p := data[1:]
	switch toc & 3 {
	case 0:
		frames = append(frames, p)
	case 1:
		if len(p)%2 != 0 {
			return 0, nil, errors.New("odd code 1 packet")
		}
		frames = append(frames, p[:len(p)/2], p[len(p)/2:])
	case 2:
		sz, n, err := parseSize(p)
		if err != nil || sz > len(p)-n {
			return 0, nil, errors.New("bad code 2 packet")
		}
		frames = append(frames, p[n:n+sz], p[n+sz:])
	case 3:
		if len(p) < 1 {
			return 0, nil, errors.New("short code 3 packet")
		}
		ch := p[0]
		p = p[1:]
		count := int(ch & 0x3F)
		if count <= 0 {
			return 0, nil, errors.New("zero frames")
		}
		pad := 0
		if ch&0x40 != 0 {
			for {
				if len(p) < 1 {
					return 0, nil, errors.New("bad padding")
				}
				v := int(p[0])
				p = p[1:]
				if v == 255 {
					pad += 254
				} else {
					pad += v
					break
				}
			}
		}
		if pad > len(p) {
			return 0, nil, errors.New("bad padding")
		}
		p = p[:len(p)-pad]
		if ch&0x80 != 0 {
			sizes := make([]int, count)
			total := 0
			for i := 0; i < count-1; i++ {
				sz, n, err := parseSize(p)
				if err != nil {
					return 0, nil, err
				}
				p = p[n:]
				sizes[i] = sz
				total += sz
			}
			if total > len(p) {
				return 0, nil, errors.New("bad vbr sizes")
			}
			sizes[count-1] = len(p) - total
			for _, sz := range sizes {
				frames = append(frames, p[:sz])
				p = p[sz:]
			}
		} else {
			if len(p)%count != 0 {
				return 0, nil, errors.New("bad cbr size")
			}
			sz := len(p) / count
			for i := 0; i < count; i++ {
				frames = append(frames, p[i*sz:(i+1)*sz])
			}
		}
	}
	for _, f := range frames {
		if len(f) > 1275 {
			return 0, nil, errors.New("frame too large")
		}
	}
	return toc, frames, nil
}

// readVector reads an RFC 8251 .bit file (4-byte BE length, 4-byte BE
// final range, payload) and returns its Opus frames.
func readVector(path string) ([]opusFrame, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return splitStream(d), nil
}

func splitStream(d []byte) []opusFrame {
	var out []opusFrame
	for len(d) >= 8 {
		l := int(binary.BigEndian.Uint32(d))
		d = d[8:]
		if l > len(d) {
			break
		}
		pkt := d[:l]
		d = d[l:]
		if l <= 0 {
			continue
		}
		toc, frames, err := parsePacket(pkt)
		if err != nil {
			continue
		}
		for _, f := range frames {
			out = append(out, opusFrame{toc: toc, data: f})
		}
	}
	return out
}

// fnv64 is FNV-1a over the float32 output samples (little-endian bits).
func fnv64(samples []float32) uint64 {
	h := uint64(14695981039346656037)
	for _, s := range samples {
		b := math.Float32bits(s)
		for k := 0; k < 4; k++ {
			h ^= uint64(byte(b >> (8 * k)))
			h *= 1099511628211
		}
	}
	return h
}

// frameDuration returns the frame duration in ms of a SILK-only or
// hybrid TOC configuration.
func frameDuration(cfg int) int {
	var dur int
	if cfg < 12 {
		dur = 10 << (cfg & 3)
	} else {
		dur = 10 << (cfg & 1)
	}
	if dur == 80 {
		dur = 60
	}
	return dur
}

// runSILK decodes frames like the C driver and calls emit for every
// silk_Decode call with the formatted line and the output samples.
func runSILK(frames []opusFrame, apiRate, apiCh, loss int, emit func(line string, out []float32)) {
	var dec Decoder
	dec.Init()
	ctl := DecControl{APISampleRate: int32(apiRate), NChannelsAPI: int32(apiCh)}
	out := make([]float32, 2*960)
	x := uint32(12345)
	prevCELT := false
	var ec entcode.Decoder
	for fi := range frames {
		toc := int(frames[fi].toc)
		cfg := toc >> 3
		if cfg >= 16 {
			prevCELT = true
			continue
		}
		if prevCELT {
			dec.Reset()
			prevCELT = false
		}
		dur := frameDuration(cfg)
		x = x*1103515245 + 12345
		lost := loss != 0 && (x>>16)%100 < 15
		lostFlag := FlagDecodeNormal
		src := frames[fi].data
		stoc := toc
		if lost {
			lostFlag = FlagPacketLost
			if loss == 2 && fi+1 < len(frames) && int(frames[fi+1].toc>>3) < 16 &&
				len(frames[fi+1].data) > 1 {
				lostFlag = FlagDecodeLBRR
				src = frames[fi+1].data
				stoc = int(frames[fi+1].toc)
			}
		} else if len(src) <= 1 {
			lostFlag = FlagPacketLost
		}
		if lostFlag != FlagPacketLost {
			scfg := stoc >> 3
			ctl.NChannelsInternal = int32((stoc>>2)&1) + 1
			switch {
			case scfg >= 12:
				ctl.InternalSampleRate = 16000
			case scfg < 4:
				ctl.InternalSampleRate = 8000
			case scfg < 8:
				ctl.InternalSampleRate = 12000
			default:
				ctl.InternalSampleRate = 16000
			}
			dur = frameDuration(scfg)
		}
		if ctl.NChannelsInternal == 0 {
			continue
		}
		ctl.PayloadSizeMs = dur
		var rd *entcode.Decoder
		if lostFlag != FlagPacketLost {
			ec.Init(src)
			rd = &ec
		}
		total := dur * apiRate / 1000
		decoded := 0
		first := true
		for decoded < total {
			n, ret := dec.Decode(&ctl, lostFlag, first, rd, out)
			var rng uint32
			if rd != nil {
				rng = rd.Rng
			}
			samples := out[:int(n)*apiCh]
			line := fmt.Sprintf(
				"%d %d %d %d %d %d %016x\n",
				fi,
				lostFlag,
				ret,
				n,
				rng,
				ctl.PrevPitchLag,
				fnv64(samples),
			)
			emit(line, samples)
			if ret != 0 || n <= 0 {
				break
			}
			decoded += int(n)
			first = false
		}
	}
}

package opus

import (
	"fmt"
	"testing"
)

type benchmarkConfig struct {
	name                 string
	rate, channels, mode int
	bitrate, complexity  int
	fec                  bool
}

var benchmarkConfigs = []benchmarkConfig{
	{
		name:       "silk8_mono",
		rate:       8000,
		channels:   1,
		mode:       ModeSilkOnly,
		bitrate:    12000,
		complexity: 10,
	},
	{
		name:       "silk16_mono",
		rate:       16000,
		channels:   1,
		mode:       ModeSilkOnly,
		bitrate:    20000,
		complexity: 10,
	},
	{
		name:       "voip24_fec",
		rate:       24000,
		channels:   1,
		mode:       Auto,
		bitrate:    24000,
		complexity: 9,
		fec:        true,
	},
	{
		name:       "hybrid48_stereo",
		rate:       48000,
		channels:   2,
		mode:       ModeHybrid,
		bitrate:    48000,
		complexity: 10,
	},
	{
		name:       "celt48_mono",
		rate:       48000,
		channels:   1,
		mode:       ModeCeltOnly,
		bitrate:    64000,
		complexity: 10,
	},
	{
		name:       "celt48_stereo",
		rate:       48000,
		channels:   2,
		mode:       ModeCeltOnly,
		bitrate:    128000,
		complexity: 10,
	},
}

func benchmarkEncoder(b *testing.B, c benchmarkConfig) (*Encoder, [][]int16) {
	b.Helper()
	enc, err := NewEncoder(c.rate, c.channels, ApplicationAudio)
	if err != nil {
		b.Fatal(err)
	}
	for _, err := range []error{enc.SetBitrate(c.bitrate), enc.SetComplexity(c.complexity), enc.SetForceMode(c.mode)} {
		if err != nil {
			b.Fatal(err)
		}
	}
	if c.fec {
		if err := enc.SetInbandFEC(1); err != nil {
			b.Fatal(err)
		}
		if err := enc.SetPacketLossPerc(5); err != nil {
			b.Fatal(err)
		}
	}
	x, err := encExcerpt()
	if err != nil {
		b.Fatal(err)
	}
	frames := make([][]int16, 64)
	n := c.rate / 50 * c.channels
	for f := range frames {
		frames[f] = make([]int16, n)
		for i := range frames[f] {
			sample := (f*(c.rate/50) + i/c.channels) * (48000 / c.rate) * 2 % len(x)
			if c.channels == 1 {
				frames[f][i] = int16((int32(x[sample]) + int32(x[sample+1])) / 2)
			} else {
				frames[f][i] = x[sample+i%c.channels]
			}
		}
	}
	return enc, frames
}

func BenchmarkEncode(b *testing.B) {
	for _, c := range benchmarkConfigs {
		b.Run(c.name, func(b *testing.B) {
			enc, frames := benchmarkEncoder(b, c)
			packet := make([]byte, 1500)
			for _, frame := range frames {
				if _, err := enc.Encode(frame, c.rate/50, packet); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(frames[0]) * 2))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := enc.Encode(frames[i&63], c.rate/50, packet); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, c := range benchmarkConfigs {
		b.Run(c.name, func(b *testing.B) {
			enc, frames := benchmarkEncoder(b, c)
			packets := make([][]byte, len(frames))
			for i, frame := range frames {
				packet := make([]byte, 1500)
				n, err := enc.Encode(frame, c.rate/50, packet)
				if err != nil {
					b.Fatal(err)
				}
				packets[i] = packet[:n]
			}
			dec, err := NewDecoder(c.rate, c.channels)
			if err != nil {
				b.Fatal(err)
			}
			pcm := make([]float32, len(frames[0]))
			b.ReportAllocs()
			b.SetBytes(int64(len(pcm) * 4))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := dec.DecodeFloat(packets[i&63], pcm, c.rate/50, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNew(b *testing.B) {
	for _, channels := range []int{1, 2} {
		b.Run(fmt.Sprintf("encoder_%d", channels), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := NewEncoder(48000, channels, ApplicationAudio); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

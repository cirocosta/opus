package opus_test

import (
	"fmt"
	"math"

	"github.com/cirocosta/opus"
)

// Example encodes one second of a 440 Hz sine wave at 48 kHz and decodes
// it again.
func Example() {
	const (
		fs        = 48000
		channels  = 1
		frameSize = fs / 50 // 20 ms
	)
	enc, err := opus.NewEncoder(fs, channels, opus.ApplicationAudio)
	if err != nil {
		panic(err)
	}
	if err := enc.SetBitrate(32000); err != nil {
		panic(err)
	}
	dec, err := opus.NewDecoder(fs, channels)
	if err != nil {
		panic(err)
	}

	pcm := make([]int16, frameSize*channels)
	out := make([]int16, 5760*channels) // room for a 120 ms packet
	packet := make([]byte, 4000)
	packets, samples := 0, 0
	for f := 0; f < 50; f++ {
		for i := range pcm {
			t := float64(f*frameSize+i) / fs
			pcm[i] = int16(10000 * math.Sin(2*math.Pi*440*t))
		}
		n, err := enc.Encode(pcm, frameSize, packet)
		if err != nil {
			panic(err)
		}
		packets++
		got, err := dec.Decode(packet[:n], out, len(out)/channels, false)
		if err != nil {
			panic(err)
		}
		samples += got
		if dec.FinalRange() != enc.FinalRange() {
			panic("range coder mismatch")
		}
	}
	fmt.Println(packets, "packets,", samples, "samples")
	// Output: 50 packets, 48000 samples
}

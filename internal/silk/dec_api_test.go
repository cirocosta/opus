package silk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cirocosta/opus/internal/entcode"
)

// TestDecodeErrors checks the silk_Decode argument errors.
func TestDecodeErrors(t *testing.T) {
	var dec Decoder
	dec.Init()
	out := make([]float32, 2*960)
	var ec entcode.Decoder
	ec.Init([]byte{0, 0, 0, 0})
	cases := []struct {
		ctl  DecControl
		want int
	}{
		{DecControl{NChannelsAPI: 1, NChannelsInternal: 1, APISampleRate: 48000, InternalSampleRate: 16000, PayloadSizeMs: 30}, DecInvalidFrameSize},
		{DecControl{NChannelsAPI: 1, NChannelsInternal: 1, APISampleRate: 48000, InternalSampleRate: 24000, PayloadSizeMs: 20}, DecInvalidSamplingFrequency},
		{DecControl{NChannelsAPI: 1, NChannelsInternal: 1, APISampleRate: 96000, InternalSampleRate: 16000, PayloadSizeMs: 20}, DecInvalidSamplingFrequency},
	}
	for i, c := range cases {
		if _, ret := dec.Decode(&c.ctl, FlagDecodeNormal, true, &ec, out); ret != c.want {
			t.Errorf("case %d: got %d, want %d", i, ret, c.want)
		}
	}
	// Packet loss with payloadSize_ms == 0 conceals 10 ms.
	ctl := DecControl{NChannelsAPI: 1, NChannelsInternal: 1, APISampleRate: 16000, InternalSampleRate: 16000}
	dec.Init()
	if n, ret := dec.Decode(&ctl, FlagPacketLost, true, nil, out); ret != NoError || n != 160 {
		t.Errorf("PLC with payloadSize_ms 0: got n=%d ret=%d", n, ret)
	}
}

// TestDecodeAllocs checks that Decode does not allocate.
func TestDecodeAllocs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "silk_mix.bit"))
	if err != nil {
		t.Fatal(err)
	}
	frames := splitStream(data)
	var dec Decoder
	dec.Init()
	out := make([]float32, 2*960)
	var ec entcode.Decoder
	i := 0
	allocs := testing.AllocsPerRun(200, func() {
		f := frames[i%len(frames)]
		i++
		cfg := int(f.toc >> 3)
		if cfg >= 16 || len(f.data) <= 1 {
			return
		}
		rate := int32(16000)
		if cfg < 4 {
			rate = 8000
		} else if cfg < 8 {
			rate = 12000
		}
		ctl := DecControl{NChannelsAPI: 2, NChannelsInternal: int32(f.toc>>2&1) + 1, APISampleRate: 48000,
			InternalSampleRate: rate, PayloadSizeMs: frameDuration(cfg)}
		ec.Init(f.data)
		dec.Decode(&ctl, FlagDecodeNormal, true, &ec, out)
		dec.Decode(&ctl, FlagPacketLost, true, nil, out)
	})
	if allocs != 0 {
		t.Errorf("Decode allocates %v times per call", allocs)
	}
}

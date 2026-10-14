package opus

import "testing"

// TestEncoderAllocs checks that Encode does not allocate in steady state.
func TestEncoderAllocs(t *testing.T) {
	x, err := encExcerpt()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{
		"t=enc fs=24000 ch=1 app=2048 fr=480 fec=1 loss=5",
		"t=enc fs=48000 ch=2 app=2049 fr=960 br=24000",
		"t=enc fs=48000 ch=2 app=2049 fr=2880 br=64000 vbr=0",
		"t=enc fs=16000 ch=1 app=2048 fr=1920 dtx=1",
		"t=enc fs=48000 ch=1 app=2048 fr=960 loss=10 br=32000",
		"t=enc fs=48000 ch=1 app=2048 fr=2880 loss=10 br=32000 mode=1002",
		"t=enc fs=48000 ch=2 app=2051 fr=240 api=f",
		"t=enc fs=48000 ch=2 app=2049 fr=960 api=24 cx=10",
		"t=sur fs=48000 ch=6 family=1 app=2049 fr=960 br=256000",
		"t=proj fs=48000 ch=9 family=3 app=2049 fr=960 br=192000 api=24",
	} {
		c, err := parseEncCase(spec + " n=1")
		if err != nil {
			t.Fatal(err)
		}
		var r encRunner
		switch c.typ {
		case "enc":
			r.enc, err = NewEncoder(c.fs, c.ch, c.app)
		case "sur":
			r.ms, err = NewMSSurroundEncoder(c.fs, c.ch, c.family, c.app)
		case "proj":
			r.proj, err = NewProjectionAmbisonicsEncoder(c.fs, c.ch, c.family, c.app)
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, ct := range c.ctls {
			r.ctl(ct.key, ct.val)
		}
		n := c.fr * c.ch
		pcm := make([]int16, n)
		p24 := make([]int32, n)
		pf := make([]float32, n)
		data := make([]byte, 4000)
		pos := 0
		encode := func() {
			for i := range pcm {
				pcm[i] = x[(pos+i)%len(x)]
				p24[i] = int32(pcm[i]) << 8
				pf[i] = float32(pcm[i]) / 32768
			}
			pos += n
			var err error
			switch {
			case r.enc != nil && c.api == "24":
				_, err = r.enc.Encode24(p24, c.fr, data)
			case r.enc != nil && c.api == "f":
				_, err = r.enc.EncodeFloat(pf, c.fr, data)
			case r.enc != nil:
				_, err = r.enc.Encode(pcm, c.fr, data)
			case r.ms != nil:
				_, err = r.ms.Encode(pcm, c.fr, data)
			default:
				_, err = r.proj.Encode24(p24, c.fr, data)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 20; i++ {
			encode()
		}
		if a := testing.AllocsPerRun(30, encode); a != 0 {
			t.Errorf("%s: %v allocs per Encode", spec, a)
		}
	}
}

func TestEncoderAPI(t *testing.T) {
	if _, err := NewEncoder(44100, 1, ApplicationAudio); err != ErrBadArg {
		t.Errorf("NewEncoder(44100): %v", err)
	}
	enc, err := NewEncoder(48000, 2, ApplicationVoIP)
	if err != nil {
		t.Fatal(err)
	}
	if enc.Channels() != 2 || enc.SampleRate() != 48000 || enc.Lookahead() != 120+192 {
		t.Errorf("Channels %d SampleRate %d Lookahead %d", enc.Channels(), enc.SampleRate(), enc.Lookahead())
	}
	if _, err := enc.Encode(make([]int16, 959*2), 960, make([]byte, 100)); err != ErrBadArg {
		t.Errorf("short pcm: %v", err)
	}
	if _, err := enc.Encode(make([]int16, 960*2), 960, nil); err != ErrBadArg {
		t.Errorf("nil data: %v", err)
	}
	if err := enc.SetForceMode(ModeCeltOnly); err != nil {
		t.Error(err)
	}

	ms, err := NewMSSurroundEncoder(48000, 6, 1, ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	if ms.Streams() != 4 || ms.CoupledStreams() != 2 || string(ms.Mapping()) != "\x00\x04\x01\x02\x03\x05" {
		t.Errorf("layout %d %d %v", ms.Streams(), ms.CoupledStreams(), ms.Mapping())
	}
	if e, err := ms.EncoderState(3); err != nil || e != &ms.encoders[3] || e.lfe != 1 {
		t.Errorf("EncoderState(3): %v", err)
	}
	if _, err := ms.EncoderState(4); err != ErrBadArg {
		t.Errorf("EncoderState(4): %v", err)
	}
	if ms.Channels() != 6 {
		t.Errorf("Channels %d", ms.Channels())
	}
	if _, err := ms.Encode(make([]int16, 960*5), 960, make([]byte, 1000)); err != ErrBadArg {
		t.Errorf("short pcm: %v", err)
	}

	pe, err := NewProjectionAmbisonicsEncoder(48000, 4, 3, ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	if err := pe.DemixingMatrix(make([]byte, 3)); err != ErrBadArg {
		t.Errorf("DemixingMatrix(short): %v", err)
	}
	if _, err := NewProjectionAmbisonicsEncoder(48000, 5, 3, ApplicationAudio); err != ErrAllocFail {
		t.Errorf("projection 5 channels: %v", err)
	}
}

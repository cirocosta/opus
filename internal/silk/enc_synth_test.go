package silk

// This file holds the deterministic test input for the encoder tests: a
// synthetic speech-like stereo signal built with integer arithmetic only
// (so that it does not depend on math.Sin and friends, which may compile
// with FMA), and a pseudo-random per-packet control schedule. The C
// golden driver (tools/oracle/silk/enc.c) reads the same inputs, written
// by TestWriteEncGoldenInputs.

// lcg is a 32-bit linear congruential generator.
type lcg uint32

func (r *lcg) next() uint32 {
	*r = lcg(uint32(*r)*1664525 + 1013904223)
	return uint32(*r)
}

// intn returns a value in [0, n).
func (r *lcg) intn(n int) int {
	return int((uint64(r.next()>>8) * uint64(n)) >> 24)
}

// formantSets holds two resonators (2 r cos(w), r^2 in Q14) per vowel at
// 48 kHz.
var formantSets = [][4]int64{
	{32427, 16192, 32181, 16129},
	{32555, 16192, 31062, 16129},
	{32497, 16192, 31573, 16129},
	{32550, 16192, 32301, 16129},
	{32454, 16192, 31691, 16129},
	{32531, 16192, 31404, 16129},
}

// synthSegment fills l and r (48 kHz) with one segment of the given
// kind.
func synthSegment(rng *lcg, kind int, l, r []int64) {
	n := len(l)
	switch kind {
	case 0, 1, 2: // voiced speech, with a vowel change halfway
		period := int64(100 + rng.intn(500)) // 80..480 Hz
		drift := int64(rng.intn(5)) - 2
		fs := formantSets[rng.intn(len(formantSets))]
		var y1a, y2a, y1b, y2b int64
		phase := int64(0)
		for i := 0; i < n; i++ {
			if i == n/2 {
				fs = formantSets[rng.intn(len(formantSets))]
			}
			var x int64
			if phase == 0 {
				x = 4000
			}
			x += int64(rng.next()>>24) - 128
			phase++
			if phase >= period {
				phase = 0
				if i&1023 < 512 {
					period += drift
				}
				if period < 60 {
					period = 60
				} else if period > 700 {
					period = 700
				}
			}
			ya := x + (fs[0]*y1a-fs[1]*y2a)>>14
			y2a, y1a = y1a, ya
			yb := ya + (fs[2]*y1b-fs[3]*y2b)>>14
			y2b, y1b = y1b, yb
			l[i] = yb
		}
	case 3: // unvoiced, high-passed noise
		prev := int64(0)
		for i := 0; i < n; i++ {
			x := int64(rng.next()>>16) - 32768
			l[i] = x - prev
			prev = x
		}
	case 4: // near silence
		for i := 0; i < n; i++ {
			l[i] = int64(rng.next()>>29) - 4
		}
	case 5: // music-like: three integer triangle waves
		var p [3]int64
		for k := range p {
			p[k] = int64(40 + rng.intn(400))
		}
		for i := 0; i < n; i++ {
			s := int64(0)
			for k := range p {
				ph := int64(i) % (2 * p[k])
				if ph >= p[k] {
					ph = 2*p[k] - ph
				}
				s += (ph*2 - p[k]) * 1000 / p[k]
			}
			l[i] = s
		}
	}

	// Stereo image.
	mode := rng.intn(5)
	delay := rng.intn(40)
	gain := int64(rng.intn(1024))
	for i := 0; i < n; i++ {
		switch mode {
		case 0: // identical
			r[i] = l[i]
		case 1: // panned
			r[i] = l[i] * gain >> 10
		case 2: // delayed and attenuated
			if i >= delay {
				r[i] = l[i-delay] * gain >> 10
			}
		case 3: // independent noise added
			r[i] = l[i]*gain>>10 + (int64(rng.next()>>20)-2048)*gain>>8
		default: // inverted
			r[i] = -l[i]
		}
	}

	// Scale to a target peak, with an attack/decay envelope.
	peak := int64(1)
	for i := 0; i < n; i++ {
		for _, v := range [2]int64{l[i], r[i]} {
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
	}
	target := int64(500 + rng.intn(31000))
	if kind == 4 {
		target = peak
	}
	ramp := int64(n / 8)
	if ramp < 1 {
		ramp = 1
	}
	for i := 0; i < n; i++ {
		env := int64(1024)
		if int64(i) < ramp {
			env = int64(i) * 1024 / ramp
		} else if int64(n-i) < ramp {
			env = int64(n-i) * 1024 / ramp
		}
		l[i] = l[i] * target / peak * env >> 10
		r[i] = r[i] * target / peak * env >> 10
	}
}

// synthPCM48 returns n stereo samples (interleaved int16) at 48 kHz.
func synthPCM48(seed uint32, n int) []int16 {
	rng := lcg(seed)
	out := make([]int16, 2*n)
	l := make([]int64, 0, 48000*2)
	r := make([]int64, 0, 48000*2)
	for pos := 0; pos < n; {
		segLen := 4800 + rng.intn(48000)
		if pos+segLen > n {
			segLen = n - pos
		}
		l, r = l[:segLen], r[:segLen]
		for i := range l {
			l[i], r[i] = 0, 0
		}
		kind := rng.intn(6)
		synthSegment(&rng, kind, l, r)
		for i := 0; i < segLen; i++ {
			out[2*(pos+i)] = clampInt16(l[i])
			out[2*(pos+i)+1] = clampInt16(r[i])
		}
		pos += segLen
	}
	return out
}

func clampInt16(v int64) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

// decimatePCM converts 48 kHz stereo interleaved PCM to rate Hz with ch
// channels, by keeping every (48000/rate)-th sample (and the left
// channel for mono).
func decimatePCM(pcm48 []int16, rate, ch int) []int16 {
	step := 48000 / rate
	n := len(pcm48) / 2 / step
	out := make([]int16, n*ch)
	for i := 0; i < n; i++ {
		for c := 0; c < ch; c++ {
			out[i*ch+c] = pcm48[2*i*step+c]
		}
	}
	return out
}

// Ported from libopus 1.6.1 src/opus_compare.c.

// Package opuscompare is a port of opus_compare, the tool that checks
// the output of an Opus decoder against the RFC 8251 test vector
// reference decodings with a pseudo-perceptual quality metric.
package opuscompare

import (
	"errors"
	"fmt"
	"math"
)

const (
	nbands      = 21
	nfreqs      = 240
	testWinSize = 480
	testWinStep = 120
	opusPi      = float32(3.14159265)
)

// bands are the bands on which the pseudo-NMR is computed (Bark-derived
// CELT bands).
var bands = [nbands + 1]int{
	0, 2, 4, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 40, 48, 56, 68, 80, 96, 120, 156, 200,
}

// Result is the outcome of a comparison.
type Result struct {
	// Pass reports whether the test vector passes (Quality >= 0).
	Pass bool
	// Quality is the Opus quality metric in percent.
	Quality float64
	// Error is the internal weighted error.
	Error float64
}

func (r Result) String() string {
	if !r.Pass {
		return fmt.Sprintf("FAILS (internal weighted error is %f)", r.Error)
	}
	return fmt.Sprintf("PASSES: Opus quality metric: %.1f %% (internal weighted error is %f)",
		r.Quality, r.Error)
}

func bandEnergy(out, ps []float32, bnds []int, nb int, in []float32, nchannels int,
	nframes, windowSz, step, downsample int) {
	window := make([]float32, windowSz)
	c := make([]float32, windowSz)
	s := make([]float32, windowSz)
	x := make([]float32, nchannels*windowSz)
	psSz := windowSz / 2
	for xj := 0; xj < windowSz; xj++ {
		window[xj] = 0.5 - float32(0.5*float32(math.Cos(float64((2*opusPi/float32(windowSz-1))*float32(xj)))))
	}
	for xj := 0; xj < windowSz; xj++ {
		c[xj] = float32(math.Cos(float64((2 * opusPi / float32(windowSz)) * float32(xj))))
	}
	for xj := 0; xj < windowSz; xj++ {
		s[xj] = float32(math.Sin(float64((2 * opusPi / float32(windowSz)) * float32(xj))))
	}
	for xi := 0; xi < nframes; xi++ {
		for ci := 0; ci < nchannels; ci++ {
			for xk := 0; xk < windowSz; xk++ {
				x[ci*windowSz+xk] = window[xk] * in[(xi*step+xk)*nchannels+ci]
			}
		}
		xj := 0
		for bi := 0; bi < nb; bi++ {
			var p [2]float32
			for ; xj < bnds[bi+1]; xj++ {
				for ci := 0; ci < nchannels; ci++ {
					var re, im float32
					ti := 0
					for xk := 0; xk < windowSz; xk++ {
						re += float32(c[ti] * x[ci*windowSz+xk])
						im -= float32(s[ti] * x[ci*windowSz+xk])
						ti += xj
						if ti >= windowSz {
							ti -= windowSz
						}
					}
					re *= float32(downsample)
					im *= float32(downsample)
					v := float32(re*re) + float32(im*im) + 100000
					ps[(xi*psSz+xj)*nchannels+ci] = v
					p[ci] += v
				}
			}
			if out != nil {
				out[(xi*nb+bi)*nchannels] = p[0] / float32(bnds[bi+1]-bnds[bi])
				if nchannels == 2 {
					out[(xi*nb+bi)*nchannels+1] = p[1] / float32(bnds[bi+1]-bnds[bi])
				}
			}
		}
	}
}

// Compare runs opus_compare on ref, the reference decoding (always
// stereo 48 kHz, as in the .dec files), and test, the decoder output
// with stereo (2) or mono (1) channels at rate Hz. It is
// "opus_compare [-s] -r rate ref test".
func Compare(ref, test []int16, stereo bool, rate int) (Result, error) {
	nchannels := 1
	if stereo {
		nchannels = 2
	}
	ybands := nbands
	yfreqs := nfreqs
	downsample := 1
	switch rate {
	case 8000:
		ybands = 13
	case 12000:
		ybands = 15
	case 16000:
		ybands = 17
	case 24000:
		ybands = 19
	case 48000:
	default:
		return Result{}, errors.New("opuscompare: sampling rate must be 8000, 12000, 16000, 24000, or 48000")
	}
	downsample = 48000 / rate
	yfreqs = nfreqs / downsample

	xlength := len(ref) / 2
	x := make([]float32, len(ref))
	for i, v := range ref[:2*xlength] {
		x[i] = float32(v)
	}
	if nchannels == 1 {
		for xi := 0; xi < xlength; xi++ {
			x[xi] = float32(.5 * (float64(x[2*xi]) + float64(x[2*xi+1])))
		}
	}
	ylength := len(test) / nchannels
	y := make([]float32, ylength*nchannels)
	for i := range y {
		y[i] = float32(test[i])
	}
	if xlength != ylength*downsample {
		return Result{}, fmt.Errorf("opuscompare: sample counts do not match (%d!=%d)",
			xlength, ylength*downsample)
	}
	if xlength < testWinSize {
		return Result{}, fmt.Errorf("opuscompare: insufficient sample data (%d<%d)",
			xlength, testWinSize)
	}
	nframes := (xlength - testWinSize + testWinStep) / testWinStep
	xb := make([]float32, nframes*nbands*nchannels)
	X := make([]float32, nframes*nfreqs*nchannels)
	Y := make([]float32, nframes*yfreqs*nchannels)
	// Compute the per-band spectral energy of the original signal and
	// the error.
	bandEnergy(xb, X, bands[:], nbands, x, nchannels, nframes, testWinSize, testWinStep, 1)
	bandEnergy(nil, Y, bands[:], ybands, y, nchannels, nframes,
		testWinSize/downsample, testWinStep/downsample, downsample)
	for xi := 0; xi < nframes; xi++ {
		// Frequency masking (low to high): 10 dB/Bark slope.
		for bi := 1; bi < nbands; bi++ {
			for ci := 0; ci < nchannels; ci++ {
				xb[(xi*nbands+bi)*nchannels+ci] += float32(0.1 * xb[(xi*nbands+bi-1)*nchannels+ci])
			}
		}
		// Frequency masking (high to low): 15 dB/Bark slope.
		for bi := nbands - 1; bi > 0; {
			bi--
			for ci := 0; ci < nchannels; ci++ {
				xb[(xi*nbands+bi)*nchannels+ci] += float32(0.03 * xb[(xi*nbands+bi+1)*nchannels+ci])
			}
		}
		if xi > 0 {
			// Temporal masking: -3 dB/2.5ms slope.
			for bi := 0; bi < nbands; bi++ {
				for ci := 0; ci < nchannels; ci++ {
					xb[(xi*nbands+bi)*nchannels+ci] += float32(0.5 * xb[((xi-1)*nbands+bi)*nchannels+ci])
				}
			}
		}
		// Allowing some cross-talk.
		if nchannels == 2 {
			for bi := 0; bi < nbands; bi++ {
				l := xb[(xi*nbands+bi)*nchannels+0]
				r := xb[(xi*nbands+bi)*nchannels+1]
				xb[(xi*nbands+bi)*nchannels+0] += float32(0.01 * r)
				xb[(xi*nbands+bi)*nchannels+1] += float32(0.01 * l)
			}
		}

		// Apply masking.
		for bi := 0; bi < ybands; bi++ {
			for xj := bands[bi]; xj < bands[bi+1]; xj++ {
				for ci := 0; ci < nchannels; ci++ {
					X[(xi*nfreqs+xj)*nchannels+ci] += float32(0.1 * xb[(xi*nbands+bi)*nchannels+ci])
					Y[(xi*yfreqs+xj)*nchannels+ci] += float32(0.1 * xb[(xi*nbands+bi)*nchannels+ci])
				}
			}
		}
	}

	// Average of consecutive frames to make comparison slightly less
	// sensitive.
	for bi := 0; bi < ybands; bi++ {
		for xj := bands[bi]; xj < bands[bi+1]; xj++ {
			for ci := 0; ci < nchannels; ci++ {
				xtmp := X[xj*nchannels+ci]
				ytmp := Y[xj*nchannels+ci]
				for xi := 1; xi < nframes; xi++ {
					xtmp2 := X[(xi*nfreqs+xj)*nchannels+ci]
					ytmp2 := Y[(xi*yfreqs+xj)*nchannels+ci]
					X[(xi*nfreqs+xj)*nchannels+ci] += xtmp
					Y[(xi*yfreqs+xj)*nchannels+ci] += ytmp
					xtmp = xtmp2
					ytmp = ytmp2
				}
			}
		}
	}

	// If working at a lower sampling rate, don't take into account the
	// last 300 Hz to allow for different transition bands. For 12 kHz,
	// we don't skip anything, because the last band already skips 400
	// Hz.
	var maxCompare int
	switch rate {
	case 48000:
		maxCompare = bands[nbands]
	case 12000:
		maxCompare = bands[ybands]
	default:
		maxCompare = bands[ybands] - 3
	}
	var errSum float64
	for xi := 0; xi < nframes; xi++ {
		var ef float64
		for bi := 0; bi < ybands; bi++ {
			var eb float64
			for xj := bands[bi]; xj < bands[bi+1] && xj < maxCompare; xj++ {
				for ci := 0; ci < nchannels; ci++ {
					re := Y[(xi*yfreqs+xj)*nchannels+ci] / X[(xi*nfreqs+xj)*nchannels+ci]
					im := float32(float64(re) - math.Log(float64(re)) - 1)
					// Make comparison less sensitive around the
					// SILK/CELT cross-over to allow for mode freedom in
					// the filters.
					if xj >= 79 && xj <= 81 {
						im *= 0.1
					}
					if xj == 80 {
						im *= 0.1
					}
					eb += float64(im)
				}
			}
			eb /= float64((bands[bi+1] - bands[bi]) * nchannels)
			ef += float64(eb * eb)
		}
		// Using a fixed normalization value means we're willing to
		// accept slightly lower quality for lower sampling rates.
		ef /= nbands
		ef *= ef
		errSum += float64(ef * ef)
	}
	e := math.Pow(errSum/float64(nframes), 1.0/16)
	q := float32(100 * (1 - 0.5*math.Log(1+e)/math.Log(1.13)))
	return Result{Pass: q >= 0, Quality: float64(q), Error: e}, nil
}

package celt

import (
	"fmt"
	"os"
	"testing"

	"github.com/cirocosta/opus/internal/entcode"
)

// encfRand is the input generator of the C driver encf.c.
type encfRand struct{ s uint32 }

func (r *encfRand) u() uint32 {
	r.s = r.s*1664525 + 1013904223
	return r.s >> 8
}

func (r *encfRand) i() int { return int(r.u()) }

func (r *encfRand) f() float32 {
	r.s = r.s*1664525 + 1013904223
	return float32(int32(r.s)>>8) * (1.0 / 8388608)
}

func (h *fnvHash) ints(v []int) {
	for _, x := range v {
		h.i32(int32(x))
	}
}

// hashRange hashes x[c*nb+start : c*nb+end] for each channel c.
func hashRange(h *fnvHash, x []float32, C, nb, start, end int) {
	for c := 0; c < C; c++ {
		h.f32s(x[c*nb+start : c*nb+end])
	}
}

// encfCase mirrors one_case of encf.c: it runs the encoder side of the
// band energy, allocation and PVQ code on synthetic input, then decodes
// the result with the decoder side.
func encfCase(m *Mode, r *encfRand, h *fnvHash) {
	C := 1 + r.i()&1
	LM := r.i() % 4
	M := 1 << uint(LM)
	N := M * m.ShortMdctSize
	nb := m.NbEBands
	start := 0
	if r.i()%4 == 0 {
		start = 17
	}
	var end int
	if start != 0 {
		if r.i()&1 != 0 {
			end = 19
		} else {
			end = 21
		}
	} else {
		end = 13 + r.i()%9
	}
	effEnd := end
	nbytes := 4 + r.i()%300
	complexity := r.i() % 11
	isTransient := false
	if LM > 0 {
		isTransient = r.i()&1 != 0
	}
	twoPass := r.i()&1 != 0
	forceIntra := r.i()%8 == 0
	lossRate := r.i() % 30
	lfe := r.i()%16 == 0
	disableInv := r.i()&1 != 0
	delayedIntra := float32(r.i()%64) * 0.25
	intensity := 0
	if C == 2 {
		intensity = start + r.i()%(end-start+1)
	}
	dualStereo := 0
	if C == 2 {
		dualStereo = r.i() & 1
	}
	allocTrim := r.i() % 11
	prev := r.i() % 21
	signalBandwidth := r.i() % 21

	freq := make([]float32, 2*960)
	X := make([]float32, 2*960)
	X2 := make([]float32, 2*960)
	var bandE, bandLogE, oldBandE, oldBandE2, errs, prev1, prev2 [2 * 21]float32
	var tfRes, offsets, caps, pulses, fineQuant, finePriority [21]int
	var pulses2, fineQuant2, finePriority2, spreadWeight [21]int
	var collapse, collapse2 [42]uint8
	buf := make([]byte, 1275)
	average := r.i() % 512
	hfAverage := r.i() % 32
	tapset := r.i() % 3
	lastDec := r.i() % 4
	seed := r.u()

	for c := 0; c < C; c++ {
		for i := 0; i < nb; i++ {
			kind := r.i() % 8
			amp := float32(int(1)<<uint(r.i()%16)) * (1.0 / 256)
			for j := M * int(m.EBands[i]); j < M*int(m.EBands[i+1]); j++ {
				v := r.f()
				if kind == 0 {
					v = 0
				} else if kind == 1 && j&3 != 0 {
					v = 0
				}
				freq[c*N+j] = float32(v * amp)
			}
		}
	}
	for i := 0; i < C*nb; i++ {
		oldBandE[i] = float32(int(r.i()%64)-40) * 0.5
		prev1[i] = float32(int(r.i()%64)-40) * 0.5
		prev2[i] = float32(int(r.i()%64)-40) * 0.5
	}
	if C == 1 {
		for i := 0; i < nb; i++ {
			prev1[nb+i] = prev1[i] - 1
			prev2[nb+i] = prev2[i] + 1
		}
	}
	oldBandE2 = oldBandE
	computeBandEnergies(m, freq, bandE[:], effEnd, C, LM)
	normaliseBands(m, freq, X, bandE[:], effEnd, C, M)
	amp2Log2(m, effEnd, end, bandE[:], bandLogE[:], C)
	h.f32s(bandE[:C*nb])
	h.f32s(X[:C*N])
	h.f32s(bandLogE[:C*nb])

	for i := 0; i < nb; i++ {
		spreadWeight[i] = 1 + r.i()%4
	}
	spread := spreadingDecision(
		m,
		X,
		&average,
		lastDec,
		&hfAverage,
		&tapset,
		r.i()&1 != 0,
		effEnd,
		C,
		M,
		spreadWeight[:],
	)
	h.ints([]int{spread, average, hfAverage, tapset})
	{
		thr := []float32{2, 5, 9}
		hys := []float32{.5, 1, 1}
		v := float32(r.i()%48) * 0.25
		h.i32(int32(hysteresisDecision(v, thr, hys, 3, r.i()%4)))
	}

	var enc entcode.Encoder
	enc.Init(buf[:nbytes])
	quantCoarseEnergy(
		m,
		start,
		end,
		effEnd,
		bandLogE[:],
		oldBandE[:],
		uint32(nbytes*8),
		errs[:],
		&enc,
		C,
		LM,
		nbytes,
		forceIntra,
		&delayedIntra,
		twoPass,
		lossRate,
		lfe,
	)
	h.f32s(oldBandE[:C*nb])
	hashRange(h, errs[:], C, nb, start, end)
	h.f32s([]float32{delayedIntra})
	h.u32(enc.TellFrac())

	for i := start; i < end; i++ {
		raw := r.i() & 1
		sel := r.i() & 1
		tfRes[i] = int(tfSelectTable[LM][4*b2i(isTransient)+2*sel+raw])
	}
	initCaps(m, caps[:], LM, C)
	for i := 0; i < nb; i++ {
		width := C * int(m.EBands[i+1]-m.EBands[i]) << uint(LM)
		quanta := min(width<<bitRes, max(6<<bitRes, width))
		if r.i()%6 == 0 {
			offsets[i] = quanta * (1 + r.i()%3)
		} else {
			offsets[i] = 0
		}
	}
	bits := (int32(nbytes*8) << bitRes) - int32(enc.TellFrac()) - 1
	antiRsv := int32(0)
	if isTransient && LM >= 2 && bits >= int32((LM+2)<<bitRes) {
		antiRsv = 1 << bitRes
	}
	bits -= antiRsv
	var balance int32
	rc := rangeCoder{enc: &enc}
	codedBands := cltComputeAllocation(
		m,
		start,
		end,
		offsets[:],
		caps[:],
		allocTrim,
		&intensity,
		&dualStereo,
		bits,
		&balance,
		pulses[:],
		fineQuant[:],
		finePriority[:],
		C,
		LM,
		rc,
		true,
		prev,
		signalBandwidth,
	)
	h.ints([]int{codedBands, intensity, dualStereo, int(balance)})
	h.ints(pulses[:])
	h.ints(fineQuant[:])
	h.ints(finePriority[:])
	quantFineEnergy(m, start, end, oldBandE[:], errs[:], nil, fineQuant[:], &enc, C)
	h.f32s(oldBandE[:C*nb])
	hashRange(h, errs[:], C, nb, start, end)
	seed2 := seed
	var Y []float32
	if C == 2 {
		Y = X[N:]
	}
	quantAllBands(
		true,
		m,
		start,
		end,
		X,
		Y,
		collapse[:],
		bandE[:],
		pulses[:],
		isTransient,
		spread,
		dualStereo != 0,
		intensity,
		tfRes[:],
		int32(nbytes*(8<<bitRes))-antiRsv,
		balance,
		rc,
		LM,
		codedBands,
		&seed,
		complexity,
		disableInv,
	)
	h.f32s(X[:C*N])
	h.bytes(collapse[:C*nb])
	h.u32(seed)
	h.u32(enc.TellFrac())
	if antiRsv > 0 {
		antiOn := uint32(r.i() & 1)
		enc.EncBits(antiOn, 1)
	}
	quantEnergyFinalise(
		m,
		start,
		end,
		oldBandE[:],
		errs[:],
		fineQuant[:],
		finePriority[:],
		nbytes*8-enc.Tell(),
		&enc,
		C,
	)
	h.f32s(oldBandE[:C*nb])
	hashRange(h, errs[:], C, nb, start, end)
	enc.Done()
	h.bytes(buf[:nbytes])
	h.u32(enc.Rng)
	h.i32(int32(enc.Error))

	// Decode it back.
	var dec entcode.Decoder
	dec.Init(buf[:nbytes])
	{
		intra := false
		if dec.Tell()+3 <= nbytes*8 {
			intra = dec.DecBitLogp(3) != 0
		}
		unquantCoarseEnergy(m, start, end, oldBandE2[:], intra, &dec, C, LM)
	}
	h.f32s(oldBandE2[:C*nb])
	bits = (int32(nbytes*8) << bitRes) - int32(dec.TellFrac()) - 1
	bits -= antiRsv
	var intensity2, dual2 int
	var balance2 int32
	rd := rangeCoder{dec: &dec}
	codedBands2 := cltComputeAllocation(
		m,
		start,
		end,
		offsets[:],
		caps[:],
		allocTrim,
		&intensity2,
		&dual2,
		bits,
		&balance2,
		pulses2[:],
		fineQuant2[:],
		finePriority2[:],
		C,
		LM,
		rd,
		false,
		0,
		0,
	)
	h.ints([]int{codedBands2, intensity2, dual2, int(balance2)})
	h.ints(pulses2[:])
	h.ints(fineQuant2[:])
	h.ints(finePriority2[:])
	unquantFineEnergy(m, start, end, oldBandE2[:], nil, fineQuant2[:], &dec, C)
	var Y2 []float32
	if C == 2 {
		Y2 = X2[N:]
	}
	quantAllBands(
		false,
		m,
		start,
		end,
		X2,
		Y2,
		collapse2[:],
		nil,
		pulses2[:],
		isTransient,
		spread,
		dual2 != 0,
		intensity2,
		tfRes[:],
		int32(nbytes*(8<<bitRes))-antiRsv,
		balance2,
		rd,
		LM,
		codedBands2,
		&seed2,
		0,
		disableInv,
	)
	h.f32s(X2[:C*N])
	h.bytes(collapse2[:C*nb])
	h.u32(seed2)
	antiOn := false
	if antiRsv > 0 {
		antiOn = dec.DecBits(1) != 0
	}
	unquantEnergyFinalise(
		m,
		start,
		end,
		oldBandE2[:],
		fineQuant2[:],
		finePriority2[:],
		nbytes*8-dec.Tell(),
		&dec,
		C,
	)
	h.f32s(oldBandE2[:C*nb])
	if antiOn {
		antiCollapse(
			m,
			X2,
			collapse2[:],
			LM,
			C,
			N,
			start,
			end,
			oldBandE2[:],
			prev1[:],
			prev2[:],
			pulses2[:],
			seed2,
			false,
		)
		h.f32s(X2[:C*N])
	}
	h.u32(dec.Rng)
	h.u32(dec.TellFrac())
	h.i32(int32(dec.Error))
}

// TestEncodeHelpers compares the encoder side of quant_bands.c, rate.c,
// bands.c, vq.c and cwrs.c (and the matching decoder side on the result)
// with the scalar reference build on synthetic input
// (testdata/encf_golden.txt).
func TestEncodeHelpers(t *testing.T) {
	keep := os.Getenv("CELT_DUMP_REF") != ""
	m := StaticMode()
	for _, g := range readGoldens(t, "encf_golden.txt") {
		t.Run(fmt.Sprintf("seed%s", g.params["seed"]), func(t *testing.T) {
			r := &encfRand{s: uint32(g.int(t, "seed"))}
			h := newFNV(keep)
			for i := 0; i < g.int(t, "n"); i++ {
				encfCase(m, r, h)
			}
			if got := hexHash(h.h); got != g.params["hash"] {
				t.Errorf("hash = %s, want %s", got, g.params["hash"])
				compareDump(t, h.dump)
			}
		})
	}
}

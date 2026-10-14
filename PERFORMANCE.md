# Performance

This is a Go port of the default floating-point libopus 1.6.1 codec. The
optimization target is time per frame, with exact reference output and no
steady-state allocations in the benchmark matrix. The library needs no cgo.

## Measurements

Measured on 2026-10-14 UTC: Go 1.27.1, Linux amd64, Intel i9-9900K,
`GOAMD64=v1` with runtime AVX2, CPU 2 (SMT sibling 10), `GOMAXPROCS=1`.
The powersave governor was unchanged. Each binary ran six alternating
500 ms samples per case, with no concurrent builds, tests, or profiles.

Inputs are 64 reused 20 ms frames from the committed stereo PCM excerpt.
Mono cases average the channels and decimate to the required sample rate.
Encoder setup and warmup, packet generation, and buffers are outside timing.
The matrix spans SILK, hybrid, CELT, mono, stereo, and VoIP with FEC; see
[performance_test.go](performance_test.go) for rates, bitrates, and complexity.

| Operation | Workload | Original us/frame | Optimized us/frame | Time change |
| --- | --- | ---: | ---: | ---: |
| Encode | silk8_mono | 136.10 | 131.32 | -3.51% |
| Encode | silk16_mono | 293.77 | 269.41 | -8.30% |
| Encode | voip24_fec | 109.88 | 86.42 | -21.35% |
| Encode | hybrid48_stereo | 643.97 | 599.59 | -6.89% |
| Encode | celt48_mono | 145.46 | 121.22 | -16.66% |
| Encode | celt48_stereo | 274.64 | 245.96 | -10.45% |
| Decode | silk8_mono | 7.13 | 7.15 | +0.36% (noise) |
| Decode | silk16_mono | 14.27 | 13.47 | -5.65% |
| Decode | voip24_fec | 26.83 | 23.29 | -13.19% |
| Decode | hybrid48_stereo | 73.42 | 59.08 | -19.52% |
| Decode | celt48_mono | 31.08 | 28.76 | -7.45% |
| Decode | celt48_stereo | 57.20 | 53.42 | -6.61% |

The geometric mean time reduction is 10.16%. All 12 workloads report
**0 B/frame and 0 allocs/frame**, as did the original port. Medians are shown.
All reductions have `p=0.002` with six samples in `benchstat`; SILK 8 kHz
decode has no significant change (`p=0.589`). These are full codec results,
not extrapolated kernel speedups.

The original source is `ev` revision
`1ad2b5a70bdb96132675098e5c04a19c6388e912`, with the same benchmark added.
The measured optimized code is revision `154c156` (documentation changes
were pending). [Raw results and environment](tools/perf/results/2026-10-14/)
include binary hashes and the full comparison.

These results describe this input, toolchain, and processor. They do not
establish a global optimum or predict performance on another CPU. Native
arm64 execution is configured in CI; local portability checks cross-compiled
arm64, 386, macOS arm64, and Windows amd64.

## Reproduce

Use the same Go toolchain and workload for both binaries. Revision `db378c2`
is the scalar extraction before optimized kernels. Build its benchmark binary,
then return to the optimized revision and run:

```sh
tools/perf/bench.sh /absolute/path/to/scalar.test 2 /tmp/opus-bench
benchstat -ignore pkg /tmp/opus-bench/original.txt /tmp/opus-bench/current.txt
```

The script builds the candidate before timing, pins CPU 2, sets
`GOMAXPROCS=1`, and runs six alternating baseline/candidate pairs. Set
`BENCHTIME=1s` for longer samples. Keep compilation, tests, linters, profiles,
and other CPU-heavy work outside the timed runs. Inspect every case and its
allocation count; an aggregate gain does not excuse a material regression.
The `-ignore pkg` option also permits comparisons with the original `ev/opus`
package name without editing the raw measurements.

For a quick local check:

```sh
go test -run '^$' -bench '^Benchmark(Encode|Decode)$' -benchmem -count=6 .
```

## Search and implementation

CPU profiles identified SILK quantization, pitch correlation, LPC prediction,
resampling, the dense analysis network, and CELT transforms as useful targets.
The search retained exact numerical semantics: integer wrap and saturation,
per-product rounding, entropy state, and floating-point operation order.
SIMD lanes compute independent outputs. Multiply and add remain separate;
FMA and reassociated reductions can change packets.

The implementation uses:

- Runtime-guarded AVX2 pitch tiles and signed-byte matrix multiplication.
- SSE2 LPC filtering and fixed prediction, plus fused subframe synthesis and
  phase-table FIR resampling to amortize calls and coefficient loading.
- AVX2 radix-5 FFT butterflies with four independent complex butterflies.
- Register-held serial allpass feedback, conditional ring-buffer wrap, and
  copying only the live quantizer prediction history.
- A Go rotation recurrence that carries state instead of reloading it.
- Go fallbacks on other architectures and with `-tags=purego`; SSE2 remains
  the amd64 baseline when AVX2 is unavailable.

[trials.csv](tools/perf/trials.csv) records 36 numbered experiments after the
initial profile-guided kernels. Trials 1-21 sweep pitch and matrix tile widths
and unroll factors; their scores compare kernel benchmarks with the prior
SIMD implementation. Later scores compare affected codec workloads with the
current winner, rather than with the original scalar port. Ratios below 1
mean less time. These incremental scores must not be multiplied to produce
an end-to-end claim.

Each iteration built an immutable candidate, checked correctness, measured
paired runs, and retained or reverted it. Screens used a 1.5% aggregate gain
and a 4% maximum per-case regression guard, followed by confirmation. The
small Go rotation change received extra samples: CELT stereo decode improved
1.55% (`p=0.033`, 12 pairs), with no significant change in other cases. The
last FFT trial improved hybrid and CELT decode about 5% in six paired runs.
The final AVX2 entropy scan passed output checks but slowed SILK and hybrid
decoding, so it was removed. A final SSE2 paired-correlation trial slowed
SILK and hybrid encoding by 6-7% and was also removed. Other discarded trials
include MDCT fusions, quantizer table caching, AVX2 fixed prediction, and
scalar FFT specialization.

Remaining costs include serial SILK quantization and NLSF search, entropy
coding, and other FFT/MDCT passes. Future changes need a new profile and the
same full-codec correctness and performance checks.

## Correctness checks

The committed goldens cover entropy, DSP, SILK, CELT, analysis, encoder,
decoder, multistream, projection, FEC, and concealment. The full RFC 8251 run
checks all 12 vectors at five sample rates and both channel counts: entropy
state for every packet and exact PCM digests against scalar libopus. The
48 kHz output also passes the reference quality comparison. See
[oracle tooling](tools/oracle/README.md) for the reference configuration.

Assembly has differential tests for alignment, tails, extreme signed values,
overflow, saturation, cancellation, subnormals, and signed zero. Floating-point
NaN payload selection is compiler-dependent and is not an audio-domain
compatibility guarantee. Ordinary tests, `purego`, AVX2-disabled tests,
`GOAMD64=v3`, the race detector, `go vet`, and the unchanged `golangci-lint`
configuration are checked locally. Cross-builds and the configured CI matrix
cover portable code paths; native results on other processors need measurement.

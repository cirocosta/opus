# Contributing

Use the Go version in `go.mod`. Run the test suite, `go vet`, and the repository's
`golangci-lint` checks before submitting a change. CI also runs `purego`, SSE2,
arm64, race, and the RFC 8251 corpus. See `tools/oracle/README.md` for local commands.

Preserve integer overflow, saturation, per-product rounding, entropy state,
and floating-point operation order. A mathematically equivalent expression
can change packets. Test assembly against the Go implementation, including
unaligned inputs, tails, signed limits, cancellation, and subnormal values.
Use explicit buffer contracts and a portable implementation for every kernel.

Measure performance on immutable binaries. Keep compilation, test runs,
linters, and profilers outside benchmark timing. Pin a single logical CPU,
record the toolchain and machine, alternate baseline/candidate runs, and
check every workload and allocation count. Use full codec measurements to
confirm a kernel improvement. See `PERFORMANCE.md` and `tools/perf/bench.sh`.

The linter's cognitive-complexity limit remains 10. Some inherited codec
algorithms have per-function exceptions. Their branches couple entropy,
quantization, loss concealment, or numerical state; splitting them only to
reduce a score can obscure the reference operation graph. Each exception names
its rationale beside the function. New code must meet the configured limit.
Extract useful lifecycle or algorithm phases when their contracts are clear.
Do not add package-wide exceptions or change the threshold to hide failures.

New code uses the root MIT license. Preserve `LICENSES/opus-BSD-3-Clause.txt`
and source provenance for code, tables, and test material derived from libopus.

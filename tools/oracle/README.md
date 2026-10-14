# Oracle tooling

The Go port is tested against the C reference. The "oracle" is a
reference build of libopus 1.6.1 plus small C drivers that link it and
write golden files. The Go tests replay those goldens and compare
results bit for bit. This directory holds the drivers and the scripts
that build the reference and regenerate the goldens.

The directory contains no `.go` files, so the Go tooling ignores it.

## Reference configuration

The port follows the default libopus configuration (plain
`./configure`): float build, `FLOAT_APPROX` and `ENABLE_RES24` on, and no
`ENABLE_DRED`, `ENABLE_DEEP_PLC`, `ENABLE_OSCE`, `ENABLE_OSCE_BWE`,
`ENABLE_QEXT`, or `CUSTOM_MODES`. For exact float comparisons the
reference is scalar and does not fuse products:

```
./configure --disable-shared --disable-doc --disable-intrinsics \
    --disable-rtcd --disable-asm CFLAGS="-O2 -g -ffp-contract=off"
```

The generator scripts refuse a reference whose `config.h` defines one of
the DNN or other non-default options.

## Prerequisites

- `gcc` (or set `CC`), `make`
- `curl`, `tar`, `gzip`, `sha256sum`
- `python3`
- `go` and `gofmt` (the root package goldens use Go-generated inputs)

## Build the reference

```
tools/oracle/build-ref.sh <workdir>
```

The script downloads and verifies these files:

| File                               | SHA-256                                                            |
| ---------------------------------- | ------------------------------------------------------------------ |
| `opus-1.6.1.tar.gz`                | `6ffcb593207be92584df15b32466ed64bbec99109f007c82205f0194572411a1` |
| `opus_testvectors-rfc8251.tar.gz`  | `6b26a22f9ba87b2b836906a9bb7afec5f8e54d49553b1200382520ee6fedfa55` |

Then it extracts a pristine source tree, builds a second copy with the
configuration above, and prints the environment variables to export. It
skips the steps that are already done, so you can run it again.

## Environment

| Variable           | Meaning                                                          |
| ------------------ | ---------------------------------------------------------------- |
| `OPUS_SRC`         | Pristine libopus 1.6.1 source tree                               |
| `OPUS_REF`         | Built reference tree (`config.h`, `.libs/libopus.a`, `opus_demo`) |
| `OPUS_TESTVECTORS` | RFC 8251 test vector directory (`opus_newvectors`)               |
| `OPUS_WORK`        | Scratch output directory                                         |
| `OPUS_GO_DIR`      | Optional. Go module that generates inputs. Default: this `opus/` module |
| `CC`               | Optional. C compiler. Default: `gcc`                             |

The scripts find the `opus/` module from their own location. The Go
tests also read `OPUS_TESTVECTORS`; the vector tests skip when it is
unset.

## Regenerate goldens

Each covered package has a `gen.sh`:

```
tools/oracle/<pkg>/gen.sh       # generate and compare
tools/oracle/<pkg>/gen.sh -w    # generate, compare, and copy into the repository
```

A script writes its outputs below `$OPUS_WORK/<pkg>/out/`, with the same
relative paths as in the `opus/` module. Then it compares each output with
the committed file and prints `identical` or `DIFFERS`. Without `-w` it
exits with status 1 when an output differs, and the repository does not
change. Inspect the difference before you run it again with `-w`.

The scripts that need Go (`pkt`, `analysis`, `silk`, `enc`) run a Go
test to write the input records. That test must build. When the working tree does not
build, point `OPUS_GO_DIR` at a clean snapshot of the module, for example:

```
mkdir -p "$OPUS_WORK/snap"
git archive HEAD opus | tar -x -C "$OPUS_WORK/snap"
export OPUS_GO_DIR=$OPUS_WORK/snap/opus
```

## Coverage

| Package             | Directory    | Needs                    | Outputs                                                                                          | Status  |
| ------------------- | ------------ | ------------------------ | ------------------------------------------------------------------------------------------------ | ------- |
| `internal/entcode`  | `entcode/`   | SRC, REF                 | `internal/entcode/testdata/golden.txt`                                                           | covered |
| `internal/dsp`      | `dsp/`       | SRC, REF                 | `internal/dsp/static_modes.go`, `internal/dsp/testdata/*.bin`                                     | covered |
| `opus` packet layer | `pkt/`       | SRC, REF, TESTVECTORS, Go | `mapping_matrix_tables.go`, `testdata/{parse,softclip,repacketizer,pad,extensions,matrix}.txt.gz`, `testdata/vectors.txt` | covered |
| `opus` analysis     | `analysis/`  | SRC, REF, TESTVECTORS, Go | `mlp_data.go`, `testdata/analysis.txt.gz`, `testdata/analysis_vectors.txt`                        | covered |
| `internal/silk`     | `silk/`      | SRC, REF, TESTVECTORS, Go | `internal/silk/testdata/{vectors,mix,enc,enc_vectors,enc_unit}_golden.txt`, `mix_samples_16k_mono_plc.raw` | covered |
| `internal/celt`     | `celt/`      | SRC, REF, TESTVECTORS    | `internal/celt/testdata/*.pk`, `{vectors,synth,encf,enc,enc_vectors}_golden.txt`, `hybrid_v12_*.log.gz`, `enc_pcm.raw.gz` | covered |
| `opus` decoder      | `dec/`       | SRC, REF, TESTVECTORS    | `testdata/dec_mix.bit`, `testdata/{ms3,ms4i,ms51,amb4,proj4,proj9}.pk`, `testdata/decoder_golden.txt`, `testdata/decoder_vectors.txt` | covered |
| `opus` encoder      | `enc/`       | SRC, REF, TESTVECTORS, Go | `testdata/encoder_golden.txt`, `testdata/encoder_vectors.txt`                                    | covered |

### Package notes

- `entcode`: `driver.c` compiles `celt/entcode.c`, `entenc.c`,
  `entdec.c`, and `laplace.c` directly instead of linking `libopus.a`.
  `config.h` defines `ENABLE_HARDENING`, so `celt_assert` is active, and
  the driver defines the `celt_fatal` stub that `celt.c` would provide.
- `dsp`: `gen_static_modes.py` converts `celt/static_modes_float.h` into
  `static_modes.go`. The driver is built twice, with the reference
  `config.h` (`FLOAT_APPROX`) and with `FLOAT_APPROX` removed (libm
  math). The script also writes a 2^20-point math sweep to
  `$OPUS_WORK/dsp/big` (not committed) and prints the
  `DSP_GOLDEN_DIR=... go test -run Math ./internal/dsp` command that
  checks it.
- `pkt`: `TestWriteGoldenInputs` (`golden_test.go`) writes
  `<set>.bin` records; `golden.c` runs libopus on each record.
  `golden vectors` hashes the packets of the RFC test vectors.
  `gen_tables.py` converts the matrices in `src/mapping_matrix.c`.
- `analysis`: `TestWriteAnalysisSource` (`analysis_test.go`) writes
  `analysis_src.raw`; `drv.c` runs `run_analysis` for every line of
  `synth.cfg` (full output) and `vec.cfg` (one hash per vector).
  `gen_mlp.py` converts `src/mlp_data.c`.
- `silk`: `dec.c` decodes the SILK part of every SILK and hybrid frame
  with `silk_Decode`; `enc.c` runs `silk_Encode` on the scenarios that
  `TestWriteEncGoldenInputs` (`internal/silk`) writes and decodes the
  packets; `unit.c` checks encoder helpers. The drivers compile with
  `-include config.h` and refuse a DNN configuration. `silk_mix.bit` is
  a committed input. `internal/silk/testdata/README` describes the
  formats.
- `celt`: `drv.c` (CELT decoder: test vectors, `*.pk` packets of the
  reference CELT encoder, random packets), `encf.c` (shared band,
  energy, allocation and PVQ code), `hyb.c` (logs every CELT call of
  `opus_decode` with `ld --wrap`; the full set for all vectors goes to
  `$OPUS_WORK/celt/hyblogs`, which `TestHybridReplay` reads from
  `CELT_HYBRID_LOGS`), `encdrv.c` with `gen_cases.py` (CELT encoder).
- `dec`: `drv.c dec` is an `opus_demo` style decode loop with packet
  loss, FEC, PLC sizes, gain, complexity, reset and phase inversion
  options; `drv.c msdec` decodes multistream and projection streams;
  `drv.c msenc` writes the `*.pk` streams. The script also rebuilds
  `dec_mix.bit` with the reference `opus_demo` and hashes
  `opus_demo -d` of every RFC vector at every rate. Cases are in
  `cases.in`.
- `enc`: `drv.c` runs one case (tokens `t= fs= ch= app= fr= n= api= mb=`
  and controls) and prints every control result and packet.
  `gen_cases.py` writes `cases.in` and `vcases.in`; the script checks
  that the committed files match it. `TestWriteEncInputs` writes the
  input PCM. `cmp.sh <spec>` diffs the C and Go output of one case.

## Add a package

1. Create `tools/oracle/<pkg>/` with the driver sources and a `gen.sh`.
2. In `gen.sh`, source `../lib.sh` and call
   `oracle_init <pkg> "<needed variables>" "$@"`.
3. For each output, call `oracle_out <path relative to opus/>` and write
   the file to `$O`. Do not call `oracle_out` in a subshell.
4. End with `oracle_finish`.
5. Add the package to the coverage table.

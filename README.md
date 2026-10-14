# opus

An Opus encoder and decoder in Go, with no cgo or runtime libopus dependency.

[Opus](https://www.rfc-editor.org/rfc/rfc6716.html) compresses speech and music
into small audio packets. It is used for calls, streaming, and other realtime
audio applications. This library supports mono, stereo, multistream, packet
loss concealment, and in-band forward error correction.

## Example

A 20 ms frame at 48 kHz contains 960 samples per channel. This example encodes
and decodes one stereo frame. Replace the zero-filled PCM with your audio input.

```go
package main

import (
	"fmt"
	"log"

	"github.com/cirocosta/opus"
)

func main() {
	const rate, channels, frame = 48000, 2, 960
	encoder, err := opus.NewEncoder(rate, channels, opus.ApplicationAudio)
	if err != nil {
		log.Fatal(err)
	}
	decoder, err := opus.NewDecoder(rate, channels)
	if err != nil {
		log.Fatal(err)
	}

	pcm := make([]int16, frame*channels)
	packet := make([]byte, 1500)
	n, err := encoder.Encode(pcm, frame, packet)
	if err != nil {
		log.Fatal(err)
	}
	output := make([]int16, frame*channels)
	samples, err := decoder.Decode(packet[:n], output, frame, false)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d bytes; %d samples per channel\n", n, samples)
}
```

Reuse one encoder and decoder per stream. Callers own the PCM and packet
buffers. Instances need external synchronization when shared by goroutines.
The library also accepts 24-bit and float32 PCM.

## Performance and development

amd64 uses AVX2 when available, with SSE2 as the baseline. Other architectures
use Go kernels. Build with `-tags=purego` to select Go kernels everywhere.
The benchmarked encode and decode workloads allocate nothing per frame.

See [PERFORMANCE.md](PERFORMANCE.md) for benchmarks and tuning results,
[CONTRIBUTING.md](CONTRIBUTING.md) for development checks, and
[tools/oracle/README.md](tools/oracle/README.md) for reference validation.

## License

New work uses [MIT](LICENSE). Code and tables derived from libopus retain the
upstream [BSD notice](LICENSES/opus-BSD-3-Clause.txt).

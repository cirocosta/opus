package celt

import (
	"compress/gzip"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cirocosta/opus/internal/entcode"
)

// Request codes logged by the hybrid replay driver.
const (
	reqResetState      = 4028
	reqSetComplexity   = 4010
	reqSetPhaseInvDis  = 4046
	reqSetChannels     = 10008
	reqSetStartBand    = 10010
	reqSetEndBand      = 10012
	reqSetSignalling   = 10016
	hybridLogHeader    = 0
	hybridLogCtl       = 1
	hybridLogDecode    = 2
	hybridLogDecFields = 11
)

// TestHybridReplay replays every call into the CELT decoder that the
// reference opus decoder makes on the hybrid test vectors, including the
// range decoder state left by SILK, and compares the output, return value
// and final range of each call. The logs come from the driver
// tools/oracle/celt/hyb.c (ld --wrap around the CELT decoder entry
// points), which logs ec_dec.ext as 0 because ec_dec_init leaves it
// uninitialized. The test replays the compact logs in testdata
// (hybrid_vNN_*.log.gz, where NN is the vector number) and, when
// CELT_HYBRID_LOGS names a directory, the vNN*.log files in it. It needs
// OPUS_TESTVECTORS.
func TestHybridReplay(t *testing.T) {
	vecDir := vectorDir(t)
	type replayLog struct {
		name string
		load func() ([]byte, error)
	}
	var logs []replayLog
	committed, _ := filepath.Glob(filepath.Join("testdata", "hybrid_v??*.log.gz"))
	for _, p := range committed {
		logs = append(
			logs,
			replayLog{strings.TrimPrefix(filepath.Base(p), "hybrid_"), func() ([]byte, error) {
				f, err := os.Open(p)
				if err != nil {
					return nil, err
				}
				defer func() { _ = f.Close() }()
				zr, err := gzip.NewReader(f)
				if err != nil {
					return nil, err
				}
				return io.ReadAll(zr)
			}},
		)
	}
	if dir := os.Getenv("CELT_HYBRID_LOGS"); dir != "" {
		extra, _ := filepath.Glob(filepath.Join(dir, "v??*.log"))
		for _, p := range extra {
			logs = append(
				logs,
				replayLog{filepath.Base(p), func() ([]byte, error) { return os.ReadFile(p) }},
			)
		}
	}
	for _, lg := range logs {
		v := lg.name[1:3]
		t.Run(lg.name, func(t *testing.T) {
			logb, err := lg.load()
			if err != nil {
				t.Fatal(err)
			}
			vec, err := os.ReadFile(filepath.Join(vecDir, "testvector"+v+".bit"))
			if err != nil {
				t.Fatal(err)
			}
			var pkts [][]byte
			for p := 0; p < len(vec); {
				n := int(binary.BigEndian.Uint32(vec[p:]))
				pkts = append(pkts, vec[p+8:p+8+n])
				p += 8 + n
			}
			pos := 0
			rd := func() int32 {
				x := int32(binary.LittleEndian.Uint32(logb[pos:]))
				pos += 4
				return x
			}
			if rd() != hybridLogHeader {
				t.Fatal("bad log")
			}
			fs := rd()
			CC := int(rd())
			d, ret := NewDecoder(fs, CC)
			if ret != OK {
				t.Fatal("init")
			}
			pcm := make([]float32, 2*960)
			var ec entcode.Decoder
			calls, hybrid := 0, 0
			for pos < len(logb) {
				switch rd() {
				case hybridLogCtl:
					req, val := rd(), int(rd())
					switch req {
					case reqResetState:
						d.Reset()
					case reqSetComplexity:
						d.SetComplexity(val)
					case reqSetPhaseInvDis:
						d.SetPhaseInversionDisabled(val)
					case reqSetChannels:
						d.SetChannels(val)
					case reqSetStartBand:
						d.SetStartBand(val)
					case reqSetEndBand:
						d.SetEndBand(val)
					case reqSetSignalling:
						d.SetSignalling(val)
					default:
						t.Fatalf("unknown request %d", req)
					}
				case hybridLogDecode:
					idx, off, length := int(rd()), int(rd()), int(rd())
					frameSize, accum, _, hasDec := int(rd()), rd() != 0, rd(), rd() != 0
					var data []byte
					switch off {
					case -1:
					case -2:
						data = []byte{0xff, 0xff}
					default:
						data = pkts[idx-1][off : off+length]
					}
					var dec *entcode.Decoder
					if hasDec {
						var f [hybridLogDecFields]int32
						for i := range f {
							f[i] = rd()
						}
						ec.Init(data)
						ec.Storage = uint32(f[0])
						ec.EndOffs = uint32(f[1])
						ec.EndWindow = uint32(f[2])
						ec.NendBits = int(f[3])
						ec.NbitsTotal = int(f[4])
						ec.Offs = uint32(f[5])
						ec.Rng = uint32(f[6])
						ec.Val = uint32(f[7])
						ec.Ext = uint32(f[8])
						ec.Rem = int(f[9])
						ec.Error = int(f[10])
						dec = &ec
						if d.StartBand() != 0 {
							hybrid++
						}
					}
					clear(pcm)
					got := d.DecodeWithEC(data, pcm, frameSize, dec, accum)
					wantRet, wantRng := int(rd()), uint32(rd())
					wantHash := uint64(uint32(rd())) | uint64(uint32(rd()))<<32
					h := newFNV(false)
					if got > 0 {
						h.f32s(pcm[:got*CC])
					}
					if got != wantRet || d.FinalRange() != wantRng || h.h != wantHash {
						t.Fatalf("call %d (packet %d): ret %d/%d rng %08x/%08x hash %016x/%016x",
							calls, idx, got, wantRet, d.FinalRange(), wantRng, h.h, wantHash)
					}
					calls++
				default:
					t.Fatal("bad log record")
				}
			}
			t.Logf("%d CELT calls bit-exact, %d with start band 17 after SILK", calls, hybrid)
		})
	}
}

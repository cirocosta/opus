#!/usr/bin/env python3
# Generates cases.in (testdata/encoder_golden.txt) and vcases.in
# (testdata/encoder_vectors.txt, RFC 8251 vectors) of the root encoder
# goldens.
#
# usage: gen_cases.py <testvectors-dir> <out-dir>
#
# The reference is the default libopus configuration, which has no DRED:
# add() drops the DRED cases, but the script still makes the same random
# draws, so the other cases do not change.
import os
import random
import sys
R = random.Random(1234)
VOIP, AUDIO, LD, RSILK, RCELT = 2048, 2049, 2051, 2052, 2053
APPS = [VOIP, AUDIO, LD, RSILK, RCELT]
RATES = [8000, 12000, 16000, 24000, 48000]
MS = [2.5, 5, 10, 20, 40, 60, 80, 100, 120]
cases = []

def fr(fs, ms):
    return int(fs * ms / 1000)

def nfr(fs, ms, secs=1.0):
    return max(6, int(secs * 1000 / ms))

def add(**kw):
    if 'dred' in kw or 'dred=' in kw.get('extra', ''):
        return
    toks = []
    for k, v in kw.items():
        if k == 'extra':
            continue
        toks.append('%s=%s' % (k, v))
    if 'extra' in kw:
        toks.append(kw['extra'])
    cases.append(' '.join(toks))

# 1. ev configuration: 24 kHz mono VoIP, 20 ms, FEC, 5% loss.
for br in [None, 12000, 16000, 24000, 32000, 64000]:
    e = '' if br is None else 'br=%d' % br
    add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=120, fec=1, loss=5, **{'in': 'x'}, extra=e)
add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=150, fec=1, loss=5, dtx=1, **{'in': 's3'})
add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=120, fec=1, loss=5, api='f', **{'in': 'x+20000'})
add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=120, fec=1, loss=5, vbr=0, br=20000, **{'in': 'x'})
for cx in [0, 3, 6, 10]:
    add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=60, fec=1, loss=5, cx=cx, **{'in': 'x+48000'})
add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=120, fec=1, loss=5,
    extra='@20:loss=15 @40:loss=30 @60:loss=0 @80:loss=5 @90:br=9000 @100:br=40000')

# 2. Grid of application x rate x channels x frame size.
for app in APPS:
    for fs in RATES:
        for ch in [1, 2]:
            for k in range(2):
                ms = R.choice([m for m in MS if not (app == RSILK and m < 10)])
                br = R.choice(['', 'br=-1000', 'br=10000', 'br=20000', 'br=32000', 'br=64000', 'br=128000'])
                api = R.choice(['16', '16', '24', 'f'])
                add(t='enc', fs=fs, ch=ch, app=app, fr=fr(fs, ms), n=nfr(fs, ms, 0.8), api=api,
                    mb=4000, **{'in': R.choice(['x', 'x+30000', 'x+70000', 's2', 's5'])}, extra=br)

# 3. Bitrate sweep.
for br in [6000, 8000, 10000, 12000, 16000, 20000, 24000, 32000, 48000, 64000, 96000,
           128000, 192000, 256000, 320000, 510000, -1000, -1]:
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=40, br=br, mb=4000, **{'in': 'x'})
    add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=40, br=br, mb=4000, **{'in': 'x+40000'})
    add(t='enc', fs=48000, ch=1, app=VOIP, fr=480, n=40, br=br, mb=4000, **{'in': 'x+80000'})

# 4. CBR / CVBR / VBR.
for br in [8000, 16000, 24000, 40000, 96000]:
    for vbr, cvbr in [(0, 1), (1, 0), (1, 1)]:
        add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=30, br=br, vbr=vbr, cvbr=cvbr, **{'in': 'x+10000'})
        add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=30, br=br, vbr=vbr, cvbr=cvbr, **{'in': 'x+60000'})
for ms in [10, 20, 40, 60, 120]:
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=fr(48000, ms), n=nfr(48000, ms), br=48000, vbr=0, mb=4000, **{'in': 'x'})
    add(t='enc', fs=24000, ch=1, app=VOIP, fr=fr(24000, ms), n=nfr(24000, ms), br=16000, vbr=0, mb=4000, **{'in': 'x'})

# 5. Complexity.
for cx in range(11):
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=25, br=32000, cx=cx, **{'in': 'x+5000'})
    add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=25, cx=cx, **{'in': 'x+25000'})
    add(t='enc', fs=48000, ch=1, app=LD, fr=240, n=60, br=48000, cx=cx, **{'in': 'x+45000'})

# 6. In-band FEC.
for fec in [1, 2]:
    for loss in [0, 1, 5, 10, 20, 30]:
        add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=40, fec=fec, loss=loss, **{'in': 'x'})
        add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=40, fec=fec, loss=loss, br=28000, **{'in': 'x+50000'})
for loss in [3, 12, 25]:
    add(t='enc', fs=48000, ch=1, app=VOIP, fr=960, n=40, fec=1, loss=loss, br=14000, **{'in': 'x+90000'})
    add(t='enc', fs=8000, ch=1, app=VOIP, fr=160, n=40, fec=1, loss=loss, **{'in': 'x+15000'})

# 7. DTX.
for app in [VOIP, AUDIO, LD, RSILK, RCELT]:
    for cx in [0, 5, 10]:
        add(t='enc', fs=48000, ch=1, app=app, fr=960, n=150, dtx=1, cx=cx, **{'in': 's3'})
    add(t='enc', fs=16000, ch=2, app=app, fr=320, n=150, dtx=1, **{'in': 's3'})
add(t='enc', fs=48000, ch=1, app=VOIP, fr=960, n=150, dtx=1, **{'in': 's1'})
add(t='enc', fs=48000, ch=2, app=AUDIO, fr=1920, n=75, dtx=1, **{'in': 's3'})
add(t='enc', fs=16000, ch=1, app=VOIP, fr=960, n=50, dtx=1, **{'in': 's3'})
add(t='enc', fs=48000, ch=1, app=AUDIO, fr=5760, n=25, dtx=1, mb=4000, **{'in': 's3'})
add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=150, dtx=1, fec=1, loss=5, cx=3, **{'in': 's3'})

# 8. Signal type.
for sig in [3001, 3002]:
    for app in [VOIP, AUDIO]:
        add(t='enc', fs=48000, ch=2, app=app, fr=960, n=40, sig=sig, br=24000, **{'in': 'x'})
        add(t='enc', fs=48000, ch=1, app=app, fr=960, n=40, sig=sig, br=48000, **{'in': 'x+60000'})
add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=60, br=24000, extra='@10:sig=3001 @25:sig=3002 @40:sig=-1000 @45:vr=90')

# 9. Bandwidth limits.
for bw in [1101, 1102, 1103, 1104, 1105, -1000]:
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=30, bw=bw, br=64000, **{'in': 'x'})
    add(t='enc', fs=48000, ch=1, app=VOIP, fr=960, n=30, bw=bw, **{'in': 'x+30000'})
    if bw != -1000:
        add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=30, maxbw=bw, br=32000, **{'in': 'x+60000'})
        add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=30, maxbw=bw, fec=1, loss=5, **{'in': 'x+90000'})
add(t='enc', fs=48000, ch=1, app=VOIP, fr=960, n=80, br=40000,
    extra='@10:bw=1101 @20:bw=1103 @30:bw=1105 @40:bw=1102 @50:maxbw=1104 @60:bw=-1000 @70:maxbw=1105')

# 10. LSB depth.
for lsb in [8, 12, 16, 24]:
    for api in ['16', '24', 'f']:
        add(t='enc', fs=48000, ch=1, app=AUDIO, fr=960, n=75, lsb=lsb, api=api, **{'in': 's1'})
    add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=75, lsb=lsb, dtx=1, **{'in': 's1'})

# 11. Prediction and phase inversion disabled.
for app in [VOIP, AUDIO, LD]:
    add(t='enc', fs=48000, ch=2, app=app, fr=960, n=30, pd=1, br=40000, **{'in': 'x'})
    add(t='enc', fs=48000, ch=2, app=app, fr=480, n=60, pi=1, br=96000, **{'in': 'x+20000'})
    add(t='enc', fs=24000, ch=1, app=app, fr=480, n=30, pd=1, fec=1, loss=10, **{'in': 'x+40000'})

# 12. Forced modes and mode transitions (redundancy).
for mode in [1000, 1001, 1002]:
    for ms in [10, 20, 40, 60]:
        add(t='enc', fs=48000, ch=2, app=AUDIO, fr=fr(48000, ms), n=nfr(48000, ms, 0.6), mode=mode, br=48000,
            mb=4000, **{'in': 'x'})
        add(t='enc', fs=16000, ch=1, app=VOIP, fr=fr(16000, ms), n=nfr(16000, ms, 0.6), mode=mode, **{'in': 'x+35000'})
for ms in [10, 20]:
    for fs in [16000, 48000]:
        f = fr(fs, ms)
        n = nfr(fs, ms, 1.6)
        ctl = ' '.join('@%d:mode=%d' % (k * n // 8, [1002, 1000, 1001, 1002, 1001, 1000, 1002, -1000][k]) for k in range(1, 8))
        for ch in [1, 2]:
            add(t='enc', fs=fs, ch=ch, app=AUDIO, fr=f, n=n, br=40000, **{'in': 'x'}, extra=ctl)
            add(t='enc', fs=fs, ch=ch, app=VOIP, fr=f, n=n, br=24000, vbr=0, **{'in': 'x+12345'}, extra=ctl)
add(t='enc', fs=48000, ch=2, app=AUDIO, fr=1920, n=40, br=40000, mb=4000, extra='@5:mode=1002 @10:mode=1000 @15:mode=1001 @20:mode=1002 @30:mode=1000')
add(t='enc', fs=48000, ch=1, app=AUDIO, fr=2880, n=30, br=30000, mb=4000, extra='@5:mode=1002 @10:mode=1000 @15:mode=1001 @20:mode=1002 @25:mode=1001')
add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=120, mb=4000,
    extra='@10:br=12000 @20:br=64000 @30:br=9000 @40:br=128000 @50:br=16000 @60:br=24000 @70:br=40000 @80:br=6000 @90:br=320000 @100:br=-1000')
add(t='enc', fs=48000, ch=1, app=VOIP, fr=480, n=200, mb=4000,
    extra='@20:br=12000 @40:br=64000 @60:br=9000 @80:br=128000 @100:br=16000 @120:br=24000 @140:br=40000 @160:br=6000')

# 13. Force channels and stereo/mono transitions.
for fc in [1, 2, -1000]:
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=40, fc=fc, br=32000, **{'in': 'x'})
    add(t='enc', fs=24000, ch=2, app=VOIP, fr=480, n=40, fc=fc, **{'in': 's5'})
add(t='enc', fs=48000, ch=1, app=AUDIO, fr=960, n=5, fc=2, **{'in': 'x'})
add(t='enc', fs=48000, ch=2, app=VOIP, fr=960, n=100, br=24000, extra='@10:fc=1 @20:fc=2 @30:fc=1 @40:fc=-1000 @50:br=12000 @60:br=40000 @70:fc=1')
add(t='enc', fs=48000, ch=2, app=VOIP, fr=2880, n=40, br=20000, mb=4000, extra='@5:fc=1 @10:fc=2 @15:fc=1 @20:fc=-1000 @25:br=60000')
add(t='enc', fs=16000, ch=2, app=VOIP, fr=1920, n=40, br=20000, mb=4000, extra='@5:fc=1 @10:fc=2 @15:fc=1 @20:fc=-1000')
add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=120, **{'in': 's5'})

# 14. Mid-stream resets, application changes, expert frame durations.
add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=60, br=32000, extra='setapp=2048 @10:reset=1 @20:setapp=2049 @30:reset=1 @35:mode=1000 @45:reset=1')
add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=60, extra='setapp=2051 @15:reset=1 @30:setapp=2048')
add(t='enc', fs=48000, ch=1, app=RSILK, fr=960, n=20, extra='setapp=2048')
add(t='enc', fs=48000, ch=1, app=RCELT, fr=960, n=20, extra='setapp=2048 @5:pi=1 @6:mode=1000')
for fs in [48000, 16000]:
    f = fr(fs, 120)
    fds = [5001, 5002, 5003, 5004, 5005, 5006, 5007, 5008, 5009, 5000, 5010, 4999]
    ctl = ' '.join('@%d:fd=%d' % (2 * k, v) for k, v in enumerate(fds))
    for app in [VOIP, AUDIO, RSILK]:
        add(t='enc', fs=fs, ch=2, app=app, fr=f, n=2 * len(fds) + 2, br=40000, mb=8000, **{'in': 'x'}, extra=ctl)
add(t='enc', fs=48000, ch=1, app=AUDIO, fr=960, n=40, cx=10, br=24000, extra='@5:vr=0 @10:vr=100 @15:vr=50 @20:vr=-1 @25:vr=101 @30:dtx=1 @35:cx=11')

# 15. DRED.
for dred in [1, 5, 10, 50, 104]:
    for loss in [0, 5, 20]:
        add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=50, dred=dred, loss=loss, br=24000, **{'in': 'x'})
for fs, ch, app in [(48000, 1, VOIP), (48000, 2, AUDIO), (24000, 1, VOIP), (16000, 2, VOIP), (8000, 1, VOIP), (12000, 1, AUDIO)]:
    for ms in [10, 20, 40, 60, 120]:
        br = R.choice([16000, 32000, 48000, 64000])
        add(t='enc', fs=fs, ch=ch, app=app, fr=fr(fs, ms), n=nfr(fs, ms, 0.8), dred=R.choice([20, 50, 100]),
            loss=R.choice([5, 10, 25]), br=br, mb=4000, **{'in': 'x+%d' % R.randrange(100000)})
for vbr in [0, 1]:
    for fec in [0, 1]:
        add(t='enc', fs=48000, ch=1, app=VOIP, fr=960, n=50, dred=60, loss=15, fec=fec, vbr=vbr, br=32000, **{'in': 'x'})
        add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=50, dred=30, loss=5, fec=fec, vbr=vbr, **{'in': 'x+20000'})
add(t='enc', fs=48000, ch=1, app=VOIP, fr=960, n=80, loss=10, br=32000, extra='dred=40 @20:reset=1 @30:dred=80 @50:dred=0 @60:dred=104 @70:loss=0')
add(t='enc', fs=48000, ch=1, app=VOIP, fr=960, n=150, dred=50, loss=10, dtx=1, **{'in': 's3'})
add(t='enc', fs=24000, ch=1, app=VOIP, fr=480, n=120, dred=50, loss=5, fec=1, **{'in': 'x'})
add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=40, dred=50, loss=10, br=64000, api='f', **{'in': 'x'})
add(t='enc', fs=48000, ch=1, app=LD, fr=480, n=40, dred=50, loss=10, br=32000, **{'in': 'x'})
add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=40, dred=50, loss=10, br=24000, mb=60, **{'in': 'x'})

# 16. Input APIs and clipping.
for api in ['16', '24', 'f', 'f2']:
    for app in [VOIP, AUDIO, LD]:
        add(t='enc', fs=48000, ch=2, app=app, fr=960, n=40, api=api, **{'in': 's4'})
        add(t='enc', fs=24000, ch=1, app=app, fr=480, n=40, api=api, br=20000, **{'in': 'x'})

# 17. Synthetic inputs.
for s in range(6):
    for app in [VOIP, AUDIO, LD]:
        add(t='enc', fs=48000, ch=2, app=app, fr=960, n=60, **{'in': 's%d' % s})
        add(t='enc', fs=16000, ch=1, app=app, fr=160, n=120, cx=R.choice([1, 4, 8, 10]), **{'in': 's%d' % s})

# 18. Buffer limits and errors.
for mb in [1, 2, 3, 4, 8, 20, 50, 200]:
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=10, mb=mb, **{'in': 'x'})
    add(t='enc', fs=48000, ch=1, app=VOIP, fr=4800, n=6, mb=mb, **{'in': 'x'})
    add(t='enc', fs=48000, ch=1, app=VOIP, fr=2880, n=6, mb=mb, vbr=0, br=6000, **{'in': 'x'})
    add(t='enc', fs=16000, ch=1, app=VOIP, fr=640, n=8, mb=mb, br=6000, **{'in': 'x'})
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=5760, n=4, mb=mb, **{'in': 'x'})
add(t='enc', fs=48000, ch=1, app=AUDIO, fr=100, n=3, **{'in': 'x'})
add(t='enc', fs=44100, ch=1, app=AUDIO, fr=960, n=3, **{'in': 'x'})
add(t='enc', fs=48000, ch=3, app=AUDIO, fr=960, n=3, **{'in': 'x'})
add(t='enc', fs=48000, ch=1, app=2050, fr=960, n=3, **{'in': 'x'})
add(t='enc', fs=48000, ch=1, app=RSILK, fr=240, n=3, **{'in': 'x'})
add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=3,
    extra='br=0 cx=11 fec=3 loss=101 sig=7 bw=1100 maxbw=1106 fc=3 lsb=7 lsb=25 fd=5011 mode=999 vr=-2 setapp=2052')

# 19. Multistream, surround, ambisonics and projection.
add(t='ms', fs=48000, ch=3, streams=2, coupled=1, map='0.1.2', app=AUDIO, fr=960, n=30, br=96000, **{'in': 'x'})
add(t='ms', fs=48000, ch=4, streams=3, coupled=1, map='0.255.1.2', app=VOIP, fr=960, n=30, br=64000, **{'in': 'x'})
add(t='ms', fs=16000, ch=2, streams=2, coupled=0, map='1.0', app=VOIP, fr=320, n=30, **{'in': 'x'})
add(t='ms', fs=48000, ch=2, streams=1, coupled=1, map='0.1', app=AUDIO, fr=2880, n=20, br=64000, vbr=0, **{'in': 'x'})
add(t='ms', fs=48000, ch=3, streams=2, coupled=1, map='0.1.3', app=AUDIO, fr=960, n=3, **{'in': 'x'})
add(t='ms', fs=48000, ch=2, streams=2, coupled=1, map='0.1', app=AUDIO, fr=960, n=3, **{'in': 'x'})
add(t='ms', fs=48000, ch=3, streams=2, coupled=1, map='0.1.2', app=AUDIO, fr=960, n=40, mb=4000,
    extra='br=128000 @5:cx=3 @10:vbr=0 @15:fec=1 @16:loss=10 @20:bw=1103 @25:mode=1000 @30:reset=1 @32:fd=5003 @35:fd=5004 @36:dtx=1 @37:lsb=16 @38:sig=3001 @39:pd=1')
for ch in range(1, 9):
    for br in [-1000, 32000 * ch, 64000 * ch]:
        add(t='sur', fs=48000, ch=ch, family=1, app=AUDIO, fr=960, n=25, br=br, mb=8000, **{'in': 'x'})
for ch, ms, fs, app, br, extra in [(6, 10, 48000, AUDIO, 192000, ''), (6, 40, 48000, AUDIO, 256000, ''),
                                   (6, 20, 48000, AUDIO, 128000, 'vbr=0'), (8, 20, 48000, AUDIO, 256000, 'vbr=0'),
                                   (6, 20, 24000, VOIP, 96000, ''), (8, 60, 48000, AUDIO, 320000, ''),
                                   (6, 20, 48000, LD, 256000, ''), (6, 20, 48000, RSILK, 96000, ''),
                                   (5, 5, 48000, AUDIO, 160000, ''), (6, 20, 48000, AUDIO, 30000, ''),
                                   (6, 20, 16000, AUDIO, 96000, 'cx=4'), (8, 120, 48000, AUDIO, 256000, ''),
                                   (6, 20, 48000, AUDIO, -1, ''), (7, 20, 48000, AUDIO, 200000, 'api=f'),
                                   (6, 20, 48000, AUDIO, 200000, 'api=24'), (6, 20, 48000, AUDIO, 200000, '@10:reset=1 @15:br=64000')]:
    add(t='sur', fs=fs, ch=ch, family=1, app=app, fr=fr(fs, ms), n=nfr(fs, ms, 0.5), br=br, mb=8000, **{'in': 'x'}, extra=extra)
for ch, fam in [(1, 0), (2, 0), (3, 0), (3, 255), (5, 255), (4, 2), (9, 2), (11, 2), (6, 2), (16, 2), (2, 1), (9, 1), (3, 7)]:
    add(t='sur', fs=48000, ch=ch, family=fam, app=AUDIO, fr=960, n=20, br=48000 * ch, mb=8000, **{'in': 'x'})
add(t='sur', fs=48000, ch=9, family=2, app=AUDIO, fr=960, n=20, vbr=0, br=200000, mb=8000, **{'in': 'x'})
add(t='sur', fs=48000, ch=4, family=2, app=AUDIO, fr=960, n=20, mb=8000, **{'in': 'x'})
add(t='sur', fs=48000, ch=4, family=2, app=AUDIO, fr=960, n=20, br=-1, mb=8000, **{'in': 'x'})
for ch in [4, 6, 9, 11, 16, 18, 25]:
    for api in ['16', '24', 'f']:
        add(t='proj', fs=48000, ch=ch, family=3, app=AUDIO, fr=960, n=12, br=32000 * ch, api=api, mb=8000, **{'in': 'x'})
for ch, extra in [(4, 'vbr=0'), (9, 'cx=5'), (4, '@5:reset=1 @7:br=64000'), (16, 'br=-1000'), (9, 'br=-1')]:
    add(t='proj', fs=48000, ch=ch, family=3, app=AUDIO, fr=960, n=15, mb=8000, **{'in': 'x'}, extra=extra)
add(t='proj', fs=24000, ch=4, family=3, app=VOIP, fr=480, n=20, br=64000, **{'in': 'x'})
add(t='proj', fs=48000, ch=9, family=3, app=AUDIO, fr=2880, n=8, br=256000, mb=8000, **{'in': 'x'})
add(t='proj', fs=48000, ch=5, family=3, app=AUDIO, fr=960, n=3, **{'in': 'x'})
add(t='proj', fs=48000, ch=4, family=2, app=AUDIO, fr=960, n=3, **{'in': 'x'})
add(t='proj', fs=48000, ch=49, family=3, app=AUDIO, fr=960, n=3, **{'in': 'x'})

# 20. Remaining branches: PLC frames, huge float input, odd sizes.
for mb in [1, 2, 3]:
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=120, n=6, mb=mb, **{'in': 'x'})
    add(t='enc', fs=48000, ch=1, app=VOIP, fr=3840, n=4, mb=mb, **{'in': 'x'})
    add(t='enc', fs=48000, ch=1, app=VOIP, fr=1920, n=6, mb=mb, **{'in': 'x'}, extra='mode=1001')
    add(t='enc', fs=48000, ch=1, app=AUDIO, fr=960, n=6, mb=mb, **{'in': 'x'}, extra='@3:mode=1002 @3:bw=1102')
add(t='enc', fs=48000, ch=1, app=VOIP, fr=960, n=40, br=20000, extra='@10:fd=5002 @20:fd=5004 @30:fd=5001')
add(t='enc', fs=48000, ch=2, app=VOIP, fr=960, n=40, br=20000, extra='@10:fd=5003 @20:fd=5002 @30:fd=5004')
for api in ['f3', 'f4']:
    add(t='enc', fs=48000, ch=2, app=AUDIO, fr=960, n=30, api=api, **{'in': 's4'})
    add(t='enc', fs=16000, ch=1, app=VOIP, fr=320, n=30, api=api, **{'in': 'x'})
    add(t='sur', fs=48000, ch=6, family=1, app=AUDIO, fr=960, n=20, api=api, br=200000, mb=8000, **{'in': 's4'})
    add(t='proj', fs=48000, ch=4, family=3, app=AUDIO, fr=960, n=20, api=api, br=128000, mb=8000, **{'in': 'x'})
add(t='enc', fs=12000, ch=2, app=AUDIO, fr=30, n=100, br=32000, **{'in': 'x'})
add(t='enc', fs=12000, ch=2, app=LD, fr=30, n=100, br=64000, **{'in': 's5'})
add(t='ms', fs=48000, ch=3, streams=2, coupled=1, map='0.1.2', app=AUDIO, fr=4800, n=6, br=96000, mb=8000, **{'in': 'x'})
add(t='ms', fs=48000, ch=3, streams=2, coupled=1, map='0.1.2', app=VOIP, fr=4800, n=6, br=24000, vbr=0, mb=8000, **{'in': 'x'})
for mb in [2, 3, 5, 9]:
    add(t='ms', fs=48000, ch=3, streams=3, coupled=0, map='0.1.2', app=AUDIO, fr=960, n=4, mb=mb, **{'in': 'x'})
    add(t='ms', fs=48000, ch=3, streams=3, coupled=0, map='0.1.2', app=AUDIO, fr=4800, n=4, mb=mb, **{'in': 'x'})
for br in [35000, 50000, 70000]:
    add(t='sur', fs=48000, ch=6, family=1, app=AUDIO, fr=960, n=20, br=br, mb=8000, **{'in': 'x'})
    add(t='sur', fs=48000, ch=6, family=1, app=AUDIO, fr=240, n=40, br=br * 2, mb=8000, **{'in': 'x'})
for br in [30000, 48000, 64000]:
    add(t='sur', fs=48000, ch=6, family=1, app=VOIP, fr=960, n=30, br=br, fec=1, loss=25, mb=8000, **{'in': 'x'})
    add(t='sur', fs=16000, ch=6, family=1, app=VOIP, fr=320, n=30, br=br, fec=1, loss=10, mb=8000, **{'in': 'x'})
add(t='sur', fs=48000, ch=6, family=1, app=VOIP, fr=960, n=60, br=96000, dtx=1, mb=8000, **{'in': 's3'})
add(t='ms', fs=48000, ch=4, streams=2, coupled=2, map='0.1.2.3', app=AUDIO, fr=960, n=30, mb=4000,
    extra='br=160000 setapp=2048 @3:cvbr=0 @6:maxbw=1103 @9:fc=1 @12:pi=1 @15:fc=-1000 @18:maxbw=1105 @20:setapp=2049 @21:br=300')
add(t='enc', fs=48000, ch=1, app=AUDIO, fr=960, n=10, br=300, **{'in': 'x'})
add(t='proj', fs=48000, ch=36, family=3, app=AUDIO, fr=960, n=4, br=1000000, mb=16000, **{'in': 'x'})
add(t='proj', fs=48000, ch=38, family=3, app=AUDIO, fr=960, n=4, br=1000000, mb=16000, **{'in': 'x'})

VECTORS, OUTDIR = sys.argv[1], sys.argv[2]
with open(os.path.join(OUTDIR, 'cases.in'), 'w') as f:
    for c in cases:
        f.write(c.strip() + '\n')

# Vector cases.
vc = []
for v in range(1, 13):
    vn = 'v%02d' % v
    frames = os.path.getsize(os.path.join(VECTORS, 'testvector%02d.dec' % v)) // 4
    vc.append('t=enc fs=24000 ch=1 app=2048 fr=480 n=%d fec=1 loss=5 in=%s' % (frames // 960, vn))
    vc.append('t=enc fs=48000 ch=2 app=2049 fr=960 n=%d in=%s' % (frames // 960, vn))
    vc.append('t=enc fs=16000 ch=1 app=2048 fr=320 n=%d vbr=0 br=16000 dtx=1 in=%s' % (150, vn))
    vc.append('t=enc fs=48000 ch=2 app=2049 fr=480 n=%d br=96000 cx=10 in=%s+20000' % (300, vn))
    vc.append('t=enc fs=8000 ch=1 app=2052 fr=480 n=%d in=%s' % (100, vn))
    vc.append('t=enc fs=12000 ch=2 app=2048 fr=720 n=%d fec=1 loss=20 api=f in=%s+50000' % (100, vn))
    vc.append('t=sur fs=48000 ch=6 family=1 app=2049 fr=960 n=%d br=256000 mb=8000 in=%s' % (60, vn))
with open(os.path.join(OUTDIR, 'vcases.in'), 'w') as f:
    for c in vc:
        f.write(c + '\n')
print(len(cases), len(vc))

# Generates the encoder case list (core suite on the committed PCM excerpt).
import itertools
L=[]
def add(name, **kw):
    s=name+''.join(f' {k}={v}' for k,v in kw.items())
    L.append(s)
OFFS=[0,28800,57600,76800,96000]
i=0
# 1. grid
for ch in (1,2):
    for lm in range(4):
        for mode in ('cbr','vbr','cvbr'):
            brs = (12000,48000,160000) if ch==1 else (24000,96000,256000)
            for br in brs:
                n = 80 if lm<=1 else 50
                vbr = 0 if mode=='cbr' else 1
                cvbr = 1 if mode=='cvbr' else 0
                add(f'grid{i}', off=OFFS[i%5], ch=ch, lm=lm, n=n, br=br, vbr=vbr, cvbr=cvbr, cx=(i*7)%11)
                i+=1
for ch in (1,2):
    for lm in (0,3):
        for mode in (0,1,2):
            add(f'max510_{ch}_{lm}_{mode}', off=OFFS[(ch+lm)%5], ch=ch, lm=lm, n=40, br=510000, vbr=int(mode>0), cvbr=int(mode==2), cx=10)
# 2. complexity sweep
for cx in range(11):
    add(f'cx{cx}s', off=OFFS[cx%5], ch=2, lm=3, n=50, br=64000, vbr=1, cvbr=0, cx=cx)
    add(f'cx{cx}m', off=OFFS[(cx+2)%5], ch=1, lm=2, n=60, br=32000, vbr=0, cx=cx)
    add(f'cx{cx}t', off=OFFS[(cx+1)%5], ch=2, lm=1, n=80, br=96000, vbr=1, cvbr=1, cx=cx)
# 3. prediction and loss
for pred in range(3):
    for loss in (0,3,5,9,20):
        add(f'pred{pred}_loss{loss}', off=OFFS[(pred+loss)%5], ch=2, lm=3, n=50, br=48000, vbr=1, cvbr=1, cx=10, pred=pred, loss=loss)
        add(f'predm{pred}_loss{loss}', off=OFFS[(pred*3+loss)%5], ch=1, lm=2, n=50, br=24000, vbr=0, cx=7, pred=pred, loss=loss)
# 4. bitrate max with packet sizes
for nb in (2,3,5,10,20,40,100,300,700,1275):
    for lm in (0,2,3):
        add(f'nb{nb}_lm{lm}', off=OFFS[(nb+lm)%5], ch=2 if nb%3 else 1, lm=lm, n=40, nb=nb, cx=10)
# 5. hybrid
k=0
for ch in (1,2):
    for lm in (0,1,2,3):
        for vbr in (0,1):
            for br,nb in ((16000,40),(32000,80),(64000,160)):
                add(f'hyb{k}', off=OFFS[k%5], ch=ch, lm=lm, n=50 if lm>=2 else 80, start=17, end=19+2*(k%2), ext=2, vbr=vbr, br=br if vbr else -1, nb=nb if not vbr else 1275 if k%3 else nb, cx=(k*3)%11, seed=k+1)
                k+=1
# 6. external encoder, CELT-only
for ch in (1,2):
    for lm in range(4):
        add(f'ext1_{ch}_{lm}', off=OFFS[(ch*lm)%5], ch=ch, lm=lm, n=40, ext=1, br=64000*ch, vbr=lm%2, cvbr=1, cx=10)
        add(f'ext2_{ch}_{lm}', off=OFFS[(ch*lm+1)%5], ch=ch, lm=lm, n=40, ext=2, br=-1, nb=200, cx=9, seed=lm+7)
# 7. end bands
for end in (13,15,17,19,20,21):
    for ch in (1,2):
        add(f'end{end}_{ch}', off=OFFS[end%5], ch=ch, lm=3, n=40, end=end, br=32000*ch, vbr=1, cvbr=end%2, cx=10)
        add(f'endc{end}_{ch}', off=OFFS[(end+1)%5], ch=ch, lm=1, n=60, end=end, br=48000*ch, vbr=0, cx=6)
# 8. lsb depth, silence, tiny
for lsb in (8,16,24):
    for src in ('sil','tiny','pcm','gap'):
        for mode in (0,1,2):
            add(f'lsb{lsb}_{src}_{mode}', src=src, off=OFFS[lsb%5], ch=1+(mode%2), lm=1+mode, n=40, lsb=lsb, br=40000, vbr=int(mode>0), cvbr=int(mode==2), cx=10)
# 9. lfe
for lm in range(4):
    for end in (21,13):
        add(f'lfe{lm}_{end}', off=OFFS[lm%5], ch=1, lm=lm, n=50, lfe=1, end=end, br=24000, vbr=lm%2, cx=10)
# 10. energy mask
for ch in (1,2):
    for mode in (0,1,2):
        for lm in (1,3):
            add(f'mask{ch}_{mode}_{lm}', off=OFFS[(ch+mode+lm)%5], ch=ch, lm=lm, n=50, mask=1, br=48000*ch, vbr=int(mode>0), cvbr=int(mode==2), cx=10, seed=mode+lm*3+ch)
# 11. analysis info
for ana in (1,2):
    for ch in (1,2):
        for mode in (0,1,2):
            for lm in (2,3):
                add(f'ana{ana}_{ch}_{mode}_{lm}', off=OFFS[(ana+ch+mode)%5], ch=ch, lm=lm, n=50, ana=ana, br=32000*ch, vbr=int(mode>0), cvbr=int(mode==2), cx=10, seed=ana*7+mode+lm)
# 12. phase inversion disabled, stream channels
for inv in (0,1):
    for sc in (1,2):
        for lm in (0,3):
            add(f'inv{inv}_sc{sc}_{lm}', off=OFFS[(inv+sc+lm)%5], ch=2, sc=sc, lm=lm, n=50, inv=inv, br=64000, vbr=1, cvbr=0, cx=10)
# 13. sampling rates
for fs in (24000,16000,12000,8000):
    for ch in (1,2):
        for lm in (1,3):
            add(f'fs{fs}_{ch}_{lm}', off=OFFS[(fs//4000+ch)%5], ch=ch, fs=fs, lm=lm, n=50, br=24000*ch, vbr=lm%3//2, cvbr=1, cx=10)
# 14. tones
TONES=[1073649834,1071961363,1064555814,992008094,851856663,410903207,-410903207,1073733545,-1071442860,-929887697]
for ti,t in enumerate(TONES):
    for ch in (1,2):
        for lm in (0,2,3):
            add(f'tone{ti}_{ch}_{lm}', src='tone', tone=t, ch=ch, lm=lm, n=60, br=32000*ch, vbr=(ti+lm)%2, cvbr=1, cx=5+5*(lm%2))
# 15. loud / gap
for src in ('loud','gap'):
    for ch in (1,2):
        for mode in (0,1,2):
            add(f'{src}{ch}_{mode}', src=src, off=OFFS[(ch+mode)%5], ch=ch, lm=3-mode, n=60, br=40000*ch, vbr=int(mode>0), cvbr=int(mode==2), cx=10)
for ti,t in enumerate(TONES[4:]):
    for end in (13,17):
        add(f'toneend{ti}_{end}', src='tone', tone=t, ch=1+ti%2, lm=3-ti%2, n=40, end=end, br=48000, vbr=1, cvbr=0, cx=10)
for ch in (1,2):
    for fs in (48000,16000):
        add(f'huge{ch}_{fs}', src='huge', off=OFFS[ch], ch=ch, fs=fs, lm=2, n=40, br=64000, vbr=1, cvbr=1, cx=10)
add('long_cvbr', off=0, ch=2, lm=0, n=1100, br=96000, vbr=1, cvbr=1, cx=8)
add('long_vbr', off=28800, ch=1, lm=1, n=1100, br=32000, vbr=1, cvbr=0, cx=4, ana=1, seed=99)
# 16. random schedules (mid-stream changes, resets)
for sd in range(1,25):
    ch = 1 + sd%2
    hyb = sd%4==3
    kw=dict(off=OFFS[sd%5], ch=ch, lm=sd%4, n=300, rnd=1, lmvar=1, seed=sd*101, br=32000*ch, vbr=sd%2, cvbr=sd%3%2, cx=sd%11)
    if hyb:
        kw.update(start=17, end=21, ext=2, nb=300)
    if sd%5==1: kw.update(ana=1)
    if sd%7==2: kw.update(mask=1)
    if sd%6==4: kw.update(src='gap')
    if sd%8==5: kw.update(fs=16000)
    add(f'rnd{sd}', **kw)
print('\n'.join(L))

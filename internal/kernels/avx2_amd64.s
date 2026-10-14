//go:build amd64 && !purego

#include "textflag.h"

// n > 0; lags is a positive multiple of 64. y has n+lags-1 elements.
// each lane accumulates one adjacent lag in input order, without FMA.
TEXT ·crossCorrelationAVX2(SB), NOSPLIT, $0-40
	MOVQ x+0(FP), R9
	MOVQ y+8(FP), R10
	MOVQ dst+16(FP), DX
	MOVQ n+24(FP), CX
	MOVQ lags+32(FP), SI
corr_block:
	MOVQ R9, AX
	MOVQ R10, BX
	MOVQ CX, R8
	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3
	VXORPS Y4, Y4, Y4
	VXORPS Y5, Y5, Y5
	VXORPS Y6, Y6, Y6
	VXORPS Y7, Y7, Y7
corr_loop:
	VBROADCASTSS (AX), Y8
	VMULPS 0(BX), Y8, Y9
	VADDPS Y9, Y0, Y0
	VMULPS 32(BX), Y8, Y9
	VADDPS Y9, Y1, Y1
	VMULPS 64(BX), Y8, Y9
	VADDPS Y9, Y2, Y2
	VMULPS 96(BX), Y8, Y9
	VADDPS Y9, Y3, Y3
	VMULPS 128(BX), Y8, Y9
	VADDPS Y9, Y4, Y4
	VMULPS 160(BX), Y8, Y9
	VADDPS Y9, Y5, Y5
	VMULPS 192(BX), Y8, Y9
	VADDPS Y9, Y6, Y6
	VMULPS 224(BX), Y8, Y9
	VADDPS Y9, Y7, Y7
	ADDQ $4, AX
	ADDQ $4, BX
	SUBQ $1, R8
	CMPQ R8, $1
	JGE corr_loop
corr_store:
	VMOVUPS Y0, 0(DX)
	VMOVUPS Y1, 32(DX)
	VMOVUPS Y2, 64(DX)
	VMOVUPS Y3, 96(DX)
	VMOVUPS Y4, 128(DX)
	VMOVUPS Y5, 160(DX)
	VMOVUPS Y6, 192(DX)
	VMOVUPS Y7, 224(DX)
	ADDQ $256, R10
	ADDQ $256, DX
	SUBQ $64, SI
	JNZ corr_block
	VZEROUPPER
	RET

// rows is a positive multiple of 8 and cols > 0. weights is column-major
// with stride bytes per column. each lane adds columns in scalar order.
// signed weights convert exactly through int32 to float32.
TEXT ·denseAccumAVX2(SB), NOSPLIT, $0-48
	MOVQ out+0(FP), DX
	MOVQ weights+8(FP), R9
	MOVQ x+16(FP), R10
	MOVQ rows+24(FP), SI
	MOVQ cols+32(FP), CX
	MOVQ stride+40(FP), R11
avx_dense_block:
	MOVQ R9, BX
	MOVQ R10, AX
	MOVQ CX, R8
	VMOVUPS (DX), Y0
avx_dense_column:
	VBROADCASTSS (AX), Y1
	VPMOVSXBD (BX), Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VADDPS Y2, Y0, Y0
	ADDQ $4, AX
	ADDQ R11, BX
	DECQ R8
	JNZ avx_dense_column
	VMOVUPS Y0, (DX)
	ADDQ $32, DX
	ADDQ $8, R9
	SUBQ $8, SI
	JNZ avx_dense_block
	VZEROUPPER
	RET

// independent neuron blocks share each input broadcast.
// rows is a positive multiple of 24 and cols > 0. the column-major layout
// and rounding match denseAccumAVX2; four columns are unrolled in order.
TEXT ·denseAccumWideAVX2(SB), NOSPLIT, $0-48
	MOVQ out+0(FP), DX
	MOVQ weights+8(FP), R9
	MOVQ x+16(FP), R10
	MOVQ rows+24(FP), SI
	MOVQ cols+32(FP), CX
	MOVQ stride+40(FP), R11
dense_block:
	MOVQ R9, BX
	MOVQ R10, AX
	MOVQ CX, R8
	VMOVUPS 0(DX), Y0
	VMOVUPS 32(DX), Y1
	VMOVUPS 64(DX), Y2
	CMPQ R8, $4
	JL dense_tail
dense_loop:
	VBROADCASTSS (AX), Y8
	VPMOVSXBD 0(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y0, Y0
	VPMOVSXBD 8(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y1, Y1
	VPMOVSXBD 16(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y2, Y2
	ADDQ $4, AX
	ADDQ R11, BX
	VBROADCASTSS (AX), Y8
	VPMOVSXBD 0(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y0, Y0
	VPMOVSXBD 8(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y1, Y1
	VPMOVSXBD 16(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y2, Y2
	ADDQ $4, AX
	ADDQ R11, BX
	VBROADCASTSS (AX), Y8
	VPMOVSXBD 0(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y0, Y0
	VPMOVSXBD 8(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y1, Y1
	VPMOVSXBD 16(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y2, Y2
	ADDQ $4, AX
	ADDQ R11, BX
	VBROADCASTSS (AX), Y8
	VPMOVSXBD 0(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y0, Y0
	VPMOVSXBD 8(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y1, Y1
	VPMOVSXBD 16(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y2, Y2
	ADDQ $4, AX
	ADDQ R11, BX
	SUBQ $4, R8
	CMPQ R8, $4
	JGE dense_loop
dense_tail:
	TESTQ R8, R8
	JZ dense_store
dense_tail_loop:
	VBROADCASTSS (AX), Y8
	VPMOVSXBD 0(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y0, Y0
	VPMOVSXBD 8(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y1, Y1
	VPMOVSXBD 16(BX), Y9
	VCVTDQ2PS Y9, Y9
	VMULPS Y8, Y9, Y9
	VADDPS Y9, Y2, Y2
	ADDQ $4, AX
	ADDQ R11, BX
	DECQ R8
	JNZ dense_tail_loop
dense_store:
	VMOVUPS Y0, 0(DX)
	VMOVUPS Y1, 32(DX)
	VMOVUPS Y2, 64(DX)
	ADDQ $24, R9
	ADDQ $96, DX
	SUBQ $24, SI
	JNZ dense_block
	VZEROUPPER
	RET

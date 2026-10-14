//go:build amd64 && !purego

#include "textflag.h"

// lanes hold independent lags; mul and add remain separate and ordered.
TEXT ·xcorr4SSE2(SB), NOSPLIT, $0-32
	MOVQ x+0(FP), AX
	MOVQ y+8(FP), BX
	MOVQ sum+16(FP), DX
	MOVQ n+24(FP), CX
	MOVUPS (DX), X0
xcorr4_loop:
	MOVSS (AX), X1
	SHUFPS $0, X1, X1
	MOVUPS (BX), X2
	MULPS X1, X2
	ADDPS X2, X0
	ADDQ $4, AX
	ADDQ $4, BX
	DECQ CX
	JNZ xcorr4_loop
	MOVUPS X0, (DX)
	RET

// two four-lag accumulators share each input load and avoid reductions.
TEXT ·crossCorrelationSSE2(SB), NOSPLIT, $0-40
	MOVQ x+0(FP), R9
	MOVQ y+8(FP), R10
	MOVQ dst+16(FP), DX
	MOVQ n+24(FP), CX
	MOVQ lags+32(FP), SI
xcorr8_block:
	MOVQ R9, AX
	MOVQ R10, BX
	MOVQ CX, R8
	XORPS X0, X0
	XORPS X1, X1
xcorr8_loop:
	MOVSS (AX), X2
	SHUFPS $0, X2, X2
	MOVUPS (BX), X3
	MOVUPS 16(BX), X4
	MULPS X2, X3
	MULPS X2, X4
	ADDPS X3, X0
	ADDPS X4, X1
	ADDQ $4, AX
	ADDQ $4, BX
	DECQ R8
	JNZ xcorr8_loop
	MOVUPS X0, (DX)
	MOVUPS X1, 16(DX)
	ADDQ $32, R10
	ADDQ $32, DX
	SUBQ $8, SI
	JNZ xcorr8_block
	RET

// unpack signed bytes with SSE2, then accumulate eight independent neurons.
TEXT ·denseAccumSSE2(SB), NOSPLIT, $0-48
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
	MOVUPS (DX), X0
	MOVUPS 16(DX), X1
dense_column:
	MOVSS (AX), X2
	SHUFPS $0, X2, X2
	MOVQ (BX), X3
	// zero > weight builds the sign mask used to extend int8 to int32.
	PXOR X4, X4
	PCMPGTB X3, X4
	PUNPCKLBW X4, X3
	MOVO X3, X4
	PSRAW $15, X4
	MOVO X3, X5
	PUNPCKLWL X4, X3
	PUNPCKHWL X4, X5
	CVTPL2PS X3, X3
	CVTPL2PS X5, X5
	MULPS X2, X3
	MULPS X2, X5
	ADDPS X3, X0
	ADDPS X5, X1
	ADDQ $4, AX
	ADDQ R11, BX
	DECQ R8
	JNZ dense_column
	MOVUPS X0, (DX)
	MOVUPS X1, 16(DX)
	ADDQ $32, DX
	ADDQ $8, R9
	SUBQ $8, SI
	JNZ dense_block
	RET

// four output samples share each coefficient without reordering the taps.
TEXT ·lpcSSE2(SB), NOSPLIT, $0-40
	MOVQ dst+0(FP), DX
	MOVQ src+8(FP), AX
	MOVQ coeff+16(FP), R9
	MOVQ n+24(FP), SI
	MOVQ order+32(FP), CX
lpc_block:
	LEAQ -4(AX), BX
	MOVQ R9, R10
	MOVQ CX, R8
	MOVSS (R10), X1
	SHUFPS $0, X1, X1
	MOVUPS (BX), X0
	MULPS X1, X0
	SUBQ $4, BX
	ADDQ $4, R10
	DECQ R8
	JZ lpc_store
lpc_tap:
	MOVSS (R10), X1
	SHUFPS $0, X1, X1
	MOVUPS (BX), X2
	MULPS X1, X2
	ADDPS X2, X0
	SUBQ $4, BX
	ADDQ $4, R10
	DECQ R8
	JNZ lpc_tap
lpc_store:
	MOVUPS (AX), X1
	SUBPS X0, X1
	MOVUPS X1, (DX)
	ADDQ $16, AX
	ADDQ $16, DX
	SUBQ $4, SI
	JNZ lpc_block
	RET

// split each sample into a signed high word and unsigned low word.
// (a*c)>>16 = hi(a)*c + (signedlo(a)*c)>>16 + (lo(a)<0 ? c : 0).
// the low-word correction fits int16; pair additions wrap exactly as Go.
TEXT ·predictSSE2(SB), NOSPLIT, $0-28
	MOVQ history+0(FP), AX
	MOVQ coeff+8(FP), BX
	MOVQ order+16(FP), CX
	MOVQ CX, SI
	SHRQ $3, CX
	PXOR X7, X7
	PCMPEQW X6, X6
	PSRLW $15, X6
predict_block:
	MOVOU -12(AX), X0
	MOVOU -28(AX), X1
	PSHUFL $27, X0, X0
	PSHUFL $27, X1, X1
	MOVOU (BX), X4
	MOVO X0, X2
	MOVO X1, X3
	PSRAL $16, X2
	PSRAL $16, X3
	PACKSSLW X3, X2
	PMADDWL X4, X2
	PSLLL $16, X0
	PSLLL $16, X1
	PSRAL $16, X0
	PSRAL $16, X1
	PACKSSLW X1, X0
	MOVO X0, X5
	PSRAW $15, X5
	PAND X4, X5
	PMULHW X4, X0
	PADDW X5, X0
	PMADDWL X6, X0
	PADDL X2, X0
	PADDL X0, X7
	SUBQ $32, AX
	ADDQ $16, BX
	DECQ CX
	JNZ predict_block
	PSHUFL $78, X7, X0
	PADDL X0, X7
	PSHUFL $177, X7, X0
	PADDL X0, X7
	MOVD X7, DX
	CMPQ SI, $10
	JNE predict_return
	MOVLQSX (AX), R8
	MOVWQSX (BX), R9
	IMULQ R9, R8
	SARQ $16, R8
	ADDL R8, DX
	MOVLQSX -4(AX), R8
	MOVWQSX 2(BX), R9
	IMULQ R9, R8
	SARQ $16, R8
	ADDL R8, DX
predict_return:
	SHRQ $1, SI
	ADDL SI, DX
	MOVL DX, ret+24(FP)
	RET

// fuse the recurrent subframe loop around the exact fixed-point predictor.
TEXT ·synthesizeSSE2(SB), NOSPLIT, $0-56
	MOVQ history+0(FP), AX
	MOVQ residual+8(FP), R13
	MOVLQSX gain+24(FP), R11
	MOVQ dst+32(FP), R15
	MOVQ n+40(FP), R12
	MOVQ order+48(FP), SI
	PCMPEQW X6, X6
	PSRLW $15, X6
synth_sample:
	MOVQ AX, R10
	MOVQ coeff+16(FP), BX
	MOVQ SI, CX
	SHRQ $3, CX
	PXOR X7, X7
synth_predict_block:
	MOVOU -12(AX), X0
	MOVOU -28(AX), X1
	PSHUFL $27, X0, X0
	PSHUFL $27, X1, X1
	MOVOU (BX), X4
	MOVO X0, X2
	MOVO X1, X3
	PSRAL $16, X2
	PSRAL $16, X3
	PACKSSLW X3, X2
	PMADDWL X4, X2
	PSLLL $16, X0
	PSLLL $16, X1
	PSRAL $16, X0
	PSRAL $16, X1
	PACKSSLW X1, X0
	MOVO X0, X5
	PSRAW $15, X5
	PAND X4, X5
	PMULHW X4, X0
	PADDW X5, X0
	PMADDWL X6, X0
	PADDL X2, X0
	PADDL X0, X7
	SUBQ $32, AX
	ADDQ $16, BX
	DECQ CX
	JNZ synth_predict_block
	PSHUFL $78, X7, X0
	PADDL X0, X7
	PSHUFL $177, X7, X0
	PADDL X0, X7
	MOVD X7, DX
	CMPQ SI, $10
	JNE synth_shift
	MOVLQSX (AX), R8
	MOVWQSX (BX), R9
	IMULQ R9, R8
	SARQ $16, R8
	ADDL R8, DX
	MOVLQSX -4(AX), R8
	MOVWQSX 2(BX), R9
	IMULQ R9, R8
	SARQ $16, R8
	ADDL R8, DX
synth_shift:
	MOVQ SI, R8
	SHRQ $1, R8
	ADDL R8, DX
	CMPL DX, $134217727
	JG synth_prediction_max
	CMPL DX, $-134217728
	JL synth_prediction_min
	SHLL $4, DX
	JMP synth_add
synth_prediction_max:
	MOVL $0x7ffffff0, DX
	JMP synth_add
synth_prediction_min:
	MOVL $0x80000000, DX
synth_add:
	MOVQ R10, AX
	MOVL (R13), R8
	ADDL R8, DX
	JNO synth_scale
	TESTL R8, R8
	JS synth_sample_min
	MOVL $0x7fffffff, DX
	JMP synth_scale
synth_sample_min:
	MOVL $0x80000000, DX
synth_scale:
	MOVL DX, 4(AX)
	MOVLQSX DX, R8
	IMULQ R11, R8
	SARQ $16, R8
	SARL $7, R8
	ADDL $1, R8
	SARL $1, R8
	CMPL R8, $32767
	JG synth_output_max
	CMPL R8, $-32768
	JL synth_output_min
	JMP synth_output
synth_output_max:
	MOVL $32767, R8
	JMP synth_output
synth_output_min:
	MOVL $-32768, R8
synth_output:
	MOVW R8, (R15)
	ADDQ $4, AX
	ADDQ $4, R13
	ADDQ $2, R15
	DECQ R12
	JNZ synth_sample
	RET

// the allpass chain is serial; keep state and wrap points in registers.
TEXT ·warpedFeedbackAMD64(SB), NOSPLIT, $0-36
	MOVQ state+0(FP), AX
	MOVQ coeff+8(FP), BX
	MOVL diff+16(FP), CX
	MOVWQSX warp+20(FP), DX
	MOVQ order+24(FP), SI
	SHRQ $1, SI
	MOVL SI, R10
	MOVLQSX (AX), R12
	MOVQ R12, R8
	IMULQ DX, R8
	SARQ $16, R8
	ADDL CX, R8
	MOVL 4(AX), R9
	SUBL R8, R9
	MOVLQSX R9, R9
	IMULQ DX, R9
	SARQ $16, R9
	ADDL R12, R9
	MOVL R8, (AX)
	MOVLQSX R8, R8
	MOVWQSX (BX), R13
	IMULQ R13, R8
	SARQ $16, R8
	ADDL R8, R10
	ADDQ $4, AX
	ADDQ $2, BX
	DECQ SI
	JZ feedback_last
feedback_pair:
	MOVL 4(AX), R8
	SUBL R9, R8
	MOVLQSX R8, R8
	IMULQ DX, R8
	SARQ $16, R8
	ADDL (AX), R8
	MOVL R9, (AX)
	MOVLQSX R9, R12
	MOVWQSX (BX), R13
	IMULQ R13, R12
	SARQ $16, R12
	ADDL R12, R10
	MOVL 8(AX), R9
	SUBL R8, R9
	MOVLQSX R9, R9
	IMULQ DX, R9
	SARQ $16, R9
	ADDL 4(AX), R9
	MOVL R8, 4(AX)
	MOVLQSX R8, R12
	MOVWQSX 2(BX), R13
	IMULQ R13, R12
	SARQ $16, R12
	ADDL R12, R10
	ADDQ $8, AX
	ADDQ $4, BX
	DECQ SI
	JNZ feedback_pair
feedback_last:
	MOVL R9, (AX)
	MOVLQSX R9, R9
	MOVWQSX (BX), R13
	IMULQ R13, R9
	SARQ $16, R9
	ADDL R9, R10
	MOVL R10, ret+32(FP)
	RET

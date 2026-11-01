// 생성 파일 — internal/asmgen/gen.py로 만든다. 직접 고치지 않는다.

//go:build !purego

#include "textflag.h"

// func quantBlock4(src *uint8, stride int, pred *[16]uint8, qv *quantVecs, levels *[16]int16, rec *[16]uint8) uint32
TEXT ·quantBlock4(SB), NOSPLIT, $0-52
	MOVD src+0(FP), R0
	MOVD stride+8(FP), R1
	MOVD pred+16(FP), R2
	MOVD qv+24(FP), R3
	MOVD levels+32(FP), R4
	MOVD rec+40(FP), R5
	WORD $0xb9400006 // ldr w6, [x0]
	WORD $0x4e041cc0 // ins v0.s[0], w6
	WORD $0x8b010000 // add x0, x0, x1
	WORD $0xb9400006 // ldr w6, [x0]
	WORD $0x4e0c1cc0 // ins v0.s[1], w6
	WORD $0x8b010000 // add x0, x0, x1
	WORD $0xb9400006 // ldr w6, [x0]
	WORD $0x4e141cc0 // ins v0.s[2], w6
	WORD $0x8b010000 // add x0, x0, x1
	WORD $0xb9400006 // ldr w6, [x0]
	WORD $0x4e1c1cc0 // ins v0.s[3], w6
	WORD $0x4c407041 // ld1 {v1.16b}, [x2]
	WORD $0x4c40686c // ld1 {v12.4s, v13.4s, v14.4s}, [x3]
	WORD $0x9100c067 // add x7, x3, #48
	WORD $0x2e212002 // usubl v2.8h, v0.8b, v1.8b
	WORD $0x6e212003 // usubl2 v3.8h, v0.16b, v1.16b
	WORD $0x0f10a444 // sxtl v4.4s, v2.4h
	WORD $0x4f10a445 // sxtl2 v5.4s, v2.8h
	WORD $0x0f10a466 // sxtl v6.4s, v3.4h
	WORD $0x4f10a467 // sxtl2 v7.4s, v3.8h
	WORD $0x4e852890 // trn1 v16.4s, v4.4s, v5.4s
	WORD $0x4e856891 // trn2 v17.4s, v4.4s, v5.4s
	WORD $0x4e8728d2 // trn1 v18.4s, v6.4s, v7.4s
	WORD $0x4e8768d3 // trn2 v19.4s, v6.4s, v7.4s
	WORD $0x4ed22a14 // trn1 v20.2d, v16.2d, v18.2d
	WORD $0x4ed32a35 // trn1 v21.2d, v17.2d, v19.2d
	WORD $0x4ed26a16 // trn2 v22.2d, v16.2d, v18.2d
	WORD $0x4ed36a37 // trn2 v23.2d, v17.2d, v19.2d
	WORD $0x4eb78698 // add v24.4s, v20.4s, v23.4s
	WORD $0x4eb686b9 // add v25.4s, v21.4s, v22.4s
	WORD $0x6eb686ba // sub v26.4s, v21.4s, v22.4s
	WORD $0x6eb7869b // sub v27.4s, v20.4s, v23.4s
	WORD $0x4eb98704 // add v4.4s, v24.4s, v25.4s
	WORD $0x4f235484 // shl v4.4s, v4.4s, #3
	WORD $0x6eb98706 // sub v6.4s, v24.4s, v25.4s
	WORD $0x4f2354c6 // shl v6.4s, v6.4s, #3
	WORD $0x4e140585 // dup v5.4s, v12.s[2]
	WORD $0x6f8c0345 // mla v5.4s, v26.4s, v12.s[0]
	WORD $0x6fac0365 // mla v5.4s, v27.4s, v12.s[1]
	WORD $0x4f3704a5 // sshr v5.4s, v5.4s, #9
	WORD $0x4e1c0587 // dup v7.4s, v12.s[3]
	WORD $0x6f8c0367 // mla v7.4s, v27.4s, v12.s[0]
	WORD $0x6fac4347 // mls v7.4s, v26.4s, v12.s[1]
	WORD $0x4f3704e7 // sshr v7.4s, v7.4s, #9
	WORD $0x4e852890 // trn1 v16.4s, v4.4s, v5.4s
	WORD $0x4e856891 // trn2 v17.4s, v4.4s, v5.4s
	WORD $0x4e8728d2 // trn1 v18.4s, v6.4s, v7.4s
	WORD $0x4e8768d3 // trn2 v19.4s, v6.4s, v7.4s
	WORD $0x4ed22a14 // trn1 v20.2d, v16.2d, v18.2d
	WORD $0x4ed32a35 // trn1 v21.2d, v17.2d, v19.2d
	WORD $0x4ed26a16 // trn2 v22.2d, v16.2d, v18.2d
	WORD $0x4ed36a37 // trn2 v23.2d, v17.2d, v19.2d
	WORD $0x4eb78698 // add v24.4s, v20.4s, v23.4s
	WORD $0x4eb686b9 // add v25.4s, v21.4s, v22.4s
	WORD $0x6eb686ba // sub v26.4s, v21.4s, v22.4s
	WORD $0x6eb7869b // sub v27.4s, v20.4s, v23.4s
	WORD $0x4e1405bc // dup v28.4s, v13.s[2]
	WORD $0x4eb98704 // add v4.4s, v24.4s, v25.4s
	WORD $0x4ebc8484 // add v4.4s, v4.4s, v28.4s
	WORD $0x4f3c0484 // sshr v4.4s, v4.4s, #4
	WORD $0x6eb98706 // sub v6.4s, v24.4s, v25.4s
	WORD $0x4ebc84c6 // add v6.4s, v6.4s, v28.4s
	WORD $0x4f3c04c6 // sshr v6.4s, v6.4s, #4
	WORD $0x4e0405a5 // dup v5.4s, v13.s[0]
	WORD $0x6f8c0345 // mla v5.4s, v26.4s, v12.s[0]
	WORD $0x6fac0365 // mla v5.4s, v27.4s, v12.s[1]
	WORD $0x4f3004a5 // sshr v5.4s, v5.4s, #16
	WORD $0x4ebb8f7d // cmtst v29.4s, v27.4s, v27.4s
	WORD $0x6ebd84a5 // sub v5.4s, v5.4s, v29.4s
	WORD $0x4e0c05a7 // dup v7.4s, v13.s[1]
	WORD $0x6f8c0367 // mla v7.4s, v27.4s, v12.s[0]
	WORD $0x6fac4347 // mls v7.4s, v26.4s, v12.s[1]
	WORD $0x4f3004e7 // sshr v7.4s, v7.4s, #16
	WORD $0x4cdf28e8 // ld1 {v8.4s, v9.4s, v10.4s, v11.4s}, [x7], #64
	WORD $0x4cdf28f0 // ld1 {v16.4s, v17.4s, v18.4s, v19.4s}, [x7], #64
	WORD $0x4e1c05bf // dup v31.4s, v13.s[3]
	WORD $0x4f21049c // sshr v28.4s, v4.4s, #31
	WORD $0x4ea0b89d // abs v29.4s, v4.4s
	WORD $0x4ea897b0 // mla v16.4s, v29.4s, v8.4s
	WORD $0x6f2f0614 // ushr v20.4s, v16.4s, #17
	WORD $0x6ebf6e94 // umin v20.4s, v20.4s, v31.4s
	WORD $0x6e3c1e94 // eor v20.16b, v20.16b, v28.16b
	WORD $0x6ebc8694 // sub v20.4s, v20.4s, v28.4s
	WORD $0x4f2104bc // sshr v28.4s, v5.4s, #31
	WORD $0x4ea0b8bd // abs v29.4s, v5.4s
	WORD $0x4ea997b1 // mla v17.4s, v29.4s, v9.4s
	WORD $0x6f2f0635 // ushr v21.4s, v17.4s, #17
	WORD $0x6ebf6eb5 // umin v21.4s, v21.4s, v31.4s
	WORD $0x6e3c1eb5 // eor v21.16b, v21.16b, v28.16b
	WORD $0x6ebc86b5 // sub v21.4s, v21.4s, v28.4s
	WORD $0x4f2104dc // sshr v28.4s, v6.4s, #31
	WORD $0x4ea0b8dd // abs v29.4s, v6.4s
	WORD $0x4eaa97b2 // mla v18.4s, v29.4s, v10.4s
	WORD $0x6f2f0656 // ushr v22.4s, v18.4s, #17
	WORD $0x6ebf6ed6 // umin v22.4s, v22.4s, v31.4s
	WORD $0x6e3c1ed6 // eor v22.16b, v22.16b, v28.16b
	WORD $0x6ebc86d6 // sub v22.4s, v22.4s, v28.4s
	WORD $0x4f2104fc // sshr v28.4s, v7.4s, #31
	WORD $0x4ea0b8fd // abs v29.4s, v7.4s
	WORD $0x4eab97b3 // mla v19.4s, v29.4s, v11.4s
	WORD $0x6f2f0677 // ushr v23.4s, v19.4s, #17
	WORD $0x6ebf6ef7 // umin v23.4s, v23.4s, v31.4s
	WORD $0x6e3c1ef7 // eor v23.16b, v23.16b, v28.16b
	WORD $0x6ebc86f7 // sub v23.4s, v23.4s, v28.4s
	WORD $0x0e612a9c // xtn v28.4h, v20.4s
	WORD $0x4e612abc // xtn2 v28.8h, v21.4s
	WORD $0x0e612add // xtn v29.4h, v22.4s
	WORD $0x4e612afd // xtn2 v29.8h, v23.4s
	WORD $0x4c00a49c // st1 {v28.8h, v29.8h}, [x4]
	WORD $0x4c4028e8 // ld1 {v8.4s, v9.4s, v10.4s, v11.4s}, [x7]
	WORD $0x4ea89e98 // mul v24.4s, v20.4s, v8.4s
	WORD $0x4ea99eb9 // mul v25.4s, v21.4s, v9.4s
	WORD $0x4eaa9eda // mul v26.4s, v22.4s, v10.4s
	WORD $0x4eab9efb // mul v27.4s, v23.4s, v11.4s
	WORD $0x4eba8710 // add v16.4s, v24.4s, v26.4s
	WORD $0x6eba8711 // sub v17.4s, v24.4s, v26.4s
	WORD $0x4f8e833c // mul v28.4s, v25.4s, v14.s[0]
	WORD $0x4f300792 // sshr v18.4s, v28.4s, #16
	WORD $0x4fae837c // mul v28.4s, v27.4s, v14.s[1]
	WORD $0x4f30079d // sshr v29.4s, v28.4s, #16
	WORD $0x6ebd8652 // sub v18.4s, v18.4s, v29.4s
	WORD $0x4fae833c // mul v28.4s, v25.4s, v14.s[1]
	WORD $0x4f300793 // sshr v19.4s, v28.4s, #16
	WORD $0x4f8e837c // mul v28.4s, v27.4s, v14.s[0]
	WORD $0x4f30079d // sshr v29.4s, v28.4s, #16
	WORD $0x4ebd8673 // add v19.4s, v19.4s, v29.4s
	WORD $0x4eb38604 // add v4.4s, v16.4s, v19.4s
	WORD $0x4eb28625 // add v5.4s, v17.4s, v18.4s
	WORD $0x6eb28626 // sub v6.4s, v17.4s, v18.4s
	WORD $0x6eb38607 // sub v7.4s, v16.4s, v19.4s
	WORD $0x4e852890 // trn1 v16.4s, v4.4s, v5.4s
	WORD $0x4e856891 // trn2 v17.4s, v4.4s, v5.4s
	WORD $0x4e8728d2 // trn1 v18.4s, v6.4s, v7.4s
	WORD $0x4e8768d3 // trn2 v19.4s, v6.4s, v7.4s
	WORD $0x4ed22a14 // trn1 v20.2d, v16.2d, v18.2d
	WORD $0x4ed32a35 // trn1 v21.2d, v17.2d, v19.2d
	WORD $0x4ed26a16 // trn2 v22.2d, v16.2d, v18.2d
	WORD $0x4ed36a37 // trn2 v23.2d, v17.2d, v19.2d
	WORD $0x4e1405dc // dup v28.4s, v14.s[2]
	WORD $0x4ebc8694 // add v20.4s, v20.4s, v28.4s
	WORD $0x4eb68690 // add v16.4s, v20.4s, v22.4s
	WORD $0x6eb68691 // sub v17.4s, v20.4s, v22.4s
	WORD $0x4f8e82bc // mul v28.4s, v21.4s, v14.s[0]
	WORD $0x4f300792 // sshr v18.4s, v28.4s, #16
	WORD $0x4fae82fc // mul v28.4s, v23.4s, v14.s[1]
	WORD $0x4f30079d // sshr v29.4s, v28.4s, #16
	WORD $0x6ebd8652 // sub v18.4s, v18.4s, v29.4s
	WORD $0x4fae82bc // mul v28.4s, v21.4s, v14.s[1]
	WORD $0x4f300793 // sshr v19.4s, v28.4s, #16
	WORD $0x4f8e82fc // mul v28.4s, v23.4s, v14.s[0]
	WORD $0x4f30079d // sshr v29.4s, v28.4s, #16
	WORD $0x4ebd8673 // add v19.4s, v19.4s, v29.4s
	WORD $0x4eb38604 // add v4.4s, v16.4s, v19.4s
	WORD $0x4eb28625 // add v5.4s, v17.4s, v18.4s
	WORD $0x6eb28626 // sub v6.4s, v17.4s, v18.4s
	WORD $0x6eb38607 // sub v7.4s, v16.4s, v19.4s
	WORD $0x4f3d0484 // sshr v4.4s, v4.4s, #3
	WORD $0x4f3d04a5 // sshr v5.4s, v5.4s, #3
	WORD $0x4f3d04c6 // sshr v6.4s, v6.4s, #3
	WORD $0x4f3d04e7 // sshr v7.4s, v7.4s, #3
	WORD $0x4e852890 // trn1 v16.4s, v4.4s, v5.4s
	WORD $0x4e856891 // trn2 v17.4s, v4.4s, v5.4s
	WORD $0x4e8728d2 // trn1 v18.4s, v6.4s, v7.4s
	WORD $0x4e8768d3 // trn2 v19.4s, v6.4s, v7.4s
	WORD $0x4ed22a14 // trn1 v20.2d, v16.2d, v18.2d
	WORD $0x4ed32a35 // trn1 v21.2d, v17.2d, v19.2d
	WORD $0x4ed26a16 // trn2 v22.2d, v16.2d, v18.2d
	WORD $0x4ed36a37 // trn2 v23.2d, v17.2d, v19.2d
	WORD $0x0e614a98 // sqxtn v24.4h, v20.4s
	WORD $0x4e614ab8 // sqxtn2 v24.8h, v21.4s
	WORD $0x0e614ad9 // sqxtn v25.4h, v22.4s
	WORD $0x4e614af9 // sqxtn2 v25.8h, v23.4s
	WORD $0x2f08a43a // uxtl v26.8h, v1.8b
	WORD $0x6f08a43b // uxtl2 v27.8h, v1.16b
	WORD $0x4e7a0f18 // sqadd v24.8h, v24.8h, v26.8h
	WORD $0x4e7b0f39 // sqadd v25.8h, v25.8h, v27.8h
	WORD $0x2e212b1e // sqxtun v30.8b, v24.8h
	WORD $0x6e212b3e // sqxtun2 v30.16b, v25.8h
	WORD $0x4c0070be // st1 {v30.16b}, [x5]
	WORD $0x2e2023c2 // usubl v2.8h, v30.8b, v0.8b
	WORD $0x6e2023c3 // usubl2 v3.8h, v30.16b, v0.16b
	WORD $0x0e62c044 // smull v4.4s, v2.4h, v2.4h
	WORD $0x4e628044 // smlal2 v4.4s, v2.8h, v2.8h
	WORD $0x0e638064 // smlal v4.4s, v3.4h, v3.4h
	WORD $0x4e638064 // smlal2 v4.4s, v3.8h, v3.8h
	WORD $0x4eb1b884 // addv s4, v4.4s
	WORD $0x1e260086 // fmov w6, s4
	MOVW R6, ret+48(FP)
	RET

// func satd4NEON(src *uint8, stride int, pred *[16]uint8) uint32
TEXT ·satd4NEON(SB), NOSPLIT, $0-28
	MOVD src+0(FP), R0
	MOVD stride+8(FP), R1
	MOVD pred+16(FP), R2
	WORD $0xb9400006 // ldr w6, [x0]
	WORD $0x4e041cc0 // ins v0.s[0], w6
	WORD $0x8b010000 // add x0, x0, x1
	WORD $0xb9400006 // ldr w6, [x0]
	WORD $0x4e0c1cc0 // ins v0.s[1], w6
	WORD $0x8b010000 // add x0, x0, x1
	WORD $0xb9400006 // ldr w6, [x0]
	WORD $0x4e141cc0 // ins v0.s[2], w6
	WORD $0x8b010000 // add x0, x0, x1
	WORD $0xb9400006 // ldr w6, [x0]
	WORD $0x4e1c1cc0 // ins v0.s[3], w6
	WORD $0x4c407041 // ld1 {v1.16b}, [x2]
	WORD $0x2e212002 // usubl v2.8h, v0.8b, v1.8b
	WORD $0x6e212003 // usubl2 v3.8h, v0.16b, v1.16b
	WORD $0x0f10a444 // sxtl v4.4s, v2.4h
	WORD $0x4f10a445 // sxtl2 v5.4s, v2.8h
	WORD $0x0f10a466 // sxtl v6.4s, v3.4h
	WORD $0x4f10a467 // sxtl2 v7.4s, v3.8h
	WORD $0x4ea68498 // add v24.4s, v4.4s, v6.4s
	WORD $0x4ea784b9 // add v25.4s, v5.4s, v7.4s
	WORD $0x6ea784ba // sub v26.4s, v5.4s, v7.4s
	WORD $0x6ea6849b // sub v27.4s, v4.4s, v6.4s
	WORD $0x4eb98704 // add v4.4s, v24.4s, v25.4s
	WORD $0x4eba8765 // add v5.4s, v27.4s, v26.4s
	WORD $0x6eba8766 // sub v6.4s, v27.4s, v26.4s
	WORD $0x6eb98707 // sub v7.4s, v24.4s, v25.4s
	WORD $0x4e852890 // trn1 v16.4s, v4.4s, v5.4s
	WORD $0x4e856891 // trn2 v17.4s, v4.4s, v5.4s
	WORD $0x4e8728d2 // trn1 v18.4s, v6.4s, v7.4s
	WORD $0x4e8768d3 // trn2 v19.4s, v6.4s, v7.4s
	WORD $0x4ed22a14 // trn1 v20.2d, v16.2d, v18.2d
	WORD $0x4ed32a35 // trn1 v21.2d, v17.2d, v19.2d
	WORD $0x4ed26a16 // trn2 v22.2d, v16.2d, v18.2d
	WORD $0x4ed36a37 // trn2 v23.2d, v17.2d, v19.2d
	WORD $0x4eb68698 // add v24.4s, v20.4s, v22.4s
	WORD $0x4eb786b9 // add v25.4s, v21.4s, v23.4s
	WORD $0x6eb786ba // sub v26.4s, v21.4s, v23.4s
	WORD $0x6eb6869b // sub v27.4s, v20.4s, v22.4s
	WORD $0x4eb98704 // add v4.4s, v24.4s, v25.4s
	WORD $0x4eba8765 // add v5.4s, v27.4s, v26.4s
	WORD $0x6eba8766 // sub v6.4s, v27.4s, v26.4s
	WORD $0x6eb98707 // sub v7.4s, v24.4s, v25.4s
	WORD $0x4ea0b884 // abs v4.4s, v4.4s
	WORD $0x4ea0b8a5 // abs v5.4s, v5.4s
	WORD $0x4ea0b8c6 // abs v6.4s, v6.4s
	WORD $0x4ea0b8e7 // abs v7.4s, v7.4s
	WORD $0x4ea58484 // add v4.4s, v4.4s, v5.4s
	WORD $0x4ea784c6 // add v6.4s, v6.4s, v7.4s
	WORD $0x4ea68484 // add v4.4s, v4.4s, v6.4s
	WORD $0x4eb1b884 // addv s4, v4.4s
	WORD $0x1e260086 // fmov w6, s4
	MOVW R6, ret+24(FP)
	RET

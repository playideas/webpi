"""
arm64 NEON 커널 생성기.

Go 어셈블러는 정수 곱셈·부호 시프트·포화 축소 같은 NEON 명령을 모르므로,
ARM 문법으로 쓴 본문을 clang으로 어셈블해 WORD 상수로 바꿔 Go .s 파일을 만든다.
인자 읽기·반환만 Go 문법으로 쓴다.

실행: uv run python internal/asmgen/gen.py  (저장소 루트에서, macOS clang 필요)
"""

import re
import subprocess
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / "kernels_arm64.s"


def transpose(a: list[str], t: list[str], out: list[str]) -> list[str]:
    """4×4 int32 전치: 행 벡터 a[0..3] → 열 벡터 out[0..3]. t는 임시 레지스터 4개."""
    return [
        f"trn1 {t[0]}.4s, {a[0]}.4s, {a[1]}.4s",
        f"trn2 {t[1]}.4s, {a[0]}.4s, {a[1]}.4s",
        f"trn1 {t[2]}.4s, {a[2]}.4s, {a[3]}.4s",
        f"trn2 {t[3]}.4s, {a[2]}.4s, {a[3]}.4s",
        f"trn1 {out[0]}.2d, {t[0]}.2d, {t[2]}.2d",
        f"trn1 {out[1]}.2d, {t[1]}.2d, {t[3]}.2d",
        f"trn2 {out[2]}.2d, {t[0]}.2d, {t[2]}.2d",
        f"trn2 {out[3]}.2d, {t[1]}.2d, {t[3]}.2d",
    ]


def mulshr16(dst: str, x: str, kidx: str, tmp: str) -> list[str]:
    """dst = (x * k) >> 16 (int32, 곱은 디코더·Go와 같이 32비트에서 넘친다)."""
    return [f"mul {tmp}.4s, {x}.4s, {kidx}", f"sshr {dst}.4s, {tmp}.4s, #16"]


# 상수 벡터 (quantVecs.k, Go 쪽과 같은 순서):
#   v12 = [fdctK1 2217, fdctK2 5352, 1812, 937]
#   v13 = [12000, 51000, 7, maxLevel 2047]
#   v14 = [idctC2 35468, idctC1 85627, idctRound 4, 0]
K1, K2, R1812, R937 = "v12.s[0]", "v12.s[1]", "v12.s[2]", "v12.s[3]"
R12000, R51000, R7, MAXL = "v13.s[0]", "v13.s[1]", "v13.s[2]", "v13.s[3]"
C2, C1, R4 = "v14.s[0]", "v14.s[1]", "v14.s[2]"


def load_quant_inputs() -> list[str]:
    """원본·예측을 읽고 상수 벡터 v12..v14와 양자화 표 포인터 x7을 준비한다."""
    s = []
    # 원본 4행(행마다 4바이트)을 v0에, 예측 16바이트를 v1에
    for row in range(4):
        s += ["ldr w6, [x0]", f"ins v0.s[{row}], w6"]
        if row < 3:
            s.append("add x0, x0, x1")
    s += ["ld1 {v1.16b}, [x2]", "ld1 {v12.4s, v13.4s, v14.4s}, [x3]"]
    s += ["add x7, x3, #48"]
    # 잔차 int16 → 행 벡터 int32 (v4..v7)
    s += [
        "usubl v2.8h, v0.8b, v1.8b",
        "usubl2 v3.8h, v0.16b, v1.16b",
        "sxtl v4.4s, v2.4h",
        "sxtl2 v5.4s, v2.8h",
        "sxtl v6.4s, v3.4h",
        "sxtl2 v7.4s, v3.8h",
    ]
    return s


def fdct() -> list[str]:
    """v4..v7의 잔차 행을 순 DCT해 자연 순서 계수 행 v4..v7로 만든다."""
    s = []
    # fdct 행 단계: 열 벡터 D0..D3 (레인 = 행)
    s += transpose(["v4", "v5", "v6", "v7"], ["v16", "v17", "v18", "v19"], ["v20", "v21", "v22", "v23"])
    s += [
        "add v24.4s, v20.4s, v23.4s",  # a0
        "add v25.4s, v21.4s, v22.4s",  # a1
        "sub v26.4s, v21.4s, v22.4s",  # a2
        "sub v27.4s, v20.4s, v23.4s",  # a3
        "add v4.4s, v24.4s, v25.4s",
        "shl v4.4s, v4.4s, #3",  # t0
        "sub v6.4s, v24.4s, v25.4s",
        "shl v6.4s, v6.4s, #3",  # t2
        f"dup v5.4s, {R1812}",
        f"mla v5.4s, v26.4s, {K1}",
        f"mla v5.4s, v27.4s, {K2}",
        "sshr v5.4s, v5.4s, #9",  # t1
        f"dup v7.4s, {R937}",
        f"mla v7.4s, v27.4s, {K1}",
        f"mls v7.4s, v26.4s, {K2}",
        "sshr v7.4s, v7.4s, #9",  # t3
    ]
    # fdct 열 단계: tmp의 행 벡터 R0..R3 (레인 = 열)
    s += transpose(["v4", "v5", "v6", "v7"], ["v16", "v17", "v18", "v19"], ["v20", "v21", "v22", "v23"])
    s += [
        "add v24.4s, v20.4s, v23.4s",  # a0
        "add v25.4s, v21.4s, v22.4s",  # a1
        "sub v26.4s, v21.4s, v22.4s",  # a2
        "sub v27.4s, v20.4s, v23.4s",  # a3
        f"dup v28.4s, {R7}",
        "add v4.4s, v24.4s, v25.4s",
        "add v4.4s, v4.4s, v28.4s",
        "sshr v4.4s, v4.4s, #4",  # out 0..3
        "sub v6.4s, v24.4s, v25.4s",
        "add v6.4s, v6.4s, v28.4s",
        "sshr v6.4s, v6.4s, #4",  # out 8..11
        f"dup v5.4s, {R12000}",
        f"mla v5.4s, v26.4s, {K1}",
        f"mla v5.4s, v27.4s, {K2}",
        "sshr v5.4s, v5.4s, #16",
        "cmtst v29.4s, v27.4s, v27.4s",
        "sub v5.4s, v5.4s, v29.4s",  # out 4..7 (+ (a3 != 0))
        f"dup v7.4s, {R51000}",
        f"mla v7.4s, v27.4s, {K1}",
        f"mls v7.4s, v26.4s, {K2}",
        "sshr v7.4s, v7.4s, #16",  # out 12..15
    ]
    return s


def quantize() -> list[str]:
    """v4..v7 계수를 양자화해 레벨을 levels에 쓰고 역양자화 계수를 v24..v27에 둔다."""
    s = []
    # 양자화: v4..v7 계수 → 레벨 v20..v23, 역양자화 v24..v27
    s += ["ld1 {v8.4s, v9.4s, v10.4s, v11.4s}, [x7], #64"]  # iq
    s += ["ld1 {v16.4s, v17.4s, v18.4s, v19.4s}, [x7], #64"]  # rbias
    s += [f"dup v31.4s, {MAXL}"]
    for k in range(4):
        c, iq, rb, lv = f"v{4+k}", f"v{8+k}", f"v{16+k}", f"v{20+k}"
        s += [
            f"sshr v28.4s, {c}.4s, #31",
            f"abs v29.4s, {c}.4s",
            f"mla {rb}.4s, v29.4s, {iq}.4s",
            f"ushr {lv}.4s, {rb}.4s, #17",
            f"umin {lv}.4s, {lv}.4s, v31.4s",
            f"eor {lv}.16b, {lv}.16b, v28.16b",
            f"sub {lv}.4s, {lv}.4s, v28.4s",
        ]
    s += [
        "xtn v28.4h, v20.4s",
        "xtn2 v28.8h, v21.4s",
        "xtn v29.4h, v22.4s",
        "xtn2 v29.8h, v23.4s",
        "st1 {v28.8h, v29.8h}, [x4]",
        "ld1 {v8.4s, v9.4s, v10.4s, v11.4s}, [x7]",  # q
    ]
    for k in range(4):
        s.append(f"mul v{24+k}.4s, v{20+k}.4s, v{8+k}.4s")
    return s


def idct() -> list[str]:
    """v24..v27을 디코더와 같은 역 DCT로 풀어 레인 j = 행 j인 열 벡터 v4..v7을 만든다."""
    s = []
    # 역 DCT 1단계 (레인 i = 열 i): M0..M3 → v4..v7
    s += [
        "add v16.4s, v24.4s, v26.4s",  # a
        "sub v17.4s, v24.4s, v26.4s",  # b
    ]
    s += mulshr16("v18", "v25", C2, "v28") + mulshr16("v29", "v27", C1, "v28")
    s += ["sub v18.4s, v18.4s, v29.4s"]  # cc
    s += mulshr16("v19", "v25", C1, "v28") + mulshr16("v29", "v27", C2, "v28")
    s += [
        "add v19.4s, v19.4s, v29.4s",  # d
        "add v4.4s, v16.4s, v19.4s",
        "add v5.4s, v17.4s, v18.4s",
        "sub v6.4s, v17.4s, v18.4s",
        "sub v7.4s, v16.4s, v19.4s",
    ]
    # 2단계 입력: V_k 레인 j = m[k][j]
    s += transpose(["v4", "v5", "v6", "v7"], ["v16", "v17", "v18", "v19"], ["v20", "v21", "v22", "v23"])
    s += [
        f"dup v28.4s, {R4}",
        "add v20.4s, v20.4s, v28.4s",  # dc
        "add v16.4s, v20.4s, v22.4s",  # a
        "sub v17.4s, v20.4s, v22.4s",  # b
    ]
    s += mulshr16("v18", "v21", C2, "v28") + mulshr16("v29", "v23", C1, "v28")
    s += ["sub v18.4s, v18.4s, v29.4s"]  # cc
    s += mulshr16("v19", "v21", C1, "v28") + mulshr16("v29", "v23", C2, "v28")
    s += [
        "add v19.4s, v19.4s, v29.4s",  # d
        "add v4.4s, v16.4s, v19.4s",
        "add v5.4s, v17.4s, v18.4s",
        "sub v6.4s, v17.4s, v18.4s",
        "sub v7.4s, v16.4s, v19.4s",
        "sshr v4.4s, v4.4s, #3",
        "sshr v5.4s, v5.4s, #3",
        "sshr v6.4s, v6.4s, #3",
        "sshr v7.4s, v7.4s, #3",
    ]
    return s


def reconstruct_sse() -> list[str]:
    """예측에 더해 rec에 쓰고 SSE를 w6에 둔다."""
    s = []
    # 레인 j = 행 j인 열 벡터 → 행 벡터로 전치 후 예측에 포화 덧셈, 0~255로 포화 축소
    s += transpose(["v4", "v5", "v6", "v7"], ["v16", "v17", "v18", "v19"], ["v20", "v21", "v22", "v23"])
    s += [
        "sqxtn v24.4h, v20.4s",
        "sqxtn2 v24.8h, v21.4s",
        "sqxtn v25.4h, v22.4s",
        "sqxtn2 v25.8h, v23.4s",
        "uxtl v26.8h, v1.8b",
        "uxtl2 v27.8h, v1.16b",
        "sqadd v24.8h, v24.8h, v26.8h",
        "sqadd v25.8h, v25.8h, v27.8h",
        "sqxtun v30.8b, v24.8h",
        "sqxtun2 v30.16b, v25.8h",
        "st1 {v30.16b}, [x5]",
    ]
    # SSE(원본, 복원)
    s += [
        "usubl v2.8h, v30.8b, v0.8b",
        "usubl2 v3.8h, v30.16b, v0.16b",
        "smull v4.4s, v2.4h, v2.4h",
        "smlal2 v4.4s, v2.8h, v2.8h",
        "smlal v4.4s, v3.4h, v3.4h",
        "smlal2 v4.4s, v3.8h, v3.8h",
        "addv s4, v4.4s",
        "fmov w6, s4",
    ]
    return s


def quant_block4() -> list[str]:
    """
    quantBlock4(src *uint8, stride int, pred *[16]uint8, qv *quantVecs, levels *[16]int16, rec *[16]uint8) uint32
    x0=src x1=stride x2=pred x3=qv x4=levels x5=rec. 반환은 w6 (원본과 복원의 SSE).
    Go의 residual4 → fdct4 → quantize → idct4Add → regionSSE와 비트 단위로 같다.
    """
    return load_quant_inputs() + fdct() + quantize() + idct() + reconstruct_sse()


def satd4() -> list[str]:
    """
    satd4NEON(src *uint8, stride int, pred *[16]uint8) uint32
    잔차 4×4의 하다마드 계수 절대합. 덧셈만 쓰므로 세로 단계를 먼저 하고 전치 한 번 뒤 가로 단계를 해도
    Go(가로 → 세로)와 값이 같다. 계수 순서는 절대합에 영향이 없다.
    """
    s = []
    for row in range(4):
        s += ["ldr w6, [x0]", f"ins v0.s[{row}], w6"]
        if row < 3:
            s.append("add x0, x0, x1")
    s += [
        "ld1 {v1.16b}, [x2]",
        "usubl v2.8h, v0.8b, v1.8b",
        "usubl2 v3.8h, v0.16b, v1.16b",
        "sxtl v4.4s, v2.4h",
        "sxtl2 v5.4s, v2.8h",
        "sxtl v6.4s, v3.4h",
        "sxtl2 v7.4s, v3.8h",
    ]

    def butterfly(r: list[str]) -> list[str]:
        # 출력: r0+r1+r2+r3, (r0-r2)+(r1-r3), (r0-r2)-(r1-r3), (r0+r2)-(r1+r3) → v4..v7
        return [
            f"add v24.4s, {r[0]}.4s, {r[2]}.4s",
            f"add v25.4s, {r[1]}.4s, {r[3]}.4s",
            f"sub v26.4s, {r[1]}.4s, {r[3]}.4s",
            f"sub v27.4s, {r[0]}.4s, {r[2]}.4s",
            "add v4.4s, v24.4s, v25.4s",
            "add v5.4s, v27.4s, v26.4s",
            "sub v6.4s, v27.4s, v26.4s",
            "sub v7.4s, v24.4s, v25.4s",
        ]

    s += butterfly(["v4", "v5", "v6", "v7"])
    s += transpose(["v4", "v5", "v6", "v7"], ["v16", "v17", "v18", "v19"], ["v20", "v21", "v22", "v23"])
    s += butterfly(["v20", "v21", "v22", "v23"])
    s += [
        "abs v4.4s, v4.4s",
        "abs v5.4s, v5.4s",
        "abs v6.4s, v6.4s",
        "abs v7.4s, v7.4s",
        "add v4.4s, v4.4s, v5.4s",
        "add v6.4s, v6.4s, v7.4s",
        "add v4.4s, v4.4s, v6.4s",
        "addv s4, v4.4s",
        "fmov w6, s4",
    ]
    return s


def assemble(lines: list[str]) -> list[int]:
    with tempfile.TemporaryDirectory() as d:
        src, obj = Path(d) / "k.s", Path(d) / "k.o"
        src.write_text(".text\n" + "\n".join(lines) + "\n")
        subprocess.run(["clang", "-c", "-arch", "arm64", str(src), "-o", str(obj)], check=True)
        dump = subprocess.run(["objdump", "-d", str(obj)], check=True, capture_output=True, text=True).stdout
    words = [int(m.group(1), 16) for m in re.finditer(r"^\s+[0-9a-f]+:\s+([0-9a-f]{8})\s", dump, re.M)]
    if len(words) != len(lines):
        raise RuntimeError(f"명령 {len(lines)}개, 기계어 {len(words)}개")
    return words


# (이름, Go 선언, 인자 이름 목록, 반환 오프셋, 본문)
KERNELS = [
    (
        "quantBlock4",
        "func quantBlock4(src *uint8, stride int, pred *[16]uint8, qv *quantVecs, levels *[16]int16, rec *[16]uint8) uint32",
        ["src", "stride", "pred", "qv", "levels", "rec"],
        quant_block4,
    ),
    ("satd4NEON", "func satd4NEON(src *uint8, stride int, pred *[16]uint8) uint32", ["src", "stride", "pred"], satd4),
]


def emit() -> str:
    out = ["// 생성 파일 — internal/asmgen/gen.py로 만든다. 직접 고치지 않는다.", "", '#include "textflag.h"']
    for name, decl, args, body_fn in KERNELS:
        body = body_fn()
        words = assemble(body)
        ret = len(args) * 8
        out += ["", f"// {decl}", f"TEXT ·{name}(SB), NOSPLIT, $0-{ret + 4}"]
        out += [f"\tMOVD {a}+{i * 8}(FP), R{i}" for i, a in enumerate(args)]
        out += [f"\tWORD ${w:#010x} // {line}" for w, line in zip(words, body)]
        out += [f"\tMOVW R6, ret+{ret}(FP)", "\tRET"]
    return "\n".join(out) + "\n"


if __name__ == "__main__":
    OUT.write_text(emit())
    print(f"{OUT} 생성")

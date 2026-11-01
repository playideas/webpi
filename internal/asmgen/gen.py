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


# 스크래치 범용 레지스터와 반환 레지스터 (인자는 x0..x7을 쓴다)
TMP = "w9"
QTAB = "x10"
RET = "w11"


def load_rows(dst: str, ptr: str, stride: str) -> list[str]:
    """ptr에서 stride 간격 4행(행마다 4바이트)을 dst의 32비트 레인 0..3에 읽는다. ptr은 바뀐다."""
    s = []
    for row in range(4):
        s += [f"ldr {TMP}, [{ptr}]", f"ins {dst}.s[{row}], {TMP}"]
        if row < 3:
            s.append(f"add {ptr}, {ptr}, {stride}")
    return s


def store_rows(src: str, ptr: str, stride: str) -> list[str]:
    """src의 32비트 레인 0..3을 ptr부터 stride 간격 4행에 쓴다. ptr은 바뀐다."""
    s = []
    for row in range(4):
        s.append(f"st1 {{{src}.s}}[{row}], [{ptr}]")
        if row < 3:
            s.append(f"add {ptr}, {ptr}, {stride}")
    return s


def load_consts(kptr: str) -> list[str]:
    """변환 상수 v12..v14를 읽는다."""
    return [f"ld1 {{v12.4s, v13.4s, v14.4s}}, [{kptr}]"]


def residual() -> list[str]:
    """원본 v0 − 예측 v1 → 행 벡터 int32 v4..v7."""
    return [
        "usubl v2.8h, v0.8b, v1.8b",
        "usubl2 v3.8h, v0.16b, v1.16b",
        "sxtl v4.4s, v2.4h",
        "sxtl2 v5.4s, v2.8h",
        "sxtl v6.4s, v3.4h",
        "sxtl2 v7.4s, v3.8h",
    ]


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


def quantize(qtab: str, levels: str, dc: str | None = None) -> list[str]:
    """
    v4..v7 계수를 양자화해 레벨을 levels에 쓰고 역양자화 계수를 v24..v27에 둔다.
    qtab은 quantVecs.iq를 가리키며 바뀐다. dc가 있으면 i16 휘도처럼 레벨 0번은 0으로,
    역양자화 0번은 dc(Y2에서 복원한 값)로 바꾼다.
    """
    s = []
    s += [f"ld1 {{v8.4s, v9.4s, v10.4s, v11.4s}}, [{qtab}], #64"]  # iq
    s += [f"ld1 {{v16.4s, v17.4s, v18.4s, v19.4s}}, [{qtab}], #64"]  # rbias
    s += [f"dup v31.4s, {MAXL}"]
    for k in range(4):
        c, iq, rb, lv = f"v{4 + k}", f"v{8 + k}", f"v{16 + k}", f"v{20 + k}"
        s += [
            f"sshr v28.4s, {c}.4s, #31",
            f"abs v29.4s, {c}.4s",
            f"mla {rb}.4s, v29.4s, {iq}.4s",
            f"ushr {lv}.4s, {rb}.4s, #17",
            f"umin {lv}.4s, {lv}.4s, v31.4s",
            f"eor {lv}.16b, {lv}.16b, v28.16b",
            f"sub {lv}.4s, {lv}.4s, v28.4s",
        ]
    if dc is not None:
        s.append("mov v20.s[0], wzr")
    s += [
        "xtn v28.4h, v20.4s",
        "xtn2 v28.8h, v21.4s",
        "xtn v29.4h, v22.4s",
        "xtn2 v29.8h, v23.4s",
        f"st1 {{v28.8h, v29.8h}}, [{levels}]",
        f"ld1 {{v8.4s, v9.4s, v10.4s, v11.4s}}, [{qtab}]",  # q
    ]
    for k in range(4):
        s.append(f"mul v{24 + k}.4s, v{20 + k}.4s, v{8 + k}.4s")
    if dc is not None:
        s.append(f"mov v24.s[0], {dc}")
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


def reconstruct(rec: str, rstride: str) -> list[str]:
    """역 DCT 결과(v4..v7)를 예측 v1에 더해 v30에 두고 rec부터 rstride 간격 4행에 쓴다."""
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
    ]
    return s + store_rows("v30", rec, rstride)


def sse() -> list[str]:
    """원본 v0과 복원 v30의 SSE를 RET에 둔다."""
    return [
        "usubl v2.8h, v30.8b, v0.8b",
        "usubl2 v3.8h, v30.16b, v0.16b",
        "smull v4.4s, v2.4h, v2.4h",
        "smlal2 v4.4s, v2.8h, v2.8h",
        "smlal v4.4s, v3.4h, v3.4h",
        "smlal2 v4.4s, v3.8h, v3.8h",
        "addv s4, v4.4s",
        f"fmov {RET}, s4",
    ]


def quant_block4() -> list[str]:
    """
    x0=src x1=stride x2=pred x3=pstride x4=qv x5=levels x6=rec x7=rstride. 반환은 원본과 복원의 SSE.
    Go의 quantBlockGo(잔차 → fdct4 → quantize → idct4Add → regionSSE)와 비트 단위로 같다.
    """
    s = load_rows("v0", "x0", "x1") + load_rows("v1", "x2", "x3") + load_consts("x4")
    s += [f"add {QTAB}, x4, #48"]
    return s + residual() + fdct() + quantize(QTAB, "x5") + idct() + reconstruct("x6", "x7") + sse()


def quant_dc4() -> list[str]:
    """
    x0=coeffs x1=dc x2=qv x3=levels x4=pred x5=pstride x6=rec x7=rstride. 반환 없음.
    i16 휘도 블록: AC만 양자화하고 DC는 Y2에서 복원한 dc로 바꿔 역변환한다 (quantBlockDCGo와 같음).
    """
    s = ["ld1 {v4.4s, v5.4s, v6.4s, v7.4s}, [x0]"] + load_rows("v1", "x4", "x5") + load_consts("x2")
    s += [f"add {QTAB}, x2, #48"]
    return s + quantize(QTAB, "x3", dc="w1") + idct() + reconstruct("x6", "x7")


def fdct_block4() -> list[str]:
    """x0=src x1=stride x2=pred x3=pstride x4=k x5=out. 잔차의 순 DCT 계수를 out에 쓴다 (fdctBlockGo와 같음)."""
    s = load_rows("v0", "x0", "x1") + load_rows("v1", "x2", "x3") + load_consts("x4")
    return s + residual() + fdct() + ["st1 {v4.4s, v5.4s, v6.4s, v7.4s}, [x5]"]


def fdct_bins4() -> list[str]:
    """
    x0=src x1=stride x2=pred x3=pstride x4=k x5=bins. 분석 히스토그램용으로 계수마다
    min(|c|>>3, 31)을 bins에 쓴다 (fdctBinsGo와 같음).
    """
    s = load_rows("v0", "x0", "x1") + load_rows("v1", "x2", "x3") + load_consts("x4")
    s += residual() + fdct() + ["movi v28.4s, #31"]
    for k in range(4):
        r = f"v{4 + k}"
        s += [f"abs {r}.4s, {r}.4s", f"ushr {r}.4s, {r}.4s, #3", f"umin {r}.4s, {r}.4s, v28.4s"]
    return s + [
        "xtn v16.4h, v4.4s",
        "xtn2 v16.8h, v5.4s",
        "xtn v17.4h, v6.4s",
        "xtn2 v17.8h, v7.4s",
        "xtn v18.8b, v16.8h",
        "xtn2 v18.16b, v17.8h",
        "st1 {v18.16b}, [x5]",
    ]


def hadamard(r: list[str]) -> list[str]:
    """벡터 r0..r3에 Go와 같은 하다마드 나비를 건다: r0+r1+r2+r3, (r0-r2)+(r1-r3), (r0-r2)-(r1-r3), (r0+r2)-(r1+r3) → v4..v7."""
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


def sum_abs(weights: str | None = None) -> list[str]:
    """v4..v7의 절대값(weights가 있으면 v8..v11 가중치를 곱해) 합을 RET에 둔다."""
    s = [f"abs v{4 + k}.4s, v{4 + k}.4s" for k in range(4)]
    if weights is not None:
        s += [f"ld1 {{v8.4s, v9.4s, v10.4s, v11.4s}}, [{weights}]"]
        s += [f"mul v{4 + k}.4s, v{4 + k}.4s, v{8 + k}.4s" for k in range(4)]
    return s + [
        "add v4.4s, v4.4s, v5.4s",
        "add v6.4s, v6.4s, v7.4s",
        "add v4.4s, v4.4s, v6.4s",
        "addv s4, v4.4s",
        f"fmov {RET}, s4",
    ]


T_REGS = (["v16", "v17", "v18", "v19"], ["v20", "v21", "v22", "v23"])


def satd4() -> list[str]:
    """
    x0=src x1=stride x2=pred(16바이트 연속). 잔차 4×4의 하다마드 계수 절대합.
    덧셈만 쓰므로 세로 단계를 먼저 하고 전치 한 번 뒤 가로 단계를 해도 Go(가로 → 세로)와 값이 같다.
    계수 순서는 절대합에 영향이 없다.
    """
    s = load_rows("v0", "x0", "x1") + ["ld1 {v1.16b}, [x2]"] + residual()
    s += hadamard(["v4", "v5", "v6", "v7"])
    s += transpose(["v4", "v5", "v6", "v7"], *T_REGS)
    return s + hadamard(T_REGS[1]) + sum_abs()


def ttransform4() -> list[str]:
    """
    x0=in x1=stride x2=w. 4×4 픽셀의 가중 하다마드 에너지 (ttransformGo와 같음).
    가중치가 계수 위치마다 다르므로 Go와 같은 순서(가로 → 세로)로 계산해 출력 배치를 맞춘다:
    마지막 나비의 출력 v가 레인 k에 계수 (v, k)를 두고, 가중치 w[4v+k]가 v행에 그대로 놓인다.
    """
    s = load_rows("v0", "x0", "x1")
    s += [
        "uxtl v2.8h, v0.8b",
        "uxtl2 v3.8h, v0.16b",
        "uxtl v4.4s, v2.4h",
        "uxtl2 v5.4s, v2.8h",
        "uxtl v6.4s, v3.4h",
        "uxtl2 v7.4s, v3.8h",
    ]
    # 가로: 열 벡터(레인 = 행)에 나비 → v4..v7 = 출력 k, 레인 = 행
    s += transpose(["v4", "v5", "v6", "v7"], *T_REGS) + hadamard(T_REGS[1])
    # 세로: 다시 전치해 tmp 행 벡터(레인 = k)에 나비 → 출력 v, 레인 = k
    s += transpose(["v4", "v5", "v6", "v7"], *T_REGS) + hadamard(T_REGS[1])
    return s + sum_abs("x2")


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


# (이름, 인자 이름 목록, 반환 여부, 본문). Go 선언은 kernels_arm64.go에 있다.
KERNELS = [
    ("quantBlock4", ["src", "stride", "pred", "pstride", "qv", "levels", "rec", "rstride"], True, quant_block4),
    ("quantDC4", ["coeffs", "dc", "qv", "levels", "pred", "pstride", "rec", "rstride"], False, quant_dc4),
    ("fdctBlock4", ["src", "stride", "pred", "pstride", "k", "out"], False, fdct_block4),
    ("fdctBins4", ["src", "stride", "pred", "pstride", "k", "bins"], False, fdct_bins4),
    ("satd4NEON", ["src", "stride", "pred"], True, satd4),
    ("ttransform4", ["in", "stride", "w"], True, ttransform4),
]


def emit() -> str:
    out = [
        "// 생성 파일 — internal/asmgen/gen.py로 만든다. 직접 고치지 않는다.",
        "",
        "//go:build !purego",
        "",
        '#include "textflag.h"',
    ]
    for name, args, has_ret, body_fn in KERNELS:
        body = body_fn()
        words = assemble(body)
        frame = len(args) * 8
        out += ["", f"TEXT ·{name}(SB), NOSPLIT, $0-{frame + 4 if has_ret else frame}"]
        out += [f"\tMOVD {a}+{i * 8}(FP), R{i}" for i, a in enumerate(args)]
        out += [f"\tWORD ${w:#010x} // {line}" for w, line in zip(words, body)]
        if has_ret:
            out.append(f"\tMOVW R{RET[1:]}, ret+{frame}(FP)")
        out.append("\tRET")
    return "\n".join(out) + "\n"


if __name__ == "__main__":
    OUT.write_text(emit())
    print(f"{OUT} 생성")

//go:build amd64

#include "textflag.h"

// func scanAVX2(data []byte, positions []uint32) int
// 使用 AVX2 扫描 JSON 结构字符 " { } [ ] : ,
// 返回找到的结构字符数量
TEXT ·scanAVX2(SB), NOSPLIT, $0-56
    // 参数布局:
    // data.ptr:    +0(FP)
    // data.len:    +8(FP)
    // data.cap:    +16(FP)
    // positions.ptr: +24(FP)
    // positions.len: +32(FP)
    // positions.cap: +40(FP)
    // ret:         +48(FP)

    MOVQ    data_base+0(FP), SI      // SI = data ptr
    MOVQ    data_len+8(FP), CX       // CX = data len
    MOVQ    positions_base+24(FP), DI // DI = positions ptr
    MOVQ    positions_len+32(FP), R11 // R11 = positions capacity
    XORQ    R8, R8                   // R8 = count (结果数量)
    XORQ    R9, R9                   // R9 = offset (当前偏移)

    // 广播结构字符到 YMM 寄存器
    MOVQ    $0x22, AX                // " (双引号)
    MOVQ    AX, X1
    VPBROADCASTB X1, Y1

    MOVQ    $0x7b, AX                // { (左花括号)
    MOVQ    AX, X2
    VPBROADCASTB X2, Y2

    MOVQ    $0x7d, AX                // } (右花括号)
    MOVQ    AX, X3
    VPBROADCASTB X3, Y3

    MOVQ    $0x5b, AX                // [ (左方括号)
    MOVQ    AX, X4
    VPBROADCASTB X4, Y4

    MOVQ    $0x5d, AX                // ] (右方括号)
    MOVQ    AX, X5
    VPBROADCASTB X5, Y5

    MOVQ    $0x3a, AX                // : (冒号)
    MOVQ    AX, X6
    VPBROADCASTB X6, Y6

    MOVQ    $0x2c, AX                // , (逗号)
    MOVQ    AX, X7
    VPBROADCASTB X7, Y7

loop:
    // 检查是否还有 32 字节可处理
    LEAQ    32(R9), R10
    CMPQ    R10, CX
    JA      tail

    // 加载 32 字节数据
    VMOVDQU (SI)(R9*1), Y0

    // 并行比较 7 种结构字符
    VPCMPEQB Y0, Y1, Y8              // 比较 "
    VPCMPEQB Y0, Y2, Y9              // 比较 {
    VPOR     Y8, Y9, Y8
    VPCMPEQB Y0, Y3, Y9              // 比较 }
    VPOR     Y8, Y9, Y8
    VPCMPEQB Y0, Y4, Y9              // 比较 [
    VPOR     Y8, Y9, Y8
    VPCMPEQB Y0, Y5, Y9              // 比较 ]
    VPOR     Y8, Y9, Y8
    VPCMPEQB Y0, Y6, Y9              // 比较 :
    VPOR     Y8, Y9, Y8
    VPCMPEQB Y0, Y7, Y9              // 比较 ,
    VPOR     Y8, Y9, Y8

    // 提取掩码
    VPMOVMSKB Y8, AX
    TESTL   AX, AX
    JZ      next

extract:
    // 检查是否还有空间
    CMPQ    R8, R11
    JAE     done

    // 提取最低位 1 的位置
    BSFL    AX, BX
    LEAQ    (R9)(BX*1), R10          // R10 = offset + bit position
    MOVL    R10, (DI)(R8*4)          // 存储位置
    INCQ    R8                       // count++

    // 清除最低位 1
    BLSRL   AX, AX
    JNZ     extract

next:
    ADDQ    $32, R9
    JMP     loop

tail:
    // 处理剩余不足 32 字节的数据
    CMPQ    R9, CX
    JAE     done

    // 检查是否还有空间
    CMPQ    R8, R11
    JAE     done

    MOVBLZX (SI)(R9*1), AX

    // 逐个比较结构字符
    CMPB    AL, $0x22                // "
    JE      found
    CMPB    AL, $0x7b                // {
    JE      found
    CMPB    AL, $0x7d                // }
    JE      found
    CMPB    AL, $0x5b                // [
    JE      found
    CMPB    AL, $0x5d                // ]
    JE      found
    CMPB    AL, $0x3a                // :
    JE      found
    CMPB    AL, $0x2c                // ,
    JE      found
    JMP     skip

found:
    MOVL    R9, (DI)(R8*4)
    INCQ    R8

skip:
    INCQ    R9
    JMP     tail

done:
    MOVQ    R8, ret+48(FP)
    VZEROUPPER
    RET

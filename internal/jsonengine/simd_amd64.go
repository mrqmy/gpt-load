//go:build amd64

package jsonengine

import "golang.org/x/sys/cpu"

// hasAVX2 检查 CPU 是否支持 AVX2
var hasAVX2 = cpu.X86.HasAVX2

// ScanStructural 扫描 JSON 结构字符，返回位置列表
// 结构字符: " { } [ ] : ,
// positions 必须足够大以容纳所有结果（建议 len(data)/4）
// 返回找到的结构字符数量
func ScanStructural(data []byte, positions []uint32) int {
	if len(data) == 0 {
		return 0
	}
	if hasAVX2 && len(data) >= 32 {
		return scanAVX2(data, positions)
	}
	return scanGeneric(data, positions)
}

// scanAVX2 使用 AVX2 指令扫描结构字符
// 在汇编中实现
//
//go:noescape
func scanAVX2(data []byte, positions []uint32) int

// scanGeneric 通用实现（无 SIMD）
func scanGeneric(data []byte, positions []uint32) int {
	count := 0
	for i, b := range data {
		if isStructural(b) {
			if count < len(positions) {
				positions[count] = uint32(i)
				count++
			}
		}
	}
	return count
}

// isStructural 检查是否为结构字符
func isStructural(b byte) bool {
	switch b {
	case '"', '{', '}', '[', ']', ':', ',':
		return true
	default:
		return false
	}
}

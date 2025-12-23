//go:build !amd64

package jsonengine

// ScanStructural 扫描 JSON 结构字符，返回位置列表
// 结构字符: " { } [ ] : ,
// 非 amd64 平台使用通用实现
func ScanStructural(data []byte, positions []uint32) int {
	if len(data) == 0 {
		return 0
	}
	return scanGeneric(data, positions)
}

// scanGeneric 通用实现（无 SIMD）
func scanGeneric(data []byte, positions []uint32) int {
	// 使用查找表优化
	var isStruct [256]bool
	isStruct['"'] = true
	isStruct['{'] = true
	isStruct['}'] = true
	isStruct['['] = true
	isStruct[']'] = true
	isStruct[':'] = true
	isStruct[','] = true

	count := 0
	maxCount := len(positions)

	for i, b := range data {
		if isStruct[b] {
			if count >= maxCount {
				break
			}
			positions[count] = uint32(i)
			count++
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

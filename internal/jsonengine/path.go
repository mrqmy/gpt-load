package jsonengine

import (
	"strconv"
	"strings"
)

// SegmentType 路径段类型
type SegmentType uint8

const (
	SegField    SegmentType = iota // 具体字段名
	SegWildcard                    // * 任意键
	SegArrayAll                    // [*] 数组全部元素
	SegArrayIdx                    // [n] 数组具体索引
)

// Segment 路径段
type Segment struct {
	Type  SegmentType
	Value string // 字段名或索引值
	Index int    // 仅 SegArrayIdx 时有效
}

// PathRule 路径过滤规则
type PathRule struct {
	Path       string    `json:"path"`
	Action     Action    `json:"action"`
	Value      any       `json:"value,omitempty"`       // 简单值（string/int/bool）或复杂对象
	ValueBytes []byte    `json:"valueBytes,omitempty"` // 预验证的JSON字节（流式友好，优先使用）
	segments   []Segment // 解析缓存
}

// RuleAction AC 自动机输出
type RuleAction struct {
	Index      int
	Action     Action
	Value      any
	ValueBytes []byte // 预验证的JSON字节（优先使用）
}

// ParsePath 解析路径字符串为段列表
// 语法: segment.segment...
// segment: fieldName | * | [*] | [n]
func ParsePath(path string) ([]Segment, error) {
	if path == "" {
		return nil, nil
	}

	var segments []Segment
	parts := splitPath(path)

	for _, part := range parts {
		seg, err := parseSegment(part)
		if err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}

	return segments, nil
}

// splitPath 按 . 分割路径，但保留 [] 内的内容
func splitPath(path string) []string {
	var parts []string
	var current strings.Builder
	inBracket := false

	for i := 0; i < len(path); i++ {
		c := path[i]
		switch c {
		case '[':
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			inBracket = true
			current.WriteByte(c)
		case ']':
			current.WriteByte(c)
			inBracket = false
			parts = append(parts, current.String())
			current.Reset()
		case '.':
			if inBracket {
				current.WriteByte(c)
			} else {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			}
		default:
			current.WriteByte(c)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// parseSegment 解析单个段
func parseSegment(s string) (Segment, error) {
	if s == "" {
		return Segment{}, &PathError{Msg: "empty segment"}
	}

	// 通配符
	if s == "*" {
		return Segment{Type: SegWildcard, Value: "*"}, nil
	}

	// 数组索引 [*] 或 [n]
	if len(s) >= 3 && s[0] == '[' && s[len(s)-1] == ']' {
		inner := s[1 : len(s)-1]
		if inner == "*" {
			return Segment{Type: SegArrayAll, Value: "[*]"}, nil
		}
		// 解析数字索引
		idx, err := strconv.Atoi(inner)
		if err != nil {
			return Segment{}, &PathError{Msg: "invalid array index: " + inner}
		}
		return Segment{Type: SegArrayIdx, Value: s, Index: idx}, nil
	}

	// 普通字段
	return Segment{Type: SegField, Value: s}, nil
}

// Match 检查段是否匹配给定的 key 或 index
func (seg Segment) Match(key string, isArray bool, arrayIdx int) bool {
	switch seg.Type {
	case SegField:
		return !isArray && seg.Value == key
	case SegWildcard:
		return !isArray // * 匹配任意对象键
	case SegArrayAll:
		return isArray // [*] 匹配任意数组索引
	case SegArrayIdx:
		return isArray && seg.Index == arrayIdx
	default:
		return false
	}
}

// String 返回段的字符串表示
func (seg Segment) String() string {
	return seg.Value
}

// PathError 路径解析错误
type PathError struct {
	Msg string
}

func (e *PathError) Error() string {
	return "path error: " + e.Msg
}

// ParsePathRule 解析路径规则（兼容旧格式）
func ParsePathRule(path string, action Action, value any) (*PathRule, error) {
	segments, err := ParsePath(path)
	if err != nil {
		return nil, err
	}

	return &PathRule{
		Path:     path,
		Action:   action,
		Value:    value,
		segments: segments,
	}, nil
}

// Segments 返回解析后的段列表
func (r *PathRule) Segments() []Segment {
	return r.segments
}

// IsTopLevel 检查是否为顶层规则（单段路径）
func (r *PathRule) IsTopLevel() bool {
	return len(r.segments) == 1 && r.segments[0].Type == SegField
}

// ToLegacyRule 转换为旧格式 Rule（仅顶层规则）
func (r *PathRule) ToLegacyRule() *Rule {
	if !r.IsTopLevel() {
		return nil
	}
	return &Rule{
		Key:    r.segments[0].Value,
		Action: r.Action,
		Value:  r.Value,
	}
}

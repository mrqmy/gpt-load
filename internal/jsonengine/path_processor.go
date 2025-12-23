package jsonengine

import (
	"encoding/json"
	"io"
	"strconv"
)

// jsonMarshal 包装 json.Marshal，用于复杂类型的后备序列化
var jsonMarshal = json.Marshal

// pathEntry 路径栈条目
type pathEntry struct {
	key      string  // 键名（对象）或索引（数组）
	isArray  bool    // 是否数组
	arrayIdx int     // 数组索引
	acNode   *ACNode // AC 自动机状态
}

// skipState 值跳过状态机
type skipState struct {
	depth    int  // 嵌套深度 { [ 增加，} ] 减少
	inString bool // 是否在字符串内
	escaped  bool // 转义状态
}

// addAction 待插入的字段
type addAction struct {
	key   string
	value []byte // 预序列化的JSON值
}

// PathProcessor 路径过滤处理器
type PathProcessor struct {
	matcher *PathMatcher

	// SIMD 扫描结果
	positions []uint32

	// 处理状态（跨 chunk 持久化）
	pathStack     []pathEntry // 当前路径
	inString      bool        // 是否在字符串内
	escaped       bool        // 转义状态
	expectKey     bool        // 期待 key
	skipping      bool        // 跳过值模式
	skipState     skipState   // 跳过状态机
	pendingComma  bool        // 延迟逗号
	keyBuffer     []byte      // key 累积缓冲（包含引号）
	inKey         bool        // 正在读取 key
	outputBuf     []byte      // 输出缓冲
	firstField    bool        // 当前对象的第一个字段
	lastMatchNode *ACNode     // 最近 key 匹配结果，用于进入子对象

	// Set 操作状态（流式友好）
	setValue []byte // 跳过原值后要输出的新值（nil 表示 remove）

	// Add 操作状态（深度映射）
	pendingAdds map[int][]addAction // depth -> 待插入字段列表
	hasAddRules bool                // 是否存在 Add 规则（性能优化，避免每次调用都遍历规则）
}

// Reset 重置处理器状态
func (p *PathProcessor) Reset() {
	p.pathStack = p.pathStack[:0]
	p.inString = false
	p.escaped = false
	p.expectKey = false
	p.skipping = false
	p.skipState = skipState{}
	p.pendingComma = false
	p.keyBuffer = p.keyBuffer[:0]
	p.inKey = false
	p.outputBuf = p.outputBuf[:0]
	p.firstField = true
	p.lastMatchNode = nil
	p.setValue = nil
	
	// 清空 Add 操作状态
	if p.pendingAdds != nil {
		for k := range p.pendingAdds {
			delete(p.pendingAdds, k)
		}
	}
}

// ProcessChunk 处理单个 chunk
func (p *PathProcessor) ProcessChunk(chunk []byte, w io.Writer) error {
	if len(chunk) == 0 {
		return nil
	}

	// SIMD 扫描结构字符
	n := ScanStructural(chunk, p.positions)

	// 处理结构字符之间的内容
	prev := 0
	for i := 0; i < n; i++ {
		pos := int(p.positions[i])
		char := chunk[pos]

		// 输出中间内容（非结构字符部分）
		if pos > prev {
			p.handleContent(chunk[prev:pos], w)
		}

		// 处理结构字符
		p.handleStructural(char, w)
		prev = pos + 1
	}

	// 输出剩余内容
	if prev < len(chunk) {
		p.handleContent(chunk[prev:], w)
	}

	return nil
}

// handleContent 处理非结构字符内容
func (p *PathProcessor) handleContent(content []byte, w io.Writer) {
	if len(content) == 0 {
		return
	}

	// 跳过模式：不输出，但跟踪状态
	if p.skipping {
		for _, b := range content {
			if p.skipState.escaped {
				p.skipState.escaped = false
				continue
			}
			if p.skipState.inString && b == '\\' {
				p.skipState.escaped = true
			}
		}
		return
	}

	// 在 key 中，累积到缓冲
	if p.inKey {
		p.keyBuffer = append(p.keyBuffer, content...)
		return
	}

	// 正常输出
	w.Write(content)
}

// handleStructural 处理结构字符
func (p *PathProcessor) handleStructural(char byte, w io.Writer) {
	// 跳过模式
	if p.skipping {
		reprocess := p.handleSkipChar(char, w)
		if !reprocess {
			return
		}
		// 需要重新处理该字符（简单值遇到 } 或 ] 结束）
	}

	// 字符串内（key 或 value）
	if p.inString {
		if p.escaped {
			p.escaped = false
			if p.inKey {
				p.keyBuffer = append(p.keyBuffer, char)
			} else {
				w.Write([]byte{char})
			}
			return
		}

		switch char {
		case '\\':
			p.escaped = true
			if p.inKey {
				p.keyBuffer = append(p.keyBuffer, char)
			} else {
				w.Write([]byte{char})
			}
		case '"':
			p.inString = false
			if p.inKey {
				p.keyBuffer = append(p.keyBuffer, char)
				// key 字符串完成，等待 : 来决定是否输出
			} else {
				w.Write([]byte{char})
			}
		default:
			if p.inKey {
				p.keyBuffer = append(p.keyBuffer, char)
			} else {
				w.Write([]byte{char})
			}
		}
		return
	}

	// 非字符串状态
	switch char {
	case '"':
		p.inString = true
		p.escaped = false
		if p.expectKey {
			// 开始新 key
			p.inKey = true
			p.keyBuffer = p.keyBuffer[:0]
			p.keyBuffer = append(p.keyBuffer, char)
		} else {
			// value 字符串
			w.Write([]byte{char})
		}

	case ':':
		// key 完成，检查匹配
		if p.inKey {
			p.inKey = false
			key := extractKey(p.keyBuffer)

			action := p.checkKeyMatch(key)
			
			// Remove: 跳过整个键值对（不输出key）
			if action == ActionRemove {
				p.skipping = true
				p.skipState = skipState{depth: 0, inString: false, escaped: false}
				p.expectKey = false
				return
			}
			
			// Set: 输出key，然后跳过原值并替换
			// 非匹配: 正常输出key和值
			if p.pendingComma {
				w.Write([]byte{','})
				p.pendingComma = false
			}
			w.Write(p.keyBuffer)
			w.Write([]byte{char})
			p.firstField = false
			
			// Set操作：标记需要跳过原值
			if action == ActionSet {
				p.skipping = true
				p.skipState = skipState{depth: 0, inString: false, escaped: false}
			}
			

		} else {
			w.Write([]byte{char})
		}
		p.expectKey = false

	case '{':
		if p.pendingComma {
			w.Write([]byte{','})
			p.pendingComma = false
		}
		w.Write([]byte{char})

		// 进入对象：使用最近匹配的 AC 节点（如果有 key），否则使用当前节点
		var acNode *ACNode
		if p.lastMatchNode != nil {
			acNode = p.lastMatchNode
			p.lastMatchNode = nil // 清除，避免影响后续
		} else {
			acNode = p.currentACNode()
		}

		// ⚡ 关键修复：在 append 之前调用 registerPendingAdds
		// 此时 len(pathStack) 才是正确的深度
		p.registerPendingAdds(acNode)

		entry := pathEntry{
			isArray: false,
			acNode:  acNode,
		}
		p.pathStack = append(p.pathStack, entry)
		p.expectKey = true
		p.firstField = true

	case '}':
		// 退出对象：处理待添加字段
		p.handleObjectEnd(w)
		
		if len(p.pathStack) > 0 {
			p.pathStack = p.pathStack[:len(p.pathStack)-1]
		}
		w.Write([]byte{char})
		p.expectKey = false
		p.pendingComma = false

	case '[':
		if p.pendingComma {
			w.Write([]byte{','})
			p.pendingComma = false
		}
		w.Write([]byte{char})

		// 进入数组：使用最近匹配的 AC 节点（如果有 key），否则使用当前节点
		var acNode *ACNode
		if p.lastMatchNode != nil {
			acNode = p.lastMatchNode
			p.lastMatchNode = nil // 清除，避免影响后续
		} else {
			acNode = p.currentACNode()
		}
		entry := pathEntry{
			isArray:  true,
			arrayIdx: 0,
			acNode:   acNode,
		}
		p.pathStack = append(p.pathStack, entry)
		p.expectKey = false

		// 检查数组元素匹配
		p.checkArrayElementMatch()

	case ']':
		// 退出数组
		if len(p.pathStack) > 0 {
			p.pathStack = p.pathStack[:len(p.pathStack)-1]
		}
		w.Write([]byte{char})
		p.expectKey = false
		p.pendingComma = false

	case ',':
		// 处理逗号
		if len(p.pathStack) > 0 {
			top := &p.pathStack[len(p.pathStack)-1]
			if top.isArray {
				// 数组内逗号：增加索引
				top.arrayIdx++
				w.Write([]byte{char})
				// 检查新数组元素匹配
				p.checkArrayElementMatch()
			} else {
				// 对象内逗号：只有前面有输出字段时才设置 pendingComma
				if !p.firstField {
					p.pendingComma = true
				}
				p.expectKey = true
			}
		} else {
			w.Write([]byte{char})
		}

	default:
		w.Write([]byte{char})
	}
}

// extractKey 从带引号的 key 缓冲提取实际 key
func extractKey(buf []byte) string {
	if len(buf) < 2 {
		return ""
	}
	// 去掉首尾引号
	return string(buf[1 : len(buf)-1])
}

// checkKeyMatch 检查 key 是否匹配规则（remove/set/add）
// 返回匹配的Action（空字符串表示无匹配）
func (p *PathProcessor) checkKeyMatch(key string) Action {
	if p.matcher == nil {
		return ""
	}

	// 获取当前 AC 节点（当前对象进入时的状态，不会被同级 key 影响）
	var currentNode *ACNode
	if len(p.pathStack) > 0 {
		currentNode = p.pathStack[len(p.pathStack)-1].acNode
	} else {
		currentNode = p.matcher.Root()
	}

	// 匹配
	nextNode, actions := p.matcher.Match(currentNode, key, false, 0)

	// 保存匹配结果，用于进入子对象时（不更新当前对象的 acNode）
	p.lastMatchNode = nextNode

	// 检查匹配的操作（优先级：Remove > Set）
	// Add 操作在对象结束时统一处理，不在这里处理
	for _, action := range actions {
		switch action.Action {
		case ActionRemove:
			p.setValue = nil // remove 操作：跳过后不输出任何内容
			return ActionRemove
		case ActionSet:
			// set 操作：跳过原值后输出新值（优先使用预验证的ValueBytes）
			if len(action.ValueBytes) > 0 {
				p.setValue = action.ValueBytes // 零拷贝：直接使用预验证JSON
			} else {
				p.setValue = marshalValue(action.Value) // 后备：运行时序列化
			}
			return ActionSet
		}
	}
	return ""
}

// checkArrayElementMatch 检查数组元素匹配
func (p *PathProcessor) checkArrayElementMatch() {
	if p.matcher == nil || len(p.pathStack) == 0 {
		return
	}

	top := &p.pathStack[len(p.pathStack)-1]
	if !top.isArray {
		return
	}

	// 使用数组 entry 的 acNode 来匹配数组元素
	// 这个 acNode 是进入数组时从 lastMatchNode 设置的（即匹配 key 后的状态）
	parentNode := top.acNode
	if parentNode == nil {
		parentNode = p.matcher.Root()
	}

	// 匹配数组元素（[*] 或 [n]）
	nextNode, actions := p.matcher.Match(parentNode, "", true, top.arrayIdx)

	// 保存匹配结果，用于数组元素内的对象/数组
	p.lastMatchNode = nextNode

	// 检查匹配的操作
	for _, action := range actions {
		switch action.Action {
		case ActionRemove:
			p.skipping = true
			p.skipState = skipState{depth: 0, inString: false, escaped: false}
			p.setValue = nil
			return
		case ActionSet:
			// 数组元素Set：跳过原值后输出新值
			if len(action.ValueBytes) > 0 {
				p.setValue = action.ValueBytes
			} else {
				p.setValue = marshalValue(action.Value)
			}
			p.skipping = true
			p.skipState = skipState{depth: 0, inString: false, escaped: false}
			return
		}
	}
}

// handleSkipChar 处理跳过模式下的字符
// 返回 true 表示该字符需要重新处理（简单值遇到 } ] 结束时）
func (p *PathProcessor) handleSkipChar(char byte, w io.Writer) bool {
	sk := &p.skipState

	if sk.escaped {
		sk.escaped = false
		return false
	}

	if sk.inString {
		switch char {
		case '\\':
			sk.escaped = true
		case '"':
			sk.inString = false
			if sk.depth == 0 {
				// 简单值（字符串）结束
				p.finishSkipValue(w)
			}
		}
		return false
	}

	switch char {
	case '"':
		sk.inString = true
	case '{', '[':
		sk.depth++
	case '}', ']':
		if sk.depth > 0 {
			sk.depth--
			if sk.depth == 0 {
				// 复合值（对象/数组）结束
				p.finishSkipValue(w)
			}
		} else {
			// 简单值（数字/布尔/null）结束，需要重新处理这个字符
			p.finishSkipValue(w)
			return true
		}
	case ',':
		if sk.depth == 0 {
			// 简单值结束
			isSet := p.setValue != nil
			p.finishSkipValue(w)
			if isSet {
				// Set操作：逗号需要重新处理（正常输出）
				return true
			}
			// Remove操作：逗号被消费（不输出）
		}
	}
	return false
}

// finishSkipValue 完成值跳过（保持在跳过模式直到处理完分隔符）
// 参数 w 用于 set 操作时输出新值
func (p *PathProcessor) finishSkipValue(w io.Writer) {
	p.skipping = false
	p.skipState = skipState{}

	// set 操作：输出新值
	if p.setValue != nil {
		w.Write(p.setValue)
		p.setValue = nil
		// 注意：不在这里设置pendingComma
		// 逗号由后续的逗号字符或对象字段输出逻辑处理
	}

	// 准备下一个字段
	if len(p.pathStack) > 0 {
		top := &p.pathStack[len(p.pathStack)-1]
		if !top.isArray {
			p.expectKey = true
		}
	}
}

// finishSkip 完成跳过
func (p *PathProcessor) finishSkip() {
	p.skipping = false
	p.skipState = skipState{}

	// 准备下一个字段
	if len(p.pathStack) > 0 {
		top := &p.pathStack[len(p.pathStack)-1]
		if !top.isArray {
			p.expectKey = true
		}
	}
}

// currentACNode 获取当前 AC 节点
func (p *PathProcessor) currentACNode() *ACNode {
	if p.matcher == nil {
		return nil
	}
	if len(p.pathStack) > 0 {
		return p.pathStack[len(p.pathStack)-1].acNode
	}
	return p.matcher.Root()
}

// Finish 完成处理（处理跨 chunk 的未完成状态）
func (p *PathProcessor) Finish(w io.Writer) error {
	if p.skipping {
		p.skipping = false
	}
	return nil
}

// marshalValue 将值序列化为 JSON 字节
// 复用 SIMD 流式架构，避免引入 encoding/json 的反射开销
func marshalValue(v any) []byte {
	if v == nil {
		return []byte("null")
	}

	switch val := v.(type) {
	case string:
		// 手动转义字符串，避免 json.Marshal 开销
		return marshalString(val)
	case bool:
		if val {
			return []byte("true")
		}
		return []byte("false")
	case int:
		return []byte(itoa(val))
	case int64:
		return []byte(itoa64(val))
	case float64:
		return marshalFloat(val)
	case []byte:
		// 已经是 JSON 格式的字节
		return val
	default:
		// 复杂类型：使用 json.Marshal 作为后备
		// 注意：这会引入反射开销，但保持兼容性
		data, err := jsonMarshal(v)
		if err != nil {
			return []byte("null")
		}
		return data
	}
}

// marshalString 手动序列化字符串（避免 json.Marshal）
func marshalString(s string) []byte {
	// 预估容量：原长度 + 2引号 + 可能的转义
	buf := make([]byte, 0, len(s)+2+len(s)/8)
	buf = append(buf, '"')

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		default:
			if c < 0x20 {
				// 控制字符用 \u00xx
				buf = append(buf, '\\', 'u', '0', '0', hexDigit(c>>4), hexDigit(c&0xf))
			} else {
				buf = append(buf, c)
			}
		}
	}

	buf = append(buf, '"')
	return buf
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + b - 10
}

// itoa64 int64 转字符串
func itoa64(i int64) string {
	if i == 0 {
		return "0"
	}

	neg := i < 0
	if neg {
		i = -i
	}

	var buf [20]byte
	pos := len(buf)

	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}

	if neg {
		pos--
		buf[pos] = '-'
	}

	return string(buf[pos:])
}

// marshalFloat 序列化浮点数
func marshalFloat(f float64) []byte {
	// 使用 strconv 处理浮点数精度
	return []byte(strconv.FormatFloat(f, 'f', -1, 64))
}

// registerPendingAdds 进入对象时检查并注册待添加字段
func (p *PathProcessor) registerPendingAdds(acNode *ACNode) {
	if p.matcher == nil || acNode == nil {
		return
	}

	// ⚡ 性能优化：如果没有 Add 规则，直接返回（避免不必要的遍历）
	if !p.hasAddRules {
		return
	}

	// ⚡ 性能优化：如果当前节点没有子节点，直接返回
	if len(acNode.children) == 0 {
		return
	}

	depth := len(p.pathStack)

	// 遍历当前AC节点的所有精确匹配子节点
	// 这些子节点代表可能的下一层key
	for key, childNode := range acNode.children {
		// 检查子节点是否有Add操作
		for _, action := range childNode.output {
			if action.Action == ActionAdd {
				// 获取规则，检查深度是否匹配
				rule := p.matcher.rules[action.Index]
				expectedDepth := len(rule.segments) - 1

				// 只有当前深度匹配规则的目标深度时才添加
				// 例如：path="key" (len=1) 只在 depth=0 添加
				//       path="user.email" (len=2) 只在 depth=1 添加
				if depth != expectedDepth {
					continue
				}

				// 准备序列化值（优先使用预验证JSON）
				var value []byte
				if len(action.ValueBytes) > 0 {
					value = action.ValueBytes
				} else {
					value = marshalValue(action.Value)
				}

				// 注册待添加字段
				if p.pendingAdds == nil {
					p.pendingAdds = make(map[int][]addAction)
				}
				p.pendingAdds[depth] = append(p.pendingAdds[depth], addAction{
					key:   key,
					value: value,
				})
			}
		}
	}

}

// handleObjectEnd 退出对象时插入待添加字段
func (p *PathProcessor) handleObjectEnd(w io.Writer) {
	// ⚡ 修复：退出对象时，pathStack 还未 pop，所以深度是 len(pathStack)
	// 但 registerPendingAdds 是在进入对象前调用的，depth 是进入前的值
	// 所以这里应该用 len(pathStack) - 1
	depth := len(p.pathStack) - 1
	if depth < 0 {
		return
	}
	
	// 检查是否有待添加字段
	adds, hasAdds := p.pendingAdds[depth]
	if !hasAdds || len(adds) == 0 {
		return
	}

	// ⚡ 性能优化：直接添加字段，不做去重检查
	// 如果 key 重复，让 JSON 解析器处理（后面的值会覆盖前面的）
	for i, add := range adds {
		// 输出逗号（对象非空时需要逗号）
		if !p.firstField || i > 0 {
			w.Write([]byte{','})
		}

		// 输出 "key": value
		w.Write([]byte{'"'})
		w.Write([]byte(add.key))
		w.Write([]byte{'"', ':'})
		w.Write(add.value)
	}

	// 清理状态
	delete(p.pendingAdds, depth)
}

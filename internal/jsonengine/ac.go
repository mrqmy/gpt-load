package jsonengine

// ACNode AC 自动机节点
type ACNode struct {
	children map[string]*ACNode // 精确匹配子节点
	wildcard *ACNode            // * 通配子节点
	arrayAll *ACNode            // [*] 数组通配子节点
	fail     *ACNode            // 失败指针
	output   []RuleAction       // 匹配输出
	depth    int                // 节点深度
}

// ⚡ 性能优化：缓存常用的数组索引字符串（避免重复分配）
// 大多数数组索引 < 1000，预先生成这些字符串
var arrayIdxCache [1000]string

func init() {
	for i := 0; i < 1000; i++ {
		arrayIdxCache[i] = "[" + itoa(i) + "]"
	}
}

// PathMatcher 路径匹配器（预编译的 AC 自动机）
type PathMatcher struct {
	root  *ACNode
	rules []PathRule
}

// NewPathMatcher 创建路径匹配器
func NewPathMatcher() *PathMatcher {
	return &PathMatcher{
		root: newACNode(0),
	}
}

// newACNode 创建 AC 节点
func newACNode(depth int) *ACNode {
	return &ACNode{
		children: make(map[string]*ACNode),
		depth:    depth,
	}
}

// AddRule 添加规则到匹配器
func (m *PathMatcher) AddRule(rule PathRule) error {
	// 解析路径
	segments, err := ParsePath(rule.Path)
	if err != nil {
		return err
	}

	rule.segments = segments
	ruleIdx := len(m.rules)
	m.rules = append(m.rules, rule)

	// 插入到 AC 自动机
	node := m.root
	for _, seg := range segments {
		node = node.getOrCreate(seg)
	}

	// 添加输出
	node.output = append(node.output, RuleAction{
		Index:      ruleIdx,
		Action:     rule.Action,
		Value:      rule.Value,
		ValueBytes: rule.ValueBytes,
	})

	return nil
}

// Build 构建失败指针（BFS）
func (m *PathMatcher) Build() {
	queue := make([]*ACNode, 0, 64)

	// 根节点的直接子节点的失败指针指向根
	for _, child := range m.root.children {
		child.fail = m.root
		queue = append(queue, child)
	}
	if m.root.wildcard != nil {
		m.root.wildcard.fail = m.root
		queue = append(queue, m.root.wildcard)
	}
	if m.root.arrayAll != nil {
		m.root.arrayAll.fail = m.root
		queue = append(queue, m.root.arrayAll)
	}

	// BFS 构建其他节点的失败指针
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		// 处理精确匹配子节点
		for key, child := range curr.children {
			child.fail = m.findFailNode(curr.fail, key, false, 0)
			// 合并失败节点的输出
			if child.fail != nil && len(child.fail.output) > 0 {
				child.output = append(child.output, child.fail.output...)
			}
			queue = append(queue, child)
		}

		// 处理通配符子节点
		if curr.wildcard != nil {
			curr.wildcard.fail = m.findFailNodeWildcard(curr.fail)
			if curr.wildcard.fail != nil && len(curr.wildcard.fail.output) > 0 {
				curr.wildcard.output = append(curr.wildcard.output, curr.wildcard.fail.output...)
			}
			queue = append(queue, curr.wildcard)
		}

		// 处理数组通配子节点
		if curr.arrayAll != nil {
			curr.arrayAll.fail = m.findFailNodeArrayAll(curr.fail)
			if curr.arrayAll.fail != nil && len(curr.arrayAll.fail.output) > 0 {
				curr.arrayAll.output = append(curr.arrayAll.output, curr.arrayAll.fail.output...)
			}
			queue = append(queue, curr.arrayAll)
		}
	}
}

// findFailNode 寻找精确匹配的失败节点
func (m *PathMatcher) findFailNode(fail *ACNode, key string, isArray bool, arrayIdx int) *ACNode {
	for fail != nil {
		// 尝试精确匹配
		if child := fail.children[key]; child != nil {
			return child
		}
		// 尝试通配符匹配
		if !isArray && fail.wildcard != nil {
			return fail.wildcard
		}
		if isArray && fail.arrayAll != nil {
			return fail.arrayAll
		}
		fail = fail.fail
	}
	return m.root
}

// findFailNodeWildcard 寻找通配符的失败节点
func (m *PathMatcher) findFailNodeWildcard(fail *ACNode) *ACNode {
	for fail != nil {
		if fail.wildcard != nil {
			return fail.wildcard
		}
		fail = fail.fail
	}
	return m.root
}

// findFailNodeArrayAll 寻找数组通配的失败节点
func (m *PathMatcher) findFailNodeArrayAll(fail *ACNode) *ACNode {
	for fail != nil {
		if fail.arrayAll != nil {
			return fail.arrayAll
		}
		fail = fail.fail
	}
	return m.root
}

// Match 从当前状态匹配下一个段
// 返回新状态和匹配到的动作列表
func (m *PathMatcher) Match(state *ACNode, key string, isArray bool, arrayIdx int) (*ACNode, []RuleAction) {
	if state == nil {
		state = m.root
	}

	// 尝试从当前节点匹配
	node := state
	for node != nil {
		var next *ACNode

		if isArray {
			// 数组元素：尝试 [n] 具体索引 或 [*] 通配
			// 优先尝试具体索引匹配
			idxKey := "[" + itoa(arrayIdx) + "]"
			if child := node.children[idxKey]; child != nil {
				next = child
			} else if node.arrayAll != nil {
				// 回退到 [*] 通配
				next = node.arrayAll
			}
		} else {
			// 对象键：尝试精确匹配或 *
			if child := node.children[key]; child != nil {
				next = child
			} else if node.wildcard != nil {
				next = node.wildcard
			}
		}

		if next != nil {
			return next, next.output
		}

		// 走失败指针
		if node == m.root {
			break
		}
		node = node.fail
	}

	return m.root, nil
}

// itoa 高性能整数转字符串
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	
	// ⚡ 性能优化：避免每次迭代都分配内存和拷贝
	// 原实现 append([]byte{digit}, digits...) 会导致 O(n²) 复杂度
	// 新实现先 append 到末尾，然后反转，复杂度 O(n)
	digits := make([]byte, 0, 12) // 预分配足够大的容量（int 最多 10 位）
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	
	// 反转数字（因为我们是倒序添加的）
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	
	return string(digits)
}

// Root 返回根节点
func (m *PathMatcher) Root() *ACNode {
	return m.root
}

// HasRules 检查是否有规则
func (m *PathMatcher) HasRules() bool {
	return len(m.rules) > 0
}

// Rules 返回规则列表
func (m *PathMatcher) Rules() []PathRule {
	return m.rules
}

// getOrCreate 获取或创建子节点
func (n *ACNode) getOrCreate(seg Segment) *ACNode {
	switch seg.Type {
	case SegWildcard:
		if n.wildcard == nil {
			n.wildcard = newACNode(n.depth + 1)
		}
		return n.wildcard
	case SegArrayAll:
		if n.arrayAll == nil {
			n.arrayAll = newACNode(n.depth + 1)
		}
		return n.arrayAll
	case SegArrayIdx:
		// 数组索引当作精确匹配处理
		key := seg.Value
		if child := n.children[key]; child != nil {
			return child
		}
		child := newACNode(n.depth + 1)
		n.children[key] = child
		return child
	default:
		// SegField
		key := seg.Value
		if child := n.children[key]; child != nil {
			return child
		}
		child := newACNode(n.depth + 1)
		n.children[key] = child
		return child
	}
}

// BuildMatcher 从规则列表构建匹配器
func BuildMatcher(rules []PathRule) (*PathMatcher, error) {
	matcher := NewPathMatcher()

	for _, rule := range rules {
		if err := matcher.AddRule(rule); err != nil {
			return nil, err
		}
	}

	matcher.Build()
	return matcher, nil
}

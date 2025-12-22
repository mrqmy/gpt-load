package jsonengine

// Action 定义操作类型
type Action string

const (
	// ActionSet 修改已存在的字段值（字段不存在时不操作）
	ActionSet Action = "set"
	// ActionAdd 添加不存在的字段（字段已存在时不操作）
	ActionAdd Action = "add"
	// ActionRemove 删除存在的字段（字段不存在时不操作）
	ActionRemove Action = "remove"
)

// Rule 定义单条操作规则
type Rule struct {
	Key    string `json:"key"`             // 目标字段名（顶层 key）
	Action Action `json:"action"`          // 操作类型: set, add, remove
	Value  any    `json:"value,omitempty"` // 操作值（remove 时不需要）
}

// IsValid 检查规则是否有效
func (r Rule) IsValid() bool {
	if r.Key == "" {
		return false
	}
	switch r.Action {
	case ActionSet, ActionAdd:
		return true // Value 可以是任意值，包括 nil
	case ActionRemove:
		return true
	default:
		return false
	}
}

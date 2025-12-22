package jsonengine

import (
	"io"
)

// Engine JSON 操作引擎
// 提供通用的 JSON 流式处理能力，支持对顶层字段进行增删改操作
type Engine struct {
	rules []Rule
}

// New 创建引擎实例
func New(rules []Rule) *Engine {
	// 过滤无效规则
	validRules := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.IsValid() {
			validRules = append(validRules, r)
		}
	}

	return &Engine{
		rules: validRules,
	}
}

// Process 流式处理 JSON 数据
// 输入和输出都是 io.Reader，适用于任意大小的 JSON 数据
// 操作语义：
//   - set: 修改已存在的字段（字段不存在时不操作）
//   - add: 添加不存在的字段（字段已存在时不操作）
//   - remove: 删除存在的字段（字段不存在时不操作）
func (e *Engine) Process(input io.Reader) io.Reader {
	if len(e.rules) == 0 {
		return input
	}
	return newProcessor(input, e.rules).process()
}

// Rules 返回当前规则列表
func (e *Engine) Rules() []Rule {
	return e.rules
}

// HasRules 检查是否有规则
func (e *Engine) HasRules() bool {
	return len(e.rules) > 0
}

// ProcessTo 直接处理 JSON 数据并写入 writer（高性能版本，无 io.Pipe 开销）
// 适用于大型响应（如包含 base64 图像的响应）
func (e *Engine) ProcessTo(input io.Reader, output io.Writer) error {
	if len(e.rules) == 0 {
		_, err := io.Copy(output, input)
		return err
	}
	return newProcessor(input, e.rules).processDirect(output)
}

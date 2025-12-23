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

// ============================================================================
// PathEngine: 支持嵌套路径过滤的高性能引擎
// ============================================================================

// PathEngine 路径过滤引擎
// 支持嵌套路径过滤，使用 SIMD 加速和 AC 自动机
type PathEngine struct {
	matcher   *PathMatcher
	rules     []PathRule
	chunkSize int
}

// PathEngineOption 引擎配置选项
type PathEngineOption func(*PathEngine)

// WithChunkSize 设置处理块大小
func WithChunkSize(size int) PathEngineOption {
	return func(e *PathEngine) {
		if size > 0 {
			e.chunkSize = size
		}
	}
}

// NewPathEngine 创建路径过滤引擎
func NewPathEngine(rules []PathRule, opts ...PathEngineOption) (*PathEngine, error) {
	// 过滤无效规则
	validRules := make([]PathRule, 0, len(rules))
	for _, r := range rules {
		if r.Path != "" {
			// 解析路径
			segments, err := ParsePath(r.Path)
			if err != nil {
				return nil, err
			}
			r.segments = segments
			validRules = append(validRules, r)
		}
	}

	// 构建匹配器
	matcher, err := BuildMatcher(validRules)
	if err != nil {
		return nil, err
	}

	engine := &PathEngine{
		matcher:   matcher,
		rules:     validRules,
		chunkSize: 512 * 1024, // 默认 512KB
	}

	for _, opt := range opts {
		opt(engine)
	}

	return engine, nil
}

// NewPathEngineFromLegacy 从旧格式规则创建路径引擎（向后兼容）
func NewPathEngineFromLegacy(rules []Rule, opts ...PathEngineOption) (*PathEngine, error) {
	pathRules := ConvertRulesToPathRules(rules)
	return NewPathEngine(pathRules, opts...)
}

// Process 流式处理 JSON 数据
func (e *PathEngine) Process(input io.Reader, output io.Writer) error {
	if !e.matcher.HasRules() {
		_, err := io.Copy(output, input)
		return err
	}

	// 获取处理器
	proc := GetPathProcessor(e.matcher)
	defer PutPathProcessor(proc)

	// 分块读取和处理
	buf := make([]byte, e.chunkSize)
	for {
		n, err := input.Read(buf)
		if n > 0 {
			if procErr := proc.ProcessChunk(buf[:n], output); procErr != nil {
				return procErr
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return proc.Finish(output)
}

// ProcessChunk 处理单个数据块（用于流式场景）
func (e *PathEngine) ProcessChunk(proc *PathProcessor, chunk []byte, output io.Writer) error {
	return proc.ProcessChunk(chunk, output)
}

// GetProcessor 获取处理器（用于流式场景）
func (e *PathEngine) GetProcessor() *PathProcessor {
	return GetPathProcessor(e.matcher)
}

// ReleaseProcessor 释放处理器
func (e *PathEngine) ReleaseProcessor(proc *PathProcessor) {
	PutPathProcessor(proc)
}

// HasRules 检查是否有规则
func (e *PathEngine) HasRules() bool {
	return e.matcher.HasRules()
}

// Rules 返回规则列表
func (e *PathEngine) Rules() []PathRule {
	return e.rules
}

// AddRule 添加规则（用于测试和动态添加规则）
func (e *PathEngine) AddRule(rule PathRule) error {
	// 解析路径
	segments, err := ParsePath(rule.Path)
	if err != nil {
		return err
	}
	rule.segments = segments
	
	// 添加到规则列表
	e.rules = append(e.rules, rule)
	
	// 添加到匹配器
	return e.matcher.AddRule(rule)
}

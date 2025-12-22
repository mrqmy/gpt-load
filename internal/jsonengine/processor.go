package jsonengine

import (
	"bytes"
	"encoding/json"
	"io"
)

// processor 流式 JSON 处理器
type processor struct {
	input   io.Reader
	rules   []Rule
	scanner *Scanner

	// 规则分类（按操作类型）
	setRules   map[string]any  // key -> new value
	addRules   map[string]any  // key -> value to add
	removeKeys map[string]bool // keys to remove

	// 状态跟踪
	seenKeys   map[string]bool // 已扫描到的顶层 key
	depth      int             // 当前嵌套深度
	needComma  bool            // 下一个字段前是否需要逗号
	firstField bool            // 是否是对象的第一个字段
}

// newProcessor 创建处理器
func newProcessor(input io.Reader, rules []Rule) *processor {
	p := &processor{
		input:      input,
		rules:      rules,
		setRules:   make(map[string]any),
		addRules:   make(map[string]any),
		removeKeys: make(map[string]bool),
		seenKeys:   make(map[string]bool),
	}

	// 分类规则
	for _, r := range rules {
		switch r.Action {
		case ActionSet:
			p.setRules[r.Key] = r.Value
		case ActionAdd:
			p.addRules[r.Key] = r.Value
		case ActionRemove:
			p.removeKeys[r.Key] = true
		}
	}

	return p
}

// process 执行处理，返回结果流
func (p *processor) process() io.Reader {
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		p.scanner = NewScanner(p.input)
		p.depth = 0
		p.firstField = true

		var lastWasComma bool
		var pendingComma bool

		for p.scanner.Next() {
			token := p.scanner.Token()

			switch token.Type {
			case TokenObjectStart:
				if pendingComma {
					pw.Write([]byte(","))
					pendingComma = false
				}
				pw.Write(token.Raw)
				p.depth++
				if p.depth == 1 {
					p.firstField = true
				}
				lastWasComma = false

			case TokenObjectEnd:
				p.depth--
				if p.depth == 0 {
					// 顶层对象结束，插入 add 字段
					p.insertAddFields(pw)
				}
				pw.Write(token.Raw)
				lastWasComma = false
				pendingComma = false

			case TokenArrayStart:
				if pendingComma {
					pw.Write([]byte(","))
					pendingComma = false
				}
				pw.Write(token.Raw)
				p.depth++
				lastWasComma = false

			case TokenArrayEnd:
				p.depth--
				pw.Write(token.Raw)
				lastWasComma = false
				pendingComma = false

			case TokenKey:
				if p.depth == 1 {
					// 顶层 key
					key := token.Value.(string)
					p.seenKeys[key] = true

					if p.removeKeys[key] {
						// remove: 跳过 key, colon, value
						p.scanner.Next() // skip colon
						p.scanner.SkipValue()
						// 不输出，也不设置 pendingComma
						// 下一个有效字段会处理逗号
						continue
					}

					if newValue, ok := p.setRules[key]; ok {
						// set: 输出 key 和新 value，跳过原 value
						if !p.firstField {
							pw.Write([]byte(","))
						}
						p.firstField = false
						pw.Write(token.Raw)
						p.scanner.Next() // skip colon
						pw.Write([]byte(":"))
						p.scanner.SkipValue()
						p.writeValue(pw, newValue)
						lastWasComma = false
						pendingComma = false
						continue
					}

					// 普通字段：使用 CopyValue 高性能复制
					if !p.firstField {
						pw.Write([]byte(","))
					}
					p.firstField = false
					pw.Write(token.Raw)
					p.scanner.Next() // skip colon
					pw.Write([]byte(":"))
					p.scanner.CopyValue(pw) // 直接复制 value 字节
					lastWasComma = false
					pendingComma = false
					continue
				} else {
					// 非顶层：透传
					if pendingComma {
						pw.Write([]byte(","))
						pendingComma = false
					}
					pw.Write(token.Raw)
					lastWasComma = false
				}

			case TokenComma:
				if p.depth == 1 {
					// 顶层逗号：延迟处理（可能下一个字段被删除）
					pendingComma = true
					lastWasComma = true
				} else {
					// 非顶层：透传
					pw.Write(token.Raw)
					lastWasComma = true
				}

			case TokenColon:
				pw.Write(token.Raw)
				lastWasComma = false

			default:
				// 其他 token（string, number, bool, null）：透传
				if pendingComma {
					pw.Write([]byte(","))
					pendingComma = false
				}
				pw.Write(token.Raw)
				lastWasComma = false
			}
		}

		_ = lastWasComma // 消除未使用警告
	}()

	return pr
}

// insertAddFields 在对象末尾插入 add 字段
func (p *processor) insertAddFields(pw *io.PipeWriter) {
	for key, value := range p.addRules {
		if !p.seenKeys[key] {
			// key 未出现过，执行 add
			if !p.firstField {
				pw.Write([]byte(","))
			}
			p.firstField = false
			p.writeKey(pw, key)
			pw.Write([]byte(":"))
			p.writeValue(pw, value)
		}
	}
}

// writeKey 写入 key
func (p *processor) writeKey(pw *io.PipeWriter, key string) {
	pw.Write([]byte("\""))
	pw.Write([]byte(escapeString(key)))
	pw.Write([]byte("\""))
}

// writeValue 写入 value
func (p *processor) writeValue(pw *io.PipeWriter, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		pw.Write([]byte("null"))
		return
	}
	pw.Write(data)
}

// escapeString 转义字符串中的特殊字符
func escapeString(s string) string {
	var buf bytes.Buffer
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString("\\\"")
		case '\\':
			buf.WriteString("\\\\")
		case '\n':
			buf.WriteString("\\n")
		case '\r':
			buf.WriteString("\\r")
		case '\t':
			buf.WriteString("\\t")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// processDirect 直接写入 writer（高性能版本，无 io.Pipe 开销）
func (p *processor) processDirect(w io.Writer) error {
	p.scanner = NewScanner(p.input)
	p.depth = 0
	p.firstField = true

	var pendingComma bool

	for p.scanner.Next() {
		token := p.scanner.Token()

		switch token.Type {
		case TokenObjectStart:
			if pendingComma {
				w.Write([]byte(","))
				pendingComma = false
			}
			w.Write(token.Raw)
			p.depth++
			if p.depth == 1 {
				p.firstField = true
			}

		case TokenObjectEnd:
			p.depth--
			if p.depth == 0 {
				p.insertAddFieldsDirect(w)
			}
			w.Write(token.Raw)
			pendingComma = false

		case TokenArrayStart:
			if pendingComma {
				w.Write([]byte(","))
				pendingComma = false
			}
			w.Write(token.Raw)
			p.depth++

		case TokenArrayEnd:
			p.depth--
			w.Write(token.Raw)
			pendingComma = false

		case TokenKey:
			if p.depth == 1 {
				key := token.Value.(string)
				p.seenKeys[key] = true

				if p.removeKeys[key] {
					p.scanner.Next()
					p.scanner.SkipValue()
					continue
				}

				if newValue, ok := p.setRules[key]; ok {
					if !p.firstField {
						w.Write([]byte(","))
					}
					p.firstField = false
					w.Write(token.Raw)
					p.scanner.Next()
					w.Write([]byte(":"))
					p.scanner.SkipValue()
					p.writeValueDirect(w, newValue)
					pendingComma = false
					continue
				}

				if !p.firstField {
					w.Write([]byte(","))
				}
				p.firstField = false
				w.Write(token.Raw)
				p.scanner.Next()
				w.Write([]byte(":"))
				p.scanner.CopyValue(w)
				pendingComma = false
				continue
			} else {
				if pendingComma {
					w.Write([]byte(","))
					pendingComma = false
				}
				w.Write(token.Raw)
			}

		case TokenComma:
			if p.depth == 1 {
				pendingComma = true
			} else {
				w.Write(token.Raw)
			}

		case TokenColon:
			w.Write(token.Raw)

		default:
			if pendingComma {
				w.Write([]byte(","))
				pendingComma = false
			}
			w.Write(token.Raw)
		}
	}

	if err := p.scanner.Err(); err != nil {
		return err
	}
	return nil
}

// insertAddFieldsDirect 在对象末尾插入 add 字段（直写版本）
func (p *processor) insertAddFieldsDirect(w io.Writer) {
	for key, value := range p.addRules {
		if !p.seenKeys[key] {
			if !p.firstField {
				w.Write([]byte(","))
			}
			p.firstField = false
			w.Write([]byte("\""))
			w.Write([]byte(escapeString(key)))
			w.Write([]byte("\":"))
			p.writeValueDirect(w, value)
		}
	}
}

// writeValueDirect 写入 value（直写版本）
func (p *processor) writeValueDirect(w io.Writer, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		w.Write([]byte("null"))
		return
	}
	w.Write(data)
}

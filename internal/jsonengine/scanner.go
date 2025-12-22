package jsonengine

import (
	"bufio"
	"bytes"
	"io"
)

// TokenType 定义 JSON token 类型
type TokenType int

const (
	TokenError TokenType = iota
	TokenEOF
	TokenObjectStart  // {
	TokenObjectEnd    // }
	TokenArrayStart   // [
	TokenArrayEnd     // ]
	TokenColon        // :
	TokenComma        // ,
	TokenString       // "..."
	TokenNumber       // 123, 1.23, -1.23e10
	TokenTrue         // true
	TokenFalse        // false
	TokenNull         // null
	TokenKey          // 对象的 key（带引号的字符串）
)

// Token 表示一个 JSON token
type Token struct {
	Type  TokenType
	Raw   []byte // 原始字节（包含引号等）
	Value any    // 解析后的值（仅 string/number/bool/null）
}

// Scanner 流式 JSON 扫描器
type Scanner struct {
	reader    *bufio.Reader
	lastToken Token
	err       error
	depth     int  // 嵌套深度
	inObject  bool // 当前是否在对象中（用于区分 key 和 string value）
	expectKey bool // 是否期待 key
}

// NewScanner 创建扫描器
func NewScanner(r io.Reader) *Scanner {
	return &Scanner{
		reader:    bufio.NewReaderSize(r, 1024*1024), // 1MB buffer for large responses
		expectKey: false,
	}
}

// Next 扫描下一个 token
func (s *Scanner) Next() bool {
	if s.err != nil {
		return false
	}

	s.skipWhitespace()

	b, err := s.reader.ReadByte()
	if err != nil {
		if err == io.EOF {
			s.lastToken = Token{Type: TokenEOF}
		} else {
			s.err = err
			s.lastToken = Token{Type: TokenError}
		}
		return false
	}

	switch b {
	case '{':
		s.lastToken = Token{Type: TokenObjectStart, Raw: []byte{b}}
		s.depth++
		s.inObject = true
		s.expectKey = true
		return true

	case '}':
		s.lastToken = Token{Type: TokenObjectEnd, Raw: []byte{b}}
		s.depth--
		s.expectKey = false
		return true

	case '[':
		s.lastToken = Token{Type: TokenArrayStart, Raw: []byte{b}}
		s.depth++
		s.inObject = false
		s.expectKey = false
		return true

	case ']':
		s.lastToken = Token{Type: TokenArrayEnd, Raw: []byte{b}}
		s.depth--
		return true

	case ':':
		s.lastToken = Token{Type: TokenColon, Raw: []byte{b}}
		s.expectKey = false
		return true

	case ',':
		s.lastToken = Token{Type: TokenComma, Raw: []byte{b}}
		if s.inObject {
			s.expectKey = true
		}
		return true

	case '"':
		return s.scanString()

	case 't':
		return s.scanLiteral([]byte("true"), TokenTrue, true)

	case 'f':
		return s.scanLiteral([]byte("false"), TokenFalse, false)

	case 'n':
		return s.scanLiteral([]byte("null"), TokenNull, nil)

	default:
		if b == '-' || (b >= '0' && b <= '9') {
			return s.scanNumber(b)
		}
		s.err = &ScanError{Msg: "unexpected character", Char: b}
		s.lastToken = Token{Type: TokenError}
		return false
	}
}

// Token 返回当前 token
func (s *Scanner) Token() Token {
	return s.lastToken
}

// Err 返回扫描错误
func (s *Scanner) Err() error {
	return s.err
}

// Depth 返回当前嵌套深度
func (s *Scanner) Depth() int {
	return s.depth
}

// SkipValue 跳过当前 value（用于 remove 操作）
// 调用时机：在读取到 key 和 colon 之后
func (s *Scanner) SkipValue() error {
	// 先跳过空白
	s.skipWhitespace()

	b, err := s.reader.ReadByte()
	if err != nil {
		return err
	}

	switch b {
	case '{', '[':
		// 复合类型：跟踪嵌套深度
		depth := 1
		inString := false
		escape := false

		for depth > 0 {
			c, err := s.reader.ReadByte()
			if err != nil {
				return err
			}

			if escape {
				escape = false
				continue
			}

			if c == '\\' && inString {
				escape = true
				continue
			}

			if c == '"' {
				inString = !inString
				continue
			}

			if !inString {
				if c == '{' || c == '[' {
					depth++
				} else if c == '}' || c == ']' {
					depth--
				}
			}
		}
		return nil

	case '"':
		// 字符串：扫描到结束引号
		escape := false
		for {
			c, err := s.reader.ReadByte()
			if err != nil {
				return err
			}
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				return nil
			}
		}

	case 't':
		// true
		buf := make([]byte, 3)
		_, err := io.ReadFull(s.reader, buf)
		return err

	case 'f':
		// false
		buf := make([]byte, 4)
		_, err := io.ReadFull(s.reader, buf)
		return err

	case 'n':
		// null
		buf := make([]byte, 3)
		_, err := io.ReadFull(s.reader, buf)
		return err

	default:
		// 数字：读取到非数字字符
		if b == '-' || (b >= '0' && b <= '9') {
			for {
				c, err := s.reader.ReadByte()
				if err != nil {
					if err == io.EOF {
						return nil
					}
					return err
				}
				// 数字字符: 0-9, ., e, E, +, -
				if !((c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-') {
					s.reader.UnreadByte()
					return nil
				}
			}
		}
		return &ScanError{Msg: "unexpected character in value", Char: b}
	}
}

// CopyValue 直接复制当前 value 到 writer（高性能批量版本）
// 使用 Peek + Discard 模式，减少函数调用开销
func (s *Scanner) CopyValue(w io.Writer) error {
	s.skipWhitespace()

	b, err := s.reader.ReadByte()
	if err != nil {
		return err
	}

	switch b {
	case '{', '[':
		w.Write([]byte{b})
		return s.copyCompoundValue(w)

	case '"':
		w.Write([]byte{b})
		return s.copyStringValue(w)

	case 't':
		remaining := make([]byte, 3)
		if _, err := io.ReadFull(s.reader, remaining); err != nil {
			return err
		}
		w.Write([]byte{'t'})
		w.Write(remaining)
		return nil

	case 'f':
		remaining := make([]byte, 4)
		if _, err := io.ReadFull(s.reader, remaining); err != nil {
			return err
		}
		w.Write([]byte{'f'})
		w.Write(remaining)
		return nil

	case 'n':
		remaining := make([]byte, 3)
		if _, err := io.ReadFull(s.reader, remaining); err != nil {
			return err
		}
		w.Write([]byte{'n'})
		w.Write(remaining)
		return nil

	default:
		if b == '-' || (b >= '0' && b <= '9') {
			w.Write([]byte{b})
			return s.copyNumberValue(w)
		}
		return &ScanError{Msg: "unexpected character in value", Char: b}
	}
}

// copyStringValue 批量复制字符串值（从开始引号之后到结束引号）
func (s *Scanner) copyStringValue(w io.Writer) error {
	escape := false
	for {
		// Peek 1MB（bufio.Reader 会尽可能填充缓冲区）
		data, err := s.reader.Peek(1024 * 1024)
		if len(data) == 0 {
			if err != nil {
				return err
			}
			continue
		}
		// err 可能是 bufio.ErrBufferFull，表示缓冲区满了，这是正常的

		// 在 Peek 的数据中扫描找结束引号
		endPos := -1
		for i := 0; i < len(data); i++ {
			c := data[i]
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				endPos = i
				break
			}
		}

		if endPos >= 0 {
			// 找到结束引号，写出包括引号的部分，然后 Discard
			w.Write(data[:endPos+1])
			s.reader.Discard(endPos + 1)
			return nil
		}

		// 没找到，写出全部并继续
		w.Write(data)
		s.reader.Discard(len(data))
	}
}

// copyCompoundValue 批量复制复合值（对象或数组）
func (s *Scanner) copyCompoundValue(w io.Writer) error {
	depth := 1
	inString := false
	escape := false

	for depth > 0 {
		// Peek 1MB
		data, err := s.reader.Peek(1024 * 1024)
		if len(data) == 0 {
			if err != nil {
				return err
			}
			continue
		}

		// 扫描找到 depth=0 的位置
		endPos := -1
		for i := 0; i < len(data); i++ {
			c := data[i]

			if escape {
				escape = false
				continue
			}
			if c == '\\' && inString {
				escape = true
				continue
			}
			if c == '"' {
				inString = !inString
				continue
			}
			if !inString {
				if c == '{' || c == '[' {
					depth++
				} else if c == '}' || c == ']' {
					depth--
					if depth == 0 {
						endPos = i
						break
					}
				}
			}
		}

		if endPos >= 0 {
			w.Write(data[:endPos+1])
			s.reader.Discard(endPos + 1)
			return nil
		}

		w.Write(data)
		s.reader.Discard(len(data))
	}
	return nil
}

// copyNumberValue 复制数字值
func (s *Scanner) copyNumberValue(w io.Writer) error {
	for {
		// Peek 1MB
		data, err := s.reader.Peek(1024 * 1024)
		if len(data) == 0 {
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			continue
		}

		// 找到非数字字符的位置
		endPos := len(data)
		for i := 0; i < len(data); i++ {
			c := data[i]
			if !((c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-') {
				endPos = i
				break
			}
		}

		if endPos > 0 {
			w.Write(data[:endPos])
			s.reader.Discard(endPos)
		}

		if endPos < len(data) {
			// 遇到非数字字符，结束
			return nil
		}
	}
}

// skipWhitespace 跳过空白字符
func (s *Scanner) skipWhitespace() {
	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			return
		}
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			s.reader.UnreadByte()
			return
		}
	}
}

// scanString 扫描字符串
func (s *Scanner) scanString() bool {
	var buf bytes.Buffer
	buf.WriteByte('"')

	escape := false
	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			s.err = err
			s.lastToken = Token{Type: TokenError}
			return false
		}

		buf.WriteByte(b)

		if escape {
			escape = false
			continue
		}

		if b == '\\' {
			escape = true
			continue
		}

		if b == '"' {
			break
		}
	}

	raw := buf.Bytes()
	// 提取字符串值（去掉引号）
	value := string(raw[1 : len(raw)-1])

	if s.expectKey && s.inObject {
		s.lastToken = Token{Type: TokenKey, Raw: raw, Value: value}
	} else {
		s.lastToken = Token{Type: TokenString, Raw: raw, Value: value}
	}
	return true
}

// scanNumber 扫描数字
func (s *Scanner) scanNumber(first byte) bool {
	var buf bytes.Buffer
	buf.WriteByte(first)

	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			s.err = err
			s.lastToken = Token{Type: TokenError}
			return false
		}

		if (b >= '0' && b <= '9') || b == '.' || b == 'e' || b == 'E' || b == '+' || b == '-' {
			buf.WriteByte(b)
		} else {
			s.reader.UnreadByte()
			break
		}
	}

	s.lastToken = Token{Type: TokenNumber, Raw: buf.Bytes()}
	return true
}

// scanLiteral 扫描字面量（true, false, null）
func (s *Scanner) scanLiteral(expected []byte, tokenType TokenType, value any) bool {
	remaining := expected[1:] // 第一个字节已经读取
	buf := make([]byte, len(remaining))

	_, err := io.ReadFull(s.reader, buf)
	if err != nil {
		s.err = err
		s.lastToken = Token{Type: TokenError}
		return false
	}

	if !bytes.Equal(buf, remaining) {
		s.err = &ScanError{Msg: "invalid literal"}
		s.lastToken = Token{Type: TokenError}
		return false
	}

	s.lastToken = Token{Type: tokenType, Raw: expected, Value: value}
	return true
}

// ScanError 扫描错误
type ScanError struct {
	Msg  string
	Char byte
}

func (e *ScanError) Error() string {
	if e.Char != 0 {
		return e.Msg + ": " + string(e.Char)
	}
	return e.Msg
}

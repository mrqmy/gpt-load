package jsonengine

import (
	"bytes"
	"strings"
	"testing"
)

func TestPathParsing(t *testing.T) {
	tests := []struct {
		path     string
		expected []Segment
	}{
		{
			path: "a",
			expected: []Segment{
				{Type: SegField, Value: "a"},
			},
		},
		{
			path: "a.b.c",
			expected: []Segment{
				{Type: SegField, Value: "a"},
				{Type: SegField, Value: "b"},
				{Type: SegField, Value: "c"},
			},
		},
		{
			path: "a.*.c",
			expected: []Segment{
				{Type: SegField, Value: "a"},
				{Type: SegWildcard, Value: "*"},
				{Type: SegField, Value: "c"},
			},
		},
		{
			path: "a.[*].c",
			expected: []Segment{
				{Type: SegField, Value: "a"},
				{Type: SegArrayAll, Value: "[*]"},
				{Type: SegField, Value: "c"},
			},
		},
		{
			path: "a.[0].c",
			expected: []Segment{
				{Type: SegField, Value: "a"},
				{Type: SegArrayIdx, Value: "[0]", Index: 0},
				{Type: SegField, Value: "c"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			segments, err := ParsePath(tt.path)
			if err != nil {
				t.Fatalf("ParsePath(%q) error: %v", tt.path, err)
			}
			if len(segments) != len(tt.expected) {
				t.Fatalf("ParsePath(%q) = %d segments, want %d", tt.path, len(segments), len(tt.expected))
			}
			for i, seg := range segments {
				if seg.Type != tt.expected[i].Type {
					t.Errorf("segment[%d].Type = %v, want %v", i, seg.Type, tt.expected[i].Type)
				}
				if seg.Value != tt.expected[i].Value {
					t.Errorf("segment[%d].Value = %q, want %q", i, seg.Value, tt.expected[i].Value)
				}
			}
		})
	}
}

func TestPathEngineTopLevel(t *testing.T) {
	tests := []struct {
		name   string
		rules  []PathRule
		input  string
		expect string
	}{
		{
			name: "remove top level field",
			rules: []PathRule{
				{Path: "a", Action: ActionRemove},
			},
			input:  `{"a":1,"b":2}`,
			expect: `{"b":2}`,
		},
		{
			name: "remove multiple top level fields",
			rules: []PathRule{
				{Path: "a", Action: ActionRemove},
				{Path: "c", Action: ActionRemove},
			},
			input:  `{"a":1,"b":2,"c":3}`,
			expect: `{"b":2}`,
		},
		{
			name: "remove non-existent field",
			rules: []PathRule{
				{Path: "x", Action: ActionRemove},
			},
			input:  `{"a":1,"b":2}`,
			expect: `{"a":1,"b":2}`,
		},
		{
			name: "remove field with object value",
			rules: []PathRule{
				{Path: "a", Action: ActionRemove},
			},
			input:  `{"a":{"nested":1},"b":2}`,
			expect: `{"b":2}`,
		},
		{
			name: "remove field with array value",
			rules: []PathRule{
				{Path: "a", Action: ActionRemove},
			},
			input:  `{"a":[1,2,3],"b":2}`,
			expect: `{"b":2}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewPathEngine(tt.rules)
			if err != nil {
				t.Fatalf("NewPathEngine error: %v", err)
			}

			var out bytes.Buffer
			err = engine.Process(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Process error: %v", err)
			}

			result := out.String()
			if result != tt.expect {
				t.Errorf("got %q, want %q", result, tt.expect)
			}
		})
	}
}

func TestPathEngineNested(t *testing.T) {
	tests := []struct {
		name   string
		rules  []PathRule
		input  string
		expect string
	}{
		{
			name: "remove nested field",
			rules: []PathRule{
				{Path: "a.b", Action: ActionRemove},
			},
			input:  `{"a":{"b":1,"c":2}}`,
			expect: `{"a":{"c":2}}`,
		},
		{
			name: "remove deeply nested field",
			rules: []PathRule{
				{Path: "a.b.c", Action: ActionRemove},
			},
			input:  `{"a":{"b":{"c":1,"d":2}}}`,
			expect: `{"a":{"b":{"d":2}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewPathEngine(tt.rules)
			if err != nil {
				t.Fatalf("NewPathEngine error: %v", err)
			}

			var out bytes.Buffer
			err = engine.Process(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Process error: %v", err)
			}

			result := out.String()
			if result != tt.expect {
				t.Errorf("got %q, want %q", result, tt.expect)
			}
		})
	}
}

func TestPathEngineWildcard(t *testing.T) {
	tests := []struct {
		name   string
		rules  []PathRule
		input  string
		expect string
	}{
		{
			name: "wildcard match all keys",
			rules: []PathRule{
				{Path: "a.*.x", Action: ActionRemove},
			},
			input:  `{"a":{"m":{"x":1,"y":2},"n":{"x":3,"y":4}}}`,
			expect: `{"a":{"m":{"y":2},"n":{"y":4}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewPathEngine(tt.rules)
			if err != nil {
				t.Fatalf("NewPathEngine error: %v", err)
			}

			var out bytes.Buffer
			err = engine.Process(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Process error: %v", err)
			}

			result := out.String()
			if result != tt.expect {
				t.Errorf("got %q, want %q", result, tt.expect)
			}
		})
	}
}

func TestSIMDScan(t *testing.T) {
	tests := []struct {
		input    string
		expected []uint32
	}{
		{
			// {"a":1} - 结构字符: { " " : }
			// 位置:     0 1 3 4 6
			input:    `{"a":1}`,
			expected: []uint32{0, 1, 3, 4, 6},
		},
		{
			// {"a":"b"} - 结构字符: { " " : " " }
			// 位置:       0 1 3 4 5 7 8
			input:    `{"a":"b"}`,
			expected: []uint32{0, 1, 3, 4, 5, 7, 8},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			positions := make([]uint32, 100)
			n := ScanStructural([]byte(tt.input), positions)

			if n != len(tt.expected) {
				t.Errorf("ScanStructural found %d positions, want %d", n, len(tt.expected))
				return
			}

			for i := 0; i < n; i++ {
				if positions[i] != tt.expected[i] {
					t.Errorf("position[%d] = %d, want %d", i, positions[i], tt.expected[i])
				}
			}
		})
	}
}

func TestPathEngineLegacyCompatibility(t *testing.T) {
	// 测试从旧格式规则创建引擎
	legacyRules := []Rule{
		{Key: "password", Action: ActionRemove},
		{Key: "secret", Action: ActionRemove},
	}

	engine, err := NewPathEngineFromLegacy(legacyRules)
	if err != nil {
		t.Fatalf("NewPathEngineFromLegacy error: %v", err)
	}

	input := `{"username":"test","password":"123","secret":"abc","data":"ok"}`
	expected := `{"username":"test","data":"ok"}`

	var out bytes.Buffer
	err = engine.Process(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	result := out.String()
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestPathEngineArrayIndex(t *testing.T) {
	tests := []struct {
		name   string
		rules  []PathRule
		input  string
		expect string
	}{
		{
			name: "array all elements",
			rules: []PathRule{
				{Path: "items.[*].secret", Action: ActionRemove},
			},
			input:  `{"items":[{"id":1,"secret":"a"},{"id":2,"secret":"b"}]}`,
			expect: `{"items":[{"id":1},{"id":2}]}`,
		},
		{
			name: "array specific index",
			rules: []PathRule{
				{Path: "items.[0].secret", Action: ActionRemove},
			},
			input:  `{"items":[{"id":1,"secret":"a"},{"id":2,"secret":"b"}]}`,
			expect: `{"items":[{"id":1},{"id":2,"secret":"b"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewPathEngine(tt.rules)
			if err != nil {
				t.Fatalf("NewPathEngine error: %v", err)
			}

			var out bytes.Buffer
			err = engine.Process(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Process error: %v", err)
			}

			result := out.String()
			if result != tt.expect {
				t.Errorf("got %q, want %q", result, tt.expect)
			}
		})
	}
}

func TestPathEngineRealWorld(t *testing.T) {
	// 真实场景：Gemini thoughtSignature 过滤
	rules := []PathRule{
		{Path: "candidates.[*].content.parts.[*].thoughtSignature", Action: ActionRemove},
	}

	input := `{"candidates":[{"content":{"parts":[{"text":"hello","thoughtSignature":"xxx"},{"text":"world","thoughtSignature":"yyy"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10}}`

	expected := `{"candidates":[{"content":{"parts":[{"text":"hello"},{"text":"world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10}}`

	engine, err := NewPathEngine(rules)
	if err != nil {
		t.Fatalf("NewPathEngine error: %v", err)
	}

	var out bytes.Buffer
	err = engine.Process(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	result := out.String()
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func BenchmarkPathEngine(b *testing.B) {
	rules := []PathRule{
		{Path: "candidates.*.content.parts.*.thoughtSignature", Action: ActionRemove},
	}

	engine, err := NewPathEngine(rules)
	if err != nil {
		b.Fatalf("NewPathEngine error: %v", err)
	}

	// 模拟大型 JSON 响应
	input := `{"candidates":[{"content":{"parts":[{"text":"hello","thoughtSignature":"xxx"},{"text":"world","thoughtSignature":"yyy"}]}}],"other":"data"}`
	inputBytes := []byte(input)

	b.ResetTimer()
	b.SetBytes(int64(len(inputBytes)))

	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		engine.Process(bytes.NewReader(inputBytes), &out)
	}
}

func BenchmarkSIMDScan(b *testing.B) {
	// 512KB 测试数据
	data := make([]byte, 512*1024)
	for i := range data {
		switch i % 10 {
		case 0:
			data[i] = '{'
		case 1:
			data[i] = '"'
		case 5:
			data[i] = ':'
		case 9:
			data[i] = '}'
		default:
			data[i] = 'a'
		}
	}

	positions := make([]uint32, len(data)/4)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		ScanStructural(data, positions)
	}
}

// TestPathEngineSet 测试Set操作（完整版本）
func TestPathEngineSet(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		rules    []PathRule
		expected string
	}{
		{
			name:  "set_top_level_with_valuebytes",
			input: `{"a":1,"b":2,"c":3}`,
			rules: []PathRule{
				{Path: "b", Action: ActionSet, ValueBytes: []byte(`999`)}, // 使用ValueBytes（零拷贝）
			},
			expected: `{"a":1,"b":999,"c":3}`,
		},
		{
			name:  "set_nested_field",
			input: `{"user":{"name":"alice","age":20},"id":1}`,
			rules: []PathRule{
				{Path: "user.age", Action: ActionSet, ValueBytes: []byte(`25`)},
			},
			expected: `{"user":{"name":"alice","age":25},"id":1}`,
		},
		{
			name:  "set_with_large_value",
			input: `{"data":"old"}`,
			rules: []PathRule{
				{Path: "data", Action: ActionSet, ValueBytes: []byte(`{"nested":{"deep":{"value":123}}}`)}, // 复杂对象
			},
			expected: `{"data":{"nested":{"deep":{"value":123}}}}`,
		},
		{
			name:  "set_wildcard",
			input: `{"users":[{"name":"alice"},{"name":"bob"}]}`,
			rules: []PathRule{
				{Path: "users.[*].name", Action: ActionSet, ValueBytes: []byte(`"unknown"`)},
			},
			expected: `{"users":[{"name":"unknown"},{"name":"unknown"}]}`,
		},
		{
			name:  "set_array_index",
			input: `{"items":[10,20,30]}`,
			rules: []PathRule{
				{Path: "items.[1]", Action: ActionSet, ValueBytes: []byte(`999`)},
			},
			expected: `{"items":[10,999,30]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewPathEngine(nil)
			if err != nil {
				t.Fatalf("NewPathEngine failed: %v", err)
			}
			for _, rule := range tt.rules {
				if err := engine.AddRule(rule); err != nil {
					t.Fatalf("AddRule failed: %v", err)
				}
			}

			var out bytes.Buffer
			err = engine.Process(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Process failed: %v", err)
			}

			got := out.String()
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestPathEngineAdd 测试Add操作（完整版本）
func TestPathEngineAdd(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		rules    []PathRule
		expected string
	}{
		{
			name:  "add_to_empty_object",
			input: `{}`,
			rules: []PathRule{
				{Path: "new", Action: ActionAdd, ValueBytes: []byte(`123`)},
			},
			expected: `{"new":123}`,
		},
		{
			name:  "add_to_existing_object",
			input: `{"a":1,"b":2}`,
			rules: []PathRule{
				{Path: "c", Action: ActionAdd, ValueBytes: []byte(`3`)},
			},
			expected: `{"a":1,"b":2,"c":3}`,
		},
		{
			name:  "add_multiple_fields",
			input: `{"x":1}`,
			rules: []PathRule{
				{Path: "y", Action: ActionAdd, ValueBytes: []byte(`2`)},
				{Path: "z", Action: ActionAdd, ValueBytes: []byte(`3`)},
			},
			expected: `{"x":1,"y":2,"z":3}`, // 注：顺序可能因map遍历而变化，实际测试应检查包含关系
		},
		{
			name:  "add_skip_existing",
			input: `{"a":1,"b":2}`,
			rules: []PathRule{
				{Path: "b", Action: ActionAdd, ValueBytes: []byte(`999`)}, // b已存在，不添加
				{Path: "c", Action: ActionAdd, ValueBytes: []byte(`3`)},   // c不存在，添加
			},
			expected: `{"a":1,"b":2,"c":3}`,
		},
		{
			name:  "add_nested",
			input: `{"user":{"name":"alice"}}`,
			rules: []PathRule{
				{Path: "user.age", Action: ActionAdd, ValueBytes: []byte(`20`)},
			},
			expected: `{"user":{"name":"alice","age":20}}`,
		},
		{
			name:  "add_complex_value",
			input: `{"id":1}`,
			rules: []PathRule{
				{Path: "metadata", Action: ActionAdd, ValueBytes: []byte(`{"tags":["a","b"],"count":5}`)},
			},
			expected: `{"id":1,"metadata":{"tags":["a","b"],"count":5}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewPathEngine(nil)
			if err != nil {
				t.Fatalf("NewPathEngine failed: %v", err)
			}
			for _, rule := range tt.rules {
				if err := engine.AddRule(rule); err != nil {
					t.Fatalf("AddRule failed: %v", err)
				}
			}

			var out bytes.Buffer
			err = engine.Process(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Process failed: %v", err)
			}

			got := out.String()
			// Add操作由于map遍历顺序不确定，使用JSON语义比较
			if tt.name == "add_multiple_fields" {
				if !jsonEqual(t, got, tt.expected) {
					t.Errorf("got %q, want %q (order may vary)", got, tt.expected)
				}
			} else {
				if got != tt.expected {
					t.Errorf("got %q, want %q", got, tt.expected)
				}
			}
		})
	}
}

// TestPathEngineMixed 测试混合操作（Remove + Set + Add）
func TestPathEngineMixed(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		rules    []PathRule
		expected string
	}{
		{
			name:  "remove_set_add",
			input: `{"a":1,"b":2,"c":3}`,
			rules: []PathRule{
				{Path: "a", Action: ActionRemove},                        // 删除a
				{Path: "b", Action: ActionSet, ValueBytes: []byte(`999`)}, // 修改b
				{Path: "d", Action: ActionAdd, ValueBytes: []byte(`4`)},  // 添加d
			},
			expected: `{"b":999,"c":3,"d":4}`,
		},
		{
			name:  "nested_mixed",
			input: `{"user":{"name":"alice","age":20,"role":"user"}}`,
			rules: []PathRule{
				{Path: "user.role", Action: ActionRemove},                     // 删除role
				{Path: "user.age", Action: ActionSet, ValueBytes: []byte(`25`)}, // 修改age
				{Path: "user.city", Action: ActionAdd, ValueBytes: []byte(`"NYC"`)}, // 添加city
			},
			expected: `{"user":{"name":"alice","age":25,"city":"NYC"}}`,
		},
		{
			name:  "complex_scenario",
			input: `{"data":{"old":1,"keep":2},"meta":"info"}`,
			rules: []PathRule{
				{Path: "data.old", Action: ActionRemove},
				{Path: "data.keep", Action: ActionSet, ValueBytes: []byte(`999`)},
				{Path: "data.new", Action: ActionAdd, ValueBytes: []byte(`{"x":1}`)},
				{Path: "timestamp", Action: ActionAdd, ValueBytes: []byte(`1234567890`)},
			},
			expected: `{"data":{"keep":999,"new":{"x":1}},"meta":"info","timestamp":1234567890}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewPathEngine(nil)
			if err != nil {
				t.Fatalf("NewPathEngine failed: %v", err)
			}
			for _, rule := range tt.rules {
				if err := engine.AddRule(rule); err != nil {
					t.Fatalf("AddRule failed: %v", err)
				}
			}

			var out bytes.Buffer
			err = engine.Process(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Process failed: %v", err)
			}

			got := out.String()
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

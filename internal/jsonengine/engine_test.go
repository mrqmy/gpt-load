package jsonengine

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// helper: 处理 JSON 并返回结果字符串
func processJSON(t *testing.T, input string, rules []Rule) string {
	engine := New(rules)
	reader := engine.Process(strings.NewReader(input))
	result, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	return string(result)
}

// helper: 比较两个 JSON 是否语义相等
func jsonEqual(t *testing.T, got, want string) bool {
	var gotVal, wantVal any
	if err := json.Unmarshal([]byte(got), &gotVal); err != nil {
		t.Errorf("invalid JSON in got: %v\ngot: %s", err, got)
		return false
	}
	if err := json.Unmarshal([]byte(want), &wantVal); err != nil {
		t.Errorf("invalid JSON in want: %v\nwant: %s", err, want)
		return false
	}

	gotBytes, _ := json.Marshal(gotVal)
	wantBytes, _ := json.Marshal(wantVal)
	return string(gotBytes) == string(wantBytes)
}

// ==================== Remove 测试 ====================

func TestRemove_SingleField(t *testing.T) {
	input := `{"a": 1, "b": 2, "c": 3}`
	rules := []Rule{{Key: "b", Action: ActionRemove}}
	want := `{"a": 1, "c": 3}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove single field:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRemove_FirstField(t *testing.T) {
	input := `{"a": 1, "b": 2, "c": 3}`
	rules := []Rule{{Key: "a", Action: ActionRemove}}
	want := `{"b": 2, "c": 3}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove first field:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRemove_LastField(t *testing.T) {
	input := `{"a": 1, "b": 2, "c": 3}`
	rules := []Rule{{Key: "c", Action: ActionRemove}}
	want := `{"a": 1, "b": 2}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove last field:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRemove_MultipleFields(t *testing.T) {
	input := `{"a": 1, "b": 2, "c": 3, "d": 4}`
	rules := []Rule{
		{Key: "b", Action: ActionRemove},
		{Key: "d", Action: ActionRemove},
	}
	want := `{"a": 1, "c": 3}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove multiple fields:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRemove_NonExistent(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{{Key: "x", Action: ActionRemove}}
	want := `{"a": 1, "b": 2}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove non-existent field:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRemove_NestedObject(t *testing.T) {
	input := `{"a": {"b": 1, "c": 2}, "d": 3}`
	rules := []Rule{{Key: "a", Action: ActionRemove}}
	want := `{"d": 3}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove nested object:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRemove_Array(t *testing.T) {
	input := `{"a": [1, 2, 3], "b": 4}`
	rules := []Rule{{Key: "a", Action: ActionRemove}}
	want := `{"b": 4}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove array:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRemove_AllFields(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{
		{Key: "a", Action: ActionRemove},
		{Key: "b", Action: ActionRemove},
	}
	want := `{}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove all fields:\ngot:  %s\nwant: %s", got, want)
	}
}

// ==================== Set 测试 ====================

func TestSet_SingleField(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{{Key: "b", Action: ActionSet, Value: 999}}
	want := `{"a": 1, "b": 999}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Set single field:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestSet_NonExistent(t *testing.T) {
	input := `{"a": 1}`
	rules := []Rule{{Key: "b", Action: ActionSet, Value: 999}}
	want := `{"a": 1}` // 不添加

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Set non-existent field (should not add):\ngot:  %s\nwant: %s", got, want)
	}
}

func TestSet_StringValue(t *testing.T) {
	input := `{"a": 1, "b": "old"}`
	rules := []Rule{{Key: "b", Action: ActionSet, Value: "new"}}
	want := `{"a": 1, "b": "new"}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Set string value:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestSet_ObjectValue(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{{Key: "b", Action: ActionSet, Value: map[string]int{"x": 1, "y": 2}}}
	want := `{"a": 1, "b": {"x": 1, "y": 2}}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Set object value:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestSet_ArrayValue(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{{Key: "b", Action: ActionSet, Value: []int{1, 2, 3}}}
	want := `{"a": 1, "b": [1, 2, 3]}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Set array value:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestSet_NullValue(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{{Key: "b", Action: ActionSet, Value: nil}}
	want := `{"a": 1, "b": null}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Set null value:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestSet_BoolValue(t *testing.T) {
	input := `{"a": 1, "b": false}`
	rules := []Rule{{Key: "b", Action: ActionSet, Value: true}}
	want := `{"a": 1, "b": true}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Set bool value:\ngot:  %s\nwant: %s", got, want)
	}
}

// ==================== Add 测试 ====================

func TestAdd_NewField(t *testing.T) {
	input := `{"a": 1}`
	rules := []Rule{{Key: "b", Action: ActionAdd, Value: 2}}
	want := `{"a": 1, "b": 2}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Add new field:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestAdd_ExistingField(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{{Key: "b", Action: ActionAdd, Value: 999}}
	want := `{"a": 1, "b": 2}` // 不覆盖

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Add existing field (should not overwrite):\ngot:  %s\nwant: %s", got, want)
	}
}

func TestAdd_EmptyObject(t *testing.T) {
	input := `{}`
	rules := []Rule{{Key: "a", Action: ActionAdd, Value: 1}}
	want := `{"a": 1}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Add to empty object:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestAdd_MultipleFields(t *testing.T) {
	input := `{"a": 1}`
	rules := []Rule{
		{Key: "b", Action: ActionAdd, Value: 2},
		{Key: "c", Action: ActionAdd, Value: 3},
	}
	want := `{"a": 1, "b": 2, "c": 3}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Add multiple fields:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestAdd_ObjectValue(t *testing.T) {
	input := `{"a": 1}`
	rules := []Rule{{Key: "b", Action: ActionAdd, Value: map[string]any{"x": 1}}}
	want := `{"a": 1, "b": {"x": 1}}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Add object value:\ngot:  %s\nwant: %s", got, want)
	}
}

// ==================== 组合测试 ====================

func TestCombined_SetAddRemove(t *testing.T) {
	input := `{"a": 1, "b": 2, "c": 3}`
	rules := []Rule{
		{Key: "a", Action: ActionRemove},
		{Key: "b", Action: ActionSet, Value: 999},
		{Key: "d", Action: ActionAdd, Value: 4},
	}
	want := `{"b": 999, "c": 3, "d": 4}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Combined set/add/remove:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestCombined_RemoveAndAdd(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{
		{Key: "a", Action: ActionRemove},
		{Key: "c", Action: ActionAdd, Value: 3},
	}
	want := `{"b": 2, "c": 3}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove and add:\ngot:  %s\nwant: %s", got, want)
	}
}

// ==================== 边界情况 ====================

func TestNoRules(t *testing.T) {
	input := `{"a": 1, "b": 2}`
	rules := []Rule{}
	want := `{"a": 1, "b": 2}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("No rules:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestEmptyInput(t *testing.T) {
	input := `{}`
	rules := []Rule{{Key: "a", Action: ActionRemove}}
	want := `{}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Empty input:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestNestedObjectNotAffected(t *testing.T) {
	// 只操作顶层，嵌套对象内的同名字段不受影响
	input := `{"a": {"b": 1}, "b": 2}`
	rules := []Rule{{Key: "b", Action: ActionRemove}}
	want := `{"a": {"b": 1}}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Nested object not affected:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestLargeStringValue(t *testing.T) {
	// 模拟大字符串（如 base64）
	largeString := strings.Repeat("x", 10000)
	input := `{"data": "` + largeString + `", "type": "image"}`
	rules := []Rule{{Key: "data", Action: ActionRemove}}
	want := `{"type": "image"}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove large string:\ngot length: %d\nwant: %s", len(got), want)
	}
}

func TestSpecialCharactersInKey(t *testing.T) {
	input := `{"a-b": 1, "c_d": 2}`
	rules := []Rule{{Key: "a-b", Action: ActionRemove}}
	want := `{"c_d": 2}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Special characters in key:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestWhitespace(t *testing.T) {
	input := `{
		"a": 1,
		"b": 2,
		"c": 3
	}`
	rules := []Rule{{Key: "b", Action: ActionRemove}}
	want := `{"a": 1, "c": 3}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Whitespace handling:\ngot:  %s\nwant: %s", got, want)
	}
}

// ==================== 真实场景测试 ====================

func TestRealWorld_GeminiThoughtSignature(t *testing.T) {
	input := `{
		"candidates": [{"content": "hello"}],
		"thoughtSignature": "very_long_signature_data",
		"usageMetadata": {"promptTokenCount": 10}
	}`
	rules := []Rule{{Key: "thoughtSignature", Action: ActionRemove}}
	want := `{"candidates": [{"content": "hello"}], "usageMetadata": {"promptTokenCount": 10}}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Remove Gemini thoughtSignature:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRealWorld_AddDefaultParams(t *testing.T) {
	input := `{"model": "gpt-4", "messages": [{"role": "user", "content": "hi"}]}`
	rules := []Rule{
		{Key: "temperature", Action: ActionAdd, Value: 0.7},
		{Key: "stream", Action: ActionAdd, Value: true},
	}
	want := `{"model": "gpt-4", "messages": [{"role": "user", "content": "hi"}], "temperature": 0.7, "stream": true}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Add default params:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRealWorld_ModifyAndRemove(t *testing.T) {
	input := `{
		"model": "gpt-4",
		"max_tokens": 100,
		"user": "user123",
		"messages": []
	}`
	rules := []Rule{
		{Key: "max_tokens", Action: ActionSet, Value: 4096},
		{Key: "user", Action: ActionRemove},
	}
	want := `{"model": "gpt-4", "max_tokens": 4096, "messages": []}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Modify and remove:\ngot:  %s\nwant: %s", got, want)
	}
}

// ==================== 边界情况测试 ====================

func TestEdgeCase_EmptyInput(t *testing.T) {
	input := ""
	rules := []Rule{{Key: "test", Action: ActionSet, Value: "1"}}

	got := processJSON(t, input, rules)
	t.Logf("Empty input result: %q (len=%d)", got, len(got))
	// 空输入应该返回空输出
	if got != "" {
		t.Errorf("Empty input should return empty output, got: %q", got)
	}
}

func TestEdgeCase_InvalidJSON(t *testing.T) {
	input := "not json"
	rules := []Rule{{Key: "test", Action: ActionSet, Value: "1"}}

	got := processJSON(t, input, rules)
	t.Logf("Invalid JSON result: %q (len=%d)", got, len(got))
	// 无效 JSON 可能返回部分数据或空数据
}

func TestEdgeCase_EmptyObject(t *testing.T) {
	input := "{}"
	rules := []Rule{{Key: "test", Action: ActionSet, Value: "1"}}
	want := "{}"

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Empty object:\ngot:  %s\nwant: %s", got, want)
	}
}

// TestRealWorld_SetNonExistentField 测试 set 操作对不存在的字段（应该保持原样）
func TestRealWorld_SetNonExistentField(t *testing.T) {
	input := `{"candidates": [{"content": "hello"}], "usageMetadata": {"promptTokenCount": 10}}`
	rules := []Rule{{Key: "modelVersion", Action: ActionSet, Value: "1"}}
	// set 操作只修改已存在的字段，不存在的字段不会被添加
	want := `{"candidates": [{"content": "hello"}], "usageMetadata": {"promptTokenCount": 10}}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Set non-existent field should not change response:\ngot:  %s\nwant: %s", got, want)
	}
}

// TestRealWorld_GeminiImageGeneration 模拟 Gemini 图片生成响应
func TestRealWorld_GeminiImageGeneration(t *testing.T) {
	input := `{
		"candidates": [{
			"content": {
				"parts": [{
					"inlineData": {
						"mimeType": "image/png",
						"data": "base64encodeddata..."
					}
				}]
			}
		}],
		"usageMetadata": {
			"promptTokenCount": 100,
			"candidatesTokenCount": 200
		}
	}`
	rules := []Rule{{Key: "modelVersion", Action: ActionSet, Value: "1"}}
	// 原始响应应该完整保留（modelVersion 不存在，set 不会添加）
	want := `{
		"candidates": [{
			"content": {
				"parts": [{
					"inlineData": {
						"mimeType": "image/png",
						"data": "base64encodeddata..."
					}
				}]
			}
		}],
		"usageMetadata": {
			"promptTokenCount": 100,
			"candidatesTokenCount": 200
		}
	}`

	got := processJSON(t, input, rules)
	if !jsonEqual(t, got, want) {
		t.Errorf("Gemini image generation response should be preserved:\ngot:  %s\nwant: %s", got, want)
	}
}

package main

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestExtractVariablesFromString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"simple variable", "X {{a}} Y", []string{"a"}},
		{"multiple variables", "X {{a}} Y {{b}}", []string{"a", "b"}},
		{"with spaces", "{{  foo  }} and {{ bar }}", []string{"foo", "bar"}},
		{"no variables", "no variables here", []string{}},
		{"dot notation skipped", "{{ payload.user.id }}", []string{}},
		{"content dot prefix", "{{ content.userId }}", []string{"userId"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractVariablesFromString(tt.input)
			if !stringsContainSame(result, tt.expected) {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAllTransitionsTopLevel(t *testing.T) {
	nodeJSON := []byte(`{
		"id": "node1",
		"logics": [
			{"type": "api_rpc", "extra": {"key": "{{myVar}}"}, "extra_type": {"key": "string"}},
			{"type": "go", "to_node_id": "node2"}
		],
		"semaphors": []
	}`)

	var node Node
	if err := json.Unmarshal(nodeJSON, &node); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	transitions := node.allTransitions()
	if len(transitions) != 2 {
		t.Errorf("got %d transitions, want 2", len(transitions))
	}
}

func TestAllTransitionsCondition(t *testing.T) {
	nodeJSON := []byte(`{
		"id": "node1",
		"condition": {
			"logics": [
				{"type": "api", "url": "https://example.com/{{param}}"},
				{"type": "go", "to_node_id": "node2"}
			],
			"semaphors": []
		}
	}`)

	var node Node
	if err := json.Unmarshal(nodeJSON, &node); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	transitions := node.allTransitions()
	if len(transitions) != 2 {
		t.Errorf("got %d transitions, want 2", len(transitions))
	}
}

func TestCollectVarsFromLogicRaw_Extra(t *testing.T) {
	raw := map[string]interface{}{
		"type": "api_rpc",
		"extra": map[string]interface{}{
			"conv_id": "{{aiNodeProcessId}}",
			"login":   "{{corezoid_secret_login}}",
			"key":     "{{corezoid_secret_key}}",
		},
		"extra_type": map[string]interface{}{
			"conv_id": "number",
			"login":   "string",
			"key":     "string",
		},
	}

	props := map[string]interface{}{}
	requiredSet := map[string]bool{}
	collectVarsFromLogicRaw(raw, props, requiredSet)

	want := []string{"aiNodeProcessId", "corezoid_secret_login", "corezoid_secret_key"}
	got := keysOf(requiredSet)
	if !stringsContainSame(got, want) {
		t.Errorf("got vars %v, want %v", got, want)
	}

	// check type hint applied: conv_id should be number
	if p, ok := props["aiNodeProcessId"].(map[string]interface{}); ok {
		if p["type"] != "number" {
			t.Errorf("aiNodeProcessId type = %v, want number", p["type"])
		}
	} else {
		t.Errorf("aiNodeProcessId not in props")
	}
}

func TestCollectVarsFromLogicRaw_URL(t *testing.T) {
	raw := map[string]interface{}{
		"type": "api",
		"url":  "https://example.com/api?token={{token}}&user={{userId}}",
	}

	props := map[string]interface{}{}
	requiredSet := map[string]bool{}
	collectVarsFromLogicRaw(raw, props, requiredSet)

	want := []string{"token", "userId"}
	got := keysOf(requiredSet)
	if !stringsContainSame(got, want) {
		t.Errorf("got vars %v, want %v", got, want)
	}
}

func TestProcessNodeWithRealData(t *testing.T) {
	nodeJSON := []byte(`{
		"id": "6970adabe552e8fe603d4805",
		"obj_id": "6970adabe552e8fe603d4805",
		"proc": "ok",
		"obj": "node",
		"title": "get corezoid process",
		"logics": [
			{
				"conv_id": 1797994,
				"type": "api_rpc",
				"extra": {
					"conv_id": "{{aiNodeProcessId}}",
					"corezoid_secret_login": "{{corezoid_secret_login}}",
					"corezoid_secret_key": "{{corezoid_secret_key}}"
				},
				"extra_type": {
					"conv_id": "number",
					"corezoid_secret_login": "string",
					"corezoid_secret_key": "string"
				}
			},
			{"type": "go", "to_node_id": "6970b4fbb677ac2b87403b10"}
		],
		"semaphors": [],
		"obj_type": 0
	}`)

	var node Node
	if err := json.Unmarshal(nodeJSON, &node); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if node.ID != "6970adabe552e8fe603d4805" {
		t.Errorf("node ID = %s, want 6970adabe552e8fe603d4805", node.ID)
	}
	if node.Title != "get corezoid process" {
		t.Errorf("node Title = %s, want 'get corezoid process'", node.Title)
	}

	props := map[string]interface{}{}
	requiredSet := map[string]bool{}
	for _, tr := range node.allTransitions() {
		collectVarsFromLogicRaw(tr.Raw, props, requiredSet)
	}

	want := []string{"aiNodeProcessId", "corezoid_secret_login", "corezoid_secret_key"}
	got := keysOf(requiredSet)
	if !stringsContainSame(got, want) {
		t.Errorf("got vars %v, want %v", got, want)
	}
}

func TestMissingNode(t *testing.T) {
	payload := map[string]interface{}{}

	if payload["node"] == nil {
		payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "node is required"}
	}

	errVal, ok := payload["error"].(ErrorInfo)
	if !ok {
		t.Fatal("expected ErrorInfo in payload")
	}
	if errVal.Code != "BAD_INPUT" {
		t.Errorf("error code = %s, want BAD_INPUT", errVal.Code)
	}
}

// keysOf returns sorted keys of a bool map
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// stringsContainSame compares two slices order-independently
func stringsContainSame(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := make(map[string]int)
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
		if count[s] < 0 {
			return false
		}
	}
	return true
}

func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

package main

import (
	"encoding/json"
	"testing"
)

// Test extracting variables from string
func TestExtractVariablesFromString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple variable",
			input:    "X {{a}} Y",
			expected: []string{"a"},
		},
		{
			name:     "multiple variables",
			input:    "X {{a}} Y {{ b.c }}",
			expected: []string{"a", "b.c"},
		},
		{
			name:     "with spaces",
			input:    "{{  foo  }} and {{ bar }}",
			expected: []string{"bar", "foo"},
		},
		{
			name:     "no variables",
			input:    "no variables here",
			expected: []string{},
		},
		{
			name:     "dot notation",
			input:    "{{ payload.user.id }}",
			expected: []string{"payload.user.id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractVariablesFromString(tt.input)
			if !stringsEqual(result, tt.expected) {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

// Test recursive variable extraction from nested objects
func TestExtractVariablesFromObject(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
	}{
		{
			name: "nested map",
			input: map[string]interface{}{
				"field1": "{{var1}}",
				"field2": map[string]interface{}{
					"nested": "{{var2}}",
				},
			},
			expected: []string{"var1", "var2"},
		},
		{
			name: "array in object",
			input: map[string]interface{}{
				"items": []interface{}{
					"{{item1}}",
					"{{item2}}",
				},
			},
			expected: []string{"item1", "item2"},
		},
		{
			name: "deeply nested",
			input: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"level3": "{{deep_var}}",
					},
				},
			},
			expected: []string{"deep_var"},
		},
		{
			name: "with deduplication",
			input: map[string]interface{}{
				"field1": "{{same}}",
				"field2": "{{same}}",
				"field3": "{{other}}",
			},
			expected: []string{"other", "same"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractVariablesFromObject(tt.input, false)
			if !stringsEqual(result, tt.expected) {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

// Test that URL variables are excluded when includeURLVars is false
func TestURLVariablesExcluded(t *testing.T) {
	input := map[string]interface{}{
		"url": "{{domain}}/api/{{endpoint}}",
		"body": map[string]interface{}{
			"param": "{{bodyVar}}",
		},
	}

	result := extractVariablesFromObject(input, true) // excludeURL=true
	expected := []string{"bodyVar"}

	if !stringsEqual(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

// Test that URL variables are included when includeURLVars is true
func TestURLVariablesIncluded(t *testing.T) {
	input := map[string]interface{}{
		"url": "{{domain}}/api/{{endpoint}}",
		"body": map[string]interface{}{
			"param": "{{bodyVar}}",
		},
	}

	result := extractVariablesFromObject(input, false) // excludeURL=false
	expected := []string{"bodyVar", "domain", "endpoint"}

	if !stringsEqual(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

// Test JSON schema building with variables
func TestBuildJSONSchemaWithVars(t *testing.T) {
	vars := []string{"var1", "var2"}
	schema := buildJSONSchema(vars)

	// Convert to JSON to check structure
	schemaJSON, _ := json.Marshal(schema)
	var schemaObj map[string]interface{}
	json.Unmarshal(schemaJSON, &schemaObj)

	// Check required fields
	required, ok := schemaObj["required"].([]interface{})
	if !ok {
		t.Fatalf("required is not a slice")
	}

	requiredStrs := make([]string, len(required))
	for i, r := range required {
		requiredStrs[i] = r.(string)
	}

	expected := []string{"result", "reason", "var1", "var2"}
	if !stringsEqual(requiredStrs, expected) {
		t.Errorf("got %v, want %v", requiredStrs, expected)
	}

	// Check properties
	props, ok := schemaObj["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties is not a map")
	}

	if _, ok := props["var1"]; !ok {
		t.Errorf("var1 not in properties")
	}
	if _, ok := props["var2"]; !ok {
		t.Errorf("var2 not in properties")
	}
}

// Test JSON schema building without variables
func TestBuildJSONSchemaWithoutVars(t *testing.T) {
	vars := []string{}
	schema := buildJSONSchema(vars)

	// Convert to JSON to check structure
	schemaJSON, _ := json.Marshal(schema)
	var schemaObj map[string]interface{}
	json.Unmarshal(schemaJSON, &schemaObj)

	// Check required fields - should only have base fields
	required, ok := schemaObj["required"].([]interface{})
	if !ok {
		t.Fatalf("required is not a slice")
	}

	requiredStrs := make([]string, len(required))
	for i, r := range required {
		requiredStrs[i] = r.(string)
	}

	expected := []string{"result", "reason"}
	if !stringsEqual(requiredStrs, expected) {
		t.Errorf("got %v, want %v", requiredStrs, expected)
	}
}

// Test finding AI node
func TestFindAINode(t *testing.T) {
	processJSON := []byte(`{
		"scheme": {
			"nodes": [
				{"id": "node1", "title": "Start"},
				{"id": "node2", "title": "AI Node"},
				{"id": "node3", "title": "End"}
			]
		}
	}`)

	node, err := findAINode(processJSON, "node2")
	if err != nil {
		t.Fatalf("error finding node: %v", err)
	}

	if node == nil {
		t.Fatalf("node is nil")
	}

	if node.Title != "AI Node" {
		t.Errorf("got title %s, want AI Node", node.Title)
	}
}

// Test finding AI node that doesn't exist
func TestFindAINodeNotFound(t *testing.T) {
	processJSON := []byte(`{
		"scheme": {
			"nodes": [
				{"id": "node1", "title": "Start"}
			]
		}
	}`)

	_, err := findAINode(processJSON, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// Test finding next node through go transition
func TestFindNextNode(t *testing.T) {
	processJSON := []byte(`{
		"scheme": {
			"nodes": [
				{
					"id": "ai_node",
					"title": "AI",
					"condition": {
						"logics": [
							{"type": "go", "to_node_id": "next_node"}
						]
					}
				},
				{"id": "next_node", "title": "API Node"}
			]
		}
	}`)

	aiNode := &Node{
		ID: "ai_node",
		Condition: &Condition{
			Logics: []Logic{
				{Type: "go", ToNodeID: "next_node"},
			},
		},
	}

	nextNode, err := findNextNode(aiNode, processJSON)
	if err != nil {
		t.Fatalf("error finding next node: %v", err)
	}

	if nextNode == nil {
		t.Fatalf("next node is nil")
	}

	if nextNode.Title != "API Node" {
		t.Errorf("got title %s, want API Node", nextNode.Title)
	}
}

// Test finding next node when it doesn't exist
func TestFindNextNodeNotFound(t *testing.T) {
	processJSON := []byte(`{
		"scheme": {
			"nodes": [
				{"id": "ai_node", "title": "AI"}
			]
		}
	}`)

	aiNode := &Node{
		ID: "ai_node",
		Condition: &Condition{
			Logics: []Logic{
				{Type: "go", ToNodeID: "nonexistent"},
			},
		},
	}

	nextNode, err := findNextNode(aiNode, processJSON)
	if err != nil {
		t.Fatalf("error finding next node: %v", err)
	}

	if nextNode != nil {
		t.Fatalf("expected nil, got %v", nextNode)
	}
}

// Test processing event with valid input
func TestProcessEventValid(t *testing.T) {
	processJSON := json.RawMessage(`{
		"scheme": {
			"nodes": [
				{
					"id": "ai_node",
					"condition": {
						"logics": [
							{"type": "go", "to_node_id": "api_node"}
						]
					}
				},
				{
					"id": "api_node",
					"title": "API Call",
					"condition": {
						"logics": [
							{
								"type": "api",
								"url": "https://example.com/api?token={{token}}",
								"body": {"param": "{{userId}}"}
							}
						]
					}
				}
			]
		}
	}`)

	input := InputData{
		AINodeID:       "ai_node",
		Process:        processJSON,
		IncludeURLVars: false,
	}

	output := processEvent(input)

	if output.Error != nil {
		t.Fatalf("got error: %v", output.Error)
	}

	if output.Meta == nil {
		t.Fatalf("meta is nil")
	}

	if output.Meta.NextNodeID != "api_node" {
		t.Errorf("got next_node_id %s, want api_node", output.Meta.NextNodeID)
	}

	if output.Meta.VarsCount != 1 {
		t.Errorf("got vars_count %d, want 1", output.Meta.VarsCount)
	}

	// Should only include userId (not token from URL)
	if !contains(output.Meta.Vars, "userId") {
		t.Errorf("userId not found in vars: %v", output.Meta.Vars)
	}
}

// Test processing event with missing aiNodeId
func TestProcessEventMissingAINodeId(t *testing.T) {
	input := InputData{
		AINodeID: "",
		Process:  json.RawMessage(`{"scheme":{"nodes":[]}}`),
	}

	output := processEvent(input)

	if output.Error == nil {
		t.Fatal("expected error, got nil")
	}

	if output.Error.Code != "BAD_INPUT" {
		t.Errorf("got code %s, want BAD_INPUT", output.Error.Code)
	}
}

// Test processing event with missing process
func TestProcessEventMissingProcess(t *testing.T) {
	input := InputData{
		AINodeID: "ai_node",
		Process:  nil,
	}

	output := processEvent(input)

	if output.Error == nil {
		t.Fatal("expected error, got nil")
	}

	if output.Error.Code != "BAD_INPUT" {
		t.Errorf("got code %s, want BAD_INPUT", output.Error.Code)
	}
}

// Test processing event with AI node not found
func TestProcessEventAINodeNotFound(t *testing.T) {
	input := InputData{
		AINodeID: "nonexistent",
		Process:  json.RawMessage(`{"scheme":{"nodes":[]}}`),
	}

	output := processEvent(input)

	if output.Error == nil {
		t.Fatal("expected error, got nil")
	}

	if output.Error.Code != "AI_NODE_NOT_FOUND" {
		t.Errorf("got code %s, want AI_NODE_NOT_FOUND", output.Error.Code)
	}
}

// Helper function to compare string slices
func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Both should be sorted
	aCopy := make([]string, len(a))
	bCopy := make([]string, len(b))
	copy(aCopy, a)
	copy(bCopy, b)

	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}

	return true
}

// Helper function to check if slice contains a string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"bytes"
)

// Input represents the Corezoid event JSON
type Input struct {
	Data InputData `json:"data"`
}

// InputData contains the data field from the event
type InputData struct {
	AINodeID         string          `json:"aiNodeId"`
	Process          json.RawMessage `json:"process"`
	IncludeURLVars   bool            `json:"includeUrlVars,omitempty"`
	Error            *ErrorInfo      `json:"error,omitempty"`
	Meta             *Meta           `json:"meta,omitempty"`
	Schema           json.RawMessage `json:"schema,omitempty"`
	AdditionalFields map[string]interface{}
}

// UnmarshalJSON allows InputData to preserve additional fields
func (d *InputData) UnmarshalJSON(data []byte) error {
	type Alias InputData
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(d),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Preserve additional fields
	d.AdditionalFields = make(map[string]interface{})
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	knownFields := map[string]bool{
		"aiNodeId":       true,
		"process":        true,
		"includeUrlVars": true,
		"error":          true,
		"meta":           true,
		"schema":         true,
	}

	for k, v := range raw {
		if !knownFields[k] {
			d.AdditionalFields[k] = v
		}
	}

	return nil
}

// MarshalJSON preserves additional fields when marshaling
func (d InputData) MarshalJSON() ([]byte, error) {
	type Alias InputData
	aux := struct {
		*Alias
	}{
		Alias: (*Alias)(&d),
	}

	data, err := json.Marshal(aux)
	if err != nil {
		return nil, err
	}

	// Merge additional fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	for k, v := range d.AdditionalFields {
		raw[k] = v
	}

	return json.Marshal(raw)
}

// ErrorInfo represents an error in the response
type ErrorInfo struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Meta contains metadata about the processing
type Meta struct {
	AINodeID        string   `json:"ai_node_id"`
	NextNodeID      string   `json:"next_node_id,omitempty"`
	NextNodeTitle   string   `json:"next_node_title,omitempty"`
	Vars            []string `json:"vars"`
	VarsCount       int      `json:"vars_count"`
	IncludeURLVars  bool     `json:"include_url_vars"`
}

// Output represents the response JSON
type Output struct {
	Data OutputData `json:"data"`
}

// OutputData is the data field in the response
type OutputData struct {
	AINodeID         string          `json:"aiNodeId"`
	Process          json.RawMessage `json:"process"`
	IncludeURLVars   bool            `json:"includeUrlVars,omitempty"`
	Error            *ErrorInfo      `json:"error,omitempty"`
	Meta             *Meta           `json:"meta,omitempty"`
	Schema           interface{}     `json:"schema,omitempty"`
	AdditionalFields map[string]interface{}
}

// MarshalJSON preserves additional fields when marshaling
func (d OutputData) MarshalJSON() ([]byte, error) {
	type Alias OutputData
	aux := struct {
		*Alias
	}{
		Alias: (*Alias)(&d),
	}

	data, err := json.Marshal(aux)
	if err != nil {
		return nil, err
	}

	// Merge additional fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	for k, v := range d.AdditionalFields {
		raw[k] = v
	}

	return json.Marshal(raw)
}

// ProcessData represents the Corezoid process JSON
type ProcessData struct {
	Scheme *Scheme `json:"scheme,omitempty"`
}

// Scheme contains the nodes
type Scheme struct {
	Nodes []Node `json:"nodes,omitempty"`
}

// Node represents a Corezoid node
type Node struct {
	ID        string      `json:"id"`
	Title     string      `json:"title,omitempty"`
	ObjType   int         `json:"obj_type,omitempty"`
	Condition *Condition  `json:"condition,omitempty"`
	Extra     interface{} `json:"extra,omitempty"`
	Options   interface{} `json:"options,omitempty"`
}

// Condition contains the logics array
type Condition struct {
	Logics    []Logic       `json:"logics,omitempty"`
	Semaphors []interface{} `json:"semaphors,omitempty"`
}

// Logic represents a logic item in the condition
type Logic struct {
	Type        string      `json:"type"`
	ToNodeID    string      `json:"to_node_id,omitempty"`
	URL         string      `json:"url,omitempty"`
	Method      string      `json:"method,omitempty"`
	Extra       interface{} `json:"extra,omitempty"`
	ExtraType   interface{} `json:"extra_type,omitempty"`
	ExtraHeader interface{} `json:"extra_headers,omitempty"`
	RawData     map[string]interface{}
}

// UnmarshalJSON allows Logic to preserve all fields
func (l *Logic) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	l.RawData = raw

	if v, ok := raw["type"]; ok {
		l.Type = v.(string)
	}
	if v, ok := raw["to_node_id"]; ok {
		l.ToNodeID = v.(string)
	}
	if v, ok := raw["url"]; ok {
		l.URL = v.(string)
	}
	if v, ok := raw["method"]; ok {
		l.Method = v.(string)
	}
	if v, ok := raw["extra"]; ok {
		l.Extra = v
	}
	if v, ok := raw["extra_type"]; ok {
		l.ExtraType = v
	}
	if v, ok := raw["extra_headers"]; ok {
		l.ExtraHeader = v
	}

	return nil
}

// JSONSchema represents the OpenAI response format JSON Schema
type JSONSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required"`
}

// extractVariablesFromString extracts {{ var }} placeholders from a string
func extractVariablesFromString(s string) []string {
	re := regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)
	matches := re.FindAllStringSubmatch(s, -1)
	vars := make([]string, 0)
	for _, match := range matches {
		if len(match) > 1 {
			vars = append(vars, match[1])
		}
	}
	return vars
}

// extractVariablesFromObject recursively extracts variables from a JSON object
func extractVariablesFromObject(obj interface{}, excludeURL bool) []string {
	varsMap := make(map[string]bool)

	var recursiveExtract func(interface{}, bool)
	recursiveExtract = func(obj interface{}, skipURL bool) {
		switch v := obj.(type) {
		case string:
			vars := extractVariablesFromString(v)
			for _, v := range vars {
				varsMap[v] = true
			}
		case map[string]interface{}:
			for key, val := range v {
				// Skip url field when excludeURL is true
				if skipURL && key == "url" {
					continue
				}
				recursiveExtract(val, skipURL)
			}
		case []interface{}:
			for _, item := range v {
				recursiveExtract(item, skipURL)
			}
		}
	}

	recursiveExtract(obj, excludeURL)

	vars := make([]string, 0, len(varsMap))
	for v := range varsMap {
		vars = append(vars, v)
	}
	sort.Strings(vars)

	return vars
}

// findAINode finds the AI node by ID in the process
func findAINode(process []byte, aiNodeID string) (*Node, error) {
	var proc ProcessData
	if err := json.Unmarshal(process, &proc); err != nil {
		return nil, fmt.Errorf("invalid process JSON: %w", err)
	}

	if proc.Scheme == nil || proc.Scheme.Nodes == nil {
		return nil, fmt.Errorf("process has no nodes")
	}

	for i := range proc.Scheme.Nodes {
		if proc.Scheme.Nodes[i].ID == aiNodeID {
			return &proc.Scheme.Nodes[i], nil
		}
	}

	return nil, fmt.Errorf("AI node not found: %s", aiNodeID)
}

// findNextNode finds the next node after the AI node through the first "go" transition
func findNextNode(aiNode *Node, process []byte) (*Node, error) {
	if aiNode.Condition == nil || len(aiNode.Condition.Logics) == 0 {
		return nil, nil
	}

	var nextNodeID string
	for _, logic := range aiNode.Condition.Logics {
		if logic.Type == "go" {
			nextNodeID = logic.ToNodeID
			break
		}
	}

	if nextNodeID == "" {
		return nil, nil
	}

	var proc ProcessData
	if err := json.Unmarshal(process, &proc); err != nil {
		return nil, fmt.Errorf("invalid process JSON: %w", err)
	}

	if proc.Scheme == nil || proc.Scheme.Nodes == nil {
		return nil, nil
	}

	for i := range proc.Scheme.Nodes {
		if proc.Scheme.Nodes[i].ID == nextNodeID {
			return &proc.Scheme.Nodes[i], nil
		}
	}

	return nil, nil
}

// buildJSONSchema builds the OpenAI response format JSON Schema
func buildJSONSchema(vars []string) interface{} {
	properties := map[string]interface{}{
		"result": map[string]string{
			"type":        "string",
			"description": "Результат/рішення агента",
		},
		"reason": map[string]string{
			"type":        "string",
			"description": "Коротке пояснення",
		},
	}

	required := []string{"result", "reason"}

	if len(vars) > 0 {
		for _, v := range vars {
			properties[v] = map[string]string{
				"type": "string",
			}
			required = append(required, v)
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}

	return schema
}

// processEvent processes the input event
func processEvent(inputData InputData) OutputData {
	output := OutputData{
		AINodeID:         inputData.AINodeID,
		Process:          inputData.Process,
		IncludeURLVars:   inputData.IncludeURLVars,
		AdditionalFields: inputData.AdditionalFields,
	}

	// Validate input
	if inputData.AINodeID == "" {
		output.Error = &ErrorInfo{
			Message: "aiNodeId is required",
			Code:    "BAD_INPUT",
		}
		return output
	}

	if len(inputData.Process) == 0 {
		output.Error = &ErrorInfo{
			Message: "process is required",
			Code:    "BAD_INPUT",
		}
		return output
	}

	// Find AI node
	aiNode, err := findAINode(inputData.Process, inputData.AINodeID)
	if err != nil {
		output.Error = &ErrorInfo{
			Message: err.Error(),
			Code:    "AI_NODE_NOT_FOUND",
		}
		return output
	}

	// Find next node
	nextNode, err := findNextNode(aiNode, inputData.Process)
	if err != nil {
		output.Error = &ErrorInfo{
			Message: err.Error(),
			Code:    "PROCESS_ERROR",
		}
		return output
	}

	// Extract variables
	vars := []string{}
	if nextNode != nil && nextNode.Condition != nil && len(nextNode.Condition.Logics) > 0 {
		// Find the first "api" logic in the next node
		for _, logic := range nextNode.Condition.Logics {
			if logic.Type == "api" {
				// Extract variables from the API logic
				vars = extractVariablesFromObject(logic.RawData, !inputData.IncludeURLVars)
				break
			}
		}
	}

	// Build metadata
	meta := &Meta{
		AINodeID:       inputData.AINodeID,
		Vars:           vars,
		VarsCount:      len(vars),
		IncludeURLVars: inputData.IncludeURLVars,
	}

	if nextNode != nil {
		meta.NextNodeID = nextNode.ID
		meta.NextNodeTitle = nextNode.Title
	}

	output.Meta = meta
	output.Schema = buildJSONSchema(vars)

	return output
}

func main() {
	// Read input from stdin
	inputBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	// Parse input
	// Trim check: empty stdin
trimmed := inputBytes
// простий trim без bytes.TrimSpace (щоб не тягнути імпорти) — ок, але краще bytes.TrimSpace:
trimmed = bytes.TrimSpace(trimmed)

if len(trimmed) == 0 {
	output := Output{
		Data: OutputData{
			Error: &ErrorInfo{
				Message: "Empty stdin: no JSON received by git call",
				Code:    "BAD_INPUT",
			},
			AdditionalFields: map[string]interface{}{
				"input_len": 0,
			},
		},
	}
	outputJSON, _ := json.Marshal(output)
	fmt.Println(string(outputJSON))
	return
}

// Parse input
var input Input
if err := json.Unmarshal(trimmed, &input); err != nil {
	preview := string(trimmed)
	if len(preview) > 300 {
		preview = preview[:300]
	}
	output := Output{
		Data: OutputData{
			Error: &ErrorInfo{
				Message: "Invalid input JSON: " + err.Error(),
				Code:    "BAD_INPUT",
			},
			AdditionalFields: map[string]interface{}{
				"input_len":     len(trimmed),
				"input_preview": preview,
			},
		},
	}
	outputJSON, _ := json.Marshal(output)
	fmt.Println(string(outputJSON))
	return
}


	// Process the event
	outputData := processEvent(input.Data)

	// Return output
	output := Output{Data: outputData}
	outputJSON, err := json.Marshal(output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling output: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(outputJSON))
}

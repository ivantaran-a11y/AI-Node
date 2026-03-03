package main

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/corezoid/gitcall-go-runner/gitcall"
)

type Node struct {
	ID        string     `json:"id"`
	ObjID     string     `json:"obj_id,omitempty"`
	Title     string     `json:"title,omitempty"`
	ObjType   int        `json:"obj_type,omitempty"`
	Logics    []Logic    `json:"logics,omitempty"`
	Semaphors []Logic    `json:"semaphors,omitempty"`
	Condition *Condition `json:"condition,omitempty"`
}

func (n Node) allTransitions() []Logic {
	out := []Logic{}
	if len(n.Logics) > 0 {
		out = append(out, n.Logics...)
	} else if n.Condition != nil {
		out = append(out, n.Condition.Logics...)
	}
	if len(n.Semaphors) > 0 {
		out = append(out, n.Semaphors...)
	} else if n.Condition != nil {
		out = append(out, n.Condition.Semaphors...)
	}
	return out
}

type Condition struct {
	Logics    []Logic `json:"logics,omitempty"`
	Semaphors []Logic `json:"semaphors,omitempty"`
}

type Logic struct {
	Type     string                 `json:"type"`
	ToNodeID string                 `json:"to_node_id,omitempty"`
	Raw      map[string]interface{} `json:"-"`
}

func (l *Logic) UnmarshalJSON(b []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	l.Raw = raw
	if v, ok := raw["type"].(string); ok {
		l.Type = v
	}
	if v, ok := raw["to_node_id"].(string); ok {
		l.ToNodeID = v
	}
	return nil
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Meta struct {
	NodeID    string   `json:"node_id"`
	Title     string   `json:"node_title,omitempty"`
	Vars      []string `json:"vars"`
	VarsCount int      `json:"vars_count"`
}

func main() {
	gitcall.Handle(func(_ context.Context, data map[string]interface{}) error {
		payload := data
		if d, ok := data["data"].(map[string]interface{}); ok {
			if _, hasNode := d["node"]; hasNode {
				payload = d
			}
		}

		nodeObj := payload["node"]
		if nodeObj == nil {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "node is required"}
			return nil
		}

		nodeBytes, err := json.Marshal(nodeObj)
		if err != nil {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "node must be valid JSON object"}
			return nil
		}

		var node Node
		if err := json.Unmarshal(nodeBytes, &node); err != nil {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "failed to parse node"}
			return nil
		}

		props := map[string]interface{}{}
		requiredSet := map[string]bool{}

		for _, tr := range node.allTransitions() {
			collectVarsFromLogicRaw(tr.Raw, props, requiredSet)
		}

		required := make([]string, 0, len(requiredSet))
		for v := range requiredSet {
			required = append(required, v)
		}
		sort.Strings(required)

		schema := map[string]interface{}{
			"$schema":              "https://json-schema.org/draft/2020-12/schema",
			"name":                 "structured_output",
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]interface{}{},
		}

		if len(required) > 0 {
			schema["properties"] = props
			schema["required"] = required
		}

		nodeID := node.ID
		if nodeID == "" {
			nodeID = node.ObjID
		}

		payload["meta"] = Meta{
			NodeID:    nodeID,
			Title:     node.Title,
			Vars:      required,
			VarsCount: len(required),
		}

		payload["structured_output_rsp"] = map[string]interface{}{
			"status": "ok",
			"schema": schema,
		}

		delete(payload, "schema")
		delete(payload, "error")

		return nil
	})
}

func collectVarsFromLogicRaw(raw map[string]interface{}, props map[string]interface{}, requiredSet map[string]bool) {
	if raw == nil {
		return
	}

	if urlVal, ok := raw["url"].(string); ok {
		for _, v := range extractVariablesFromString(urlVal) {
			addVar(props, requiredSet, v, jsonSchemaForType("string"))
		}
	}

	if hdrs, ok := raw["extra_headers"].(map[string]interface{}); ok {
		for _, val := range hdrs {
			if s, ok := val.(string); ok {
				for _, v := range extractVariablesFromString(s) {
					addVar(props, requiredSet, v, jsonSchemaForType("string"))
				}
			}
		}
	}

	extra, _ := raw["extra"].(map[string]interface{})
	extraType, _ := raw["extra_type"].(map[string]interface{})
	for key, val := range extra {
		s, ok := val.(string)
		if !ok {
			continue
		}
		t := ""
		if extraType != nil {
			if ts, ok := extraType[key].(string); ok {
				t = strings.ToLower(ts)
			}
		}
		for _, v := range extractVariablesFromString(s) {
			addVar(props, requiredSet, v, jsonSchemaForType(t))
		}
	}

	scanRawForVars(raw, props, requiredSet)
}

func scanRawForVars(v interface{}, props map[string]interface{}, requiredSet map[string]bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			if k == "response" || k == "response_type" {
				continue
			}
			if k == "extra" || k == "extra_type" || k == "extra_headers" {
				continue
			}
			scanRawForVars(vv, props, requiredSet)
		}
	case []interface{}:
		for _, it := range t {
			scanRawForVars(it, props, requiredSet)
		}
	case string:
		for _, vname := range extractVariablesFromString(t) {
			addVar(props, requiredSet, vname, jsonSchemaForType("string"))
		}
	}
}

func addVar(props map[string]interface{}, requiredSet map[string]bool, varName string, schema map[string]interface{}) {
	if varName == "" {
		return
	}
	if existing, ok := props[varName].(map[string]interface{}); ok {
		et, _ := existing["type"].(string)
		nt, _ := schema["type"].(string)
		if et == "string" && nt != "" && nt != "string" {
			props[varName] = schema
		}
	} else if _, ok := props[varName]; !ok {
		props[varName] = schema
	}
	requiredSet[varName] = true
}

var reVar = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)
var reIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func extractVariablesFromString(s string) []string {
	m := reVar.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(m))
	for _, mm := range m {
		if len(mm) < 2 {
			continue
		}
		expr := strings.TrimSpace(mm[1])

		if strings.HasPrefix(expr, "content.") {
			cand := strings.TrimSpace(strings.TrimPrefix(expr, "content."))
			if reIdent.MatchString(cand) {
				out = append(out, cand)
			}
			continue
		}

		if reIdent.MatchString(expr) {
			out = append(out, expr)
		}
	}
	return out
}

func jsonSchemaForType(t string) map[string]interface{} {
	switch t {
	case "string":
		return map[string]interface{}{"type": "string"}
	case "array":
		return map[string]interface{}{"type": "array", "items": map[string]interface{}{}}
	case "object":
		return map[string]interface{}{"type": "object", "additionalProperties": true}
	case "integer":
		return map[string]interface{}{"type": "integer"}
	case "number":
		return map[string]interface{}{"type": "number"}
	case "boolean":
		return map[string]interface{}{"type": "boolean"}
	default:
		return map[string]interface{}{"type": "string"}
	}
}

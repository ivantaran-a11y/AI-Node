package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/corezoid/gitcall-go-runner/gitcall"
)

type ProcessData struct {
	Scheme *Scheme `json:"scheme,omitempty"`
	Nodes  []Node  `json:"nodes,omitempty"`
}

type Scheme struct {
	Nodes []Node `json:"nodes,omitempty"`
}

type Node struct {
	ID        string     `json:"id"`
	Title     string     `json:"title,omitempty"`
	ObjType   int        `json:"obj_type,omitempty"`
	Logics    []Logic    `json:"logics,omitempty"`
	Condition *Condition `json:"condition,omitempty"`
}

type Condition struct {
	Logics []Logic `json:"logics,omitempty"`
}

func (n Node) allLogics() []Logic {
	if len(n.Logics) > 0 {
		return n.Logics
	}
	if n.Condition != nil && len(n.Condition.Logics) > 0 {
		return n.Condition.Logics
	}
	return nil
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
	AINodeID       string   `json:"ai_node_id"`
	NextNodeID     string   `json:"next_node_id,omitempty"`
	NextNodeTitle  string   `json:"next_node_title,omitempty"`
	Vars           []string `json:"vars"`
	VarsCount      int      `json:"vars_count"`
	IncludeURLVars bool     `json:"include_url_vars"` // лишаємо, але тут не використовується
}

func main() {
	gitcall.Handle(func(_ context.Context, data map[string]interface{}) error {
		// зчитуємо або з root, або з data.*
		payload := data
		if d, ok := data["data"].(map[string]interface{}); ok {
			if _, hasProc := d["process"]; hasProc {
				payload = d
			}
		}

		aiNodeID, _ := payload["aiNodeId"].(string)
		procObj := payload["process"]

		if aiNodeID == "" {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "aiNodeId is required"}
			return nil
		}
		if procObj == nil {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "process is required"}
			return nil
		}

		procBytes, err := json.Marshal(procObj)
		if err != nil {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "process must be valid JSON object"}
			return nil
		}

		aiNode, nextNode := findAiAndNext(procBytes, aiNodeID)
		if aiNode == nil {
			payload["error"] = ErrorInfo{Code: "AI_NODE_NOT_FOUND", Message: "AI node not found by id: " + aiNodeID}
			return nil
		}

		// дефолтна схема (порожні properties)
		schema := map[string]interface{}{
			"$schema":              "https://json-schema.org/draft/2020-12/schema",
			"name":                 "structured_output",
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]interface{}{},
		}
		vars := []string{}

		if nextNode != nil {
			for _, lg := range nextNode.allLogics() {
				if strings.ToLower(lg.Type) == "api" {
					s, req := buildStructuredOutputSchemaFromExtra(lg.Raw)
					schema = s
					vars = req
					break
				}
			}
		}

		payload["meta"] = Meta{
			AINodeID:       aiNodeID,
			NextNodeID:     safe(nextNode, func(n *Node) string { return n.ID }),
			NextNodeTitle:  safe(nextNode, func(n *Node) string { return n.Title }),
			Vars:           vars,
			VarsCount:      len(vars),
			IncludeURLVars: false,
		}

		payload["structured_output_rsp"] = map[string]interface{}{
			"status": "ok",
			"schema": schema,
		}

		// якщо раніше було поле schema — прибираємо, щоб не плутало
		delete(payload, "schema")
		delete(payload, "error")
		return nil
	})
}

func findAiAndNext(procBytes []byte, aiNodeID string) (*Node, *Node) {
	var p ProcessData
	if err := json.Unmarshal(procBytes, &p); err != nil {
		// fallback: process може бути масивом
		var arr []ProcessData
		if err2 := json.Unmarshal(procBytes, &arr); err2 == nil && len(arr) > 0 {
			p = arr[0]
		} else {
			return nil, nil
		}
	}

	nodes := p.Nodes
	if len(nodes) == 0 && p.Scheme != nil {
		nodes = p.Scheme.Nodes
	}
	if len(nodes) == 0 {
		return nil, nil
	}

	var ai *Node
	for i := range nodes {
		if nodes[i].ID == aiNodeID {
			ai = &nodes[i]
			break
		}
	}
	if ai == nil {
		return nil, nil
	}

	// next node по першому unconditional go
	var nextID string
	for _, lg := range ai.allLogics() {
		if lg.Type == "go" && lg.ToNodeID != "" {
			nextID = lg.ToNodeID
			break
		}
	}
	if nextID == "" {
		return ai, nil
	}

	for i := range nodes {
		if nodes[i].ID == nextID {
			return ai, &nodes[i]
		}
	}
	return ai, nil
}

func buildStructuredOutputSchemaFromExtra(raw map[string]interface{}) (map[string]interface{}, []string) {
	props := map[string]interface{}{}
	required := []string{}

	extra, _ := raw["extra"].(map[string]interface{})
	extraType, _ := raw["extra_type"].(map[string]interface{})

	for key := range extra {
		t := ""
		if extraType != nil {
			if ts, ok := extraType[key].(string); ok {
				t = strings.ToLower(ts)
			}
		}
		props[key] = jsonSchemaForType(t)
		required = append(required, key)
	}

	sort.Strings(required)

	schema := map[string]interface{}{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"name":                 "structured_output",
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema, required
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

func safe[T any](p *T, f func(*T) string) string {
	if p == nil {
		return ""
	}
	return f(p)
}

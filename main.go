package main

import (
	"context"
	"encoding/json"
	"regexp"
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
	ID      string  `json:"id"`
	Title   string  `json:"title,omitempty"`
	ObjType int     `json:"obj_type,omitempty"`
	Logics  []Logic `json:"logics,omitempty"`

	// інший можливий формат (якщо десь прилетить)
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
	IncludeURLVars bool     `json:"include_url_vars"`

	DebugFoundAPILogic  bool     `json:"debug_found_api_logic,omitempty"`
	DebugAPIRawHasExtra bool     `json:"debug_api_raw_has_extra,omitempty"`
	DebugAPIRawKeys     []string `json:"debug_api_raw_keys,omitempty"`
}


func main() {
	gitcall.Handle(func(_ context.Context, data map[string]interface{}) error {
		// Підтримка двох форматів: root.* або data.*
		payload := data
		if d, ok := data["data"].(map[string]interface{}); ok {
			// якщо користувач кладе все в data.*
			if _, hasProc := d["process"]; hasProc {
				payload = d
			}
		}

		aiNodeID, _ := payload["aiNodeId"].(string)
		includeURLVars, _ := payload["includeUrlVars"].(bool)

		procObj, ok := payload["process"]
		if !ok || procObj == nil {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "process is required"}
			return nil
		}
		if aiNodeID == "" {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "aiNodeId is required"}
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

		vars := []string{}
		if nextNode != nil {
			vars = extractVarsFromNextNode(nextNode, !includeURLVars)
		}

	payload["meta"] = Meta{
	AINodeID:       aiNodeID,
	NextNodeID:     safe(nextNode, func(n *Node) string { return n.ID }),
	NextNodeTitle:  safe(nextNode, func(n *Node) string { return n.Title }),
	Vars:           vars,
	VarsCount:      len(vars),
	IncludeURLVars: includeURLVars,

	DebugFoundAPILogic:  foundAPI,
	DebugAPIRawHasExtra: hasExtra,
	DebugAPIRawKeys:     rawKeys,
}

		payload["schema"] = buildSchema(vars)

		// якщо все ок — прибираємо попередні помилки (якщо були)
		delete(payload, "error")
		return nil
	})
}

func findAiAndNext(procBytes []byte, aiNodeID string) (*Node, *Node) {
	var p ProcessData
	if err := json.Unmarshal(procBytes, &p); err != nil {
		return nil, nil
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

func extractVarsFromNextNode(next *Node, excludeURL bool) (vars []string, foundAPI bool, hasExtra bool, rawKeys []string) {
	for _, lg := range next.allLogics() {
		if isAPILogic(lg.Type) {
			foundAPI = true

			// keys для дебагу
			for k := range lg.Raw {
				rawKeys = append(rawKeys, k)
			}
			sort.Strings(rawKeys)

			// чи є extra
			if _, ok := lg.Raw["extra"]; ok {
				hasExtra = true
			}

			vars = extractVariablesFromObject(lg.Raw, excludeURL)
			if vars == nil {
				vars = []string{}
			}
			return
		}
	}

	return []string{}, false, false, nil
}

func isAPILogic(t string) bool {
	t = strings.ToLower(t)
	return t == "api" || t == "api_call" || strings.Contains(t, "api")
}

func buildSchema(vars []string) map[string]interface{} {
	props := map[string]interface{}{
		"result": map[string]interface{}{"type": "string", "description": "Результат/рішення агента"},
		"reason": map[string]interface{}{"type": "string", "description": "Коротке пояснення"},
	}
	required := []string{"result", "reason"}

	for _, v := range vars {
		props[v] = map[string]interface{}{"type": "string"}
		required = append(required, v)
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func extractVariablesFromString(s string) []string {
	re := regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)
	m := re.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(m))
	for _, mm := range m {
		if len(mm) > 1 {
			out = append(out, mm[1])
		}
	}
	return out
}

func extractVariablesFromObject(obj interface{}, excludeURL bool) []string {
	set := map[string]bool{}

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case string:
			for _, name := range extractVariablesFromString(x) {
				set[name] = true
			}
		case map[string]interface{}:
			for k, vv := range x {
				if excludeURL && k == "url" {
					continue
				}
				walk(vv)
			}
		case []interface{}:
			for _, it := range x {
				walk(it)
			}
		}
	}

	walk(obj)

	var vars []string
	for k := range set {
		vars = append(vars, k)
	}
	sort.Strings(vars)
	return vars
}

func safe[T any](p *T, f func(*T) string) string {
	if p == nil {
		return ""
	}
	return f(p)
}

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
	ID        string     `json:"id"`
	Title     string     `json:"title,omitempty"`
	ObjType   int        `json:"obj_type,omitempty"`
	Logics    []Logic    `json:"logics,omitempty"`
	Condition *Condition `json:"condition,omitempty"`
}

type Condition struct {
	Logics    []Logic `json:"logics,omitempty"`
	Semaphors []Logic `json:"semaphors,omitempty"` // <-- додали
}

// Логіки з node.logics або node.condition.logics
func (n Node) allLogics() []Logic {
	if len(n.Logics) > 0 {
		return n.Logics
	}
	if n.Condition != nil && len(n.Condition.Logics) > 0 {
		return n.Condition.Logics
	}


	return nil
}

// Переходи для графа: logics + semaphors
func (n Node) allTransitions() []Logic {
	out := []Logic{}
	out = append(out, n.allLogics()...)
	if n.Condition != nil && len(n.Condition.Semaphors) > 0 {
		out = append(out, n.Condition.Semaphors...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

		nodes := parseNodes(procBytes)
		if len(nodes) == 0 {
			payload["error"] = ErrorInfo{Code: "BAD_INPUT", Message: "process.scheme.nodes (or nodes) is empty"}
			return nil
		}

		aiNode, startID := findAiNodeAndStart(nodes, aiNodeID)
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

		var nextNode *Node
		if startID != "" {
			props, required, next := buildSchemaFromReachable(nodes, startID)
			nextNode = next

			if len(required) > 0 {
				schema["properties"] = props
				schema["required"] = required
				vars = required
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

		delete(payload, "schema")
		delete(payload, "error")

			if c, ok := payload["content"].(map[string]interface{}); ok {
	if props, ok2 := c["properties"].(map[string]interface{}); ok2 {
		payload["content"] = props
	}
}
		return nil
	})
}

func parseNodes(procBytes []byte) []Node {
	var p ProcessData
	if err := json.Unmarshal(procBytes, &p); err != nil {
		// fallback: process може бути масивом
		var arr []ProcessData
		if err2 := json.Unmarshal(procBytes, &arr); err2 == nil && len(arr) > 0 {
			p = arr[0]
		} else {
			return nil
		}
	}

	nodes := p.Nodes
	if len(nodes) == 0 && p.Scheme != nil {
		nodes = p.Scheme.Nodes
	}
	return nodes
}

// Повертаємо AI node та startID = перший unconditional go з AI node
func findAiNodeAndStart(nodes []Node, aiNodeID string) (*Node, string) {
	var ai *Node
	for i := range nodes {
		if nodes[i].ID == aiNodeID {
			ai = &nodes[i]
			break
		}
	}
	if ai == nil {
		return nil, ""
	}

	var startID string
	for _, lg := range ai.allTransitions() {
		if lg.Type == "go" && lg.ToNodeID != "" {
			startID = lg.ToNodeID
			break
		}
	}
	return ai, startID
}

// === ГОЛОВНЕ: збір змінних з усіх досяжних вузлів ===

func buildSchemaFromReachable(nodes []Node, startID string) (map[string]interface{}, []string, *Node) {
	id2node := map[string]*Node{}
	for i := range nodes {
		id2node[nodes[i].ID] = &nodes[i]
	}

	visited := map[string]bool{}
	queue := []string{startID}

	// properties[var] = json-schema
	props := map[string]interface{}{}
	requiredSet := map[string]bool{}

	var firstNode *Node
	if n, ok := id2node[startID]; ok {
		firstNode = n
	}

	for len(queue) > 0 {
		curID := queue[0]
		queue = queue[1:]

		if visited[curID] {
			continue
		}
		visited[curID] = true

		n, ok := id2node[curID]
		if !ok || n == nil {
			continue
		}

		// збираємо vars з усіх transitions (logics+semaphors) цього node
		for _, tr := range n.allTransitions() {
			collectVarsFromLogicRaw(tr.Raw, props, requiredSet)

			// будуємо граф за будь-якими to_node_id (go / go_if_* / time / ...)
			if tr.ToNodeID != "" && !visited[tr.ToNodeID] {
				queue = append(queue, tr.ToNodeID)
			}
		}
	}

	required := make([]string, 0, len(requiredSet))
	for v := range requiredSet {
		required = append(required, v)
	}
	sort.Strings(required)

	return props, required, firstNode
}

func collectVarsFromLogicRaw(raw map[string]interface{}, props map[string]interface{}, requiredSet map[string]bool) {
	if raw == nil {
		return
	}

	// 1) url
	if urlVal, ok := raw["url"].(string); ok {
		for _, v := range extractVariablesFromString(urlVal) {
			addVar(props, requiredSet, v, jsonSchemaForType("string"))
		}
	}

	// 2) extra_headers (наприклад Authorization: Bearer {{token}})
	if hdrs, ok := raw["extra_headers"].(map[string]interface{}); ok {
		for _, val := range hdrs {
			if s, ok := val.(string); ok {
				for _, v := range extractVariablesFromString(s) {
					addVar(props, requiredSet, v, jsonSchemaForType("string"))
				}
			}
		}
	}

	// 3) extra з типізацією через extra_type
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
		propSchema := jsonSchemaForType(t)

		for _, v := range extractVariablesFromString(s) {
			addVar(props, requiredSet, v, propSchema)
		}
	}

	// 4) (опційно) conditions.param та інші місця, де можуть бути {{...}}
	// Сканимо raw рекурсивно, але пропускаємо response/response_type (це вихід, не вхід).
	scanRawForVars(raw, props, requiredSet)
}

func scanRawForVars(v interface{}, props map[string]interface{}, requiredSet map[string]bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			// пропускаємо вихідні мапінги
			if k == "response" || k == "response_type" {
				continue
			}
			// extra / extra_type / extra_headers вже оброблені типізовано, тут можна пропустити, щоб не затирати типи
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
	// merge: якщо вже є string, а новий більш конкретний — оновимо
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

// === ВАЖЛИВО: нормалізація змінних ===
// - {{content.actorId}} -> actorId
// - {{token}} -> token
// - env_var[...] / root.* / складні вирази -> ігноруємо
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

		// content.xxx -> xxx
		if strings.HasPrefix(expr, "content.") {
			cand := strings.TrimSpace(strings.TrimPrefix(expr, "content."))
			if reIdent.MatchString(cand) {
				out = append(out, cand)
			}
			continue
		}

		// простий ident -> беремо
		if reIdent.MatchString(expr) {
			out = append(out, expr)
			continue
		}

		// решту (env_var[@...], root.node_id, і т.п.) — не додаємо
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


func safe[T any](p *T, f func(*T) string) string {

	if p == nil {
		return ""
	}
	return f(p)
}

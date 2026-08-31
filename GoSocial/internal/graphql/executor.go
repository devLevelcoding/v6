package graphql

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Executor resolves a parsed Selection set against a backing HTTP
// service, including nested/related object resolution via further HTTP
// calls (not in-process struct lookups).
type Executor struct {
	baseURL string
	client  *http.Client
}

func NewExecutor(baseURL string) *Executor {
	return &Executor{baseURL: baseURL, client: &http.Client{}}
}

func (e *Executor) fetch(path string) (map[string]any, error) {
	resp, err := e.client.Get(e.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("backing request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backing service returned %d: %s", resp.StatusCode, string(body))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("bad backing response: %w", err)
	}
	return out, nil
}

// nestedRelations declares, per root object "type", which of its fields
// are related objects resolved via a further HTTP call (here, post->author).
var nestedRelations = map[string]map[string]func(parent map[string]any) (path string, ok bool){
	"post": {
		"author": func(parent map[string]any) (string, bool) {
			id, ok := parent["authorId"]
			if !ok {
				return "", false
			}
			return fmt.Sprintf("/users/%v", id), true
		},
	},
}

// Execute runs the full query.
func (e *Executor) Execute(query string) (map[string]any, error) {
	sels, err := Parse(query)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	result := map[string]any{}
	for _, sel := range sels {
		val, err := e.resolveRoot(sel)
		if err != nil {
			return nil, err
		}
		result[sel.Name] = val
	}
	return result, nil
}

func (e *Executor) resolveRoot(sel Selection) (map[string]any, error) {
	var path, typeName string
	switch sel.Name {
	case "user":
		path = "/users/" + sel.Args["id"]
		typeName = "user"
	case "post":
		path = "/posts/" + sel.Args["id"]
		typeName = "post"
	default:
		return nil, fmt.Errorf("unknown root field %q", sel.Name)
	}
	obj, err := e.fetch(path)
	if err != nil {
		return nil, err
	}
	return e.applySelection(typeName, obj, sel.Sub)
}

func (e *Executor) applySelection(typeName string, obj map[string]any, sub []Selection) (map[string]any, error) {
	if len(sub) == 0 {
		return obj, nil
	}
	out := map[string]any{}
	relations := nestedRelations[typeName]
	for _, s := range sub {
		if relations != nil {
			if pathFn, isRelation := relations[s.Name]; isRelation {
				path, ok := pathFn(obj)
				if !ok {
					out[s.Name] = nil
					continue
				}
				related, err := e.fetch(path)
				if err != nil {
					return nil, fmt.Errorf("resolving related field %q: %w", s.Name, err)
				}
				resolved, err := e.applySelection(s.Name, related, s.Sub)
				if err != nil {
					return nil, err
				}
				out[s.Name] = resolved
				continue
			}
		}
		val, ok := obj[s.Name]
		if !ok {
			return nil, fmt.Errorf("unknown field %q on %s", s.Name, typeName)
		}
		out[s.Name] = val
	}
	return out, nil
}

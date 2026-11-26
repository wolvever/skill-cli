package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
)

type operation struct {
	method, path string
	op           *openapi3.Operation
	params       openapi3.Parameters
}
type openAPIProvider struct {
	cfg         ProviderConfig
	ops         map[string]operation
	limit       int64
	definitions openapi3.Schemas
}

func newOpenAPI(ctx context.Context, c ProviderConfig, limit int64) (Provider, error) {
	var data []byte
	var err error
	if isHTTP(c.Spec) {
		if err = validateURL(c.Spec); err != nil {
			return nil, err
		}
		req, e := http.NewRequestWithContext(ctx, "GET", c.Spec, nil)
		if e != nil {
			return nil, e
		}
		// Spec acquisition is unauthenticated: never forward provider credentials to a spec host.
		h, _ := httpClient(ProviderConfig{}, limit)
		resp, e := h.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("spec fetch returned HTTP %d", resp.StatusCode)
		}
		data, err = readBounded(resp.Body, limit)
	} else {
		f, e := os.Open(c.Spec)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		data, err = readBounded(f, limit)
	}
	if err != nil {
		return nil, err
	}
	loader := openapi3.NewLoader()
	loader.Context = ctx
	// External refs are disabled to avoid hidden filesystem/network reads. Internal $refs work.
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, err
	}
	if doc.OpenAPI == "" || !strings.HasPrefix(doc.OpenAPI, "3.0") {
		return nil, errors.New("only OpenAPI 3.0 JSON/YAML is currently supported")
	}
	if c.URL == "" {
		if len(doc.Servers) == 0 {
			return nil, errors.New("set provider url or supply an OpenAPI servers entry")
		}
		u, err := url.Parse(doc.Servers[0].URL)
		if err != nil {
			return nil, err
		}
		if !u.IsAbs() {
			if !isHTTP(c.Spec) {
				return nil, errors.New("relative servers URL requires an explicit provider url")
			}
			base, _ := url.Parse(c.Spec)
			u = base.ResolveReference(u)
		}
		c.URL = u.String()
	}
	if err = validateURL(c.URL); err != nil {
		return nil, err
	}
	p := &openAPIProvider{cfg: c, ops: map[string]operation{}, limit: limit}
	if doc.Components != nil {
		p.definitions = doc.Components.Schemas
	}
	if doc.Paths == nil {
		return p, nil
	}
	for path, item := range doc.Paths.Map() {
		if len(item.Servers) > 0 {
			return nil, errors.New("path-level servers are unsupported; configure a separate provider")
		}
		for method, op := range item.Operations() {
			id := op.OperationID
			if id == "" {
				id = strings.ToLower(method) + ":" + path
			}
			if _, ok := p.ops[id]; ok {
				return nil, fmt.Errorf("duplicate operation ID: %s", id)
			}
			// Operation-level parameters override path-level parameters by (in,name).
			params := openapi3.Parameters{}
			for _, v := range item.Parameters {
				if v.Value == nil {
					return nil, errors.New("unresolved parameter")
				}
				overridden := false
				for _, o := range op.Parameters {
					if o.Value != nil && o.Value.In == v.Value.In && o.Value.Name == v.Value.Name {
						overridden = true
					}
				}
				if !overridden {
					params = append(params, v)
				}
			}
			params = append(params, op.Parameters...)
			names := map[string]bool{}
			for _, param := range params {
				if param.Value == nil {
					return nil, errors.New("unresolved parameter")
				}
				name := param.Value.Name
				if names[name] || (name == "body" && op.RequestBody != nil) {
					return nil, fmt.Errorf("ambiguous argument name %q in operation %s", name, id)
				}
				names[name] = true
			}
			p.ops[id] = operation{method, path, op, params}
		}
	}
	return p, nil
}
func (p *openAPIProvider) Close() error { return nil }
func schemaFor(o operation, definitions openapi3.Schemas) map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, param := range o.params {
		v := param.Value
		if v != nil {
			props[v.Name] = v.Schema
			if v.Required {
				required = append(required, v.Name)
			}
		}
	}
	if b := o.op.RequestBody; b != nil && b.Value != nil {
		if media := b.Value.Content.Get("application/json"); media != nil {
			props["body"] = media.Schema
			if b.Value.Required {
				required = append(required, "body")
			}
		}
	}
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	// Bundle only transitively referenced components so the described schema is
	// self-contained without loading unrelated API definitions into agent context.
	b, _ := json.Marshal(s)
	var root map[string]any
	_ = json.Unmarshal(b, &root)
	defs := map[string]any{}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok && strings.HasPrefix(ref, "#/components/schemas/") {
				tail := strings.TrimPrefix(ref, "#/components/schemas/")
				name := strings.SplitN(tail, "/", 2)[0]
				key := strings.ReplaceAll(strings.ReplaceAll(name, "~1", "/"), "~0", "~")
				v["$ref"] = "#/$defs/" + tail
				if _, seen := defs[key]; !seen {
					defs[key] = nil
					if schema, found := definitions[key]; found {
						b, _ := json.Marshal(schema)
						var definition any
						_ = json.Unmarshal(b, &definition)
						defs[key] = definition
						visit(definition)
					}
				}
			}
			for _, child := range v {
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(root)
	if len(defs) > 0 {
		root["$defs"] = defs
	}
	return root
}
func (p *openAPIProvider) Discover(context.Context) ([]Capability, error) {
	out := []Capability{}
	for id, o := range p.ops {
		description := o.op.Summary
		if description == "" {
			description = o.op.Description
		}
		out = append(out, Capability{ID: id, Description: description, Protocol: "openapi", Annotations: map[string]any{"method": o.method, "path": o.path, "readOnly": o.method == "GET" || o.method == "HEAD"}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (p *openAPIProvider) Describe(_ context.Context, id string) (Capability, error) {
	o, ok := p.ops[id]
	if !ok {
		return Capability{}, fmt.Errorf("unknown operation %q", id)
	}
	return Capability{ID: id, Description: o.op.Summary, Protocol: "openapi", InputSchema: schemaFor(o, p.definitions), Annotations: map[string]any{"method": o.method, "path": o.path}}, nil
}
func scalar(v any) (string, error) {
	switch v.(type) {
	case string, bool, float64, int, int64, json.Number:
		return fmt.Sprint(v), nil
	default:
		return "", errors.New("only scalar path/query/header parameters are currently supported")
	}
}
func (p *openAPIProvider) Invoke(ctx context.Context, id string, args map[string]any) (any, error) {
	o, ok := p.ops[id]
	if !ok {
		return nil, fmt.Errorf("unknown operation %q", id)
	}
	if o.op.Servers != nil && len(*o.op.Servers) > 0 {
		return nil, errors.New("operation-level servers are unsupported; configure a separate provider")
	}
	path := o.path
	query := url.Values{}
	known := map[string]bool{"body": true}
	// Per-call non-secret headers use the same request builder as configured headers.
	headers := map[string]string{}
	for _, param := range o.params {
		v := param.Value
		if v == nil {
			return nil, errors.New("unresolved parameter")
		}
		known[v.Name] = true
		value, exists := args[v.Name]
		if !exists {
			if v.Required {
				return nil, fmt.Errorf("missing required argument: %s", v.Name)
			}
			continue
		}
		if v.Schema != nil && v.Schema.Value != nil {
			if err := v.Schema.Value.VisitJSON(value); err != nil {
				return nil, fmt.Errorf("invalid argument %s: %w", v.Name, err)
			}
		}
		s, err := scalar(value)
		if err != nil {
			return nil, err
		}
		switch v.In {
		case "path":
			path = strings.ReplaceAll(path, "{"+v.Name+"}", url.PathEscape(s))
		case "query":
			query.Set(v.Name, s)
		case "header":
			if strings.EqualFold(v.Name, "Authorization") || strings.EqualFold(v.Name, "Host") {
				return nil, errors.New("set authentication through provider environment bindings")
			}
			headers[v.Name] = s
		default:
			return nil, fmt.Errorf("unsupported parameter location: %s", v.In)
		}
	}
	for key := range args {
		if !known[key] {
			return nil, fmt.Errorf("unknown argument %q; run describe", key)
		}
	}
	var body map[string]any
	if b := o.op.RequestBody; b != nil && b.Value != nil {
		value, exists := args["body"]
		if b.Value.Required && !exists {
			return nil, errors.New("missing required argument: body")
		}
		if exists {
			media := b.Value.Content.Get("application/json")
			if media == nil {
				return nil, errors.New("only application/json bodies are supported")
			}
			if media.Schema != nil && media.Schema.Value != nil {
				if err := media.Schema.Value.VisitJSON(value); err != nil {
					return nil, fmt.Errorf("invalid body: %w", err)
				}
			}
			var ok bool
			body, ok = value.(map[string]any)
			if !ok {
				return nil, errors.New("only JSON object request bodies are currently supported")
			}
		}
	} else if _, exists := args["body"]; exists {
		return nil, errors.New("operation has no request body")
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return requestWithHeaders(ctx, p.cfg, o.method, path, body, headers, p.limit)
}

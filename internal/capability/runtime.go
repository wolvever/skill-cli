package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Capability struct {
	ID           string `json:"id"`
	Description  string `json:"description,omitempty"`
	Protocol     string `json:"protocol"`
	InputSchema  any    `json:"inputSchema,omitempty"`
	OutputSchema any    `json:"outputSchema,omitempty"`
	Annotations  any    `json:"annotations,omitempty"`
}
type Provider interface {
	Discover(context.Context) ([]Capability, error)
	Invoke(context.Context, string, map[string]any) (any, error)
	Close() error
}
type Runtime struct {
	config    Config
	mu        sync.Mutex
	providers map[string]Provider
	maxBytes  int64
}

func New(c Config, maxBytes int64) *Runtime {
	return &Runtime{config: c, providers: map[string]Provider{}, maxBytes: maxBytes}
}
func (r *Runtime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.providers {
		_ = p.Close()
	}
}
func (r *Runtime) provider(ctx context.Context, name string) (Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.providers[name]; ok {
		return p, nil
	}
	c, ok := r.config.Providers[name]
	if !ok {
		return nil, NewFault("NOT_FOUND", fmt.Sprintf("unknown provider %q; run skill-cli list", name), "Run skill-cli list to discover configured providers.", nil)
	}
	var p Provider
	var err error
	switch c.Type {
	case "mcp":
		p, err = newMCP(ctx, c, r.maxBytes)
	case "openapi":
		p, err = newOpenAPI(ctx, c, r.maxBytes)
	case "http":
		p = &rawProvider{}
	default:
		err = errors.New("unsupported provider")
	}
	if err == nil {
		r.providers[name] = p
	} else if c.Type == "openapi" {
		var fault *Fault
		code, _ := ErrorDetails(err)
		if !errors.As(err, &fault) && code == "UPSTREAM_ERROR" {
			err = NewFault("CONFIG_ERROR", err.Error(), "Check the OpenAPI spec and its supported version and provider URL.", err)
		}
	}
	return p, err
}
func splitID(id string) (string, string, error) {
	a, b, ok := strings.Cut(id, ".")
	if !ok || a == "" || b == "" {
		return "", "", InvalidArgument("expected provider.operation; run skill-cli list <provider>")
	}
	return a, b, nil
}

type Request struct {
	Action    string              `json:"action"`
	Target    string              `json:"target,omitempty"`
	Query     string              `json:"query,omitempty"`
	Args      map[string]any      `json:"args,omitempty"`
	Method    string              `json:"method,omitempty"`
	Path      string              `json:"path,omitempty"`
	Limit     int                 `json:"limit,omitempty"`
	Match     string              `json:"match,omitempty"`
	Full      bool                `json:"full,omitempty"`
	HTTPQuery map[string][]string `json:"httpQuery,omitempty"`
	Headers   map[string]string   `json:"headers,omitempty"`
	Body      json.RawMessage     `json:"body,omitempty"`
}

func (r *Runtime) Execute(ctx context.Context, req Request) (any, error) {
	if req.Limit < 0 {
		return nil, InvalidArgument("limit must be nonnegative")
	}
	switch req.Action {
	case "list", "search":
		if req.Match != "" && req.Match != "ranked" && req.Match != "substring" {
			return nil, InvalidArgument("match must be ranked or substring")
		}
		if req.Action == "list" && req.Target == "" {
			names := []map[string]string{}
			keys := []string{}
			for k := range r.config.Providers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				cfg := r.config.Providers[k]
				item := map[string]string{"name": k, "type": cfg.Type}
				description := cfg.Description
				if description == "" && len(cfg.SkillDescriptions) > 0 {
					description = cfg.SkillDescriptions[0]
				}
				if description != "" {
					if !req.Full {
						description = compactDescription(description, 180)
					}
					item["description"] = description
				}
				names = append(names, item)
			}
			if req.Limit > 0 && len(names) > req.Limit {
				names = names[:req.Limit]
			}
			return names, nil
		}
		names := []string{}
		if req.Target != "" {
			names = append(names, req.Target)
		} else {
			for k := range r.config.Providers {
				names = append(names, k)
			}
			sort.Strings(names)
		}
		out := []Capability{}
		for _, name := range names {
			p, err := r.provider(ctx, name)
			if err != nil {
				return nil, err
			}
			caps, err := p.Discover(ctx)
			if err != nil {
				return nil, fmt.Errorf("%s discovery failed: %w", name, err)
			}
			for _, c := range caps {
				if !permitted(r.config.Providers[name], c.ID) {
					continue
				}
				c.ID = name + "." + c.ID
				out = append(out, c)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		if req.Action == "search" {
			if req.Match == "substring" {
				matches := []Capability{}
				for _, c := range out {
					if strings.Contains(strings.ToLower(c.ID+" "+c.Description), strings.ToLower(req.Query)) {
						matches = append(matches, c)
					}
				}
				out = matches
			} else {
				out = NewSearchIndex(out, r.config.Providers).Search(req.Query)
			}
		}
		if req.Limit > 0 && len(out) > req.Limit {
			out = out[:req.Limit]
		}
		for i := range out {
			out[i] = compactCapability(out[i], req.Full)
		}
		return out, nil
	case "describe", "call":
		name, op, err := splitID(req.Target)
		if err != nil {
			return nil, err
		}
		cfg, ok := r.config.Providers[name]
		if !ok {
			return nil, NewFault("NOT_FOUND", fmt.Sprintf("unknown provider %q", name), "Run skill-cli list to discover configured providers.", nil)
		}
		if !permitted(cfg, op) {
			return nil, NewFault("PERMISSION_DENIED", "operation denied by provider allowlist", "Use an allowed operation or have the operator update the provider policy.", nil)
		}
		p, err := r.provider(ctx, name)
		if err != nil {
			return nil, err
		}
		if req.Action == "call" {
			return p.Invoke(ctx, op, req.Args)
		}
		if d, ok := p.(interface {
			Describe(context.Context, string) (Capability, error)
		}); ok {
			c, err := d.Describe(ctx, op)
			c.ID = req.Target
			return c, err
		}
		caps, err := p.Discover(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range caps {
			if c.ID == op {
				c.ID = req.Target
				return c, nil
			}
		}
		return nil, NewFault("NOT_FOUND", fmt.Sprintf("unknown operation %q", op), "Run skill-cli list <provider> to discover exact operation IDs.", nil)
	case "request":
		cfg, ok := r.config.Providers[req.Target]
		if !ok || !(cfg.Type == "http" || cfg.Type == "openapi") {
			return nil, InvalidArgument("request requires an http or openapi provider")
		}
		if len(cfg.Allow) > 0 {
			return nil, NewFault("PERMISSION_DENIED", "raw requests disabled when provider has an operation allowlist", "Use call with an explicitly allowed operation.", nil)
		}
		if cfg.URL == "" {
			return nil, errors.New("raw requests require an explicit provider url")
		}
		if req.Body != nil && req.Args != nil {
			return nil, InvalidArgument("request body and args cannot be combined")
		}
		body := req.Body
		if req.Args != nil {
			var err error
			body, err = json.Marshal(req.Args)
			if err != nil {
				return nil, InvalidArgument("request args are not valid JSON")
			}
		}
		return requestWithOptions(ctx, cfg, req.Method, req.Path, body, req.Headers, req.HTTPQuery, r.maxBytes)
	default:
		return nil, InvalidArgument("unknown action")
	}
}

type rawProvider struct{}

func (*rawProvider) Discover(context.Context) ([]Capability, error) { return []Capability{}, nil }
func (*rawProvider) Invoke(context.Context, string, map[string]any) (any, error) {
	return nil, errors.New("use request for an HTTP provider")
}
func (*rawProvider) Close() error { return nil }

type Envelope struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

func Encode(data any, err error) []byte {
	e := Envelope{OK: err == nil, Data: data}
	if err != nil {
		e.Error = err.Error()
		e.Code, e.Hint = ErrorDetails(err)
	}
	b, encodeErr := json.Marshal(e)
	if encodeErr != nil {
		return []byte(`{"ok":false,"error":"result is not JSON serializable","code":"INTERNAL_ERROR","hint":"Report the failing operation and response type."}`)
	}
	return b
}

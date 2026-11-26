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
		return nil, fmt.Errorf("unknown provider %q; run skill-cli list", name)
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
	}
	return p, err
}
func splitID(id string) (string, string, error) {
	a, b, ok := strings.Cut(id, ".")
	if !ok || a == "" || b == "" {
		return "", "", errors.New("expected provider.operation; run skill-cli list <provider>")
	}
	return a, b, nil
}

type Request struct {
	Action string         `json:"action"`
	Target string         `json:"target,omitempty"`
	Query  string         `json:"query,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
	Method string         `json:"method,omitempty"`
	Path   string         `json:"path,omitempty"`
}

func (r *Runtime) Execute(ctx context.Context, req Request) (any, error) {
	switch req.Action {
	case "list", "search":
		if req.Action == "list" && req.Target == "" {
			names := []map[string]string{}
			keys := []string{}
			for k := range r.config.Providers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				names = append(names, map[string]string{"name": k, "type": r.config.Providers[k].Type})
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
				c.InputSchema = nil
				c.OutputSchema = nil
				if req.Query == "" || strings.Contains(strings.ToLower(c.ID+" "+c.Description), strings.ToLower(req.Query)) {
					out = append(out, c)
				}
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	case "describe", "call":
		name, op, err := splitID(req.Target)
		if err != nil {
			return nil, err
		}
		cfg, ok := r.config.Providers[name]
		if !ok {
			return nil, fmt.Errorf("unknown provider %q", name)
		}
		if !permitted(cfg, op) {
			return nil, errors.New("operation denied by provider allowlist")
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
		return nil, fmt.Errorf("unknown operation %q", op)
	case "request":
		cfg, ok := r.config.Providers[req.Target]
		if !ok || !(cfg.Type == "http" || cfg.Type == "openapi") {
			return nil, errors.New("request requires an http or openapi provider")
		}
		if len(cfg.Allow) > 0 {
			return nil, errors.New("raw requests disabled when provider has an operation allowlist")
		}
		if cfg.URL == "" {
			return nil, errors.New("raw requests require an explicit provider url")
		}
		return rawRequest(ctx, cfg, req.Method, req.Path, req.Args, r.maxBytes)
	default:
		return nil, errors.New("unknown action")
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
}

func Encode(data any, err error) []byte {
	e := Envelope{OK: err == nil, Data: data}
	if err != nil {
		e.Error = err.Error()
	}
	b, encodeErr := json.Marshal(e)
	if encodeErr != nil {
		return []byte(`{"ok":false,"error":"result is not JSON serializable"}`)
	}
	return b
}

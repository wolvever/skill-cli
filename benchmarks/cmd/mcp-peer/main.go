// mcp-peer is a JSONL benchmark driver using the official SDK directly for MCP.
// OpenAPI providers are exposed through a local in-memory MCP bridge sharing
// skill-cli's HTTP operation mapper, so both arms receive identical operations.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wolvever/skill-cli/internal/capability"
)

type request struct {
	Action   string         `json:"action"`
	Provider string         `json:"provider"`
	Tool     string         `json:"tool"`
	Query    string         `json:"query"`
	Match    string         `json:"match"`
	Limit    int            `json:"limit"`
	Args     map[string]any `json:"args"`
}
type driver struct {
	config   capability.Config
	sessions map[string]*mcp.ClientSession
	servers  []*mcp.ServerSession
	runtime  *capability.Runtime
}

func (d *driver) close() {
	for _, s := range d.sessions {
		_ = s.Close()
	}
	for _, s := range d.servers {
		_ = s.Close()
	}
	d.runtime.Close()
}
func allowed(c capability.ProviderConfig, name string) bool {
	if len(c.Allow) == 0 {
		return true
	}
	for _, n := range c.Allow {
		if n == name {
			return true
		}
	}
	return false
}
func (d *driver) session(ctx context.Context, name string) (*mcp.ClientSession, error) {
	if s := d.sessions[name]; s != nil {
		return s, nil
	}
	cfg, ok := d.config.Providers[name]
	if !ok {
		return nil, errors.New("unknown provider")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "skill-cli-native-benchmark", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}, MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
	var transport mcp.Transport
	if cfg.Type == "mcp" {
		// Public benchmark configurations deliberately contain no auth or stdio env.
		if cfg.BearerEnv != "" || len(cfg.HeadersEnv) > 0 || len(cfg.Env) > 0 {
			return nil, errors.New("native benchmark peer accepts public credential-free providers only")
		}
		switch cfg.Transport {
		case "http":
			transport = &mcp.StreamableClientTransport{Endpoint: cfg.URL, MaxRetries: -1}
		case "sse":
			transport = &mcp.SSEClientTransport{Endpoint: cfg.URL}
		case "stdio":
			if len(cfg.Command) == 0 {
				return nil, errors.New("missing command")
			}
			cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
			cmd.Stderr = os.Stderr
			transport = &mcp.CommandTransport{Command: cmd}
		default:
			return nil, errors.New("unsupported transport")
		}
	} else if cfg.Type == "openapi" {
		data, err := d.runtime.Execute(ctx, capability.Request{Action: "list", Target: name, Full: true})
		if err != nil {
			return nil, err
		}
		server := mcp.NewServer(&mcp.Implementation{Name: "openapi-benchmark-bridge", Version: "1"}, nil)
		for _, cap := range data.([]capability.Capability) {
			cap := cap
			_, op, _ := strings.Cut(cap.ID, ".")
			schema, err := d.runtime.Execute(ctx, capability.Request{Action: "describe", Target: cap.ID})
			if err != nil {
				return nil, err
			}
			desc := schema.(capability.Capability)
			server.AddTool(&mcp.Tool{Name: op, Description: cap.Description, InputSchema: desc.InputSchema}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				var args map[string]any
				dec := json.NewDecoder(strings.NewReader(string(r.Params.Arguments)))
				dec.UseNumber()
				if err := dec.Decode(&args); err != nil {
					return nil, err
				}
				result, err := d.runtime.Execute(ctx, capability.Request{Action: "call", Target: cap.ID, Args: args})
				if err != nil {
					return &mcp.CallToolResult{IsError: true, StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
				}
				b, err := json.Marshal(result)
				if err != nil {
					return nil, err
				}
				return &mcp.CallToolResult{StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
			})
		}
		st, ct := mcp.NewInMemoryTransports()
		ss, err := server.Connect(ctx, st, nil)
		if err != nil {
			return nil, err
		}
		d.servers = append(d.servers, ss)
		transport = ct
	} else {
		return nil, errors.New("native benchmark requires mcp or openapi provider")
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	d.sessions[name] = session
	return session, nil
}
func (d *driver) catalog(ctx context.Context, name string) ([]capability.Capability, error) {
	s, err := d.session(ctx, name)
	if err != nil {
		return nil, err
	}
	out := []capability.Capability{}
	for t, err := range s.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		if !allowed(d.config.Providers[name], t.Name) {
			continue
		}
		out = append(out, capability.Capability{ID: name + "." + t.Name, Description: t.Description, Protocol: "mcp", InputSchema: t.InputSchema, OutputSchema: t.OutputSchema, Annotations: t.Annotations})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (d *driver) run(ctx context.Context, r request) (any, error) {
	if r.Limit < 0 {
		return nil, errors.New("limit must be nonnegative")
	}
	switch r.Action {
	case "metadata":
		out := map[string]any{}
		for name, cfg := range d.config.Providers {
			out[name] = map[string]any{"description": cfg.Description, "keywords": cfg.Keywords, "skill_descriptions": cfg.SkillDescriptions}
		}
		return out, nil
	case "catalog", "list", "search", "describe":
		names := []string{}
		if r.Provider != "" {
			names = append(names, r.Provider)
		} else {
			for n := range d.config.Providers {
				names = append(names, n)
			}
			sort.Strings(names)
		}
		out := []capability.Capability{}
		for _, name := range names {
			caps, err := d.catalog(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			for _, cap := range caps {
				if r.Action == "describe" {
					if cap.ID == r.Tool {
						return cap, nil
					}
					continue
				}
				if r.Action == "search" && r.Match != "ranked" && !strings.Contains(strings.ToLower(cap.ID+" "+cap.Description), strings.ToLower(r.Query)) {
					continue
				}
				out = append(out, cap)
			}
		}
		if r.Action == "describe" {
			return nil, errors.New("unknown tool")
		}
		if r.Action == "search" && r.Match == "ranked" {
			out = capability.NewSearchIndex(out, d.config.Providers).Search(r.Query)
		}
		if r.Limit > 0 && len(out) > r.Limit {
			out = out[:r.Limit]
		}
		if r.Action == "list" || r.Action == "search" {
			for i := range out {
				out[i].InputSchema = nil
				out[i].OutputSchema = nil
			}
		}
		return out, nil
	case "call":
		name, op, ok := strings.Cut(r.Tool, ".")
		if !ok {
			return nil, errors.New("expected provider.tool")
		}
		cfg, ok := d.config.Providers[name]
		if !ok || !allowed(cfg, op) {
			return nil, errors.New("tool denied")
		}
		s, err := d.session(ctx, name)
		if err != nil {
			return nil, err
		}
		result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: op, Arguments: r.Args})
		if err != nil {
			return nil, err
		}
		if result.IsError {
			return result, errors.New("MCP tool execution error")
		}
		return result, nil
	default:
		return nil, errors.New("unknown action")
	}
}
func main() {
	path := flag.String("config", "benchmarks/public.json", "public provider config")
	deadline := flag.Duration("timeout", 30*time.Second, "request deadline")
	flag.Parse()
	cfg, err := capability.LoadConfig(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	d := &driver{config: cfg, sessions: map[string]*mcp.ClientSession{}, runtime: capability.New(cfg, 16<<20)}
	defer d.close()
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 16<<20)
	for scan.Scan() {
		var r request
		dec := json.NewDecoder(strings.NewReader(scan.Text()))
		dec.UseNumber()
		err := dec.Decode(&r)
		var data any
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), *deadline)
			data, err = d.run(ctx, r)
			cancel()
		}
		fmt.Println(string(capability.Encode(data, err)))
	}
	if err := scan.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

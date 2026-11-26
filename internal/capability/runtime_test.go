package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixtureMCP(counter *atomic.Int32) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	type input struct {
		Message string `json:"message"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "echo", Description: "Echo a message"}, func(ctx context.Context, r *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		counter.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Message}}}, nil, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "fail", Description: "Deliberate error"}, func(ctx context.Context, r *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		counter.Add(1)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "fixture failure"}}}, nil, nil
	})
	return s
}
func TestMCPStdioHelper(t *testing.T) {
	if os.Getenv("SKILL_CLI_TEST_HELPER") != "1" {
		return
	}
	var counter atomic.Int32
	err := fixtureMCP(&counter).Run(context.Background(), &mcp.StdioTransport{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
func TestMCPTransportsAndSessionReuse(t *testing.T) {
	for _, transport := range []string{"http", "sse", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var count atomic.Int32
			s := fixtureMCP(&count)
			cfg := ProviderConfig{Type: "mcp", Transport: transport}
			switch transport {
			case "http":
				h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
				srv := httptest.NewServer(h)
				defer srv.Close()
				cfg.URL = srv.URL
			case "sse":
				h := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return s }, nil)
				srv := httptest.NewServer(h)
				defer srv.Close()
				cfg.URL = srv.URL
			case "stdio":
				t.Setenv("SKILL_CLI_TEST_HELPER", "1")
				cfg.Command = []string{os.Args[0], "-test.run=^TestMCPStdioHelper$"}
			}
			r := New(Config{Providers: map[string]ProviderConfig{"fixture": cfg}}, 1<<20)
			defer r.Close()
			data, err := r.Execute(ctx, Request{Action: "list", Target: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			caps := data.([]Capability)
			if len(caps) != 2 || caps[0].InputSchema != nil {
				t.Fatalf("expected concise discovery: %#v", caps)
			}
			data, err = r.Execute(ctx, Request{Action: "describe", Target: "fixture.echo"})
			if err != nil {
				t.Fatal(err)
			}
			if data.(Capability).InputSchema == nil {
				t.Fatal("schema missing")
			}
			first, err := r.provider(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				data, err = r.Execute(ctx, Request{Action: "call", Target: "fixture.echo", Args: map[string]any{"message": "hello"}})
				if err != nil {
					t.Fatal(err)
				}
				if got := data.(*mcp.CallToolResult).Content[0].(*mcp.TextContent).Text; got != "hello" {
					t.Fatal(got)
				}
			}
			second, _ := r.provider(ctx, "fixture")
			if first != second {
				t.Fatal("provider session was not retained")
			}
			data, err = r.Execute(ctx, Request{Action: "call", Target: "fixture.fail"})
			if err == nil || !data.(*mcp.CallToolResult).IsError {
				t.Fatal("tool error must retain result and fail")
			}
			if transport != "stdio" && count.Load() != 3 {
				t.Fatalf("unexpected execution or retry: %d", count.Load())
			}
		})
	}
}
func TestAllowlistBlocksBeforeConnection(t *testing.T) {
	r := New(Config{Providers: map[string]ProviderConfig{"locked": {Type: "mcp", Transport: "stdio", Command: []string{"nonexistent"}, Allow: []string{"read"}}}}, 1000)
	defer r.Close()
	_, err := r.Execute(context.Background(), Request{Action: "call", Target: "locked.write"})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatal(err)
	}
	_, err = r.Execute(context.Background(), Request{Action: "request", Target: "locked", Method: "POST", Path: "/write"})
	if err == nil {
		t.Fatal("raw request bypass")
	}
}
func TestHTTPBoundaries(t *testing.T) {
	t.Setenv("SKILL_CLI_TEST_TOKEN", "test-secret")
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, target.URL, 302)
			return
		}
		if r.URL.Path == "/big" {
			_, _ = w.Write([]byte(strings.Repeat("x", 2000)))
			return
		}
		if r.URL.Path == "/error" {
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"conflict":true}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing credential binding")
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	cfg := ProviderConfig{Type: "http", URL: srv.URL, BearerEnv: "SKILL_CLI_TEST_TOKEN"}
	for _, path := range []string{"//evil.test/path", "https://evil.test/", "/../outside", "/%2e%2e/outside", "/redirect", "/big"} {
		if _, err := rawRequest(context.Background(), cfg, "GET", path, nil, 100); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
	if reached.Load() != 0 {
		t.Fatal("redirect forwarded credentials")
	}
	result, err := rawRequest(context.Background(), cfg, "GET", "/ok", nil, 100)
	if err != nil || result.(HTTPResult).Status != 200 {
		t.Fatalf("%v %v", result, err)
	}
	result, err = rawRequest(context.Background(), cfg, "POST", "/error", nil, 100)
	if err == nil || result.(HTTPResult).Status != 409 {
		t.Fatal("HTTP error body lost")
	}
	client, _ := httpClient(cfg, 100)
	req, _ := http.NewRequest("GET", target.URL, nil)
	if _, err = client.Do(req); err == nil {
		t.Fatal("cross-origin auth forwarding allowed")
	}
	if reached.Load() != 0 {
		t.Fatal("unexpected target request")
	}
}
func writeSpec(t *testing.T, base string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.json")
	spec := fmt.Sprintf(`{"openapi":"3.0.3","info":{"title":"fixture","version":"1"},"servers":[{"url":%q}],"paths":{"/pets/{id}":{"get":{"operationId":"getPet","parameters":[{"in":"path","name":"id","required":true,"schema":{"type":"string"}},{"in":"query","name":"verbose","schema":{"type":"boolean"}},{"in":"header","name":"X-Mode","schema":{"type":"string"}}],"responses":{"200":{"description":"OK"}}}},"/pets":{"post":{"operationId":"createPet","requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}},"responses":{"200":{"description":"OK"}}}}},"components":{"schemas":{"Pet":{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}}}}`, base)
	if err := os.WriteFile(path, []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestOpenAPIDiscoveryValidationAndEncoding(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method == "GET" {
			if r.URL.Query().Get("verbose") != "true" || r.Header.Get("X-Mode") != "safe" || !strings.Contains(r.RequestURI, "space%20") || !strings.Contains(r.RequestURI, "%2F") {
				t.Errorf("incorrect request: %s %v", r.RequestURI, r.Header)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Mochi"}`))
	}))
	defer srv.Close()
	cfg := ProviderConfig{Type: "openapi", Spec: writeSpec(t, srv.URL)}
	r := New(Config{Providers: map[string]ProviderConfig{"pets": cfg}}, 1<<20)
	defer r.Close()
	data, err := r.Execute(context.Background(), Request{Action: "describe", Target: "pets.createPet"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(data)
	if !strings.Contains(string(encoded), "name") {
		t.Fatal("internal reference not resolved")
	}
	for _, args := range []map[string]any{{}, {"id": 42}, {"id": "a", "verbose": "true"}, {"id": "a", "typo": 1}} {
		if _, err = r.Execute(context.Background(), Request{Action: "call", Target: "pets.getPet", Args: args}); err == nil {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input reached API")
	}
	_, err = r.Execute(context.Background(), Request{Action: "call", Target: "pets.getPet", Args: map[string]any{"id": "space /&?", "verbose": true, "X-Mode": "safe"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Execute(context.Background(), Request{Action: "call", Target: "pets.createPet", Args: map[string]any{"body": map[string]any{"name": "Mochi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	_, err = r.Execute(context.Background(), Request{Action: "call", Target: "pets.createPet", Args: map[string]any{"body": map[string]any{}}})
	if err == nil {
		t.Fatal("invalid body accepted")
	}
}
func TestRequestDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := rawRequest(ctx, ProviderConfig{URL: srv.URL}, "GET", "/", nil, 100)
	if err == nil {
		t.Fatal("request ignored deadline")
	}
}

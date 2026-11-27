package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Port the Skillhub fixture's observable issue-search contract to Go so the
// comparison runs without its Windows Python virtualenv or hard-coded port.
func TestSkillhubIssueSearchMCPAndOpenAPIParity(t *testing.T) {
	search := func(query string, limit int) map[string]any {
		return map[string]any{"query": query, "limit": limit, "issues": []map[string]any{{"id": 42, "title": "Fix login"}}}
	}
	type input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "issue-fixture", Version: "1"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "search_issues", Description: "Search demo issues by query"}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		return nil, search(in.Query, in.Limit), nil
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	mux.HandleFunc("GET /issues", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil {
			n = 3
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(search(r.URL.Query().Get("query"), n))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := New(Config{Providers: map[string]ProviderConfig{"mcp": {Type: "mcp", Transport: "http", URL: srv.URL + "/mcp"}, "api": {Type: "openapi", Spec: "testdata/skillhub-openapi.json", URL: srv.URL}}}, 1<<20)
	defer r.Close()
	for _, query := range []string{"login", "登录 \"C:\\work with spaces\"", "--limit &?=quoted"} {
		args := map[string]any{"query": query, "limit": json.Number("2")}
		m, err := r.Execute(context.Background(), Request{Action: "call", Target: "mcp.search_issues", Args: args})
		if err != nil {
			t.Fatal(err)
		}
		a, err := r.Execute(context.Background(), Request{Action: "call", Target: "api.searchIssues", Args: args})
		if err != nil {
			t.Fatal(err)
		}
		left, _ := json.Marshal(m.(*mcp.CallToolResult).StructuredContent)
		right, _ := json.Marshal(a.(HTTPResult).Body)
		if string(left) != string(right) {
			t.Fatalf("different protocol results: %s / %s", left, right)
		}
	}
}

type catalogFixture struct{ last string }

func (*catalogFixture) Discover(context.Context) ([]Capability, error) {
	return []Capability{{ID: "zeta", Description: "Search last"}, {ID: "search.issues-v2", Description: "Search issues"}, {ID: "searchIssues", Description: "Search issues"}, {ID: "search_issues", Description: "Search issues"}}, nil
}
func (p *catalogFixture) Invoke(_ context.Context, name string, _ map[string]any) (any, error) {
	p.last = name
	return name, nil
}
func (*catalogFixture) Close() error { return nil }
func TestSearchLimitAndLiteralOperationNames(t *testing.T) {
	r := New(Config{Providers: map[string]ProviderConfig{"fixture": {Type: "http"}}}, 1000)
	p := &catalogFixture{}
	r.providers["fixture"] = p
	data, err := r.Execute(context.Background(), Request{Action: "search", Query: "SEARCH", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	caps := data.([]Capability)
	if len(caps) != 2 || caps[0].ID != "fixture.search.issues-v2" || caps[1].ID != "fixture.searchIssues" {
		t.Fatal(caps)
	}
	if _, err = r.Execute(context.Background(), Request{Action: "search", Limit: -1}); err == nil {
		t.Fatal("negative limit accepted")
	}
	for _, op := range []string{"search.issues-v2", "searchIssues", "search_issues"} {
		if _, err = r.Execute(context.Background(), Request{Action: "call", Target: "fixture." + op}); err != nil || p.last != op {
			t.Fatalf("operation normalized or split: %s %v", p.last, err)
		}
	}
}
func TestHTTPQueryHeaderAndJSONBody(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !reflect.DeepEqual(r.URL.Query()["tag"], []string{"one", "a b", "x&y"}) {
			t.Error(r.URL.RawQuery)
		}
		if r.Header.Get("X-Mode") != "safe" {
			t.Error("header missing")
		}
		if string(body) != `[9007199254740993,true,null,{"name":"登录"}]` {
			t.Errorf("body changed: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	r := New(Config{Providers: map[string]ProviderConfig{"api": {Type: "http", URL: srv.URL}}}, 1<<20)
	defer r.Close()
	req := Request{Action: "request", Target: "api", Method: "POST", Path: "/echo?tag=one", HTTPQuery: map[string][]string{"tag": {"a b", "x&y"}}, Headers: map[string]string{"X-Mode": "safe"}, Body: json.RawMessage(`[9007199254740993,true,null,{"name":"登录"}]`)}
	if _, err := r.Execute(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"Authorization", "Cookie", "Host", "Content-Length", "Transfer-Encoding", "Connection", "bad header"} {
		bad := req
		bad.Headers = map[string]string{header: "unsafe"}
		if _, err := r.Execute(context.Background(), bad); err == nil {
			t.Fatal("accepted header", header)
		}
	}
	bad := req
	bad.Args = map[string]any{}
	if _, err := r.Execute(context.Background(), bad); err == nil {
		t.Fatal("two body sources accepted")
	}
	if calls.Load() != 1 {
		t.Fatal("invalid input reached upstream", calls.Load())
	}
}
func TestOpenAPIJSONBodiesOfAnyType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "api.json")
	spec := `{"openapi":"3.0.3","info":{"title":"bodies","version":"1"},"paths":{"/echo":{"post":{"operationId":"echo","requestBody":{"required":true,"content":{"application/json":{"schema":{"nullable":true}}}},"responses":{"200":{"description":"echo"}}}}}}`
	if err := os.WriteFile(path, []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	r := New(Config{Providers: map[string]ProviderConfig{"api": {Type: "openapi", Spec: path, URL: srv.URL}}}, 1<<20)
	defer r.Close()
	for _, value := range []any{nil, true, "hello", json.Number("9007199254740993"), []any{true, nil}, map[string]any{"nested": []any{false, nil}}} {
		got, err := r.Execute(context.Background(), Request{Action: "call", Target: "api.echo", Args: map[string]any{"body": value}})
		if err != nil {
			t.Fatalf("%v: %v", value, err)
		}
		want, _ := json.Marshal(value)
		b, _ := json.Marshal(got.(HTTPResult).Body)
		if string(want) != string(b) {
			t.Fatalf("body changed: %s / %s", want, b)
		}
	}
}
func TestErrorCodesAndStrictConfiguration(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{context.DeadlineExceeded, "TIMEOUT"}, {context.Canceled, "CANCELLED"}, {InvalidArgument("bad input"), "INVALID_ARGUMENT"}, {NewFault("TOOL_ERROR", "bad tool", "inspect details", nil), "TOOL_ERROR"}} {
		var e Envelope
		if err := json.Unmarshal(Encode(map[string]any{"detail": true}, fmt.Errorf("wrapped: %w", tc.err)), &e); err != nil {
			t.Fatal(err)
		}
		if e.OK || e.Code != tc.code || e.Hint == "" || e.Data == nil {
			t.Fatal(e)
		}
	}
	for _, data := range []string{`null`, `{"providers":{}} {}`, `{"providers":{"p":{"type":"http","url":"https://example.com","allowlist":["read"]}}}`, `{"providers":{"a.b":{"type":"http","url":"https://example.com"}}}`} {
		path := filepath.Join(t.TempDir(), "config.json")
		_ = os.WriteFile(path, []byte(data), 0600)
		_, err := LoadConfig(path)
		if err == nil {
			t.Fatal("invalid config accepted")
		}
		if code, _ := ErrorDetails(err); code != "CONFIG_ERROR" {
			t.Fatal(code, err)
		}
	}
}
func TestMutationFailureIsNotReplayed(t *testing.T) {
	var count atomic.Int32
	s := mcp.NewServer(&mcp.Implementation{Name: "counter", Version: "1"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "create"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		count.Add(1)
		return nil, nil, errors.New("failure after side effect")
	})
	srv := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	defer srv.Close()
	r := New(Config{Providers: map[string]ProviderConfig{"counter": {Type: "mcp", Transport: "http", URL: srv.URL}}}, 1<<20)
	defer r.Close()
	_, err := r.Execute(context.Background(), Request{Action: "call", Target: "counter.create"})
	code, _ := ErrorDetails(err)
	if count.Load() != 1 || code != "TOOL_ERROR" {
		t.Fatalf("calls=%d code=%s", count.Load(), code)
	}
}
func TestHTTPFailureCodes(t *testing.T) {
	for status, code := range map[int]string{401: "AUTH_FAILED", 403: "PERMISSION_DENIED", 404: "NOT_FOUND", 429: "RATE_LIMITED", 500: "UPSTREAM_ERROR"} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"detail":"upstream"}`))
			}))
			defer srv.Close()
			data, err := rawRequest(context.Background(), ProviderConfig{URL: srv.URL}, "GET", "/", nil, 1000)
			got, _ := ErrorDetails(err)
			if got != code || data.(HTTPResult).Status != status {
				t.Fatal(got, data)
			}
		})
	}
	if err := validateRequestHeaders(map[string]string{"X-Mode": "a\nb"}); err == nil || !strings.Contains(err.Error(), "control") {
		t.Fatal(err)
	}
}

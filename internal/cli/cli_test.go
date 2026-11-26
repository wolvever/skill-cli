package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wolvever/skill-cli/internal/capability"
)

func TestJSONInput(t *testing.T) {
	for _, s := range []string{"null", "[]", "true", `{"a":1} {"b":2}`, "{oops"} {
		if _, err := readArgs(s, strings.NewReader(""), 1024); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
	if _, err := readArgs(`{"long":"123456789"}`, nil, 5); err == nil {
		t.Fatal("unbounded args")
	}
	got, err := readArgs("-", strings.NewReader(`{"a":[1,true,null]}`), 1000)
	if err != nil || len(got["a"].([]any)) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	path := filepath.Join(t.TempDir(), "args.json")
	_ = os.WriteFile(path, []byte(`{"message":"hello"}`), 0600)
	if _, err = readArgs("@"+path, nil, 1000); err != nil {
		t.Fatal(err)
	}
}
func TestGatewayAuthLimitsAllowlistAndRetainedSession(t *testing.T) {
	var count atomic.Int32
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway-fixture", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "count", Description: "Count calls"}, func(ctx context.Context, r *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		n := count.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "count"}}}, map[string]any{"count": n}, nil
	})
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer upstream.Close()
	runtime := capability.New(capability.Config{Providers: map[string]capability.ProviderConfig{"test": {Type: "mcp", Transport: "http", URL: upstream.URL, Allow: []string{"count"}}}}, 1<<20)
	defer runtime.Close()
	opts := &options{timeout: 5 * time.Second, maxBytes: 1024, tokenEnv: "SKILL_CLI_TEST_GATEWAY_TOKEN"}
	token := strings.Repeat("a", 32)
	t.Setenv(opts.tokenEnv, token)
	gateway := httptest.NewUnstartedServer(nil)
	host := gateway.Listener.Addr().String()
	gateway.Config.Handler = gatewayHandler(runtime, opts, token, host)
	gateway.Start()
	defer gateway.Close()
	opts.gateway = gateway.URL
	post := func(body, bearer, origin, requestHost string) int {
		t.Helper()
		req, _ := http.NewRequest("POST", gateway.URL+"/v1/invoke", strings.NewReader(body))
		req.Header.Set("Authorization", bearer)
		req.Header.Set("Origin", origin)
		if requestHost != "" {
			req.Host = requestHost
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	for _, tc := range []struct {
		body, bearer, origin, host string
		status                     int
	}{
		{`{"action":"list"}`, "", "", "", 401},
		{`{"action":"list"}`, "Bearer " + token, "https://evil.test", "", 403},
		{`{"action":"list"}`, "Bearer " + token, "", "evil.test", 403},
		{`{"action":"call","target":"test.denied"}`, "Bearer " + token, "", "", 422},
		{`{"action":"list","extra":1}`, "Bearer " + token, "", "", 400},
		{`{"action":"list"} {}`, "Bearer " + token, "", "", 400},
		{strings.Repeat("x", 1025), "Bearer " + token, "", "", 400},
	} {
		if got := post(tc.body, tc.bearer, tc.origin, tc.host); got != tc.status {
			t.Fatalf("status %d want %d", got, tc.status)
		}
	}
	if count.Load() != 0 {
		t.Fatal("denied request invoked upstream")
	}
	for i := int32(1); i <= 2; i++ {
		data, err := remote(context.Background(), opts, capability.Request{Action: "call", Target: "test.count"})
		if err != nil {
			t.Fatal(err)
		}
		r := data.(map[string]any)
		got := r["structuredContent"].(map[string]any)["count"].(json.Number)
		if n, _ := got.Int64(); n != int64(i) {
			t.Fatal(got)
		}
	}
	if count.Load() != 2 {
		t.Fatal("unexpected replay")
	}
	// A thin CLI call succeeds with no local provider config and prints one envelope.
	path := filepath.Join(t.TempDir(), "missing.json")
	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--config", path, "--gateway", gateway.URL, "--gateway-token-env", opts.tokenEnv, "call", "test.count"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var e capability.Envelope
	if err := json.Unmarshal(out.Bytes(), &e); err != nil || !e.OK {
		t.Fatalf("bad envelope: %s %v", out.String(), err)
	}
}
func TestCLIErrorPreservesEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	_ = os.WriteFile(path, []byte(`{"providers":{}}`), 0600)
	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--config", path, "call", "unknown.operation"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("missing failure")
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatal("expected one JSON line")
	}
	var e capability.Envelope
	if json.Unmarshal(out.Bytes(), &e) != nil || e.OK || e.Error == "" {
		t.Fatal(out.String())
	}
}
func TestGatewayURLPolicy(t *testing.T) {
	for _, s := range []string{"http://remote.test", "http://localhost:7331", "https://user:pass@example.test", "https://example.test/?token=x"} {
		if gatewayURL(s) == nil {
			t.Errorf("accepted %s", s)
		}
	}
	for _, s := range []string{"http://127.0.0.1:7331", "https://gateway.example.test"} {
		if err := gatewayURL(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := url.Parse("http://127.0.0.1"); err != nil {
		t.Fatal(err)
	}
}

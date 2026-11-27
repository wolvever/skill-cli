package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wolvever/skill-cli/internal/capability"
)

func TestCLIHTTPOptionsAndIsolatedHome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"bodyText": string(body), "tag": r.URL.Query()["tag"], "mode": r.Header.Get("X-Mode")})
	}))
	defer srv.Close()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]any{"providers": map[string]any{"api": map[string]any{"type": "http", "url": srv.URL}}})
	_ = os.WriteFile(cfg, data, 0600)
	// A home/cache path that is a file catches the kind of unconditional usage
	// cache write that caused Skillhub's mcp2cli smoke run to exit after success.
	home := filepath.Join(dir, "not-a-directory")
	_ = os.WriteFile(home, []byte("unchanged"), 0600)
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME"} {
		t.Setenv(key, home)
	}
	for _, body := range []string{`null`, `[9007199254740993,true,null]`, `{"text":"登录 C:\\work with spaces"}`} {
		cmd := NewCommand()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(io.Discard)
		cmd.SetIn(strings.NewReader(body))
		cmd.SetArgs([]string{"--config", cfg, "request", "api", "POST", "/echo", "--body", "-", "--query", "tag=a b", "--query", "tag=x&y", "--header", "X-Mode=safe"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				Body struct {
					Text string   `json:"bodyText"`
					Tag  []string `json:"tag"`
					Mode string   `json:"mode"`
				} `json:"body"`
			} `json:"data"`
		}
		if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		got := envelope.Data.Body
		if !envelope.OK || got.Text != body || got.Mode != "safe" || !reflect.DeepEqual(got.Tag, []string{"a b", "x&y"}) {
			t.Fatal(output.String())
		}
	}
	b, _ := os.ReadFile(home)
	if string(b) != "unchanged" {
		t.Fatal("CLI wrote home/cache state")
	}
	for _, flags := range [][]string{{"--args", "{}", "--body", "[]"}, {"--query", "invalid"}, {"--header", "X-Mode=a", "--header", "x-mode=b"}} {
		cmd := NewCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(append([]string{"--config", cfg, "request", "api", "POST", "/echo"}, flags...))
		if err := cmd.Execute(); err == nil {
			t.Fatal("invalid request options accepted", flags)
		}
	}
}

func TestGatewayPreservesNewRequestFieldsAndFailureCode(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/limited" {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"detail":"slow down"}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"bodyText": string(b), "tag": r.URL.Query()["tag"], "mode": r.Header.Get("X-Mode")})
	}))
	defer upstream.Close()
	runtime := capability.New(capability.Config{Providers: map[string]capability.ProviderConfig{"api": {Type: "http", URL: upstream.URL}}}, 1<<20)
	defer runtime.Close()
	opts := &options{timeout: time.Second, maxBytes: 1 << 20, tokenEnv: "SKILL_CLI_MERGE_TEST_TOKEN"}
	token := strings.Repeat("b", 32)
	t.Setenv(opts.tokenEnv, token)
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = gatewayHandler(runtime, opts, token, srv.Listener.Addr().String())
	srv.Start()
	defer srv.Close()
	opts.gateway = srv.URL
	data, err := remote(context.Background(), opts, capability.Request{Action: "request", Target: "api", Method: "POST", Path: "/echo", Body: json.RawMessage(`null`), HTTPQuery: map[string][]string{"tag": {"one", "two"}}, Headers: map[string]string{"X-Mode": "safe"}})
	if err != nil {
		t.Fatal(err)
	}
	body := data.(map[string]any)["body"].(map[string]any)
	if body["bodyText"] != "null" || body["mode"] != "safe" || len(body["tag"].([]any)) != 2 {
		t.Fatal(body)
	}
	data, err = remote(context.Background(), opts, capability.Request{Action: "request", Target: "api", Method: "GET", Path: "/limited"})
	code, hint := capability.ErrorDetails(err)
	if code != "RATE_LIMITED" || hint == "" || data == nil {
		t.Fatalf("%v %s %s", data, code, hint)
	}
}

package cli

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/wolvever/skill-cli/internal/capability"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"
)

const Version = "0.1.0-dev"

type options struct {
	config, gateway, tokenEnv string
	timeout                   time.Duration
	maxBytes                  int64
}

func Run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	root := NewCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		var printed *printedError
		if !errors.As(err, &printed) {
			var fault *capability.Fault
			if !errors.As(err, &fault) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				err = capability.InvalidArgument(err.Error())
			}
			fmt.Fprintln(os.Stdout, string(capability.Encode(nil, err)))
		}
		return 1
	}
	return 0
}
func NewCommand() *cobra.Command {
	o := &options{}
	root := &cobra.Command{Use: "skill-cli", Short: "Discover, inspect and invoke MCP tools and HTTP APIs", Version: Version, SilenceUsage: true, SilenceErrors: true}
	root.Long = `One CLI for progressive capability discovery and JSON invocation.

Start with list, search, describe, then call. Operational results are one JSON
envelope on stdout: {"ok":true,"data":...} or {"ok":false,"error":...}.
Failures exit 1, including MCP isError results. Help and version are plain text.
Calls are never automatically replayed. A timeout may have an unknown outcome.`
	root.PersistentFlags().StringVar(&o.config, "config", "skill-cli.json", "Provider configuration file")
	root.PersistentFlags().StringVar(&o.gateway, "gateway", "", "Invoke through a trusted gateway URL; no local provider config required")
	root.PersistentFlags().StringVar(&o.tokenEnv, "gateway-token-env", "SKILL_CLI_GATEWAY_TOKEN", "Environment variable holding the gateway bearer token")
	root.PersistentFlags().DurationVar(&o.timeout, "timeout", 30*time.Second, "Total request deadline, including discovery and connection")
	root.PersistentFlags().Int64Var(&o.maxBytes, "max-bytes", 16<<20, "Maximum JSON input/output and HTTP response bytes")
	root.PersistentPreRunE = func(*cobra.Command, []string) error {
		if o.timeout <= 0 || o.maxBytes <= 0 || o.maxBytes > 1<<30 {
			return capability.InvalidArgument("timeout must be positive and max-bytes must be between 1 and 1073741824")
		}
		return nil
	}
	list := &cobra.Command{Use: "list [provider]", Short: "List providers, or concise operations without schemas", Args: cobra.MaximumNArgs(1), RunE: func(c *cobra.Command, a []string) error {
		r := capability.Request{Action: "list"}
		if len(a) > 0 {
			r.Target = a[0]
		}
		return execute(c, o, r)
	}}
	search := &cobra.Command{Use: "search <text>", Short: "Search operation names and descriptions (case-insensitive substring)", Args: cobra.ExactArgs(1)}
	var provider string
	var limit int
	search.Flags().StringVar(&provider, "provider", "", "Search only this provider")
	search.Flags().IntVar(&limit, "limit", 0, "Maximum returned operations, sorted by ID (0 returns all)")
	search.RunE = func(c *cobra.Command, a []string) error {
		return execute(c, o, capability.Request{Action: "search", Target: provider, Query: a[0], Limit: limit})
	}
	describe := &cobra.Command{Use: "describe <provider.operation>", Short: "Load the input/output schema for one operation", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		return execute(c, o, capability.Request{Action: "describe", Target: a[0]})
	}}
	call := &cobra.Command{Use: "call <provider.operation>", Short: "Invoke once with JSON arguments", Example: "  skill-cli call docs.search --args '{\"query\":\"MCP\"}'\n  skill-cli call docs.search --args @args.json\n  cat args.json | skill-cli call docs.search --args -", Args: cobra.ExactArgs(1)}
	var argText string
	call.Flags().StringVar(&argText, "args", "{}", "JSON object, @file, or - for stdin")
	call.RunE = func(c *cobra.Command, a []string) error {
		args, err := readArgs(argText, c.InOrStdin(), o.maxBytes)
		if err != nil {
			return err
		}
		return execute(c, o, capability.Request{Action: "call", Target: a[0], Args: args})
	}
	request := &cobra.Command{Use: "request <provider> <method> </path>", Short: "Make an HTTP request against a configured base URL", Args: cobra.ExactArgs(3)}
	var bodyText string
	request.Flags().StringVar(&bodyText, "args", "", "Optional JSON object body, @file, or - for stdin")
	var rawBody string
	var queries, headers []string
	request.Flags().StringVar(&rawBody, "body", "", "JSON body of any type, @file, or - for stdin")
	request.Flags().StringArrayVar(&queries, "query", nil, "Query key=value; repeat for multiple values")
	request.Flags().StringArrayVar(&headers, "header", nil, "Non-credential header Name=value; repeat as needed")
	request.MarkFlagsMutuallyExclusive("args", "body")
	request.RunE = func(c *cobra.Command, a []string) error {
		var body map[string]any
		var err error
		if bodyText != "" {
			body, err = readArgs(bodyText, c.InOrStdin(), o.maxBytes)
			if err != nil {
				return err
			}
		}
		var raw json.RawMessage
		if c.Flags().Changed("body") {
			raw, err = readJSON(rawBody, c.InOrStdin(), o.maxBytes)
			if err != nil {
				return err
			}
		}
		query := map[string][]string{}
		for _, item := range queries {
			key, value, ok := strings.Cut(item, "=")
			if !ok || key == "" {
				return capability.InvalidArgument("query must use key=value")
			}
			query[key] = append(query[key], value)
		}
		header := map[string]string{}
		for _, item := range headers {
			key, value, ok := strings.Cut(item, "=")
			if !ok || key == "" {
				return capability.InvalidArgument("header must use Name=value")
			}
			key = http.CanonicalHeaderKey(key)
			if _, exists := header[key]; exists {
				return capability.InvalidArgument("duplicate header; combine its values explicitly")
			}
			header[key] = value
		}
		return execute(c, o, capability.Request{Action: "request", Target: a[0], Method: a[1], Path: a[2], Args: body, Body: raw, HTTPQuery: query, Headers: header})
	}
	serve := &cobra.Command{Use: "serve", Short: "Run a loopback gateway retaining MCP sessions (single principal)", Args: cobra.NoArgs}
	var listen string
	serve.Flags().StringVar(&listen, "listen", "127.0.0.1:7331", "Loopback listening address; token environment variable is required")
	serve.RunE = func(c *cobra.Command, _ []string) error {
		if o.gateway != "" {
			return errors.New("serve cannot be combined with --gateway")
		}
		return serveGateway(c.Context(), o, listen, c.ErrOrStderr())
	}
	root.AddCommand(list, search, describe, call, request, serve)
	return root
}
func readArgs(s string, stdin io.Reader, limit int64) (map[string]any, error) {
	b, err := readJSON(s, stdin, limit)
	if err != nil {
		return nil, err
	}
	var args map[string]any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err = decoder.Decode(&args); err != nil || args == nil {
		return nil, capability.InvalidArgument("arguments must be one valid JSON object; use --args @file or --args - to avoid shell quoting")
	}
	return args, nil
}
func readJSON(s string, stdin io.Reader, limit int64) (json.RawMessage, error) {
	var reader io.Reader = strings.NewReader(s)
	if s == "-" {
		reader = stdin
	} else if strings.HasPrefix(s, "@") {
		f, err := os.Open(s[1:])
		if err != nil {
			return nil, err
		}
		defer f.Close()
		reader = f
	}
	b, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, capability.InvalidArgument("JSON input exceeds max-bytes")
	}
	if !json.Valid(b) {
		return nil, capability.InvalidArgument("input must be a single valid JSON value; use @file or - for stdin to avoid shell quoting")
	}
	return json.RawMessage(b), nil
}
func execute(c *cobra.Command, o *options, req capability.Request) error {
	ctx, cancel := context.WithTimeout(c.Context(), o.timeout)
	defer cancel()
	var data any
	var err error
	if o.gateway != "" {
		data, err = remote(ctx, o, req)
	} else {
		var cfg capability.Config
		cfg, err = capability.LoadConfig(o.config)
		if err == nil {
			r := capability.New(cfg, o.maxBytes)
			defer r.Close()
			data, err = r.Execute(ctx, req)
		}
	}
	b := capability.Encode(data, err)
	if int64(len(b)) > o.maxBytes {
		err = capability.NewFault("RESPONSE_TOO_LARGE", "result exceeds max-bytes; increase the limit or request a smaller result", "Request a smaller result or explicitly increase --max-bytes.", nil)
		b = capability.Encode(nil, err)
	}
	// Print errors here too to preserve tool-error data, then signal exit without double printing.
	fmt.Fprintln(c.OutOrStdout(), string(b))
	if err != nil {
		return &printedError{err}
	}
	return nil
}

type printedError struct{ error }

func gatewayURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid gateway URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && net.ParseIP(u.Hostname()).IsLoopback()) {
		return errors.New("gateway requires HTTPS, or HTTP with a loopback IP")
	}
	return nil
}
func remote(ctx context.Context, o *options, r capability.Request) (any, error) {
	if err := gatewayURL(o.gateway); err != nil {
		return nil, err
	}
	token := os.Getenv(o.tokenEnv)
	if len(token) < 24 {
		return nil, errors.New("gateway token must contain at least 24 characters")
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(o.gateway, "/")+"/v1/invoke", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("gateway redirect refused") }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, o.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > o.maxBytes {
		return nil, errors.New("gateway response exceeds max-bytes")
	}
	var e capability.Envelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err = decoder.Decode(&e); err != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("gateway returned invalid JSON")
	}
	if !e.OK {
		code := e.Code
		if code == "" {
			code = "UPSTREAM_ERROR"
		}
		return e.Data, capability.NewFault(code, fmt.Sprintf("gateway: %s", e.Error), e.Hint, nil)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gateway HTTP %d", resp.StatusCode)
	}
	return e.Data, nil
}
func serveGateway(ctx context.Context, o *options, listen string, logs io.Writer) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("serve must bind to a loopback IP address")
	}
	token := os.Getenv(o.tokenEnv)
	if len(token) < 24 {
		return errors.New("set the gateway token environment variable to at least 24 random characters")
	}
	cfg, err := capability.LoadConfig(o.config)
	if err != nil {
		return err
	}
	runtime := capability.New(cfg, o.maxBytes)
	defer runtime.Close()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: gatewayHandler(runtime, o, token, listener.Addr().String()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: o.timeout, WriteTimeout: o.timeout + 5*time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	fmt.Fprintln(logs, "skill-cli gateway listening on", listener.Addr())
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = srv.Shutdown(shutdown)
		if err != nil {
			_ = srv.Close()
		}
		return err
	}
}
func gatewayHandler(runtime *capability.Runtime, o *options, token, host string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/invoke", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		send := func(status int, data any, err error) {
			var fault *capability.Fault
			if err != nil && !errors.As(err, &fault) {
				code := "UPSTREAM_ERROR"
				hint := "Inspect the request and gateway configuration."
				switch status {
				case 400, 405:
					code = "INVALID_ARGUMENT"
				case 401:
					code = "AUTH_FAILED"
				case 403:
					code = "PERMISSION_DENIED"
				case 413:
					code = "RESPONSE_TOO_LARGE"
				}
				err = capability.NewFault(code, err.Error(), hint, err)
			}
			b := capability.Encode(data, err)
			if int64(len(b)) > o.maxBytes {
				status = 413
				b = capability.Encode(nil, errors.New("gateway result exceeds max-bytes"))
			}
			w.WriteHeader(status)
			_, _ = w.Write(b)
		}
		if r.Host != host {
			send(403, nil, errors.New("unexpected gateway Host"))
			return
		}
		if r.Method != "POST" {
			send(405, nil, errors.New("POST required"))
			return
		}
		if r.Header.Get("Origin") != "" {
			send(403, nil, errors.New("browser origins are not accepted"))
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			send(401, nil, errors.New("invalid gateway token"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, o.maxBytes)
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		dec.DisallowUnknownFields()
		var request capability.Request
		if err := dec.Decode(&request); err != nil {
			send(400, nil, errors.New("invalid request JSON"))
			return
		}
		if dec.Decode(new(any)) != io.EOF {
			send(400, nil, errors.New("expected one JSON request"))
			return
		}
		callCtx, cancel := context.WithTimeout(r.Context(), o.timeout)
		defer cancel()
		data, err := runtime.Execute(callCtx, request)
		status := 200
		if err != nil {
			status = 422
		}
		send(status, data, err)
	})
	return mux
}

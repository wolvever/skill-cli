package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func isHTTP(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}
func validateURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || !(u.Scheme == "http" || u.Scheme == "https") || u.User != nil || u.Fragment != "" {
		return NewFault("CONFIG_ERROR", "expected an HTTP(S) URL without credentials or fragment", "Correct the configured provider URL.", err)
	}
	return nil
}

type authTransport struct {
	cfg  ProviderConfig
	next http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header = req.Header.Clone()
	if t.cfg.URL != "" {
		base, err := url.Parse(t.cfg.URL)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(base.Scheme, r.URL.Scheme) || !strings.EqualFold(base.Host, r.URL.Host) {
			return nil, errors.New("cross-origin credential forwarding refused")
		}
	}
	if t.cfg.BearerEnv != "" {
		v, ok := os.LookupEnv(t.cfg.BearerEnv)
		if !ok || v == "" {
			return nil, NewFault("AUTH_FAILED", fmt.Sprintf("required credential environment variable is unset: %s", t.cfg.BearerEnv), "Set the credential in the trusted host environment; do not put secrets in command arguments.", nil)
		}
		r.Header.Set("Authorization", "Bearer "+v)
	}
	for name, envName := range t.cfg.HeadersEnv {
		v, ok := os.LookupEnv(envName)
		if !ok || v == "" {
			return nil, NewFault("AUTH_FAILED", fmt.Sprintf("required credential environment variable is unset: %s", envName), "Set the credential in the trusted host environment; do not put secrets in command arguments.", nil)
		}
		r.Header.Set(name, v)
	}
	return t.next.RoundTrip(r)
}
func httpClient(c ProviderConfig, _ int64) (*http.Client, error) {
	return &http.Client{Transport: &authTransport{c, http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirect refused; configure the final endpoint explicitly")
	}}, nil
}
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, NewFault("RESPONSE_TOO_LARGE", fmt.Sprintf("response exceeds %d bytes; increase --max-bytes", limit), "Request a smaller result or explicitly increase --max-bytes.", nil)
	}
	return b, nil
}

type HTTPResult struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType,omitempty"`
	Body        any    `json:"body"`
}

func rawRequest(ctx context.Context, c ProviderConfig, method, path string, body map[string]any, limit int64) (any, error) {
	return requestWithHeaders(ctx, c, method, path, body, nil, limit)
}
func requestWithHeaders(ctx context.Context, c ProviderConfig, method, path string, body map[string]any, headers map[string]string, limit int64) (any, error) {
	var raw json.RawMessage
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, InvalidArgument("body is not valid JSON")
		}
		raw = b
	}
	return requestWithOptions(ctx, c, method, path, raw, headers, nil, limit)
}

// Request headers can supply API options, but credentials and connection framing
// remain controlled by operator configuration and net/http.
func validateRequestHeaders(headers map[string]string) error {
	for name, value := range headers {
		if name == "" {
			return InvalidArgument("header name cannot be empty")
		}
		for _, c := range name {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
				return InvalidArgument("invalid HTTP header name")
			}
		}
		for _, c := range value {
			if (c < 32 && c != '\t') || c == 127 {
				return InvalidArgument("HTTP header values cannot contain control characters")
			}
		}
		switch strings.ToLower(name) {
		case "authorization", "proxy-authorization", "cookie":
			return InvalidArgument("credential headers must use provider environment bindings")
		case "host", "content-length", "transfer-encoding", "connection", "proxy-connection", "upgrade", "te", "trailer":
			return InvalidArgument("request cannot override HTTP routing or connection headers")
		}
	}
	return nil
}

func requestWithOptions(ctx context.Context, c ProviderConfig, method, path string, body json.RawMessage, headers map[string]string, query map[string][]string, limit int64) (any, error) {
	if err := validateRequestHeaders(headers); err != nil {
		return nil, err
	}
	if body != nil && !json.Valid(body) {
		return nil, InvalidArgument("body must be a single valid JSON value")
	}
	if int64(len(body)) > limit {
		return nil, InvalidArgument("body exceeds max-bytes")
	}
	if err := validateURL(c.URL); err != nil {
		return nil, err
	}
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, InvalidArgument("path must start with / and stay on the configured provider")
	}
	// Retain the base path. Reject traversal, including percent-encoded traversal.
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == ".." || segment == "." {
			return nil, InvalidArgument("path traversal is not allowed")
		}
	}
	base, _ := url.Parse(c.URL)
	if base.RawQuery != "" {
		return nil, NewFault("CONFIG_ERROR", "provider url must not contain a query", "Move query parameters to the request path or --query flags.", nil)
	}
	if len(query) > 0 {
		values, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return nil, InvalidArgument("path contains an invalid query")
		}
		for key, items := range query {
			for _, item := range items {
				values.Add(key, item)
			}
		}
		u.RawQuery = values.Encode()
		path = u.String()
	}
	endpoint := strings.TrimRight(c.URL, "/") + path
	method = strings.ToUpper(method)
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		return nil, InvalidArgument("unsupported HTTP method")
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client, err := httpClient(c, limit)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := readBounded(resp.Body, limit)
	if err != nil {
		return nil, err
	}
	var value any
	if len(b) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
			value = string(b)
		}
	}
	result := HTTPResult{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: value}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, hint := "UPSTREAM_ERROR", "Inspect data.status and data.body before correcting or retrying the request."
		switch resp.StatusCode {
		case 401:
			code, hint = "AUTH_FAILED", "Check the provider's credential environment bindings."
		case 403:
			code, hint = "PERMISSION_DENIED", "Check the upstream credential scope and provider access policy."
		case 404:
			code, hint = "NOT_FOUND", "Verify the resource path or identifier against the provider schema."
		case 429:
			code, hint = "RATE_LIMITED", "Respect the provider's rate limit before making another request."
		}
		return result, NewFault(code, fmt.Sprintf("HTTP %d", resp.StatusCode), hint, nil)
	}
	return result, nil
}

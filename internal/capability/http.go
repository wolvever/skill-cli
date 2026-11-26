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
		return errors.New("expected an HTTP(S) URL without credentials or fragment")
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
			return nil, fmt.Errorf("required credential environment variable is unset: %s", t.cfg.BearerEnv)
		}
		r.Header.Set("Authorization", "Bearer "+v)
	}
	for name, envName := range t.cfg.HeadersEnv {
		v, ok := os.LookupEnv(envName)
		if !ok || v == "" {
			return nil, fmt.Errorf("required credential environment variable is unset: %s", envName)
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
		return nil, fmt.Errorf("response exceeds %d bytes; increase --max-bytes", limit)
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
	if err := validateURL(c.URL); err != nil {
		return nil, err
	}
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, errors.New("path must start with / and stay on the configured provider")
	}
	// Retain the base path. Reject traversal, including percent-encoded traversal.
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == ".." || segment == "." {
			return nil, errors.New("path traversal is not allowed")
		}
	}
	base, _ := url.Parse(c.URL)
	if base.RawQuery != "" {
		return nil, errors.New("provider url must not contain a query")
	}
	endpoint := strings.TrimRight(c.URL, "/") + path
	method = strings.ToUpper(method)
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		return nil, errors.New("unsupported HTTP method")
	}
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		reader = bytes.NewReader(b)
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
		return result, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return result, nil
}

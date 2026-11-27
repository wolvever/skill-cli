package capability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

type Config struct {
	Providers map[string]ProviderConfig `json:"providers"`
}
type ProviderConfig struct {
	Type       string            `json:"type"`
	Transport  string            `json:"transport,omitempty"`
	Command    []string          `json:"command,omitempty"`
	URL        string            `json:"url,omitempty"`
	Spec       string            `json:"spec,omitempty"`
	BearerEnv  string            `json:"bearer_env,omitempty"`
	HeadersEnv map[string]string `json:"headers_env,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Allow      []string          `json:"allow,omitempty"`
}

func LoadConfig(path string) (c Config, err error) {
	defer func() {
		if err != nil {
			if _, ok := err.(*Fault); !ok {
				err = NewFault("CONFIG_ERROR", err.Error(), "Correct the provider configuration fields.", err)
			}
		}
	}()
	b, err := os.ReadFile(path)
	if err != nil {
		return c, NewFault("CONFIG_ERROR", fmt.Sprintf("read config: %v", err), "Supply --config with an existing provider configuration file.", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return c, NewFault("CONFIG_ERROR", fmt.Sprintf("invalid config: %v", err), "Correct the provider configuration fields.", err)
	}
	if decoder.Decode(new(any)) != io.EOF || c.Providers == nil {
		return c, NewFault("CONFIG_ERROR", "config must be one JSON object containing providers", "Use the documented providers configuration format.", nil)
	}
	valid := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	for name, p := range c.Providers {
		if !valid.MatchString(name) {
			return c, fmt.Errorf("invalid provider name %q", name)
		}
		switch p.Type {
		case "mcp":
			switch p.Transport {
			case "stdio":
				if len(p.Command) == 0 {
					return c, fmt.Errorf("%s: command is required", name)
				}
			case "http", "sse":
				if p.URL == "" {
					return c, fmt.Errorf("%s: url is required", name)
				}
			default:
				return c, fmt.Errorf("%s: transport must be stdio, http or sse", name)
			}
		case "openapi":
			if p.Spec == "" {
				return c, fmt.Errorf("%s: spec is required", name)
			}
			if !isHTTP(p.Spec) && !filepath.IsAbs(p.Spec) {
				p.Spec = filepath.Join(filepath.Dir(path), p.Spec)
			}
		case "http":
			if p.URL == "" {
				return c, fmt.Errorf("%s: url is required", name)
			}
		default:
			return c, fmt.Errorf("%s: type must be mcp, openapi or http", name)
		}
		c.Providers[name] = p
	}
	return c, nil
}

func permitted(p ProviderConfig, operation string) bool {
	if len(p.Allow) == 0 {
		return true
	}
	for _, a := range p.Allow {
		if a == operation {
			return true
		}
	}
	return false
}

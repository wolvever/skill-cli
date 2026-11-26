package capability

import (
	"encoding/json"
	"fmt"
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

func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("invalid config: %w", err)
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

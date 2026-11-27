package capability

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
)

type Config struct {
	Providers map[string]ProviderConfig `json:"providers"`
}
type ProviderConfig struct {
	Type              string            `json:"type"`
	Transport         string            `json:"transport,omitempty"`
	Command           []string          `json:"command,omitempty"`
	URL               string            `json:"url,omitempty"`
	Spec              string            `json:"spec,omitempty"`
	BearerEnv         string            `json:"bearer_env,omitempty"`
	HeadersEnv        map[string]string `json:"headers_env,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	Allow             []string          `json:"allow,omitempty"`
	Description       string            `json:"description,omitempty"`
	Keywords          []string          `json:"keywords,omitempty"`
	SkillFiles        []string          `json:"skill_files,omitempty"`
	SkillDescriptions []string          `json:"skill_descriptions,omitempty"`
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
		for _, skillPath := range p.SkillFiles {
			if !filepath.IsAbs(skillPath) {
				skillPath = filepath.Join(filepath.Dir(path), skillPath)
			}
			metadata, err := skillMetadata(skillPath)
			if err != nil {
				return c, fmt.Errorf("%s skill metadata: %w", name, err)
			}
			p.SkillDescriptions = append(p.SkillDescriptions, metadata)
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

// Only explicitly configured frontmatter is indexed, never skill instructions,
// scripts, or a recursive scan of the user's filesystem.
func skillMetadata(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		path = filepath.Join(path, "SKILL.md")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 65537))
	scanner.Buffer(make([]byte, 4096), 65537)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", fmt.Errorf("skill must start with YAML frontmatter")
	}
	var header strings.Builder
	closed := false
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "---" {
			closed = true
			break
		}
		header.WriteString(scanner.Text())
		header.WriteByte('\n')
	}
	if !closed || header.Len() > 65536 {
		return "", fmt.Errorf("skill frontmatter must close within 64 KiB")
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(header.String()), &meta); err != nil {
		return "", fmt.Errorf("invalid skill frontmatter: %w", err)
	}
	if strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
		return "", fmt.Errorf("skill name and description are required")
	}
	return meta.Name + ": " + meta.Description, nil
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

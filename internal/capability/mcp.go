package capability

import (
	"context"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"os"
	"os/exec"
)

type mcpProvider struct{ session *mcp.ClientSession }

func newMCP(ctx context.Context, c ProviderConfig, maxBytes int64) (Provider, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "skill-cli", Version: "0.1.0"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}, MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
	var transport mcp.Transport
	if c.Transport == "stdio" {
		// No shell interpolation. Session owns the subprocess beyond this request.
		cmd := exec.Command(c.Command[0], c.Command[1:]...)
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		for name, envName := range c.Env {
			v, ok := os.LookupEnv(envName)
			if !ok {
				return nil, NewFault("AUTH_FAILED", "required stdio environment variable is unset: "+envName, "Set the required variable in the trusted host environment.", nil)
			}
			cmd.Env = append(cmd.Env, name+"="+v)
		}
		transport = &mcp.CommandTransport{Command: cmd}
	} else {
		h, err := httpClient(c, maxBytes)
		if err != nil {
			return nil, err
		}
		if err = validateURL(c.URL); err != nil {
			return nil, err
		}
		if c.Transport == "sse" {
			transport = &mcp.SSEClientTransport{Endpoint: c.URL, HTTPClient: h, MaxEventSize: int(maxBytes)}
		} else {
			transport = &mcp.StreamableClientTransport{Endpoint: c.URL, HTTPClient: h, MaxRetries: -1, MaxEventSize: int(maxBytes)}
		}
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		var fault *Fault
		if errors.As(err, &fault) {
			return nil, err
		}
		return nil, NewFault("PROVIDER_UNAVAILABLE", err.Error(), "Check the configured server command or endpoint. Do not replay an uncertain mutation.", err)
	}
	return &mcpProvider{session}, nil
}
func (p *mcpProvider) Discover(ctx context.Context) ([]Capability, error) {
	out := []Capability{}
	for t, err := range p.session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		out = append(out, Capability{ID: t.Name, Description: t.Description, Protocol: "mcp", InputSchema: t.InputSchema, OutputSchema: t.OutputSchema, Annotations: t.Annotations})
	}
	return out, nil
}
func (p *mcpProvider) Invoke(ctx context.Context, op string, args map[string]any) (any, error) {
	if args == nil {
		args = map[string]any{}
	}
	result, err := p.session.CallTool(ctx, &mcp.CallToolParams{Name: op, Arguments: args})
	if err != nil {
		return nil, err
	}
	if result.IsError {
		return result, NewFault("TOOL_ERROR", "MCP tool reported an execution error; inspect data.content", "Inspect data.content and correct the request; do not blindly replay a mutation.", nil)
	}
	return result, nil
}
func (p *mcpProvider) Close() error { return p.session.Close() }

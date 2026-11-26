// A local, credential-free server for trying MCP and HTTP invocation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	listen := flag.String("listen", "", "Serve MCP + REST over HTTP instead of stdio (e.g. 127.0.0.1:8080)")
	flag.Parse()
	server := mcp.NewServer(&mcp.Implementation{Name: "skill-cli-demo", Version: "1"}, nil)
	type echoArgs struct {
		Message string         `json:"message"`
		Payload map[string]any `json:"payload,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "Echo a message and optional nested JSON"}, func(ctx context.Context, r *mcp.CallToolRequest, in echoArgs) (*mcp.CallToolResult, any, error) {
		return nil, in, nil
	})
	type addArgs struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "add", Description: "Add two numbers"}, func(ctx context.Context, r *mcp.CallToolRequest, in addArgs) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"sum": in.A + in.B}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "fail", Description: "Return a tool execution error"}, func(ctx context.Context, r *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "deliberate demo error"}}}, nil, nil
	})
	if *listen == "" {
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	mux.Handle("/sse", mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil))
	mux.HandleFunc("GET /pets/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "name": "Mochi", "verbose": r.URL.Query().Get("verbose")})
	})
	fmt.Println("demo MCP and REST listening on", *listen)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

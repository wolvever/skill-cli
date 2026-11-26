"""Dependency-free, deterministic MCP + REST fixture for comparative CLI trials."""
import argparse
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

TOOLS = [
    {"name": "echo", "description": "Echo a message and nested JSON without side effects", "inputSchema": {"type": "object", "properties": {"message": {"type": "string"}, "payload": {"type": "object"}}, "required": ["message"]}, "annotations": {"readOnlyHint": True, "idempotentHint": True}},
    {"name": "add", "description": "Add two numbers", "inputSchema": {"type": "object", "properties": {"a": {"type": "number"}, "b": {"type": "number"}}, "required": ["a", "b"]}, "annotations": {"readOnlyHint": True}},
    {"name": "fail", "description": "Return an MCP tool execution error", "inputSchema": {"type": "object", "properties": {}}},
]

def rpc(req):
    method, p = req.get("method"), req.get("params", {})
    if "id" not in req:
        return None
    out = {"jsonrpc": "2.0", "id": req["id"]}
    if method == "initialize":
        out["result"] = {"protocolVersion": "2024-11-05", "capabilities": {"tools": {}, "resources": {}, "prompts": {}}, "serverInfo": {"name": "skill-cli-fixture", "version": "1.0"}}
    elif method == "ping":
        out["result"] = {}
    elif method == "tools/list":
        out["result"] = {"tools": TOOLS}
    elif method == "tools/call":
        args, name = p.get("arguments", {}), p.get("name")
        if name == "echo" and isinstance(args.get("message"), str):
            data = args
        elif name == "add" and all(isinstance(args.get(x), (int, float)) for x in ["a", "b"]):
            data = {"sum": args["a"] + args["b"]}
        elif name == "fail":
            out["result"] = {"content": [{"type": "text", "text": "deliberate fixture failure"}], "isError": True}
            return out
        else:
            out["error"] = {"code": -32602, "message": "unknown tool or invalid arguments"}
            return out
        out["result"] = {"content": [{"type": "text", "text": json.dumps(data)}]}
    elif method == "resources/list":
        out["result"] = {"resources": [{"uri": "fixture://readme", "name": "readme", "mimeType": "text/plain"}]}
    elif method == "resources/read":
        out["result"] = {"contents": [{"uri": "fixture://readme", "mimeType": "text/plain", "text": "fixture resource"}]}
    elif method == "prompts/list":
        out["result"] = {"prompts": [{"name": "greeting", "description": "A fixture prompt"}]}
    elif method == "prompts/get":
        out["result"] = {"messages": [{"role": "user", "content": {"type": "text", "text": "Hello"}}]}
    else:
        out["error"] = {"code": -32601, "message": "Method not found"}
    return out

SPEC = {"openapi": "3.0.3", "info": {"title": "CLI fixture", "version": "1"}, "paths": {"/pets/{id}": {"get": {"operationId": "getPet", "summary": "Get a pet", "parameters": [{"name": "id", "in": "path", "required": True, "schema": {"type": "string"}}, {"name": "verbose", "in": "query", "schema": {"type": "boolean"}}], "responses": {"200": {"description": "pet"}}}}, "/echo": {"post": {"operationId": "echoBody", "summary": "Echo JSON body", "requestBody": {"required": True, "content": {"application/json": {"schema": {"type": "object"}}}}, "responses": {"200": {"description": "echo"}}}}}}

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def send_json(self, status, data):
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/openapi.json"):
            spec = dict(SPEC, servers=[{"url": "http://127.0.0.1:%s" % self.server.server_port}])
            self.send_json(200, spec)
        elif self.path.startswith("/pets/"):
            self.send_json(200, {"id": self.path.split("/")[2].split("?")[0], "name": "Mochi", "query": self.path.partition("?")[2]})
        else:
            self.send_json(405, {"error": "GET unsupported"})

    def do_DELETE(self):
        self.send_json(200, {})

    def do_POST(self):
        if self.path != "/echo" and getattr(self.server, "rest_only", False):
            self.send_json(404, {"error": "not an MCP endpoint"})
            return
        req = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))))
        if self.path == "/echo":
            self.send_json(200, req)
            return
        out = rpc(req)
        if out is None:
            self.send_response(202)
            self.send_header("Content-Length", "0")
            self.end_headers()
        else:
            self.send_json(200, out)

if __name__ == "__main__":
    p = argparse.ArgumentParser()
    p.add_argument("--http", type=int)
    p.add_argument("--rest-only", action="store_true")
    a = p.parse_args()
    if a.http:
        server = ThreadingHTTPServer(("127.0.0.1", a.http), Handler)
        server.rest_only = a.rest_only
        server.serve_forever()
    else:
        for line in sys.stdin:
            out = rpc(json.loads(line))
            if out is not None:
                print(json.dumps(out), flush=True)

# skill-cli

A Go CLI for AI agents and scripts to discover, inspect, and invoke MCP tools and HTTP APIs without placing an entire tool catalog in model context. The executable and project name are **`skill-cli`**.

**Working alpha.** Supports MCP over stdio, Streamable HTTP and legacy SSE; OpenAPI 3.0 JSON/YAML; and direct HTTP requests with query parameters, headers, and JSON bodies. An optional authenticated loopback gateway retains MCP connections and resolves provider credentials outside the calling CLI. Discovery stays small; operation schemas are requested separately. [Skillhub comparison and merge decisions](docs/skillhub-merge.md).

## Try it locally

Requires Go 1.25 or newer to build. The resulting executable needs no Go, Node, or Python runtime. A configured stdio server may require its own runtime.

```sh
git clone https://github.com/wolvever/skill-cli.git
cd skill-cli
go build -o bin/skill-cli ./cmd/skill-cli
go run ./examples/demo-server --listen 127.0.0.1:8080
```

In another terminal:

```sh
bin/skill-cli --config examples/demo.json list
bin/skill-cli --config examples/demo.json list demo
bin/skill-cli --config examples/demo.json search add
bin/skill-cli --config examples/demo.json search search --limit 5
bin/skill-cli --config examples/demo.json describe demo.add
bin/skill-cli --config examples/demo.json call demo.add --args '{"a":2,"b":3}'
bin/skill-cli --config examples/demo.json call legacy.add --args '{"a":2,"b":3}'
bin/skill-cli --config examples/demo.json call pets.getPet --args '{"id":"42","verbose":true}'
bin/skill-cli --config examples/demo.json request rest GET /pets/42
```

On Windows, build `bin/skill-cli.exe` and invoke `./bin/skill-cli.exe`. JSON files avoid differences in shell quoting:

```sh
skill-cli call demo.echo --args @args.json
cat args.json | skill-cli call demo.echo --args -
```

In PowerShell, pipe with `Get-Content -Raw args.json`. No demo operation changes external state or needs credentials. `examples/skill-cli.json` shows real-service configuration patterns and requires adapting the local server path and credentials.

For a complete PowerShell example, start the server above, then run in a second terminal:

```powershell
go build -o bin/skill-cli.exe ./cmd/skill-cli
$cli = ".\bin\skill-cli.exe"
$cfg = "examples/demo.json"
& $cli --config $cfg search search --limit 5
& $cli --config $cfg describe demo.search_issues
'{"query":"login","limit":2}' | & $cli --config $cfg call demo.search_issues --args -
'{"query":"login","limit":2}' | & $cli --config $cfg call issues.searchIssues --args -
```

The last two commands return the same issue-search data through MCP and OpenAPI. This fixture was adapted from the earlier Skillhub evaluation into a portable Go demo and regression tests.

## Agent interface

| Command | Contract |
| --- | --- |
| `list` | Provider names, protocols and configured descriptions; does not connect |
| `list <provider> [--limit N] [--full]` | Operation IDs and short descriptions, without schemas |
| `search <text> [--provider name] [--limit N]` | Local BM25 ranking; top 10 short candidates, no schemas |
| `describe <provider.operation>` | One input schema and available output schema/annotations |
| `call <provider.operation> --args JSON\|@file\|-` | Invoke once with a JSON object |
| `request <provider> <method> </path>` | HTTP request with optional `--query`, `--header`, `--body` |
| `serve` | Persistent gateway for one trusted configuration and principal |

Operational stdout is exactly one JSON envelope. Help/version are plain text. Both protocol errors and MCP `isError` results exit **1**, preserving tool-error data when available:

```json
{"ok":true,"data":{"structuredContent":{"sum":5}}}
```

The exact MCP result may also contain content blocks and protocol metadata. HTTP results contain `status`, `contentType`, and `body`. Errors keep a readable `error` string and add stable `code` and recovery `hint` fields, optionally with upstream `data`:

```json
{"ok":false,"error":"HTTP 429","code":"RATE_LIMITED","hint":"Respect the provider's rate limit before making another request."}
```

Codes distinguish `INVALID_ARGUMENT`, `CONFIG_ERROR`, `NOT_FOUND`, `AUTH_FAILED`, `PERMISSION_DENIED`, `RATE_LIMITED`, `PROVIDER_UNAVAILABLE`, `TIMEOUT`, `CANCELLED`, `TOOL_ERROR`, and other `UPSTREAM_ERROR` failures. `RESPONSE_TOO_LARGE` asks for a smaller response or a larger explicit limit. Unknown MCP protocol errors remain upstream errors rather than being classified by guessing at their message text. Machine-readable failures stay on stdout so existing consumers can parse one envelope; incidental server diagnostics go to stderr. MCP content is preserved rather than guessing whether a text block should be decoded as JSON.

Search ranks operation names, descriptions, argument names and configured provider/skill metadata using local BM25, with identifier splitting and a typo fallback when no lexical terms match. It needs no model download, API key, database or service. Exact IDs take priority; semantic reasoning and reliable negation handling still require the agent. Search returns at most 10 candidates by default; `--limit 0` returns all matches. List/search descriptions are bounded to 180 runes; `--full` restores complete descriptions and annotations, while `describe` returns the complete available schema and instructions. Limits reduce output, not upstream tools/list work. Legacy matching remains available with `--match substring --limit 0 --full`. Read the [retrieval design and alternatives](docs/research/lightweight-retrieval.md).

Use `--` before option-like positional values. Provider names allow letters, digits, underscores, and hyphens. Operation names are preserved literally after the first dot: `docs.search.issues-v2` addresses provider `docs`, operation `search.issues-v2`, without normalization or alias collisions.

`--timeout 30s` covers connection setup and invocation. `--max-bytes 16777216` bounds CLI input/output, HTTP response bodies, spec files, gateway payloads, and individual MCP HTTP SSE events. It is **not** a pre-allocation memory limit for SDK-decoded MCP stdio results or a limit on the number of discovered schemas. A timeout can leave an unknown execution outcome. Calls and multi-round continuations are never automatically replayed.

## Providers and credentials

```json
{
  "providers": {
    "docs": {"type":"mcp","transport":"http","url":"https://example.com/mcp","bearer_env":"DOCS_TOKEN","allow":["search"]},
    "local": {"type":"mcp","transport":"stdio","command":["python","/absolute/server.py"],"env":{"API_KEY":"LOCAL_API_KEY"}},
    "api": {"type":"openapi","spec":"./api.yaml","url":"https://api.example.com","headers_env":{"X-API-Key":"API_KEY"}},
    "rest": {"type":"http","url":"https://api.example.com","bearer_env":"API_TOKEN"}
  }
}
```

Spec file paths resolve relative to the configuration file. Stdio arguments are an executable plus an argument array, without an implicit shell. Working directory is the process working directory. Environment bindings map child variable names to host variable names; other process environment variables are inherited. Use executable programs such as `node` or `python` on Windows; invoke batch scripts explicitly through a shell only when needed.

Credentials are environment variable **names**, not values in configuration or command arguments. They are resolved on each HTTP request. Remote spec downloads are unauthenticated; download authenticated specs separately. HTTP redirects and cross-origin credential forwarding are refused. Explicit plaintext provider HTTP URLs are allowed; use HTTPS for remote credentials.

An optional nonempty `allow` list hides and denies every operation not listed, including gateway calls. Omitting it (or setting an empty list) permits all operations. Raw HTTP requests are disabled for providers with an operation allowlist so they cannot bypass it. Configure a provider URL explicitly when the API spec should not select the destination. Treat configs, specs, and stdio executables as trusted operator input.

Configuration rejects unknown fields and extra JSON values, so a misspelled policy field cannot silently disappear. One-shot commands do not write home-directory discovery or usage caches.

Providers may add `description`, `keywords`, `skill_descriptions` and `skill_files` for discovery. Skill paths resolve relative to the config and accept a SKILL.md file or folder. Only the explicitly configured frontmatter `name` and `description` are indexed, bounded to 64 KiB; skill bodies/scripts are not indexed or executed. Provider summaries appear in `list` without opening server connections. Keep broad metadata concise because it applies to every allowed tool from that provider.

Try the public catalog (10 MCP servers and two OpenAPI sources):

```sh
skill-cli --config benchmarks/public.json list
skill-cli --config benchmarks/public.json search "Svelte component fixes" --provider svelte --limit 3
skill-cli --config benchmarks/public.json list nuxt --limit 5
skill-cli --config benchmarks/public.json describe svelte.svelte-autofixer
```

These discovery commands contact public servers when listing tools or searching. Public availability and anonymous quotas can change. Narrow searches by provider to reduce network work and isolate failures.

## HTTP requests

Use a configured provider, a method, and a relative path. The local demo supports:

```sh
skill-cli --config examples/demo.json request rest GET /issues --query query=login --query limit=2
skill-cli --config examples/demo.json request rest POST /echo --query tag=one --query tag=two --header X-Mode=safe --body @body.json
```

`--query key=value` can repeat, including the same key, and values are URL-encoded. Existing path queries are retained. `--header Name=value` supplies non-credential API options. Routing/framing headers and `Authorization`, `Proxy-Authorization`, and `Cookie` overrides are rejected; use environment bindings for secrets. There is no per-request arbitrary upstream override.

`--body` accepts any single JSON value, `@file`, or `-` for stdin, including arrays, booleans, strings, and explicit `null`. Large integer literals are preserved. The earlier `--args` form remains supported for object bodies, but cannot be combined with `--body`. OpenAPI `call` still takes an argument object; its `body` member can be any JSON value permitted by that operation's schema.

## Persistent gateway

Start one gateway per principal/security context. In the trusted host environment, set provider credentials and a random `SKILL_CLI_GATEWAY_TOKEN` of at least 24 characters, then:

```sh
skill-cli --config skill-cli.json serve --listen 127.0.0.1:7331
```

The calling environment needs the gateway URL and gateway token:

```sh
skill-cli --gateway http://127.0.0.1:7331 list
skill-cli --gateway http://127.0.0.1:7331 call docs.search --args @args.json
```

No local provider config is read in gateway mode. The foreground service lazily opens one MCP session per configured provider and retains it until shutdown. Requests share those sessions; tool catalogs are queried live, without a disk schema cache. OpenAPI documents are loaded once per provider per gateway lifetime. Restart to load config/spec changes or recover a broken MCP session; this alpha does not implement automatic reconnect, OAuth refresh, idle eviction, or multiple principals.

The gateway binds only to loopback, validates Host, rejects browser Origins, limits request sizes/deadlines, and authenticates every invocation. It does not itself create sandbox network isolation. Reaching a gateway in another container requires an operator-managed authenticated/TLS bridge; do not expose loopback through an unauthenticated port forward. The gateway token grants the configured capability set and is itself sensitive. This is a building block, not a production multitenant credential vault, policy engine, or sandbox escape boundary.

## OpenAPI support and current limits

OpenAPI 3.0 JSON/YAML supports operation IDs (fallback: `get:/path`), path/query/header scalar parameters, JSON bodies validated against the schema, internal references, and configured/environment authentication. `describe` bundles only transitively referenced schema components. Parameter location/name collisions and body-name collisions fail explicitly. Path/operation server overrides, cookie/array/object parameters, multipart, external references, Swagger 2, and OpenAPI 3.1+ are not implemented. Unsupported parameter forms fail before invocation when supplied. `securitySchemes` do not automatically authenticate; use explicit environment bindings.

GraphQL, gRPC, general JSON-RPC, semantic search, SDK/CLI generation, result storage, async tasks, prompts/resources/subscriptions, OAuth/keychain integration, cross-principal session pools, audit logging, rate limits, and approval policies remain roadmap items. Protocol-specific interfaces should own those lifecycle features rather than overloading synchronous invocation.

## Development and provenance

```sh
go vet ./...
go test ./...
go test -race ./... # requires a supported C toolchain
go build ./cmd/skill-cli
```

Tests exercise stdio, Streamable HTTP and legacy SSE against the official Go MCP SDK, nested JSON, session retention, tool errors, OpenAPI validation/encoding, HTTP deadlines and size limits, redirect/credential boundaries, gateway authentication/allowlists, and CLI output. CI runs on Linux, Windows, and macOS, with a Linux race check.

The [hands-on evaluation](docs/research/evaluation.md) explains the ranking and implementation choices. Design references: [UXC](https://github.com/holon-run/uxc) for protocol adapters and compact discovery, [mcpc](https://github.com/apify/mcpc) for session lifecycle and error semantics, [MCPorter](https://github.com/openclaw/mcporter) for embedding/code generation, and [mcp-cli](https://github.com/philschmid/mcp-cli) for a small agent-facing command surface. This implementation uses the [official Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk) and [kin-openapi](https://github.com/getkin/kin-openapi); it does not copy the reference projects' implementations.

## Agent skill and native-MCP benchmark

The reusable [skill-cli skill](skills/skill-cli/SKILL.md) teaches progressive discovery, schema inspection, portable JSON input, failure handling, and gateway use. Install the `skills/skill-cli` folder into your agent's skills directory; for Codex the usual destination is `~/.codex/skills/skill-cli`.

The [benchmark suite](benchmarks/README.md) now includes **10 public MCP servers, two OpenAPI sources, 46 operations and 80 labeled tasks**, an official-SDK native MCP peer, frozen catalog replay, retrieval metrics, token-component counts, live read-only probes, and a three-arm model-selection runner with provider-reported token usage. Read the [expanded results](benchmarks/RESULTS-10.md) and the [historical three-server baseline](benchmarks/RESULTS.md) before interpreting savings or precision/recall. Model accuracy and billed cost require a configured model endpoint; tokenizer counts are not billing measurements.

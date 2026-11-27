---
name: skill-cli
description: Discover, inspect, and invoke MCP tools or OpenAPI/HTTP operations through the skill-cli executable, especially when an agent needs external capabilities from a shell without loading every tool schema.
---

# Skill CLI

Use the configured `skill-cli` executable and provider configuration. Respect the user's chosen providers and existing authorization. Creating a provider configuration does not authorize every operation it exposes.

## Locate and discover

Use `skill-cli` on PATH, a supplied executable path, or `SKILL_CLI_BIN` if provided. A checkout can be built with `go build -o bin/skill-cli ./cmd/skill-cli` (Windows: `bin/skill-cli.exe`). Use the user's config, `SKILL_CLI_CONFIG` if supplied, or the default `skill-cli.json`; the CLI itself does not automatically read those two environment variables, so pass their values explicitly.

```sh
skill-cli --config providers.json list
skill-cli --config providers.json search "search" --provider docs --limit 10
skill-cli --config providers.json describe docs.search
skill-cli --config providers.json call docs.search --args @args.json
```

`list` without a provider only reads configuration and shows provider descriptions. `search` ranks names, descriptions, argument names and configured skill metadata locally using BM25, returning at most 10 concise candidates by default. Use a natural request or discriminating terms, add `--provider` when known, and inspect the best candidates. Exact operation IDs take priority. Scores are lexical evidence, not semantic understanding or proof that an operation exists.

`list <provider> --limit 10` is a bounded fallback for unclear results; use `--limit 0` to enumerate all. Search/list omit schemas and truncate descriptions; `--full` restores full descriptions and annotations. Read `describe` for complete instructions and schemas before deciding or invoking. A missing lexical match can need different words; an unrelated match must be rejected, especially on unsupported actions. One unavailable provider can fail a global search; narrow with `--provider`. `--match substring --limit 0 --full` preserves the old substring search behavior.

For an unfamiliar operation, read `describe` before constructing its arguments. Use exact IDs returned by discovery; do not guess server tool names or API parameter names. Schemas are loaded on demand into agent context, although upstream MCP tools/list may transmit the full catalog to the client.

## Invoke and interpret

Prefer `--args @file.json` or stdin (`--args -`) for nested JSON and shell portability. In PowerShell, use `Get-Content -Raw args.json | ... --args -`; in POSIX shells use `cat args.json | ... --args -`. Arguments must be a JSON object. OpenAPI JSON request bodies go under a `body` argument.

Operational stdout is one envelope: `{"ok":true,"data":...}` or `{"ok":false,"error":"...","code":"...","hint":"...","data":...}`. Exit status is nonzero for invocation failures, including MCP `isError` results. Use the machine-readable code and hint and preserve useful error data. MCP `data` can contain `content` blocks and/or `structuredContent`; HTTP `data` contains `status`, `contentType`, and `body`. Do not assume every text block is JSON.

Keep large outputs in a local file and parse/filter them in code before returning relevant records to the model. Filtering reduces model context only if the original full output is not also displayed. Count discovery, schema inspection, shell commands, tool outputs, and retries when comparing token use.

Calls are never automatically replayed. After a timeout or disconnect, the operation may have executed: verify the outcome before repeating a mutation. Correct a rejected argument from its schema; do not keep retrying an unsupported operation. `--timeout 30s` changes the request deadline; `--max-bytes` changes payload/output limits, not a complete memory budget.

## Other modes

`request <provider> <METHOD> </path>` sends an HTTP request to a configured HTTP/OpenAPI base URL. Use repeated `--query key=value`, non-credential `--header Name=value`, and `--body @file.json` or `--body -` for any JSON body type. Credential and connection-routing header overrides are rejected; bind secrets through provider configuration. It cannot bypass an operation allowlist. Preserve the user's authorization when choosing a method.

For repeated calls, use an already configured `--gateway URL`; it avoids reopening MCP sessions on each CLI invocation. Gateway mode does not read the local provider config. Credentials remain in the gateway host; the caller needs a gateway token in the named environment variable. Do not start or reuse a gateway across different principals.

Read [configuration.md](references/configuration.md) when setting up providers, authentication, or a new gateway. OpenAPI support is currently 3.0 JSON/YAML with scalar parameters and JSON bodies validated against the schema. Swagger 2, OpenAPI 3.1+, OAuth setup, GraphQL/gRPC, and MCP resources/prompts/tasks are not exposed by this alpha; use a suitable authorized alternative or report the limitation.

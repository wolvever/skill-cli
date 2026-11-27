# Skillhub: CLI interface evaluation

Archived from the retired `~/work/skillhub` workspace after consolidation into `skill-cli`. Commands, fixture paths, and recorded-result references below describe that historical evaluation; use the current repository README for working setup and usage.

Date: 2026-11-25. This is a source-informed design assessment, not a model benchmark. The project names and source revisions below identify the checked-out code; published package versions used for help tests can differ from these checkouts.

## Recommendation

Use the explicit discover → inspect → execute shape of **mcp-cli**, the `provider.operation` addressing of **MCPorter**, and the JSON/file/stdin input and explicit JSON output of **mcpc**. Borrow **UXC**'s multi-protocol adapter model and **mcp2cli**'s schema-based parameter conversion, but keep the core Skillhub command grammar fixed for all protocols. This is a qualitative recommendation, not proof that any CLI achieves a higher agent success rate.

```sh
skillhub providers list --json
skillhub search "find issues" --limit 10 --json
skillhub describe github.search_issues --json
skillhub run github.search_issues --input '{"query":"login","limit":2}' --json
skillhub run github.search_issues --input-file ./args.json --json
skillhub run github.search_issues --input-file - --json < args.json
skillhub request github GET /repos/acme/demo --json
```

These are proposals, not implemented commands. A separate `request` escape hatch should support direct HTTP method, path, query, headers, body and approved upstream selection without exposing provider credentials to the sandbox. Do not allow agent-specified arbitrary upstreams without network policy. Pick one canonical syntax in the skill documentation; aliases can be added after usability tests. A dot selector needs a defined escaping/normalization policy for provider and operation names; never silently collapse collisions. Prefer JSON/file input as the canonical form because it preserves arrays, booleans, null and nested objects. Optional named flags can be a later convenience, not the only form.

Return JSON on stdout, diagnostics on stderr, nonzero exit status on failure, stable machine-readable error codes and recovery hints. Ensure JSON-mode errors are also machine-readable. Include upstream error details in a bounded safe form and distinguish validation failure, auth failure, unavailable provider, timeout and upstream tool `isError`. Support `--` for option-like values. Consider PowerShell, POSIX shells and Windows quoting independently. Never automatically retry a non-idempotent operation after ambiguous delivery; require explicit policy or an idempotency key.

## Comparison of main references

| Reference | Interface | Strength for an agent | Drawback or qualification |
|---|---|---|---|
| mcp-cli | `info server tool`; `grep pattern`; `call server tool JSON` | Minimal verbs, progressive discovery, stdin JSON. Source `src/commands/call.ts` handles stdin and JSON output. | Primarily MCP; `grep` is glob-based, not semantic search. `src/client.ts:356` wraps tool execution in retry logic: inspect policy before reuse. |
| MCPorter | `list server --schema`; `call server.tool key=value` | Compact selector; schema-aware argument handling and typed client generation. See `src/cli/call-command.ts`. | MCP-focused; multiple argument forms require a documented canonical grammar. |
| mcpc | `@session tools-list`, `tools-get`, `tools-call` | Strong structured MCP lifecycle, inline JSON/stdin, explicit `--json`. `src/cli/index.ts:1178`. | Session setup and protocol-specific nouns cost commands and cognitive load; argument validation errors are plain text even in JSON mode (`src/cli/index.ts:240`). |
| mcp2cli | `--spec/--mcp ... --list`, then generated operation flags | Operation-specific help, OpenAPI/MCP/GraphQL discovery; JSON listing and full MCP result envelope. | Source selection flags recur, generated grammar varies, and options can be position-sensitive. The actual test rejected `--json` after the operation; it belongs before it. |
| UXC | `<host> <operation> key=value` | Compact consistent multi-protocol idea; nested and JSON arguments; schema-aware coercion in `src/arg_coercion.rs`. | Operation IDs and types vary by provider; host-first syntax and compact argument mini-language need careful disambiguation. Not built or run in this environment. |

Examples reflect the checked source or project help. The final command choice should be tested with actual target models rather than relying on subjective syntax preferences.

## Additional cloned references

| Project and revision | What to reuse or inspect | Test status |
|---|---|---|
| mcp2cli `bed40af` | Dynamic OpenAPI operation parser and `--search`; `src/mcp2cli/__init__.py` | Local source installed; help, OpenAPI listing and invocation succeeded; see `test-results.json`. MCP stdio failed or timed out. |
| UXC `35176b1` | Multi-protocol schema normalization, `src/arg_coercion.rs`; execution architecture | Cargo could not execute installed rustc (access denied); not run. |
| MCPorter `aa8d0c5` | Selectors, schema-aware arguments and typed clients; `src/cli/call-command.ts` | Published npm package `--help` run; checkout itself not built. |
| mcpc `cdeae24` | Session lifecycle, structured results, MCP features; `src/cli/index.ts` | Published npm package `--help` run; checkout itself not built. |
| mcp-cli `d77672a` | Small discovery grammar, stdin, retry behavior; `src/commands/call.ts`, `src/client.ts` | Read source; Bun unavailable, not run. |
| mcp2cli-go `f078c6b` | Go distribution and CLI semantics, `cmd/`, `internal/` | Go toolchain download/build not completed; not run. |
| mcp-inspect `e096c6e` | Explicit schema inspection and `call --args`, `call.go` | Ran source with `go run . --help`; did not test against fixture. |
| MCP Inspector `eb54b44` | Protocol debugging and conformance test patterns, `clients/`, `test-servers/` | Source reviewed at directory/documentation level; not run. |
| Restish `79c3d88` | REST API discovery, HTTP formatting and scripting, `cmd/` | Source reviewed at directory/documentation level; not run. |
| FastMCP `f7d9a20` | MCP server/client examples and fixtures, `src/`, `examples/` | Source reviewed at directory/documentation level; not run. |
| skill-creator `f59b7d6` | CLI structure in `src/cli/main.ts`, including `commands` and legacy aliases | Source reviewed; not run. |

The input also listed openapi2cli via PyPI without a verified repository URL. It was not cloned; do not infer a repository identity from its name. Composio was discussed as a managed platform rather than a cloneable primary reference. The report does not claim comprehensive source audits of all repositories.

## Hands-on checks and reproducibility

`fixtures/` holds a deterministic stdio MCP server and local OpenAPI server; `run_fixture.py` records subprocess argv, exit status, stdout and stderr in `test-results.json`. It uses an isolated temporary user profile and does not require provider credentials. The `mcp2cli` project was installed from the cloned source into `.venv-mcp2cli`. The Python fixture requires the MCP SDK installed in that environment. Run with `python run_fixture.py` from this folder. Local loopback binding and child process spawning must be allowed.

Observed: mcp2cli help, OpenAPI JSON listing, and invocation worked (exit 0, expected JSON). The first invocation put `--json` after the dynamic command and failed argument parsing. MCP stdio attempts returned Windows error 5 or timed out, so MCP discovery/schema/invocation is unverified. The corrected run clears inherited proxy variables and isolates the home/cache directory. An additional attempt without home isolation returned the correct HTTP result but failed on a usage-cache write, and exited 1; see `openapi-invoke-result.json`. This is relevant when a tool runs inside a sandbox with restricted home directories. `mcpc` and `mcporter` were run with `--help` from npm, not against the fixture. `mcp-inspect` was run from source with `--help`. No latency, token count, agent success rate, credential security or production reliability has been measured. Failures may be environment-specific; do not infer incompatibility from them.

## Proposed model evaluation

Implement adapters/scripts that expose the same fixture through each candidate syntax. Pin repository commits and runtime versions. For each syntax, test at least two models and 30 independent tasks including discovery among distractors, exact schema inspection, nested JSON, arrays/booleans/null, Unicode, quoted paths, missing required arguments, unknown tools, large results, auth failure and a non-idempotent action. Use fresh sessions, fixed prompts and tool catalog, balanced randomized ordering and identical sandbox/shell conditions. Record tokens, subprocess attempts, wall time, stdout/stderr, exit code, calls made, and duplicate side effects. Separate correctness of the CLI from model behavior.

Primary metrics: task completion with correct arguments and side effects; unsafe or duplicate calls; median and P95 turns, tokens and time; recoverability after a structured error. Report confidence intervals and raw runs; compare POSIX and PowerShell separately. Treat ties and uncertain differences as such. Before scoring, resolve the current Windows fixture failures and get all candidates running on the same workload; otherwise the evaluation would confound command format with installation or protocol differences.

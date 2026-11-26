# Hands-on evaluation: MCP/API CLIs

Date: 2026-11-27 (Asia/Shanghai). Scope: the top three in the supplied research's **final ranking**: UXC, mcpc, and MCPorter. Twelve reference projects were cloned into `work/gh/mcp tools/`; their default-branch SHAs are recorded in [repositories.json](repositories.json).

## Result

| Rank | Tool tested | Agent usability /10 | Maturity /10 | Equal-weight overall /10 |
| --- | --- | ---: | ---: | ---: |
| 1 | mcpc 0.8.0 | 9.0 | 8.5 | **8.8** |
| 2 | MCPorter 0.17.1 | 8.6 | 8.6 | **8.6** |
| 3 | UXC 0.23.1 | 7.9 | 7.6 | **7.8** |

These are reasoned engineering assessments informed by small reproducible trials, not statistically measured reliability or production certification. Maturity has lower confidence than usability: repository/CI inspection and documented features complement the hands-on tests, but this was not a security audit or long-running soak test. Stars do not determine the score.

mcpc is the first choice for an agent that primarily needs to operate MCP servers. MCPorter is particularly attractive for a host embedding TypeScript or generating typed clients. UXC is the best multi-protocol architecture reference of the three, but its successful exit status on a failed MCP tool is a meaningful scripting concern.

## Method and reproducibility

The host was Windows amd64. mcpc and MCPorter ran using an isolated Node **24.22.3** installation. UXC's current published binaries cover Linux/macOS, so its Linux musl binary ran under Ubuntu WSL. Python 3.11 supplied a dependency-free fixture on Windows; Python 3 supplied the same fixture under WSL. Package installs and runtime files stay in the research directory, not in this Go project's runtime dependencies.

Latest default-branch code was cloned, and latest published packages/binaries were exercised. **The published artifacts are not claimed to be builds of every cloned HEAD.** For example, mcpc HEAD `cd060046f3b7` includes changes after the v0.8.0 tag `5d13b250d950`; MCPorter HEAD is `aa8d0c5ac6cd` while its v0.17.1 tag object is `5c0fe8c649c1`. UXC HEAD `35176b115023` is the v0.23.1 release preparation commit. This separation makes the tested artifact versions explicit rather than attributing untested HEAD fixes to a package.

The local research directory contains `evaluation/fixture.py`, `run_trials.py`, `followup_trials.py`, `openapi_trials.py`, `collect_metadata.py`, the original outputs, configs, and generated TypeScript client. Sanitized copies of the 73 recorded command results are [trials.json](trials.json), [followup-trials.json](followup-trials.json), and [openapi-trials.json](openapi-trials.json). Workstation paths have been replaced in these committed copies; originals remain locally.

Workflows covered help, discovery, schema inspection, nested JSON, invalid arguments, a tool returning `isError`, repeated calls, persistent-session status, HTTP MCP, stdin variants, resources/prompts where exposed, and TypeScript client generation. UXC additionally called an OpenAPI operation. OAuth consent, enterprise auth, legacy SSE in these reference clients, large payloads, cancellation, crash recovery, 100 concurrent calls, and 10,000-tool catalogs were **not** benchmarked.

## Observations

| Trial | mcpc | MCPorter | UXC |
| --- | --- | --- | --- |
| MCP stdio discovery and nested JSON call | Passed | Passed | Passed |
| Streamable HTTP discovery/call | Passed | Passed | Passed |
| Retrieve one tool schema | `tools-get echo` | `list fixture.echo --schema --json` | `<host> echo -h` |
| Invalid arguments | Exit 2, JSON diagnostic on stderr | Exit 1, JSON diagnostic on stdout | Exit 1, JSON error envelope on stdout |
| MCP `isError: true` | Exit 2; result preserved | Exit 1; result preserved | **Exit 0, top-level `ok:true`; nested `isError:true`** |
| Stdin form tested | Bare piped JSON accepted | `--args -` accepted | Bare piped JSON was not consumed as arguments |
| Persistent connection evidence | Named bridge used for sequential calls | Connected server shown by daemon status after keep-alive calls | Daemon reported reused stdio/HTTP sessions and reuse hits |
| Extra working capability | Resource read and prompt retrieval | Resource read and typed client generation | OpenAPI discovery/schema/call |

**mcpc:** predictable session commands, searchable tools, single-tool inspection, useful errors, and correct failure exit status make it easy to operate from an agent shell. `tools-list --json` includes full schemas, so an agent should prefer targeted search and `tools-get`. Its documented OAuth, credential storage, proxy, tasks, resources, and recovery behavior make it a strong MCP subsystem reference, although only basic sessions/resources/prompts were exercised here. [Repository](https://github.com/apify/mcpc), [reference](https://github.com/apify/mcpc/blob/main/docs/REFERENCE.md).

**MCPorter:** its selector syntax, single-tool schema listing, JSON calls, stdin mode, resource reads, and generated TypeScript client all worked. Keep-alive mode worked after adding `"lifecycle":"keep-alive"` to the fixture config; ordinary one-shot calls should not be mislabeled as daemon benchmarks. It required upgrading the local Node 22 runtime to meet the package's Node >=24 declaration. Default JSON listing included schemas and CLI-option metadata; targeted listing reduces the returned catalog. [Repository](https://github.com/openclaw/mcporter), [daemon design](https://github.com/openclaw/mcporter/blob/main/docs/daemon.md).

**UXC:** JSON is the default and host help returns compact operation summaries. Scalar `a=2 b=3`, nested JSON, HTTP MCP, daemon reuse, and OpenAPI invocation worked. An MCP tool error was wrapped as a successful invocation with exit 0, so a script must inspect nested `data.isError`. The published release lacks a Windows binary; the WSL route adds installation friction on this host. Its much smaller community lowers confidence in independent deployment coverage rather than proving lower code quality. [Repository](https://github.com/holon-run/uxc), [MCP implementation notes](https://uxc.holon.run/protocols/mcp/).

Two harness corrections are retained in the evidence. First, compact numeric JSON passed through Windows `wsl.exe` lost quoting; JSON with spaces and documented scalar arguments both worked. The initial failures are **not** counted as UXC argument-parser defects. Second, the first HTTP fixture answered MCP requests on every POST path, causing protocol autodetection to correctly see MCP even at its OpenAPI URL. A REST-only fixture on a separate port plus `--schema-url` produced successful OpenAPI discovery, description, and execution. Initial OpenAPI failures are likewise excluded from scoring.

## Scoring rubric

Each dimension is worth 0–2 points: 0 absent/blocking, 1 partial/operational friction, 2 strong within the evaluated scope. Fractional scores express judgment, not measurement precision.

| Agent usability dimension | mcpc | MCPorter | UXC |
| --- | ---: | ---: | ---: |
| Installation and first successful call on this host | 1.8 | 1.6 | 1.4 |
| Discovery and focused schema retrieval | 1.8 | 1.7 | 1.9 |
| Structured output and argument fidelity | 1.8 | 1.8 | 1.8 |
| Composition, stdin, and connection reuse | 1.9 | 1.8 | 1.6 |
| Error semantics and recovery guidance | 1.7 | 1.7 | 1.2 |
| **Total** | **9.0** | **8.6** | **7.9** |

| Maturity dimension | mcpc | MCPorter | UXC |
| --- | ---: | ---: | ---: |
| Test/CI breadth visible in repository | 1.8 | 1.8 | 1.6 |
| Session lifecycle and recovery design | 1.8 | 1.8 | 1.7 |
| Documented authentication/security mechanisms | 1.8 | 1.7 | 1.5 |
| Distribution and runtime portability | 1.5 | 1.6 | 1.3 |
| Release/documentation and maintenance evidence | 1.6 | 1.7 | 1.5 |
| **Total** | **8.5** | **8.6** | **7.6** |

GitHub metadata at collection: mcpc 1,118 stars, MCPorter 5,144, UXC 120; all unarchived and pushed during November 24–26, 2026. mcpc is Apache-2.0; MCPorter and UXC are MIT. Recent pushes and repository size are context, not evidence of production reliability. The cloned commits provide reproducible source snapshots even if these counts change.

## Timing observations

Five successful sequential reused-connection invocations per tool:

| Tool | Host path | Median elapsed | Range |
| --- | --- | ---: | ---: |
| mcpc | Windows Node + persistent session | 2,308 ms | 2,025–2,545 ms |
| MCPorter | Windows Node + keep-alive daemon | 1,842 ms | 1,533–2,119 ms |
| UXC | WSL Linux binary + daemon, scalar arguments | 362 ms | 328–436 ms |

These include shell/process startup and local filesystem/environment overhead. They mix operating systems/runtimes and contain only five samples, so they **must not be read as a fair cross-platform performance ranking, P95/P99 estimate, or server execution benchmark**. Performance did not determine the usability/maturity ranking. Use the broader workload matrix from the original research before making production sizing decisions.

## Choices implemented in skill-cli

1. UXC-inspired adapters behind a common discover/describe/invoke boundary, with compact schema-free discovery and on-demand OpenAPI schema bundling.
2. mcpc-inspired persistent ownership of MCP sessions in a gateway, explicit operation allowlists, and nonzero exits for tool execution failures.
3. MCPorter-inspired separation between CLI and invocation runtime. Typed client/code generation remains future work; the current Go runtime is internal and its API is not yet a stability promise.
4. Small `list/search/describe/call` command vocabulary, plus direct HTTP `request` and optional `serve`.
5. Environment credential bindings, same-origin forwarding, no implicit shell, no automatic replay, and explicit deadlines.

The implementation intentionally starts with working MCP + HTTP/OpenAPI capabilities. The README lists protocol and security features that remain unimplemented. It should be evaluated as an alpha, independently of the mature reference tools' scores.

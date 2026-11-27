# Skillhub comparison and integration

Source reviewed: `~/work/skillhub` (`README.md`, `EVALUATION.md`, `run_fixture.py`, the MCP/HTTP/OpenAPI fixtures, and recorded outputs). Destination: `wolvever/skill-cli`. The source directory has no Git repository, service, CLI binary implementation, or Go module. Its README explicitly identifies a proposal; its executable code is a Python evaluation harness and fixtures. There was no separate implementation to merge by Git history.

The final project and executable name remains **skill-cli**. The source evaluation is preserved; no files in the original Skillhub workspace were changed.

| Useful source idea | Existing skill-cli behavior | Integrated result |
| --- | --- | --- |
| Fixed discover → inspect → execute grammar | `list/search/describe/call` already implemented | Retained the established grammar; did not add competing `providers list` / `run --input` aliases |
| Compact `provider.operation` addressing | First-dot selector and validated provider names | Documented literal operation names; tested dotted IDs and similarly named operations without normalization |
| Bound discovery output | Search returned every match | Added `search --limit N`, deterministically sorted, including through the gateway |
| Machine-readable error codes and recovery hints | Readable error plus status only | Added additive `code` / `hint`, preserving old `error` text, data, exit behavior, and gateway error identity |
| HTTP escape hatch with query/header/body options | Method/path and object body | Added repeated encoded query values, non-credential headers, and any JSON body through direct and gateway modes |
| Preserve arrays, booleans, null, nested data | Call arguments preserved nested JSON; OpenAPI bodies restricted to objects | Extended HTTP and schema-validated OpenAPI bodies to arrays/scalars/null, preserving large integer literals |
| Prevent arbitrary upstream/credential selection | Configured base URL, same-origin credential forwarding, allowlist enforcement | Retained those controls; blocked request routing/framing and known credential header overrides |
| Windows/sandbox failures from cache writes | No one-shot usage cache | Added an executable command test with home/cache paths pointing to a file; successful calls do not attempt cache writes |
| Deterministic issue-search target across protocols | Separate generic demo targets | Ported the issue-search behavior to the Go demo, retained the original OpenAPI fixture as test data, and asserted MCP/OpenAPI result parity |
| No replay after uncertain mutations | No automatic operation retries | Added a side-effect counter test: a tool failing after its write executes once |
| Model benchmark design separated from client correctness | Existing 40-task native/CLI benchmark | Kept the benchmark and historical results; did not relabel earlier environment failures as compatibility findings |

The integration uses the source's design and fixture contract rather than copying third-party reference implementations. MCP resources/tasks/subscriptions, OAuth, more protocols, a multitenant control plane, and SDK generation remain separate future work.

Validation covers actual CLI commands, MCP/HTTP/OpenAPI equivalence, bounded discovery, typed errors, gateway preservation, denied routing/credential overrides, typed JSON bodies, Unicode/quoted strings, restricted home/cache access, and mutation replay prevention. The compiled smoke suite now has 26 cases. The README is the maintained purpose and usage entrypoint; the earlier benchmark report is explicitly pinned to its pre-merge baseline.

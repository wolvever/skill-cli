# Ten MCP servers: local ranked discovery

Measured 2026-11-27 on Windows amd64, Go 1.25.0, using the compiled executables and official Go MCP SDK. [Catalog](artifacts/catalog-10.json), [per-query results](artifacts/ranked-10.json), [tasks](tasks_10.py), [retrieval design](../docs/research/lightweight-retrieval.md).

## Coverage and verification

**10 live public MCP servers, 37 MCP tools; two OpenAPI sources, nine allowed operations; 46 operations total.** Native SDK and CLI discovery IDs matched for every provider. All discovery was anonymous at capture time. This verifies metadata compatibility, not every tool's execution, uptime or anonymous invocation quota.

| MCP server | Endpoint | Captured tools |
| --- | --- | ---: |
| DeepWiki | `https://mcp.deepwiki.com/mcp` | 3 |
| Microsoft Learn | `https://learn.microsoft.com/api/mcp` | 3 |
| Cloudflare Docs | `https://docs.mcp.cloudflare.com/mcp` | 2 |
| Context7 | `https://mcp.context7.com/mcp` | 2 |
| grep.app | `https://mcp.grep.app` | 1 |
| Svelte | `https://mcp.svelte.dev/mcp` | 4 |
| Nuxt | `https://nuxt.com/mcp` | 13 |
| Astro | `https://mcp.docs.astro.build/mcp` | 1 |
| Hugging Face | `https://huggingface.co/mcp` | 4 |
| Cloudflare Blog | `https://blog.mcp.cloudflare.com/mcp` | 4 |

Petstore contributes five allowed OpenAPI operations; APIs.guru contributes four. The initial Nuxt hostname guess failed; its documented `nuxt.com/mcp` endpoint succeeded. That failed attempt is preserved in [probe receipts](artifacts/server-probes-10.json); it is not counted as another server. Official endpoint documentation is linked in [sources.json](sources.json) and [README](README.md).

## Retrieval quality

The expanded corpus has **80 tasks**: 23 exact-name, 29 natural requests, eight paraphrases, five keyword, four multi-tool and 11 unsupported-action tasks. Gold IDs are manually authored from server contracts. The 40 new tasks were written before evaluating the ranker. This remains an author-labeled development corpus, with no independent adjudication or held-out generalization claim. Some services offer overlapping alternatives; labels are the pilot's required IDs, not an exhaustive set of equivalent workflows.

Two query styles are measured separately: the complete user request and a hand-prescribed short search probe. The latter is not an agent-generated query. All arms use the same frozen descriptions/schemas, with no external operations executed during replay.

**Full user request**:

| Candidate strategy | Micro candidate precision | Required-ID recall | Positive MRR | Mean response tokens |
| --- | ---: | ---: | ---: | ---: |
| Legacy substring, up to 5 | Undefined: no candidates | 0% | 0 | 8 |
| Ranked, top 1 | 71.25% | 78.08% | 0.8261 | 52.2 |
| Ranked, top 3 | 30.08% | 97.26% | 0.9010 | 138.0 |
| Ranked, top 5 | 18.70% | 98.63% | 0.9010 | 224.7 |
| Ranked, top 10 (CLI default) | 9.69% | 98.63% | 0.9010 | 444.4 |
| Eager full catalog | 1.98% | 100% | Not applicable | See schema tokens below |

Precision counts **all returned candidates**, including false candidates on negative tasks; its denominator is actual returned count, not a padded fixed k. Recall is micro-averaged over required IDs on positive tasks. There are 73 required IDs across 69 positive tasks. Top five retrieved 72/73; the missed task paraphrased a Petstore lookup as “animal record ... numeric identifier.” No benchmark-specific synonym mapping was added. MRR uses the first relevant result on positive tasks; it does not verify all dependencies in a multi-tool workflow.

**Prescribed short probes**, top five:

| Candidate strategy | Micro candidate precision | Required-ID recall | Negative abstention |
| --- | ---: | ---: | ---: |
| Legacy substring | 50.00% | 47.95% | 90.91% |
| Ranked | 26.21% | 89.04% | 45.45% |

The full-request substring stress test is unsuitable usage of the legacy interface. The prescribed-query comparison is the more realistic baseline. Ranking improves recall while exposing more false candidates; it does not improve every precision measure. Full natural requests supply additional vocabulary and performed better for recall than these short probes.

**Important failure mode:** all 11 unsupported full-request prompts produced ranked candidates, so retrieval abstention was **0%**, compared with the substring arm's 100% abstention caused by returning nothing for every prompt. A documentation tool about deployment cannot deploy a Worker. Search is a candidate generator, not a permission check or final tool selector. Inspect `describe`, reject unsupported actions, and measure final model decisions separately.

Native SDK ranked search and CLI ranked search returned the same ordered IDs for **160 queries** (80 tasks × two styles), yielding identical retrieval metrics. They share the ranking implementation; this controls interface/transport differences and is not independent validation of the ranker. A native MCP integration can use the same progressive strategy.

Removing provider summaries while retaining tool descriptions and argument metadata reduced full-request top-five recall from **98.63% to 97.26%**, and MRR from **0.9010 to 0.8913**. This is one additional required ID in a small pilot, not evidence of a statistically reliable gain. Public metadata comes from service summaries; skill frontmatter ingestion is verified with isolated fixtures rather than fabricated vendor skills. No gold IDs or task-specific synonym lists are placed in provider metadata.

## Token components

Tokenizer: `o200k_base`. The 46 native function schemas occupy **12,055 tokens**. CLI skill instructions occupy **1,064 tokens**; CLI bootstrap including common system text, shell definition and provider listing occupies **1,542 tokens**.

| Exposure per task | Mean visible component tokens |
| --- | ---: |
| Native eager schemas + common system text + user prompt | 12,141.4 |
| CLI ranked top 3, including bootstrap, prompt, search request/result | 1,717.8 |
| CLI ranked top 5, same accounting | 1,804.5 |
| CLI ranked top 10, same accounting | 2,024.2 |
| CLI ranked top 5 plus a request/result describing its first candidate | 2,165.7 |

Top-five exposure is **85.1% lower** than eager exposure for those counted components. This is not billed savings, a complete conversation estimate or a guarantee of preserved model accuracy. The describe row counts the top candidate on every task; it does not assume that candidate is correct or that a single describe suffices. Model-specific framing, repeated conversation input, additional inspections, reasoning and final output are excluded. Native progressive can use identical ranking and compact results; these reductions come from progressive exposure and compaction, not intrinsically from shell syntax. Native ranked token rows use the same compact projection for this reason; the metadata-ablation arm keeps its native full descriptions and is used for retrieval metrics only.

Listing each provider's tools used **2,126 tokens** total in compact mode versus **8,346** with `--full`: **74.5% fewer**. Descriptions are limited to 180 runes, and annotations are omitted; no schemas appear in either list mode. For Nuxt alone, 13 tools took 734 versus 3,522 tokens. Complete descriptions and schemas remain available through `describe`.

**Actual model selection precision/recall, provider-reported token usage and dollar cost remain unmeasured (`null`).** No paid or local model was run. The model runner can use the expanded snapshot with the same model in all three arms; do not substitute retrieval scores for its final selection accuracy.

## Lightweight runtime measurements

The implementation uses in-memory Go BM25 and a conservative trigram fallback. No model assets, index-building command, search service or database are required. Provider configuration/network access still apply.

A short `BenchmarkRankedIndex` run on an Intel i5-1335U measured synthetic short documents, index construction **plus one matching lexical query**:

| Documents | Time / operation | Allocated bytes / operation |
| --- | ---: | ---: |
| 100 | 0.745 ms | 270,490 |
| 1,000 | 8.19 ms | 2,667,879 |
| 10,000 | 84.72 ms | 31,247,210 |

These are synthetic indexing costs, not 10,000 real MCP tools, peak RSS or fuzzy-query measurements. The 10,000-document run had only three iterations; use longer runs for production capacity planning. The actual 46-operation local replay CLI search measured P50 **256.2 ms**, P95 **354.2 ms**, including process startup, config, 12 local MCP connections/catalog transfers and indexing. Other local validation began during the run, so treat latency as an indicative observation, not a controlled performance comparison. Public network latency is excluded. Gateway sessions avoid reconnection, but live discovery and index building still occur each search.

## Reproduce

Build `bin/skill-cli` and `bin/mcp-peer` (`.exe` on Windows), install the benchmark requirements, then:

```sh
python benchmarks/ranked.py                  # frozen replay, no upstream calls
python benchmarks/ranked.py --capture        # refresh public metadata first
python -m unittest discover -s benchmarks -p 'test_*.py' -v
go test -run '^$' -bench BenchmarkRankedIndex -benchtime 200ms -benchmem ./internal/capability
python benchmarks/model_runner.py --snapshot benchmarks/artifacts/catalog-10.json --model YOUR_MODEL --task-limit 80
```

The model command requires an already configured endpoint/key or local model and can incur charges; it is provided for reproduction and was not run for these results. The earlier [three-server baseline](RESULTS.md) and artifacts are preserved as historical measurements.

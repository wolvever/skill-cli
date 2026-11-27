# Native MCP versus skill-cli benchmark

This suite compares capability discovery and selection with the same tool catalog. It separates three questions:

1. Does the interface expose the right candidates (retrieval precision/recall)?
2. Does the **same model** select the right capabilities (model precision/recall)?
3. How many tokens are visible or billed, including instructions and discovery overhead?

The current [ten-server results](RESULTS-10.md) cover **10 MCP servers, two OpenAPI sources, 46 operations and 80 tasks**, with local BM25 ranking, a metadata ablation, compact listings and an SDK/CLI parity control. The [three-server results](RESULTS.md) remain a historical baseline. A transport smoke test is not scored as model accuracy.

Run `python benchmarks/ranked.py` against the checked-in `artifacts/catalog-10.json`; add `--capture` to refresh public metadata. This is the current expanded benchmark. The original `run.py`, `tasks.py` and older artifacts retain the 40-task substring experiment. That script passes explicit legacy matching flags to the current CLI; its original measurements are pinned to the commit named in RESULTS.md.

## Public services and provenance

| Service | Source | Coverage |
| --- | --- | --- |
| DeepWiki | [Vendor docs](https://docs.devin.ai/work-with-devin/deepwiki-mcp), `https://mcp.deepwiki.com/mcp` | 3 public repository documentation tools |
| Microsoft Learn | [Vendor docs](https://learn.microsoft.com/en-us/training/support/mcp), `https://learn.microsoft.com/api/mcp` | 3 documentation/code retrieval tools |
| Cloudflare Docs | [Vendor repository](https://github.com/cloudflare/mcp-server-cloudflare), `https://docs.mcp.cloudflare.com/mcp` | 2 documentation tools |
| Context7 | [Vendor repository](https://github.com/upstash/context7), `https://mcp.context7.com/mcp` | 2 library resolution/documentation tools |
| grep.app | [Vendor introduction](https://vercel.com/blog/grep-a-million-github-repositories-via-mcp), `https://mcp.grep.app` | 1 public code search tool |
| Svelte | [Official setup](https://svelte.dev/docs/ai/remote-setup), `https://mcp.svelte.dev/mcp` | 4 documentation, diagnostics and Playground tools |
| Nuxt | [Official setup](https://nuxt.com/docs/4.x/guide/ai/mcp), `https://nuxt.com/mcp` | 13 docs, modules, release, blog, hosting and contributor tools |
| Astro | [Official setup](https://docs.astro.build/en/guides/build-with-ai/), `https://mcp.docs.astro.build/mcp` | 1 documentation search tool |
| Hugging Face | [Official setup](https://huggingface.co/docs/hub/en/hf-mcp-server), `https://huggingface.co/mcp` | 4 repository/identity tools |
| Cloudflare Blog | [Vendor repository](https://github.com/cloudflare/mcp-server-cloudflare), `https://blog.mcp.cloudflare.com/mcp` | 4 article/tag tools |
| Swagger Petstore | [Publisher UI/spec](https://petstore3.swagger.io/) | 5 allowlisted read-only OpenAPI 3.0 operations |
| APIs.guru | [Publisher spec](https://api.apis.guru/v2/openapi.yaml) | 4 allowlisted read-only OpenAPI 3.0 operations |
| Open-Meteo | [Publisher elevation spec](https://github.com/open-meteo/open-meteo/blob/main/openapi/elevation.yml) | Compatibility-only exclusion: OpenAPI 3.1 is unsupported by this alpha |

`specs/` contains unmodified publisher documents. Petstore is Apache-2.0, APIs.guru's spec is CC0-1.0, and Open-Meteo's specification identifies CC-BY-4.0. Keep the source/attribution in those files. [sources.json](sources.json) and the catalog snapshot contain source URLs and SHA-256 hashes. Do not silently convert an unsupported specification and then report it as native compatibility.

## Build and run without a model key

From the repository root, build the two executables. On Windows append `.exe` to both output names:

```sh
go build -o bin/skill-cli ./cmd/skill-cli
go build -o bin/mcp-peer ./benchmarks/cmd/mcp-peer
python -m pip install -r benchmarks/requirements.txt
python benchmarks/run.py --capture
python benchmarks/public_probes.py
python -m unittest discover -s benchmarks -p 'test_*.py' -v
```

`--capture` contacts the public services once to freeze the catalogs and checks native/CLI ID parity. A normal `python benchmarks/run.py` reuses the frozen snapshot and makes **no upstream service calls**: a local MCP replay server serves the captured descriptions and schemas to both implementations. Invocation through replay is disabled. Public availability checks use `public_probes.py` separately, never during CI. Do not create shared Petstore records just to make a missing-ID read pass.

The Go peer talks directly to public MCP servers through the official Go MCP SDK. OpenAPI has no intrinsic MCP interface, so those operations go through an in-memory MCP bridge with the **same OpenAPI mapper** used by skill-cli. This controls operation translation, not independent adapter correctness. It is not a model vendor's hosted-MCP product benchmark. The original public adapters are exercised by the live probes.

## Corpus and retrieval metrics

[tasks.py](tasks.py) holds 40 manually labeled tasks: 16 exact-name, 5 keyword, 8 paraphrase, 4 multi-capability, and 7 unsupported-operation/abstention tasks. Gold labels are capability IDs needed to perform the requested action; a documentation search about deployment is not a deployment operation. Labels and prompts are public and were authored for this pilot, not held out or independently adjudicated. Generalization claims require a larger independently labeled set.

For returned candidates `C` and gold required IDs `G`:

- Precision = `|C ∩ G| / |C|`.
- Positive-task recall = `|C ∩ G| / |G|`.
- Micro metrics sum true/false positives and false negatives before dividing.
- Negative tasks have no recall denominator; report abstention separately.
- Empty selection on a negative task is correct; empty selection on a positive task has recall zero. Undefined micro precision (no candidates at all) is `null`.

Retrieval **context relevance** treats every exposed candidate as a candidate, so the eager baseline has full context recall and low relevance precision by construction. This is **not the precision of a model choosing from that context**. Native eager's candidate-set exact-match score is not agent accuracy and should not be used as a quality ranking.

Measured retrieval arms:

- `native_eager`: expose every complete tool schema.
- `native_progressive_prescribed`: use the native peer's substring search with a prescribed query.
- `cli_prescribed`: run the actual CLI with the same prescribed query.
- `cli_literal`: feed the complete natural-language task to the CLI's substring search.
- `cli_literal_list_fallback`: if that search is empty, list every provider's tools.

`task.query` is a hand-prescribed retrieval probe, **not a model-generated query and not free semantic search**. The model runner never receives it or the gold labels. Native progressive and CLI prescribed share substring semantics; exact candidate parity is asserted. The literal arm is a stress test of inappropriate natural-language use, not the skill's recommended workflow.

## Token component measurements

The no-key run counts canonical JSON/text components with `tiktoken`'s `o200k_base`, recording package versions. It includes the common system instructions and task, eager schemas or progressive router/shell schemas, the CLI skill and provider listing, and prescribed search requests/results. It does not count a model-specific chat/tool framing format, repeated full-conversation billing, describe followups, or final model output. Counts are exact **for those serialized components and that encoding**, not provider-reported usage or dollars. Progressive native has its own small router instructions; the CLI arm pays for the entire installed skill. These are different integration styles, not transport-only byte comparisons.

The synthetic 170/1,700-tool scaling rows replicate captured schema definitions with distinct names. They demonstrate schema-size scaling only; they do not represent additional real integrations or measure quality at scale.

## Paired model-selection runner

Configure an existing OpenAI-compatible Chat Completions endpoint through `OPENAI_API_KEY` and optional `OPENAI_BASE_URL`, or a local endpoint. Do not paste credentials into committed files. Choose the model explicitly:

```sh
python benchmarks/model_runner.py --model YOUR_MODEL --task-limit 40 --repeats 3
```

For a local compatible model:

```sh
python benchmarks/model_runner.py --model YOUR_LOCAL_MODEL --base-url http://127.0.0.1:1234/v1
```

All three arms use the same model, prompts, frozen catalog, output limit, and turn limit. Every task/repeat starts fresh; arm order is randomized with a recorded seed. The model selects IDs; external tool actions are disabled:

- **Native eager:** register all MCP schemas as ordinary model function tools; `tool_choice=none` allows only the final selection JSON. Schemas remain visible.
- **Native progressive:** register discovery/description functions that call the official-SDK peer.
- **Skill CLI:** provide the full generated skill and one restricted argv-based shell tool that actually executes `skill-cli list/search/describe`. Config overrides, arbitrary shell commands, and operation invocation are blocked. This does not test arbitrary shell composition or code-mode filtering.

The runner grades final `selected_tools` against gold IDs, reporting micro/macro precision/recall, exact set match, negative abstention, and valid-output rate. Invalid JSON and turn-limit failures count as failures, including on negative tasks. All attempted model turns and tool observations are retained. It records provider-reported input, cached input, output, and reasoning token usage; it does not substitute tokenizer estimates if usage is missing. Account-invoiced charges can still differ due to provider pricing/tiering. Failed API requests may have unknown billed usage and must not support a complete-cost claim.

For the expanded corpus use `--snapshot benchmarks/artifacts/catalog-10.json --task-limit 80`; the runner selects the 80-task corpus for that snapshot. Current native progressive and CLI search both use local ranked retrieval with a limit of 10; native progressive retains full descriptions while the CLI defaults to compact output. This is an integration comparison, not an output-identical transport benchmark. The no-key ranked benchmark asserts ordered-ID parity and uses an identical compact projection for native token comparisons. It also reports metadata ablation and unsupported-action candidates. No expanded model-inference results are checked in.

For dollar accounting, supply verified rates for the selected model/tier:

```sh
python benchmarks/model_runner.py --model YOUR_MODEL --task-limit 12 \
  --input-usd-per-million INPUT_RATE \
  --cached-input-usd-per-million CACHE_RATE \
  --output-usd-per-million OUTPUT_RATE --budget-usd 5
```

Replace the rate placeholders with numbers. Cost is `(uncached_input × input_rate + cached_input × cache_rate + output × output_rate) / 1,000,000`. The admission check reserves a conservative upper estimate before each request; it is not an account-level hard spending limit. Partial episodes retain known usage when the budget stops a run. Budget-stopped episodes are reported as incomplete coverage rather than scored as model mistakes. No model request is automatically retried. A failed endpoint response or missing usage is recorded as an error, not silently excluded; unknown usage stops further requests so it cannot be presented as a complete-cost comparison. When rates are omitted, dollar fields stay `null`.

Matched task/repeat pairs enter comparisons; repeats are averaged per task before a paired bootstrap 95% interval. Report completed/planned episodes so budget exhaustion cannot hide missing comparisons. Start with a pilot; the full design is 40 tasks × 3 arms × 3 repeats = **360 episodes**, potentially several model requests per episode.

## What this pilot cannot establish

Capability selection is distinct from executing a workflow and giving a correct grounded answer. The live probe's nonempty text/domain checks are deliberately basic, not semantic answer graders. For an end-to-end extension, add frozen tool outputs, independent gold answer facts, typed argument checks, multi-call dependency tasks, and claim-level answer precision/recall. Report availability failures, stale schemas, unknown execution outcomes, and client bugs separately. Hold session policy and result filtering constant, then ablate eager versus progressive exposure and code filtering independently. This prevents attributing progressive loading or server-side processing gains to the CLI syntax alone.

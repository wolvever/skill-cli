# Local retrieval with no additional setup

Reviewed 2026-11-27. Here, **no setup** means the existing binary and provider configuration suffice: no search service, model weights, paid API, separate database installation, or index-building command. Provider network access and the runtime of a configured stdio server still apply. This is retrieval for tool discovery, not a full answer-generating RAG system.

| Method | Additional runtime setup | Advantages | Tradeoffs | Decision |
| --- | --- | --- | --- | --- |
| Substring | None | Tiny, predictable exact phrase lookup | Word order, inflections and natural requests frequently miss | Keep as compatibility mode |
| TF-IDF / cosine | None if implemented in Go | Simple sparse retrieval | Requires normalization; does not directly account for term saturation like BM25 | Reasonable alternative, not separately benchmarked |
| BM25 | None if implemented in Go | Term rarity, saturated frequency and length normalization; inspectable | Lexical overlap required; does not understand negation or intent | Implement now |
| Character trigrams | None | Small spelling errors can still retrieve candidates | Can match unrelated words; costly if applied to every query | Conservative fallback only |
| SQLite FTS5 | No separate service when embedded | BM25, weighted columns, snippets, trigram tokenizer and persisted indexes | Requires an SQLite driver/build configuration and database/index lifecycle in Go | Consider if catalog persistence becomes necessary |
| Bleve | Embedded Go library; no external service | Rich text indexing and analyzers | Adds dependencies and index lifecycle beyond this small corpus | Consider for much larger catalogs |
| Small local embeddings | Model asset and inference runtime, even if bundled | Can bridge synonyms and languages | Download/binary/memory costs, model versioning and cold startup | Optional future backend; fails the strict no-model requirement |
| Hosted embeddings/reranker | Credentials, endpoint and network | Semantic matching | Setup, privacy, availability and request costs | Optional future backend |

BM25 uses term frequency, document frequency and length normalization; see the [Stanford IR textbook](https://nlp.stanford.edu/IR-book/html/htmledition/okapi-bm25-a-non-binary-model-1.html). [SQLite FTS5](https://www.sqlite.org/fts5.html) supplies BM25, column weights, highlighting/snippets and a trigram tokenizer. [Bleve](https://github.com/blevesearch/bleve) is an embedded Go search library. These alternatives were researched, not installed or comparatively benchmarked here. The selection above is an architectural assessment, not a measured claim that our implementation outperforms them.

## Implemented search

The ranker is Go standard-library code. Existing YAML support is reused only when a configured skill file is read. It builds an in-memory index from the currently allowed discovered operations for each request; there is no home-directory scan, persistent cache, training or additional service.

1. Full operation ID or exact unqualified operation name is the strongest match. Ambiguous unqualified names may return multiple providers. Invocation names are preserved literally.
2. Split camelCase, acronyms, underscores and punctuation; lowercase Unicode letters; remove a fixed set of common English query words; apply a small English plural fold. This is not multilingual segmentation or a full stemmer.
3. Index weighted fields: ID **4**, tool description **1**, provider description **0.6**, keywords **0.8**, skill descriptions **0.8**, input property names/descriptions **0.35**. Input text is bounded to 128 properties, depth 4, 256 runes per property description and 1 MiB serialized schema. Nested arrays and schema references are not expanded for search. These are initial documented heuristics, not learned/tuned relevance probabilities.
4. Sum positive-IDF BM25 contributions, with `k1=1.2`, `b=0.75`, over weighted term counts. This combines field-weighted term frequencies and lengths; it is not a complete BM25F implementation. Ties use IDs for deterministic ordering.
5. If there are **no** lexical hits, use padded character-trigram Jaccard similarity for words of at least four runes, with threshold **0.5**. Fallback ranks candidates; it does not correct the query or guarantee semantic relevance.
6. Return the top 10 by default. List/search return at most 180 description runes, omitting schemas and annotations. `--full` restores full descriptions and annotations; `describe` always returns complete available operation metadata.

An empty query enumerates tools in ID order, subject to `--limit`. An all-stopword query returns no matches. `--match substring --limit 0 --full` reproduces the legacy substring behavior. Provider listing reads config and optional skill frontmatter without connecting. Tool search/list still fetch catalogs; a small model-visible result does not mean a small upstream transfer. The gateway reuses sessions, but currently rebuilds the local search index and rediscovers live MCP catalogs for each search.

## Skill descriptions and policy

Optional provider metadata:

```json
{
  "providers": {
    "docs": {
      "type": "mcp",
      "transport": "http",
      "url": "https://example.com/mcp",
      "description": "Official framework documentation",
      "keywords": ["routing", "rendering"],
      "skill_files": ["./skills/framework/SKILL.md"]
    }
  }
}
```

`skill_files` accepts files or skill folders relative to the config. Only explicitly configured YAML frontmatter names/descriptions are indexed, with a 64 KiB bound. Bodies/scripts are neither indexed nor executed. Inline `skill_descriptions` can be used instead. Broad provider-level metadata applies to every allowed operation, which can reduce precision; keep operation-specific information in tool descriptions. Benchmark provider summaries describe public services; they do not contain gold task IDs or query-specific synonyms. The public corpus does not fabricate vendor skill files; real frontmatter ingestion is tested with isolated fixtures.

Allowlists are enforced before indexing and independently before calls. Search scores, descriptions and MCP annotations do not authorize actions. Lexical retrieval cannot reliably distinguish “deploy a Worker” from “find documentation about deploying a Worker,” so an agent must inspect the full contract before deciding to call. Unsupported-operation false candidates are reported separately in the [expanded results](../../benchmarks/RESULTS-10.md).

## Further options

If separate lexical and semantic retrievers are eventually added, [reciprocal rank fusion](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/reciprocal-rank-fusion) can combine result orders without calibrating their raw scores. RRF is not implemented here. First collect independent labels, alternative valid tool sets, multilingual queries and real agent-generated discovery traces. Do not add hand-written mappings from the benchmark's paraphrases to gold tools or treat this development corpus as a holdout.

For thousands of mostly unchanged tools, a versioned persistent index or in-memory per-session cache could reduce repeated indexing. Cache invalidation must account for catalog changes and permission context. The current benchmarks report cold index-plus-query cost separately from CLI/network discovery overhead; no claim is made that catalog discovery is free.

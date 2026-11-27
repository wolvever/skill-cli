# Provider configuration

```json
{
  "providers": {
    "docs": {"type":"mcp","transport":"http","url":"https://example.com/mcp","bearer_env":"DOCS_TOKEN","allow":["search"]},
    "legacy": {"type":"mcp","transport":"sse","url":"https://example.com/sse"},
    "local": {"type":"mcp","transport":"stdio","command":["python","/absolute/server.py"],"env":{"API_KEY":"LOCAL_API_KEY"}},
    "api": {"type":"openapi","spec":"api.yaml","url":"https://api.example.com","headers_env":{"X-API-Key":"API_KEY"}},
    "rest": {"type":"http","url":"https://api.example.com","bearer_env":"API_TOKEN"}
  }
}
```

`spec` resolves relative to the config file. Stdio `command` is an executable/argument array without an implicit shell; use `node` plus a script path on Windows instead of assuming a `.cmd` wrapper can run directly. Other environment variables are inherited by stdio servers, so provider executables must be trusted. Environment binding values above are **variable names**, not secret values. Provider URL overrides pin the destination instead of trusting a spec's servers entry. Spec downloads are unauthenticated; redirects and cross-origin credential forwarding are refused.

A nonempty `allow` list both hides and denies unlisted operations. An omitted or empty list permits all operations. Raw HTTP `request` is disabled when a provider has an allowlist. The operator configuration defines the available authority; provider descriptions and annotations do not grant authorization.

Optional provider fields improve local discovery: `description` (a concise service summary), `keywords` (strings), `skill_descriptions` (strings), and `skill_files` (paths to skill folders or SKILL.md files, relative to this config). Only YAML frontmatter `name` and `description` are read from these explicitly configured skills, bounded to 64 KiB. Bodies and scripts are not indexed or executed; no filesystem crawl occurs. Provider metadata applies to all its allowed tools, so broad skill descriptions can produce irrelevant candidates. Prefer tool descriptions for operation-specific detail. No model, database, or search service is required.

Gateway setup, when requested: set a random `SKILL_CLI_GATEWAY_TOKEN` of at least 24 characters in the trusted host and caller environment, and start `skill-cli --config providers.json serve --listen 127.0.0.1:7331`. Call `skill-cli --gateway http://127.0.0.1:7331 list`. `--gateway-token-env NAME` selects a different variable. The foreground gateway binds only to loopback, validates Host and browser Origin, and retains one session per provider until shutdown. It is single-principal; it does not create container networking, TLS termination, or a multitenant security boundary. Restart to reload config/spec changes or recover a broken session. It does not implement OAuth refresh or automatic session recovery.

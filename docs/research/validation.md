# Initial implementation validation

Validated locally on 2026-11-27:

- Windows amd64: `go vet ./...` and `go test ./...` passed.
- Ubuntu WSL amd64: `go vet ./...` and `go test -race ./...` passed.
- Compiled Windows CLI: all **17** cases in `python scripts/smoke.py` passed. [Recorded outputs](skill-cli-smoke.json).
- Linux amd64: built with `CGO_ENABLED=0`, `-trimpath`, and `-ldflags '-s -w'`; executable version check passed under WSL.
- Stripped artifacts: Windows 11,298,304 bytes; Linux 10,977,464 bytes. These describe the initial local build, not guaranteed future release sizes.

The smoke cases exercise actual executables for discovery, search, description, stdio/Streamable HTTP/legacy SSE calls, nested stdin JSON, tool errors and invalid input, OpenAPI calls, raw HTTP, and authenticated thin-client gateway calls/allowlist rejection. Go tests cover additional parameter encoding, internal schema references, large integer argument preservation, HTTP response limits/deadlines, credential redirect boundaries, and browser-Origin/Host rejection.

An aggregate Windows `go test -cover ./...` attempt failed because the downloaded toolchain could not find `covdata` for command packages with no tests; package test results themselves passed. No complete coverage percentage is claimed. Regular tests and Linux race checks succeeded independently.

macOS checks are configured in GitHub Actions and require a successful CI run before being described as verified. Authentication testing used synthetic gateway and bearer tokens; interactive provider OAuth and live third-party credentials were not tested.

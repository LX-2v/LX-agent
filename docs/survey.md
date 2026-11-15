# Survey: Go profiling analysis over MCP

Surveyed 2026-11-15.

## Direct overlap

[`ZephyrDeng/pprof-analyzer-mcp`](https://github.com/ZephyrDeng/pprof-analyzer-mcp) is a Go MCP server with a broad pprof analysis tool, comparison and memory growth analysis, flame graph generation, and interactive pprof process management. Its documented interface accepts file and remote URLs; some features invoke `go tool pprof`, require Graphviz, or launch a local process. It also documents migration to the official Go MCP SDK.

LX-agent shares local pprof inspection and profile comparison. It differs by keeping analysis offline and read-only, parsing pprof through `github.com/google/pprof/profile`, placing hard bounds on input, decompression, store memory, rows, strings, and response size, and returning narrowly structured tables for downstream diagnosis. It also provides a dedicated goroutine dump grouper. It does not generate visualizations or perform diagnosis.

## Adjacent tooling

GoLand documents an MCP server for IDE profiling workflows and pprof profile inspection. That serves users already working inside its profiler; LX-agent is a standalone stdio process for local files and does not depend on an IDE.

The standard `go tool pprof` remains the full-featured interactive and visualization tool. LX-agent intentionally exposes a smaller deterministic analysis surface suited to bounded tool calls.

## SDK choice

The official [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) v1.8.0 is recommended. It is the protocol project's official Go SDK, has a published supported-protocol matrix, and includes a stdio transport. The competing [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) is a mature, actively maintained alternative; its latest release during this survey is v1.5.0 (2026-10-30), with fixes to stdio cancellation and bounded HTTP reads. Given this server's small stdio-only needs, protocol stewardship and direct MCP API are the deciding factors; the SDK's wider optional dependencies are accepted to avoid implementing protocol framing ourselves.

The required pprof parser is [`github.com/google/pprof/profile`](https://pkg.go.dev/github.com/google/pprof/profile). No other direct third-party dependencies are planned.

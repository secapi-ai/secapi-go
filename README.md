# SEC API Go SDK

Official Go SDK for [SEC API](https://secapi.ai), the SEC data API designed for agents.

SEC API turns SEC filings and adjacent market data into traceable, source-backed investor workflows. It provides REST, SDK, CLI, and hosted MCP surfaces over filings, statements, ownership, governance, market data, events, webhooks, and agent-ready intelligence workflows.

Use this SDK when you want SEC API inside Go services, workers, command-line tools, or backend jobs while preserving the response metadata that matters: `requestId`, provenance, freshness, materialization state, trace references, accession numbers, and SEC source URLs.

## Install

```bash
go get github.com/secapi-ai/secapi-go
```

## Quick Start

```go
package main

import (
    "fmt"
    "os"

    secapi "github.com/secapi-ai/secapi-go"
)

func main() {
    client := secapi.NewClient(os.Getenv("SECAPI_API_KEY"))

    entity, err := client.ResolveEntity(map[string]string{"ticker": "AAPL"})
    if err != nil {
        panic(err)
    }

    fmt.Println(entity["name"])
}
```

## Authentication

Create an API key from the SEC API site, then export it:

```bash
export SECAPI_API_KEY=your_key_here
```

The SDK sends your key with the `x-api-key` header.

## Configuration

```go
client := secapi.NewClient(os.Getenv("SECAPI_API_KEY"))
client.BaseURL = "https://api.secapi.ai"
```

`NewSecApiClient` remains available as a compatibility alias for `NewClient`.

## Common Workflows

Resolve a company:

```go
entity, err := client.ResolveEntity(map[string]string{"ticker": "AAPL"})
if err != nil {
    panic(err)
}
fmt.Println(entity["name"])
```

Fetch the latest 10-K:

```go
filing, err := client.LatestFiling(map[string]string{
    "ticker": "AAPL",
    "form": "10-K",
})
if err != nil {
    panic(err)
}
fmt.Println(filing["title"], filing["filingDate"])
```

Find high-risk dilution issuers:

```go
ratings, err := client.DilutionRatings(map[string]string{
    "overall_risk": "high",
    "limit": "10",
})
if err != nil {
    panic(err)
}
fmt.Println(ratings["object"])
```

## Agent Setup

Give this prompt to a coding agent:

```text
Install github.com/secapi-ai/secapi-go in this Go service. Start with one entity lookup to confirm auth, then add a filing or ownership workflow that preserves requestId, freshness, provenance, and trace metadata in structured logs. Use https://docs.secapi.ai/llms.txt as the documentation index and prefer the OpenAPI spec over guessing endpoint paths.
```

For broader agent instructions, use:

- [`llms.txt`](https://docs.secapi.ai/llms.txt)
- [Give this prompt to your agent](https://docs.secapi.ai/give-this-prompt-to-your-agent)
- [LLM and agent guide](https://docs.secapi.ai/llm-guide)

## Environment Variables

| Variable | Description |
|---|---|
| `SECAPI_API_KEY` | SEC API key |
| `SECAPI_BASE_URL` | API base URL override |

## Validate

```bash
go test ./...
go list -m github.com/secapi-ai/secapi-go
```

## Links

- [SEC API](https://secapi.ai)
- [Developer docs](https://docs.secapi.ai)
- [Go SDK guide](https://docs.secapi.ai/go-sdk)
- [API reference](https://docs.secapi.ai/api-reference)
- [API playground](https://docs.secapi.ai/api-playground)
- [Libraries and SDKs](https://docs.secapi.ai/libraries-and-sdks)
- [Auth and pricing](https://docs.secapi.ai/auth-and-pricing)
- [Freshness and trust](https://docs.secapi.ai/freshness-and-trust)
- [Status](https://docs.secapi.ai/status)
- [Support](https://docs.secapi.ai/support)

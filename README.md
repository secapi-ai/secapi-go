# SEC API Go SDK

A lightweight Go client for SEC API factor data, filings, statements, ownership, and agent workflows.

## Installation

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

    // Resolve an entity
    entity, err := client.ResolveEntity(map[string]string{"ticker": "AAPL"})
    if err != nil {
        panic(err)
    }
    fmt.Printf("Entity: %s (%s)\n", entity["name"], entity["ticker"])

    // Get latest 10-K filing
    filing, err := client.LatestFiling(map[string]string{
        "ticker": "AAPL",
        "form":   "10-K",
    })
    if err != nil {
        panic(err)
    }
    fmt.Printf("Filing: %s (%s)\n", filing["title"], filing["filingDate"])
}
```

### Live agent workflow example

For a production-backed copy/paste path, run the focused example that resolves
an entity, fetches the latest 10-K, and extracts Item 1A in compact mode:

```bash
export SECAPI_API_KEY="secapi_live_..."
cd packages/sdk-go
go run ./examples/agent_workflow
```

From the monorepo root, `bun run smoke:sdk-examples` runs the matching
JavaScript, Python, Go, and Rust examples and asserts that each returns entity,
filing, and compact-section metadata.

## Structured Errors

Non-2xx API responses return `*secapi.APIError`, which preserves the HTTP
status, SEC API error code, request ID, message, and parsed response body:

```go
import "errors"

var apiErr *secapi.APIError
if errors.As(err, &apiErr) {
    fmt.Println(apiErr.StatusCode, apiErr.Code, apiErr.RequestID)
}
```

## Contexts and Deadlines

Production services can use `WithContext` variants on common workflows to carry
request deadlines and cancellation through to the underlying HTTP request:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

filing, err := client.LatestFilingWithContext(ctx, map[string]string{
    "ticker": "AAPL",
    "form":   "10-K",
})
```

Context-aware helpers are available for health/auth, entity resolution, filing
and search workflows, statements, factor history/dashboard/valuations,
portfolio analysis/attribution/hedging/optimization, model factor analysis, and
hosted MCP calls.

## Grouped Service Fields

Flat methods remain the complete SDK surface, and common workflows are also
available as grouped service fields for easier discovery in editors and agent
tool planners:

```go
// Flat and grouped calls are equivalent.
filing, err := client.Filings.Latest(map[string]string{
    "ticker": "AAPL",
    "form":   "10-K",
})

section, err := client.Sections.Latest("item_1a", map[string]string{
    "ticker": "AAPL",
    "form":   "10-K",
    "mode":   "compact",
})

results, err := client.Search.Semantic(map[string]string{
    "q":      "supply chain risk",
    "ticker": "AAPL",
    "mode":   "hybrid",
    "view":   "agent",
})

history, err := client.Factors.History("VALUE", map[string]string{
    "range":         "1y",
    "response_mode": "compact",
    "include":       "trust,series",
})
```

Start with `client.Entities`, `client.Filings`, `client.Sections`,
`client.Search`, and `client.Factors` when exploring. Use the flat methods when
you need an endpoint outside those high-signal groups. The service fields are
initialized by `secapi.NewClient`, `secapi.NewBearerTokenClient`, and
`secapi.NewSecApiClient`; if you construct `Client` literals manually, continue
using the flat methods or switch to the constructors before using grouped fields.

## Auto-pagination

Cursor endpoints can be consumed with a pull-based iterator instead of
hand-rolling `nextCursor` loops:

```go
iter := client.PaginateFilings(map[string]string{
    "ticker": "AAPL",
    "form":   "10-K",
    "limit":  "100",
})

for iter.Next(context.Background()) {
    filing := iter.Item()
    fmt.Println(filing["accessionNumber"])
}
if err := iter.Err(); err != nil {
    panic(err)
}
```

Built-in helpers cover common discovery flows: `PaginateFilings`,
`PaginateSections`, `PaginateEntities`, and the typed `PaginateSectionsAgent`.
Each helper also has a `WithOptions` variant for bounded agent loops:

```go
iter := client.PaginateFilingsWithOptions(params, secapi.PaginationOptions[map[string]any]{
    MaxPages: 5,
    MaxItems: 200,
})
```

An all-zero `PaginationOptions` passed to a `WithOptions` helper is a no-op, so
agents can intentionally request zero work. A single positive cap still works as
the only bound. For unbounded helpers, call `PaginateFilings`, `PaginateSections`,
or `PaginateEntities`. For unbounded custom `WithOptions` or `NewPageIterator`
calls, set both caps to `secapi.UnlimitedPaginationCap`.

For other cursor endpoints, use `NewPageIterator` with a page function and
custom item extraction.

## Typed Request Params

High-frequency read paths accept the existing `map[string]string` query params,
and the SDK also exposes typed params for discoverability in editors:

```go
filing, err := client.LatestFiling(secapi.LatestFilingParams{
    Ticker: "AAPL",
    Form:   "10-K",
    View:   secapi.ResponseViewAgent,
}.Params())

history, err := client.FactorHistory("VALUE", secapi.FactorHistoryParams{
    Range:        "1y",
    ResponseMode: "compact",
    Include:      "trust,series",
}.Params())
```

Typed params are available for `LatestFiling`, `LatestSection`,
`SearchSections`, `AllStatements`, and `FactorHistory`. Each struct includes an
`Extra map[string]string` escape hatch for newer or less common query
parameters while preserving the exact REST query contract.

## Typed Agent Responses

When you are building an agent workflow and want typed response fields instead
of map assertions, use the `*Agent` helpers. They force `view=agent` on routes
that support it and decode into exported Go structs:

```go
filing, err := client.LatestFilingAgent(secapi.LatestFilingParams{
    Ticker: "AAPL",
    Form:   "10-K",
}.Params())
if err != nil {
    panic(err)
}
value := func(s *string) string {
    if s == nil {
        return ""
    }
    return *s
}
fmt.Println(filing.AccessionNumber, value(filing.FilingURL))

sections, err := client.SearchSectionsAgent(secapi.SectionSearchParams{
    Ticker: "AAPL",
    Form:   "10-K",
    Query:  "risk factors",
    Limit:  "3",
}.Params())
if err != nil {
    panic(err)
}
for _, section := range sections.Data {
    fmt.Println(value(section.SectionKey), value(section.SourceURL))
}
```

Typed agent helpers are available for `ResolveEntityAgent`,
`LatestFilingAgent`, `SearchSectionsAgent`, and `StatementAgent`. Nullable API
fields decode as pointers so callers can distinguish missing data from empty
strings or zeros.

## Factor Quickstart

Use `response_mode=compact` when you are feeding an agent, LLM, notebook, or UI card and want the smallest useful payload. Compact catalog responses still include readiness/proof summaries. Add `include=trust` only when you need the full trust/provenance envelope plus full methodology/materialization/revision/source-rights objects for citations or checks. For catalog/tool-discovery calls, start narrow with `category` plus `limit` before requesting trust metadata; the full trust envelope can be larger than a simple picker payload.

```go
package main

import (
    "fmt"
    "os"

    secapi "github.com/secapi-ai/secapi-go"
)

func main() {
    client := secapi.NewClient(os.Getenv("SECAPI_API_KEY"))

    catalog, err := client.FactorCatalog(map[string]string{
        "category":      "style",
        "limit":         "25",
        "response_mode": "compact",
        "include":       "trust",
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(catalog["data"])

    history, err := client.FactorHistory("VALUE", map[string]string{
        "range":         "1y",
        "response_mode": "compact",
        "include":       "trust,series",
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(history["factorKey"], history["dataAsOf"])

    valuations, err := client.FactorValuations(map[string]string{
        "keys":          "VALUE,QUALITY,MOMENTUM",
        "side":          "all",
        "sort":          "opportunity_score",
        "response_mode": "compact",
        "include":       "trust",
        "limit":         "25",
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(valuations["data"])

    dashboard, err := client.FactorDashboard(map[string]string{
        "country":       "US",
        "category":      "style",
        "ticker":        "AAPL",
        "response_mode": "compact",
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(dashboard["data"])

    extremeMoves, err := client.FactorExtremeMoves(map[string]string{
        "category":      "style",
        "window":        "1d",
        "min_z_score":   "2",
        "response_mode": "compact",
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(extremeMoves["data"])

    extremePairs, err := client.FactorExtremePairs(map[string]string{
        "category":      "style",
        "window":        "1m",
        "min_z_score":   "1",
        "response_mode": "compact",
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(extremePairs["data"])

    holdings := []map[string]any{
        {"symbol": "AAPL", "weight": 0.4},
        {"symbol": "MSFT", "weight": 0.35},
        {"symbol": "NVDA", "weight": 0.25},
    }

    attribution, err := client.PortfolioAttribution(map[string]any{
        "holdings":  holdings,
        "window":    "1y",
        "frequency": "monthly",
    }, map[string]string{"response_mode": "compact", "include": "trust"})
    if err != nil {
        panic(err)
    }
    fmt.Println(attribution["portfolio"])

    hedge, err := client.PortfolioHedge(map[string]any{
        "holdings":  holdings,
        "objective": "factor_neutral",
        "constraints": map[string]any{"maxHedges": 5},
    }, map[string]string{"response_mode": "compact", "include": "trust"})
    if err != nil {
        panic(err)
    }
    fmt.Println(hedge["residualExposure"])

    optimized, err := client.PortfolioOptimize(map[string]any{
        "holdings":  holdings,
        "objective": "regime_aware",
        "constraints": map[string]any{"longOnly": true, "maxPositionWeight": 0.35},
    }, map[string]string{"response_mode": "compact", "include": "trust"})
    if err != nil {
        panic(err)
    }
    fmt.Println(optimized["optimizationNotes"])

    modelAnalysis, err := client.ModelFactorAnalysis(map[string]any{
        "model": map[string]any{"id": "growth-core", "label": "Growth Core"},
        "holdings": holdings,
        "include": map[string]any{"attribution": true, "hedge": true, "optimizer": true},
    }, map[string]string{"response_mode": "compact", "include": "trust"})
    if err != nil {
        panic(err)
    }
    fmt.Println(modelAnalysis["summaryMd"])
}
```

Factor and portfolio helpers include `FactorCatalog`, `FactorReturns`, `FactorHistory`, `FactorSparklines`, `FactorDashboard`, `FactorScreen`, `FactorExtremeMoves`, `FactorExtremePairs`, `FactorValuations`, `FactorValuationStocks`, `FactorExposures`, `FactorDecomposition`, `FactorRelatedStocks`, `FactorSimilarityPack`, `FactorPairs`, `FactorPairHistory`, `FactorBulkDownload`, `FactorCustom`, `PortfolioAnalyze`, `PortfolioAttribution`, `PortfolioHedge`, `PortfolioOptimize`, `PortfolioStressTest`, `ModelPortfolioFactorView`, and `ModelFactorAnalysis`.

Market helpers include `MarketCalendar`, `MarketEstimates`, `MarketSnapshots`, `MarketBars`, `MarketUniverse`, `MarketCorporateActions`, and `MarketReference`.

## MCP Tool Calls

Use `CallMCPTool` to invoke hosted MCP tools without hand-writing the JSON-RPC envelope:

```go
result, err := client.CallMCPTool("filings.latest", map[string]any{
    "ticker": "AAPL",
    "form":   "10-K",
}, "agent-request-1")
if err != nil {
    panic(err)
}
fmt.Println(result)
```

## Configuration

```go
// Reads SECAPI_API_KEY and SECAPI_BASE_URL from the environment when present.
client := secapi.NewClient("")

// Or pass the API key explicitly.
client = secapi.NewClient(os.Getenv("SECAPI_API_KEY"))

// Dashboard/account workflows can use a bearer token instead.
client = secapi.NewBearerTokenClient(os.Getenv("SECAPI_BEARER_TOKEN"))
```

`NewSecApiClient` remains available as a compatibility alias for `NewClient`.

New clients use a 30-second HTTP timeout by default. For different transport,
proxy, or timeout behavior, replace `client.HTTPClient`:

```go
client.HTTPClient = &http.Client{Timeout: 5 * time.Second}
```

Requests include `Accept: application/json` and a `User-Agent` like
`secapi-go/1.0.1` so support and server logs can identify SDK traffic.

Safe read requests retry transient network failures and 408/429/502/503/504
responses twice by default. All requests retry 429 rate limits and honor
`Retry-After`; mutating requests do not retry other transient failures
automatically. Tune or disable retries with `RetryConfig`:

```go
client.RetryConfig.MaxRetries = 0 // disable retries
```

## Environment Variables

| Variable | Description |
|---|---|
| `SECAPI_API_KEY` | SEC API key |
| `SECAPI_BEARER_TOKEN` | Optional OAuth bearer token env var |
| `SECAPI_BASE_URL` | API base URL override |
| `SECAPI_API_BASE_URL` | API base URL override alias |
| `OMNI_DATASTREAM_API_KEY` | Compatibility API key env var |
| `OMNI_DATASTREAM_BEARER_TOKEN` | Compatibility bearer token env var |
| `OMNI_DATASTREAM_BASE_URL` | Compatibility base URL override |
| `OMNI_DATASTREAM_API_BASE_URL` | Compatibility base URL override alias |

## Links

- [API Documentation](https://docs.secapi.ai)
- [Go SDK Reference](https://docs.secapi.ai/go-sdk)

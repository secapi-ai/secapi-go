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

## Factor Quickstart

Use `response_mode=compact` when you are feeding an agent, LLM, notebook, or UI card and want the smallest useful payload. Add `include=trust` when you need freshness, methodology, and materialization metadata for citations or launch checks.

```go
package main

import (
    "fmt"
    "os"

    secapi "github.com/secapi-ai/secapi-go"
)

func main() {
    client := secapi.NewClient(os.Getenv("SECAPI_API_KEY"))

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

## Configuration

```go
apiKey := os.Getenv("SECAPI_API_KEY")
client := secapi.NewClient(apiKey)
client.BaseURL = "https://api.secapi.ai"  // default
```

`NewSecApiClient` remains available as a compatibility alias for `NewClient`.

## Environment Variables

| Variable | Description |
|---|---|
| `SECAPI_API_KEY` | SEC API key |
| `SECAPI_BASE_URL` | API base URL override |

## Links

- [API Documentation](https://docs.secapi.ai)
- [Go SDK Reference](https://docs.secapi.ai/go-sdk)

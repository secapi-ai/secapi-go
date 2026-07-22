# SEC API Go SDK

`github.com/secapi-ai/secapi-go` is a Go client for SEC API filings, statements, ownership data, factor data, and filing sections.

[Documentation](https://docs.secapi.ai) · [Pricing](https://secapi.ai/pricing) · [Get an API key](https://secapi.ai/signup) · [Support](https://github.com/secapi-ai/secapi-go) · [Status](https://status.secapi.ai)

## Install and make a request

```bash
go get github.com/secapi-ai/secapi-go
export SECAPI_API_KEY="secapi_live_..."
```

```go
package main

import (
    "fmt"
    "os"

    secapi "github.com/secapi-ai/secapi-go"
)

func main() {
    client := secapi.NewClient(os.Getenv("SECAPI_API_KEY"))
    filing, err := client.LatestFilingAgent(map[string]string{"ticker": "AAPL", "form": "10-K"})
    if err != nil {
        panic(err)
    }
    fmt.Println(filing.AccessionNumber)
    if filing.FilingURL != nil {
        fmt.Println(*filing.FilingURL)
    }
}
```

Run it with `go run .`. It prints the latest matching filing's accession number and SEC source URL; both can change after a new filing.

## Common requests

```go
company, err := client.ResolveEntity(map[string]string{"ticker": "AAPL", "view": "agent"})
filings, err := client.SearchFilings(map[string]string{"ticker": "AAPL", "form": "10-K", "limit": "20"})
section, err := client.LatestSection("item_1a", map[string]string{"ticker": "AAPL", "form": "10-K", "mode": "compact"})
```

Flat methods are the complete interface. `client.Entities`, `client.Filings`, `client.Sections`, `client.Search`, and `client.Factors` provide grouped access for common workflows. Use `WithContext` variants to carry cancellation and deadlines.

## Special Situations

Use `ListSituations`, `GetSituation`, `SituationsByForm`, `SituationFilings`, `SituationSummary`, `SituationsFeed`, `SituationsCalendar`, `SituationsStats`, `SituationsIssues`, `ExportSituation`, `UnderwriteSituation`, and `WatchSituations` for the authenticated paid Special Situations workflow. Use `EmbedSituations` and `EmbedSituationExport` for an anonymous, recent-only public projection. The [Special Situations workflow guide](https://docs.secapi.ai/special-situations-workflows) has concise examples and source-review guidance.

## Factor response modes

Use `response_mode: "compact"` when you want the smallest useful payload. Compact catalog responses still include readiness/proof summaries. Set `include: "trust"` only when you need the full trust/provenance envelope plus full methodology/materialization/revision/source-rights objects for citations or checks. For catalog/tool-discovery calls, start narrow with `category` and `limit`; the full trust envelope can be larger than a simple picker payload.

## Configuration and compatibility

Pass an API key to `secapi.NewClient`; set `client.BaseURL` when targeting a non-default origin. `secapi.NewBearerTokenClient` is for signed-in account endpoints. Go 1.23 is required by this module.

Non-2xx responses return `*secapi.APIError`, including status, SEC API error code, and request ID. Include the request ID when filing an issue in the [Go SDK repository](https://github.com/secapi-ai/secapi-go). See the [API documentation](https://docs.secapi.ai) for full endpoint coverage, pagination, and typed request helpers.

## License

MIT

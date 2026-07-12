# SEC API Go SDK

Use SEC API from Go to resolve issuers, retrieve SEC filings and filing sections, search disclosures, and work with statements, factors, and portfolio endpoints.

## Install

```bash
go get github.com/secapi-ai/secapi-go
```

This module requires Go 1.23 or later.

## Start with an issuer and its latest filing

Set an API key, then resolve an issuer and retrieve its latest annual filing:

```bash
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

	entity, err := client.Entities.Resolve(map[string]string{
		"ticker": "AAPL",
	})
	if err != nil {
		panic(err)
	}

	filing, err := client.Filings.Latest(map[string]string{
		"ticker": "AAPL",
		"form":   "10-K",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(entity["name"], filing["filingDate"], filing["title"])
}
```

`NewClient` uses the supplied API key or, when it is empty, reads `SECAPI_API_KEY`. The client sends API-key requests to `https://api.secapi.ai` by default.

## Work with filings

Use the grouped services to keep issuer, filing, section, search, and factor workflows discoverable in an editor. Flat methods remain available on `Client` for the full SDK surface.

```go
section, err := client.Sections.Latest("item_1a", map[string]string{
	"ticker": "AAPL",
	"form":   "10-K",
	"mode":   "compact",
})
if err != nil {
	panic(err)
}

fmt.Println(section["title"])
```

The SDK includes services for:

- `Entities`: issuer resolution and search.
- `Filings` and `Sections`: filing discovery, latest filings, accession lookups, and filing sections.
- `Search`: full-text and semantic disclosure search.
- `Factors`: factor catalog, returns, history, valuations, correlations, and related analysis.

The `Client` also exposes statements, company financials, ownership and offering events, market data, portfolio analysis, intelligence, MCP, and other API endpoints.

## Context, typed responses, and pagination

Common operations have `WithContext` variants for caller-controlled cancellation and deadlines:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

filing, err := client.LatestFilingWithContext(ctx, map[string]string{
	"ticker": "AAPL",
	"form":   "10-K",
})
```

For agent-oriented workflows, typed helpers decode selected responses into Go structs and request the API's `agent` view where it is supported:

```go
filing, err := client.Filings.LatestAgent(secapi.LatestFilingParams{
	Ticker: "AAPL",
	Form:   "10-K",
}.Params())
if err != nil {
	panic(err)
}

fmt.Println(filing.AccessionNumber, filing.FilingDate)
```

Cursor-based filing, entity, and section discovery can be consumed with an iterator:

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

Use `PaginateFilingsWithOptions`, `PaginateEntitiesWithOptions`, or `PaginateSectionsWithOptions` to cap pages or items in bounded workflows. The default iterator is unbounded; for an options-based iterator, set both caps to `secapi.UnlimitedPaginationCap` for the same behavior. Leaving both caps at zero fetches no results. When only one cap is positive, the other is treated as unlimited.

## Errors and authentication

Non-2xx API responses return `*secapi.APIError`. It retains the HTTP status, API error code, request ID, message, and parsed response body.

```go
var apiErr *secapi.APIError
if errors.As(err, &apiErr) {
	fmt.Println(apiErr.StatusCode, apiErr.Code, apiErr.RequestID)
}
```

For bearer-token authentication, construct the client explicitly:

```go
client := secapi.NewBearerTokenClient(os.Getenv("SECAPI_BEARER_TOKEN"))
```

## Configuration

| Variable | Purpose |
| --- | --- |
| `SECAPI_API_KEY` | Recommended API-key fallback for `NewClient("")`. |
| `SECAPI_BEARER_TOKEN` | Bearer-token fallback for `NewClient("")`; `NewBearerTokenClient("")` uses this value and does not fall back to an API key. |
| `SECAPI_BASE_URL` | Recommended override for the default API base URL. |

For compatibility with older SEC API integrations, the client also accepts `OMNI_DATASTREAM_API_KEY`, `OMNI_DATASTREAM_BEARER_TOKEN`, `SECAPI_API_BASE_URL`, `OMNI_DATASTREAM_BASE_URL`, and `OMNI_DATASTREAM_API_BASE_URL`.

The default HTTP client times out after 30 seconds and makes two retry attempts. `GET` and `HEAD` requests retry transient network failures and `408`, `429`, `502`, `503`, and `504` responses; other methods retry `429` only. Set `RetryConfig.MaxRetries` to `0` to disable retries.

## Documentation

- [SEC API documentation](https://docs.secapi.ai)
- [Entity resolution reference](https://docs.secapi.ai/api-reference/entities/get-v1-entities-resolve)

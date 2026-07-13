# SEC API Go SDK

The official Go client for [SEC API](https://secapi.ai): resolve companies, retrieve filings, and search SEC disclosures.

## Install

```bash
go get github.com/secapi-ai/secapi-go
```

## First call

[Create an API key](https://secapi.ai/login), then keep it in your environment:

```bash
export SECAPI_API_KEY="secapi_live_..."
```

Resolve a company by ticker with the compact, typed entity response:

```go
package main

import (
	"encoding/json"
	"log"
	"os"

	secapi "github.com/secapi-ai/secapi-go"
)

func main() {
	apiKey := os.Getenv("SECAPI_API_KEY")
	if apiKey == "" {
		log.Fatal("SECAPI_API_KEY is not set")
	}

	client := secapi.NewClient(apiKey)
	entity, err := client.Entities.ResolveAgent(map[string]string{
		"ticker": "AAPL",
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(entity); err != nil {
		log.Fatal(err)
	}
}
```

`ResolveAgent` requests `view=agent` and returns `*secapi.AgentEntity`. The response contains an entity `id`, `ticker`, `cik`, `name`, `primaryIdentifiers`, match metadata, and `requestId`:

```json
{
  "object": "entity",
  "id": "cent_...",
  "ticker": "AAPL",
  "cik": "0000320193",
  "name": "Apple Inc.",
  "primaryIdentifiers": [{ "type": "ticker", "value": "AAPL" }],
  "matchConfidence": 1,
  "matchBasis": "ticker",
  "requestId": "req_..."
}
```

## Authentication, errors, and retries

`NewClient` sends the API key as `x-api-key`. Passing an empty key uses `SECAPI_API_KEY`; the default base URL is `https://api.secapi.ai`. For bearer-token authentication, use `secapi.NewBearerTokenClient` with `SECAPI_BEARER_TOKEN`.

Non-2xx responses return `*secapi.APIError`, with the status code, API error code, message, request ID, and parsed response body. Log the request ID when contacting support.

```go
var apiErr *secapi.APIError
if errors.As(err, &apiErr) {
	log.Printf("SEC API request %s failed: %s (%d)", apiErr.RequestID, apiErr.Code, apiErr.StatusCode)
}
```

The client uses a 30-second HTTP timeout and retries up to twice by default. It honors `Retry-After` (capped at two seconds), retries `429` responses, and retries transient transport and `408`/`502`/`503`/`504` failures only for `GET` and `HEAD` requests. Configure `client.RetryConfig` for a different policy.

## Documentation and support

- [SEC API documentation](https://docs.secapi.ai)
- [Entity resolution reference](https://docs.secapi.ai/api-reference/entities/get-v1-entities-resolve)
- [API conventions](https://docs.secapi.ai/api-conventions)
- [Report an issue or request support](https://github.com/secapi-ai/secapi-go/issues)

## Compatibility and status

This SDK requires Go 1.23 or later.

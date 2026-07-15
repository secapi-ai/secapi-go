# SEC API Go SDK

The official Go client for [SEC API](https://secapi.ai), for resolving public companies and retrieving SEC filings, filing sections, and disclosure-search results.

## Install

```bash
go get github.com/secapi-ai/secapi-go
```

Create an API key in the [SEC API dashboard](https://secapi.ai/login), then make it available to your application:

```bash
export SECAPI_API_KEY="secapi_live_..."
```

## First request

Resolve a company by ticker. `ResolveAgent` requests SEC API's compact `view=agent` response and decodes it into `*secapi.AgentEntity`.

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
	entity, err := client.Entities.ResolveAgent(map[string]string{"ticker": "AAPL"})
	if err != nil {
		log.Fatal(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(entity); err != nil {
		log.Fatal(err)
	}
}
```

The result identifies the resolved company and records how the match was made:

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

## Authentication and errors

`NewClient` sends the supplied key in the `x-api-key` header. Passing an empty string makes it read `SECAPI_API_KEY`; the client uses `https://api.secapi.ai` unless `SECAPI_BASE_URL` is set.

For an endpoint that explicitly uses bearer authentication, construct a separate client with `secapi.NewBearerTokenClient`. It accepts a token directly or reads `SECAPI_BEARER_TOKEN` when passed an empty string.

Every non-2xx API response is returned as `*secapi.APIError`. It preserves the HTTP status, API code, message, request ID, and decoded response body. Preserve the request ID when reporting an issue:

```go
var apiErr *secapi.APIError
if errors.As(err, &apiErr) {
	log.Printf("SEC API request %s failed: %s (%d)", apiErr.RequestID, apiErr.Code, apiErr.StatusCode)
}
```

The default HTTP timeout is 30 seconds. The client retries transient transport failures and `408`, `429`, `502`, `503`, and `504` responses according to the request method and retry policy; configure `client.RetryConfig` to change it.

## More usage

- [Basic example](examples/basic/main.go): resolve an entity and print its typed response.
- [Filing and section workflow](examples/agent_workflow/main.go): resolve a company, fetch its latest 10-K, and retrieve Item 1A.
- [Entity resolution reference](https://docs.secapi.ai/api-reference/entities/get-v1-entities-resolve)
- [SEC API documentation](https://docs.secapi.ai)
- [API conventions](https://docs.secapi.ai/api-conventions)

## Compatibility and support

This module requires Go 1.23 or later. The current client sends SEC API version `2026-03-19` by default. Report SDK bugs and request support through the [issue tracker](https://github.com/secapi-ai/secapi-go/issues); include the API request ID for API-response problems.

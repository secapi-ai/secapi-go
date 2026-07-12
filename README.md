# SEC API Go SDK

The official Go client for [SEC API](https://secapi.ai). Resolve public companies and retrieve SEC data from Go.

## Install

```bash
go get github.com/secapi-ai/secapi-go
```

## Get started

Create an API key, then keep it in your environment:

```bash
export SECAPI_API_KEY="secapi_live_..."
```

Resolve a company by ticker:

```go
package main

import (
	"fmt"
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
	entity, err := client.Entities.Resolve(map[string]string{
		"ticker": "AAPL",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s (%s), CIK %s\n", entity["name"], entity["ticker"], entity["cik"])
}
```

The request resolves Apple by its ticker. The response is a `map[string]any`; this example reads its company name, ticker, and CIK.

## Documentation and support

- [SEC API documentation](https://docs.secapi.ai)
- [Entity resolution reference](https://docs.secapi.ai/api-reference/entities/get-v1-entities-resolve)
- [Report an issue or request support](https://github.com/secapi-ai/secapi-go/issues)

## Compatibility and status

This SDK requires Go 1.23 or later. It is the official Go client for the SEC API; the default API base URL is `https://api.secapi.ai`.

# SEC API Go SDK

Official Go SDK for [SEC API](https://secapi.ai).

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

    entity, err := client.ResolveEntity(map[string]string{"ticker": "AAPL"})
    if err != nil {
        panic(err)
    }

    fmt.Println(entity["name"])
}
```

## Configuration

```go
client := secapi.NewClient(os.Getenv("SECAPI_API_KEY"))
client.BaseURL = "https://api.secapi.ai"
```

`NewSecApiClient` remains available as a compatibility alias for `NewClient`.

## Environment Variables

| Variable | Description |
|---|---|
| `SECAPI_API_KEY` | SEC API key |
| `SECAPI_BASE_URL` | API base URL override |

## Links

- [API documentation](https://docs.secapi.ai)
- [Go SDK guide](https://docs.secapi.ai/go-sdk)
- [Support](https://docs.secapi.ai/support)

package main

import (
	"fmt"
	"os"

	secapi "github.com/secapi-ai/secapi-go"
)

func main() {
	client := secapi.NewClient(os.Getenv("SECAPI_API_KEY"))
	client.BaseURL = getenv("SECAPI_BASE_URL", getenv("SECAPI_API_BASE_URL", "https://api.secapi.ai"))

	entity, err := client.ResolveEntity(map[string]string{"ticker": "AAPL"})
	if err != nil {
		panic(err)
	}
	filing, err := client.LatestFiling(map[string]string{"ticker": "AAPL", "form": "10-K"})
	if err != nil {
		panic(err)
	}
	section, err := client.LatestSection("item_1a", map[string]string{"ticker": "AAPL", "form": "10-K", "mode": "compact"})
	if err != nil {
		panic(err)
	}

	dilutionEvents, err := client.DilutionEvents(map[string]string{"ticker": "AAPL", "limit": "3"})
	if err != nil {
		panic(err)
	}
	dilutionRatings, err := client.DilutionRatings(map[string]string{"limit": "3"})
	if err != nil {
		panic(err)
	}
	dilutionCoverage, err := client.DilutionCoverage(map[string]string{})
	if err != nil {
		panic(err)
	}

	fmt.Println(entity["name"], filing["id"], section["title"], dilutionEvents["object"], dilutionRatings["object"], dilutionCoverage["object"])
}

func getenv(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

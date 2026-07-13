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

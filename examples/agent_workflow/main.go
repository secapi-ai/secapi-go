package main

import (
	"encoding/json"
	"fmt"

	secapi "github.com/secapi-ai/secapi-go"
)

type workflowSummary struct {
	Object   string         `json:"object"`
	SDK      string         `json:"sdk"`
	Workflow map[string]any `json:"workflow"`
	Entity   map[string]any `json:"entity"`
	Filing   map[string]any `json:"filing"`
	Section  map[string]any `json:"section"`
}

func main() {
	client := secapi.NewClient("")

	entity, err := client.ResolveEntity(map[string]string{
		"ticker": "AAPL",
		"view":   string(secapi.ResponseViewAgent),
	})
	if err != nil {
		panic(err)
	}

	filing, err := client.LatestFiling(secapi.LatestFilingParams{
		Ticker: "AAPL",
		Form:   "10-K",
	}.Params())
	if err != nil {
		panic(err)
	}
	accessionNumber, ok := filing["accessionNumber"].(string)
	if !ok || accessionNumber == "" {
		panic("latest filing response did not include an accession number")
	}

	section, err := client.FilingSectionByAccession(accessionNumber, "item_1a", map[string]string{
		"ticker": "AAPL",
		"mode":   "compact",
	})
	if err != nil {
		panic(err)
	}

	summary := workflowSummary{
		Object: "secapi_sdk_agent_workflow",
		SDK:    "go",
		Workflow: map[string]any{
			"ticker":     "AAPL",
			"form":       "10-K",
			"sectionKey": "item_1a",
			"mode":       "compact",
		},
		Entity: map[string]any{
			"name":   entity["name"],
			"ticker": entity["ticker"],
			"cik":    entity["cik"],
		},
		Filing: map[string]any{
			"id":              filing["id"],
			"accessionNumber": accessionNumber,
			"form":            filing["form"],
			"filingDate":      filing["filingDate"],
		},
		Section: map[string]any{
			"title":           section["title"],
			"key":             firstPresent(section, "key", "section_key"),
			"mode":            "compact",
			"accessionNumber": firstPresentOr(section, accessionNumber, "accessionNumber", "accession_number", "accession"),
			"contentLength":   contentLength(section),
		},
	}

	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func firstPresentOr(values map[string]any, fallback any, keys ...string) any {
	value := firstPresent(values, keys...)
	if value == nil {
		return fallback
	}
	return value
}

func contentLength(values map[string]any) int {
	for _, key := range []string{"contentMd", "snippet"} {
		if value, ok := values[key].(string); ok {
			return len(value)
		}
	}
	return 0
}

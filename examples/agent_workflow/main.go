package main

import (
	"encoding/json"
	"fmt"

	secapi "github.com/secapi-ai/secapi-go/v2"
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
			"name":   stringField(entity, "name"),
			"ticker": stringField(entity, "ticker"),
			"cik":    stringField(entity, "cik"),
		},
		Filing: map[string]any{
			"id":              stringField(filing, "id"),
			"accessionNumber": accessionNumber,
			"form":            stringField(filing, "form"),
			"filingDate":      stringField(filing, "filingDate", "filing_date"),
		},
		Section: map[string]any{
			"title":           stringField(section, "title"),
			"key":             stringField(section, "key", "section_key"),
			"mode":            "compact",
			"accessionNumber": stringFieldOr(section, accessionNumber, "accessionNumber", "accession_number", "accession"),
			"contentLength":   contentLength(section),
		},
	}

	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
}

func stringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}

func stringFieldOr(values map[string]any, fallback string, keys ...string) string {
	value := stringField(values, keys...)
	if value == "" {
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

package secapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capturedRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

func newCaptureClient(t *testing.T) (*Client, *[]capturedRequest, func()) {
	t.Helper()
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := ""
		if r.Body != nil {
			defer r.Body.Close()
			var decoded any
			_ = json.NewDecoder(r.Body).Decode(&decoded)
			if decoded != nil {
				payload, _ := json.Marshal(decoded)
				body = string(payload)
			}
		}
		captured = append(captured, capturedRequest{
			Method: r.Method,
			Path:   r.URL.EscapedPath(),
			Query:  r.URL.RawQuery,
			Body:   body,
		})
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	return client, &captured, server.Close
}

func TestSearchWrappersRouteToSearchPaths(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.SearchFulltext(map[string]string{"q": "going concern", "form": "10-K"}); err != nil {
		t.Fatalf("SearchFulltext failed: %v", err)
	}
	if _, err := client.SemanticSearch(map[string]string{"q": "going concern", "mode": "hybrid"}); err != nil {
		t.Fatalf("SemanticSearch failed: %v", err)
	}
	if _, err := client.SearchSections(map[string]string{"ticker": "AAPL", "q": "risk"}); err != nil {
		t.Fatalf("SearchSections failed: %v", err)
	}

	wantPaths := []string{"/v1/search/fulltext", "/v1/search/semantic", "/v1/sections/search"}
	for i, want := range wantPaths {
		if (*captured)[i].Path != want {
			t.Fatalf("path %d = %q, want %q", i, (*captured)[i].Path, want)
		}
		if (*captured)[i].Method != http.MethodGet {
			t.Fatalf("method %d = %q, want GET", i, (*captured)[i].Method)
		}
	}
	if !strings.Contains((*captured)[0].Query, "q=going+concern") && !strings.Contains((*captured)[0].Query, "q=going%20concern") {
		t.Fatalf("fulltext query missing q: %q", (*captured)[0].Query)
	}
}

func TestFactorParityWrappersRouteToLaunchPaths(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	calls := []func() error{
		func() error {
			_, err := client.FactorHistory("MKT/US", map[string]string{"response_mode": "compact"})
			return err
		},
		func() error {
			_, err := client.FactorSparklines(map[string]string{"factors": "MOMENTUM,VALUE"})
			return err
		},
		func() error { _, err := client.FactorExtremeMoves(map[string]string{"side": "both"}); return err },
		func() error {
			_, err := client.FactorExtremePairs(map[string]string{"sort": "abs_spread_return"})
			return err
		},
		func() error { _, err := client.FactorValuations(map[string]string{"side": "tailwind"}); return err },
		func() error {
			_, err := client.FactorValuationStocks(map[string]string{"factor": "VALUE", "sort": "score"})
			return err
		},
		func() error {
			_, err := client.FactorPairs(map[string]string{"factor1": "MOMENTUM", "factor2": "VALUE"})
			return err
		},
		func() error {
			_, err := client.FactorPairHistory("MOM/US", "VAL/US", map[string]string{"response_mode": "compact"})
			return err
		},
		func() error { _, err := client.FactorBulkDownload(map[string]string{"include": "series"}); return err },
		func() error {
			_, err := client.FactorCustom(map[string]any{"symbol": "AAPL"}, map[string]string{"response_mode": "compact"})
			return err
		},
		func() error {
			_, err := client.PortfolioAttribution(
				map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}},
				map[string]string{"response_mode": "compact"},
			)
			return err
		},
		func() error {
			_, err := client.ModelFactorAnalysis(
				map[string]any{"model": map[string]any{"id": "draft"}, "holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}},
				map[string]string{"response_mode": "compact"},
			)
			return err
		},
		func() error {
			_, err := client.PortfolioHedge(
				map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}, "constraints": map[string]any{"maxHedges": 1}},
				map[string]string{"response_mode": "compact"},
			)
			return err
		},
	}

	for _, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("wrapper call failed: %v", err)
		}
	}

	wantPaths := []string{
		"/v1/factors/history/MKT%2FUS",
		"/v1/factors/sparklines",
		"/v1/factors/extreme-moves",
		"/v1/factors/extreme-pairs",
		"/v1/factors/valuations",
		"/v1/factors/valuations/stocks",
		"/v1/factors/pairs",
		"/v1/factors/pair-history/MOM%2FUS/VAL%2FUS",
		"/v1/factors/bulk-download",
		"/v1/factors/custom",
		"/v1/portfolio/attribution",
		"/v1/models/factor-analysis",
		"/v1/portfolio/hedge",
	}
	for i, want := range wantPaths {
		if (*captured)[i].Path != want {
			t.Fatalf("path %d = %q, want %q", i, (*captured)[i].Path, want)
		}
	}
	if !strings.Contains((*captured)[0].Query, "response_mode=compact") {
		t.Fatalf("factor history query missing response_mode: %q", (*captured)[0].Query)
	}
	if !strings.Contains((*captured)[9].Query, "response_mode=compact") {
		t.Fatalf("factor custom query missing response_mode: %q", (*captured)[9].Query)
	}
	if !strings.Contains((*captured)[12].Query, "response_mode=compact") {
		t.Fatalf("portfolio hedge query missing response_mode: %q", (*captured)[12].Query)
	}
	for i := 9; i < len(*captured); i++ {
		if (*captured)[i].Method != http.MethodPost {
			t.Fatalf("method %d = %q, want POST", i, (*captured)[i].Method)
		}
	}
	if strings.Contains((*captured)[12].Body, "response_mode") {
		t.Fatalf("portfolio hedge body should not contain response_mode: %q", (*captured)[12].Body)
	}
	if !strings.Contains((*captured)[12].Body, "constraints") {
		t.Fatalf("portfolio hedge body missing constraints: %q", (*captured)[12].Body)
	}
}

package secapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type capturedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   string
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newCaptureClient(t *testing.T) (*Client, *[]capturedRequest, func()) {
	t.Helper()
	return newCaptureClientWithPayload(t, map[string]any{"ok": true})
}

func newCaptureClientWithPayload(t *testing.T, responseBody any) (*Client, *[]capturedRequest, func()) {
	t.Helper()
	payload, err := json.Marshal(responseBody)
	if err != nil {
		t.Fatalf("marshal response body: %v", err)
	}
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
			Header: r.Header.Clone(),
			Body:   body,
		})
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(payload)
	}))

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	return client, &captured, server.Close
}

func TestRequestHeadersIdentifyGoSDKAndJsonContract(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()
	client.APIKey = "secapi_test_key"

	if _, err := client.Health(); err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	request := (*captured)[0]
	if request.Header.Get("Accept") != "application/json" {
		t.Fatalf("Accept = %q, want application/json", request.Header.Get("Accept"))
	}
	wantUserAgent := "secapi-go/" + SDKVersion
	if request.Header.Get("User-Agent") != wantUserAgent {
		t.Fatalf("User-Agent = %q, want %s", request.Header.Get("User-Agent"), wantUserAgent)
	}
	if request.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", request.Header.Get("Content-Type"))
	}
	if request.Header.Get("Secapi-Version") != "2026-03-19" {
		t.Fatalf("Secapi-Version = %q, want 2026-03-19", request.Header.Get("Secapi-Version"))
	}
	if request.Header.Get("X-Api-Key") != "secapi_test_key" {
		t.Fatalf("X-Api-Key = %q, want configured key", request.Header.Get("X-Api-Key"))
	}
}

func TestRequestDiagnosticsEscapesRequestID(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	payload, err := client.RequestDiagnostics("req/with space")
	if err != nil {
		t.Fatalf("RequestDiagnostics failed: %v", err)
	}

	if payload["ok"] != true {
		t.Fatalf("payload = %#v, want ok response", payload)
	}
	request := (*captured)[0]
	if request.Method != http.MethodGet {
		t.Fatalf("method = %s, want GET", request.Method)
	}
	if request.Path != "/v1/diagnostics/requests/req%2Fwith%20space" {
		t.Fatalf("path = %q, want escaped diagnostics path", request.Path)
	}
}

func TestNewClientLoadsBearerTokenEnvironmentFallback(t *testing.T) {
	t.Setenv("SECAPI_API_KEY", "")
	t.Setenv("OMNI_DATASTREAM_API_KEY", "")
	t.Setenv("SECAPI_BEARER_TOKEN", "")
	t.Setenv("OMNI_DATASTREAM_BEARER_TOKEN", "bearer_OMNI_FALLBACK")

	client := NewClient("")

	if client.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty when only bearer env is configured", client.APIKey)
	}
	if client.BearerToken != "bearer_OMNI_FALLBACK" {
		t.Fatalf("BearerToken = %q, want compatibility env fallback", client.BearerToken)
	}
}

func TestNewBearerTokenClientSetsAuthorizationHeader(t *testing.T) {
	captureClient, captured, closeServer := newCaptureClient(t)
	defer closeServer()
	client := NewBearerTokenClient("bearer_explicit_token")
	client.BaseURL = captureClient.BaseURL
	client.HTTPClient = captureClient.HTTPClient

	if _, err := client.Health(); err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	request := (*captured)[0]
	if request.Header.Get("Authorization") != "Bearer bearer_explicit_token" {
		t.Fatalf("Authorization = %q, want bearer token", request.Header.Get("Authorization"))
	}
	if request.Header.Get("X-Api-Key") != "" {
		t.Fatalf("X-Api-Key = %q, want empty for bearer-only client", request.Header.Get("X-Api-Key"))
	}
}

func TestNewBearerTokenClientDoesNotLoadApiKeyEnvironmentFallback(t *testing.T) {
	t.Setenv("SECAPI_API_KEY", "env_api_key")

	client := NewBearerTokenClient("bearer_explicit_token")

	if client.APIKey != "" {
		t.Fatalf("APIKey = %q, want explicit bearer constructor to avoid API key env fallback", client.APIKey)
	}
	if client.BearerToken != "bearer_explicit_token" {
		t.Fatalf("BearerToken = %q, want explicit bearer token", client.BearerToken)
	}
}

func TestNewBearerTokenClientLoadsBearerEnvironmentFallback(t *testing.T) {
	t.Setenv("SECAPI_API_KEY", "env_api_key")
	t.Setenv("SECAPI_BEARER_TOKEN", "bearer_ENV_FALLBACK")
	t.Setenv("OMNI_DATASTREAM_BEARER_TOKEN", "bearer_OMNI_FALLBACK")

	client := NewBearerTokenClient("")

	if client.APIKey != "" {
		t.Fatalf("APIKey = %q, want bearer constructor to avoid API key env fallback", client.APIKey)
	}
	if client.BearerToken != "bearer_ENV_FALLBACK" {
		t.Fatalf("BearerToken = %q, want bearer env fallback", client.BearerToken)
	}
}

func TestNewClientLoadsEnvironmentFallbacks(t *testing.T) {
	t.Setenv("SECAPI_API_KEY", "env_fallback_api_key")
	t.Setenv("SECAPI_BASE_URL", "https://env.secapi.test/")

	client := NewClient("")

	if client.APIKey != "env_fallback_api_key" {
		t.Fatalf("APIKey = %q, want env fallback", client.APIKey)
	}
	if client.BaseURL != "https://env.secapi.test/" {
		t.Fatalf("BaseURL = %q, want env fallback", client.BaseURL)
	}
}

func TestNewClientExplicitApiKeyOverridesEnvironmentFallback(t *testing.T) {
	t.Setenv("SECAPI_API_KEY", "env_fallback_api_key")

	client := NewClient("explicit_api_key")

	if client.APIKey != "explicit_api_key" {
		t.Fatalf("APIKey = %q, want explicit key", client.APIKey)
	}
}

func TestNewClientLoadsCompatibilityEnvironmentFallbacks(t *testing.T) {
	t.Setenv("SECAPI_API_KEY", "")
	t.Setenv("SECAPI_BASE_URL", "")
	t.Setenv("OMNI_DATASTREAM_API_KEY", "omni_fallback_api_key")
	t.Setenv("OMNI_DATASTREAM_BASE_URL", "")
	t.Setenv("OMNI_DATASTREAM_API_BASE_URL", "https://omni-api.secapi.test/")

	client := NewClient("")

	if client.APIKey != "omni_fallback_api_key" {
		t.Fatalf("APIKey = %q, want compatibility env fallback", client.APIKey)
	}
	if client.BaseURL != "https://omni-api.secapi.test/" {
		t.Fatalf("BaseURL = %q, want compatibility env fallback", client.BaseURL)
	}
}

func TestGroupedServiceFieldsDelegateToFlatClientMethods(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	calls := []struct {
		name      string
		call      func() error
		wantPath  string
		wantQuery map[string]string
	}{
		{
			name: "entities resolve",
			call: func() error {
				_, err := client.Entities.Resolve(map[string]string{"ticker": "AAPL"})
				return err
			},
			wantPath:  "/v1/entities/resolve",
			wantQuery: map[string]string{"ticker": "AAPL"},
		},
		{
			name: "filings latest",
			call: func() error {
				_, err := client.Filings.Latest(map[string]string{"ticker": "AAPL", "form": "10-K"})
				return err
			},
			wantPath:  "/v1/filings/latest",
			wantQuery: map[string]string{"ticker": "AAPL", "form": "10-K"},
		},
		{
			name: "sections latest",
			call: func() error {
				_, err := client.Sections.Latest("item_1a", map[string]string{"ticker": "AAPL", "form": "10-K", "mode": "compact"})
				return err
			},
			wantPath:  "/v1/filings/latest/sections/item_1a",
			wantQuery: map[string]string{"ticker": "AAPL", "form": "10-K", "mode": "compact"},
		},
		{
			name: "search semantic",
			call: func() error {
				_, err := client.Search.Semantic(map[string]string{"q": "supply chain risk", "ticker": "AAPL", "mode": "hybrid", "view": "agent"})
				return err
			},
			wantPath:  "/v1/search/semantic",
			wantQuery: map[string]string{"q": "supply chain risk", "ticker": "AAPL", "mode": "hybrid", "view": "agent"},
		},
		{
			name: "factors history",
			call: func() error {
				_, err := client.Factors.History("VALUE", map[string]string{"range": "1y", "response_mode": "compact", "include": "trust,series"})
				return err
			},
			wantPath:  "/v1/factors/history/VALUE",
			wantQuery: map[string]string{"range": "1y", "response_mode": "compact", "include": "trust,series"},
		},
		{
			name: "factors dashboard",
			call: func() error {
				_, err := client.Factors.Dashboard(map[string]string{"country": "US", "category": "style", "ticker": "AAPL", "response_mode": "compact"})
				return err
			},
			wantPath:  "/v1/factors/dashboard",
			wantQuery: map[string]string{"country": "US", "category": "style", "ticker": "AAPL", "response_mode": "compact"},
		},
	}

	for _, test := range calls {
		if err := test.call(); err != nil {
			t.Fatalf("%s failed: %v", test.name, err)
		}
	}
	if len(*captured) != len(calls) {
		t.Fatalf("captured %d requests, want %d", len(*captured), len(calls))
	}
	for i, test := range calls {
		request := (*captured)[i]
		if request.Path != test.wantPath {
			t.Fatalf("%s path = %q, want %q", test.name, request.Path, test.wantPath)
		}
		query, err := url.ParseQuery(request.Query)
		if err != nil {
			t.Fatalf("%s query parse failed: %v", test.name, err)
		}
		for key, value := range test.wantQuery {
			if query.Get(key) != value {
				t.Fatalf("%s query %s = %q, want %q in %q", test.name, key, query.Get(key), value, request.Query)
			}
		}
	}
}

func TestConstructorsInitializeGroupedServiceFields(t *testing.T) {
	clients := []*Client{
		NewClient("explicit_api_key"),
		NewBearerTokenClient("bearer_explicit_token"),
		NewSecApiClient("explicit_api_key"),
	}

	for i, client := range clients {
		if client.Entities == nil || client.Filings == nil || client.Sections == nil || client.Search == nil || client.Factors == nil {
			t.Fatalf("client %d has nil grouped service field", i)
		}
	}
}

func TestNewClientUsesDefaultHTTPTimeout(t *testing.T) {
	client := NewClient("")

	if client.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}
	if client.HTTPClient.Timeout != DefaultHTTPTimeout {
		t.Fatalf("HTTPClient.Timeout = %s, want %s", client.HTTPClient.Timeout, DefaultHTTPTimeout)
	}
}

func TestNewClientHTTPTimeoutsArePerClient(t *testing.T) {
	first := NewClient("")
	second := NewClient("")

	first.HTTPClient.Timeout = time.Second

	if second.HTTPClient.Timeout != DefaultHTTPTimeout {
		t.Fatalf("second timeout = %s, want isolated default %s", second.HTTPClient.Timeout, DefaultHTTPTimeout)
	}
}

func TestCustomHTTPClientOverridesDefaultTimeout(t *testing.T) {
	client := NewClient("")
	custom := &http.Client{Timeout: 2 * time.Second}
	client.HTTPClient = custom

	if client.httpClient() != custom {
		t.Fatal("httpClient did not return the configured custom client")
	}
}

func TestNilHTTPClientFallsBackToDefaultTimeoutClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = nil

	result, err := client.Health()
	if err != nil {
		t.Fatalf("Health with nil HTTPClient failed: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v, want ok true", result)
	}
}

func TestRetriesSafeGETOnTransientStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "application/json")
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"temporarily_unavailable","message":"try again"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.RetryConfig = RetryConfig{MaxRetries: 1, InitialBackoff: 0}

	result, err := client.LatestFiling(map[string]string{"ticker": "AAPL"})
	if err != nil {
		t.Fatalf("LatestFiling failed after retry: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v, want ok true", result)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestRetryAfterIsClampedToMaxBackoff(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "application/json")
		if attempts == 1 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"rate_limited","message":"try later"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.RetryConfig = RetryConfig{MaxRetries: 1, InitialBackoff: 0, MaxBackoff: time.Nanosecond}

	startedAt := time.Now()
	result, err := client.LatestFiling(map[string]string{"ticker": "AAPL"})
	if err != nil {
		t.Fatalf("LatestFiling failed after retry: %v", err)
	}
	if time.Since(startedAt) > 200*time.Millisecond {
		t.Fatalf("retry slept too long despite MaxBackoff clamp")
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v, want ok true", result)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestRetriesSafeGETOnTransportError(t *testing.T) {
	attempts := 0
	client := NewClient("")
	client.BaseURL = "https://api.secapi.test"
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("temporary network failure")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Request:    req,
			}, nil
		}),
	}
	client.RetryConfig = RetryConfig{MaxRetries: 1, InitialBackoff: 0}

	result, err := client.LatestFiling(map[string]string{"ticker": "AAPL"})
	if err != nil {
		t.Fatalf("LatestFiling failed after transport retry: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v, want ok true", result)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestDoesNotRetryPOSTOnTransientStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"temporarily_unavailable","message":"try again"}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.RetryConfig = RetryConfig{MaxRetries: 2, InitialBackoff: 0}

	_, err := client.PortfolioAnalyze(map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want *APIError", err, err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 for POST", attempts)
	}
}

func TestRetriesPOSTOnRateLimitStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "application/json")
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"rate_limited","message":"try again"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.RetryConfig = RetryConfig{MaxRetries: 1, InitialBackoff: 0}

	result, err := client.PortfolioAnalyze(map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}})
	if err != nil {
		t.Fatalf("PortfolioAnalyze failed after 429 retry: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v, want ok true", result)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestRetriesRateLimitWithStructuredRetryTiming(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "application/json")
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"rate_limited","message":"try again","details":{"retryAfterMs":0}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.RetryConfig = RetryConfig{MaxRetries: 1, InitialBackoff: time.Hour, MaxBackoff: time.Hour}

	startedAt := time.Now()
	result, err := client.PortfolioAnalyze(map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}})
	if err != nil {
		t.Fatalf("PortfolioAnalyze failed after structured retry timing: %v", err)
	}
	if time.Since(startedAt) > 200*time.Millisecond {
		t.Fatalf("retry ignored structured retry timing and slept too long")
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v, want ok true", result)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestRetryAfterHeaderPrecedesStructuredRetryTiming(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "application/json")
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"rate_limited","message":"try again","retryAfterSeconds":60}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.RetryConfig = RetryConfig{MaxRetries: 1, InitialBackoff: time.Hour, MaxBackoff: time.Hour}

	startedAt := time.Now()
	result, err := client.PortfolioAnalyze(map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}})
	if err != nil {
		t.Fatalf("PortfolioAnalyze failed after Retry-After retry: %v", err)
	}
	if time.Since(startedAt) > 200*time.Millisecond {
		t.Fatalf("retry ignored Retry-After header precedence and slept too long")
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v, want ok true", result)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestRetriesSafeGETWithNestedStructuredRetryTiming(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "application/json")
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":-32006,"message":"mcp_tool_circuit_open","data":{"retryAfterSeconds":0}}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.RetryConfig = RetryConfig{MaxRetries: 1, InitialBackoff: time.Hour, MaxBackoff: time.Hour}

	startedAt := time.Now()
	result, err := client.LatestFiling(map[string]string{"ticker": "AAPL"})
	if err != nil {
		t.Fatalf("LatestFiling failed after nested structured retry timing: %v", err)
	}
	if time.Since(startedAt) > 200*time.Millisecond {
		t.Fatalf("retry ignored nested structured retry timing and slept too long")
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v, want ok true", result)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestDoesNotRetryPOSTOnTransportError(t *testing.T) {
	attempts := 0
	client := NewClient("")
	client.BaseURL = "https://api.secapi.test"
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			attempts++
			return nil, errors.New("temporary network failure")
		}),
	}
	client.RetryConfig = RetryConfig{MaxRetries: 2, InitialBackoff: 0}

	_, err := client.PortfolioAnalyze(map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 for POST transport error", attempts)
	}
}

func TestRetryConfigCanDisableRetries(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"temporarily_unavailable","message":"try again"}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.RetryConfig = RetryConfig{MaxRetries: 0}

	_, err := client.LatestFiling(map[string]string{"ticker": "AAPL"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want *APIError", err, err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want retries disabled", attempts)
	}
}

func TestAPIErrorPreservesStructuredErrorFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"missing_api_key","message":"Missing API key","requestId":"req_body_123","docsUrl":"https://docs.secapi.ai"}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	_, err := client.LatestFiling(map[string]string{"ticker": "AAPL"})
	if err == nil {
		t.Fatal("expected APIError")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusUnauthorized)
	}
	if apiErr.Code != "missing_api_key" {
		t.Fatalf("Code = %q, want missing_api_key", apiErr.Code)
	}
	if apiErr.Message != "Missing API key" {
		t.Fatalf("Message = %q, want Missing API key", apiErr.Message)
	}
	if apiErr.RequestID != "req_body_123" {
		t.Fatalf("RequestID = %q, want req_body_123", apiErr.RequestID)
	}
	if apiErr.Body["docsUrl"] != "https://docs.secapi.ai" {
		t.Fatalf("Body docsUrl = %v, want docs URL", apiErr.Body["docsUrl"])
	}
	if !strings.Contains(apiErr.Error(), "request_id=req_body_123") {
		t.Fatalf("Error() missing request id: %q", apiErr.Error())
	}
}

func TestAPIErrorUsesRequestIDHeaderFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Header().Set("Request-Id", "req_header_456")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"rate_limited","message":"Too many requests"}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	_, err := client.SearchFilings(map[string]string{"ticker": "AAPL"})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.RequestID != "req_header_456" {
		t.Fatalf("RequestID = %q, want header fallback", apiErr.RequestID)
	}
	if apiErr.Code != "rate_limited" {
		t.Fatalf("Code = %q, want rate_limited", apiErr.Code)
	}
}

func TestAPIErrorHandlesNonJSONErrorPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain")
		w.Header().Set("X-Correlation-Id", "req_plain_789")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	_, err := client.Health()

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadGateway)
	}
	if apiErr.Message != "upstream unavailable" {
		t.Fatalf("Message = %q, want plain payload", apiErr.Message)
	}
	if apiErr.RequestID != "req_plain_789" {
		t.Fatalf("RequestID = %q, want correlation id", apiErr.RequestID)
	}
	if apiErr.Body != nil {
		t.Fatalf("Body = %#v, want nil for non-JSON payload", apiErr.Body)
	}
}

func TestAPIErrorHandlesEmptyNon2xxPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Request-Id", "req_not_modified")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	_, err := client.Health()

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotModified {
		t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusNotModified)
	}
	if apiErr.RequestID != "req_not_modified" {
		t.Fatalf("RequestID = %q, want header request id", apiErr.RequestID)
	}
}

func TestAPIErrorHandlesJSONRedirectPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"code":"multiple_choices","message":"Choose an origin","requestId":"req_redirect"}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	_, err := client.Health()

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusMultipleChoices {
		t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusMultipleChoices)
	}
	if apiErr.Code != "multiple_choices" {
		t.Fatalf("Code = %q, want multiple_choices", apiErr.Code)
	}
	if apiErr.RequestID != "req_redirect" {
		t.Fatalf("RequestID = %q, want body request id", apiErr.RequestID)
	}
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

func TestPublicEarningsWrappersRouteToPublicPaths(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.IntelligenceEarningsPreview(map[string]string{"ticker": "AAPL", "view": "compact"}); err != nil {
		t.Fatalf("IntelligenceEarningsPreview failed: %v", err)
	}
	if _, err := client.EarningsTranscripts(map[string]string{"ticker": "AAPL", "limit": "3"}); err != nil {
		t.Fatalf("EarningsTranscripts failed: %v", err)
	}

	wantPaths := []string{"/v1/intelligence/earnings-preview", "/v1/earnings/transcripts"}
	for i, want := range wantPaths {
		if (*captured)[i].Path != want {
			t.Fatalf("path %d = %q, want %q", i, (*captured)[i].Path, want)
		}
		if (*captured)[i].Method != http.MethodGet {
			t.Fatalf("method %d = %q, want GET", i, (*captured)[i].Method)
		}
	}
	if !strings.Contains((*captured)[0].Query, "ticker=AAPL") || !strings.Contains((*captured)[0].Query, "view=compact") {
		t.Fatalf("earnings preview query missing params: %q", (*captured)[0].Query)
	}
	if !strings.Contains((*captured)[1].Query, "ticker=AAPL") || !strings.Contains((*captured)[1].Query, "limit=3") {
		t.Fatalf("earnings transcripts query missing params: %q", (*captured)[1].Query)
	}
}

func TestEmbedSituationHelpersRouteToPublicSurface(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.EmbedSituations(map[string]string{"limit": "20", "tickers": "AAPL"}); err != nil {
		t.Fatalf("EmbedSituations failed: %v", err)
	}
	if _, err := client.EmbedSituationsFeed(map[string]string{"limit": "5", "types": "merger"}); err != nil {
		t.Fatalf("EmbedSituationsFeed failed: %v", err)
	}
	if _, err := client.EmbedSituationsFeedRSS(map[string]string{"types": "merger"}); err != nil {
		t.Fatalf("EmbedSituationsFeedRSS failed: %v", err)
	}
	if _, err := client.EmbedSituationsStats(nil); err != nil {
		t.Fatalf("EmbedSituationsStats failed: %v", err)
	}
	if _, err := client.EmbedSituation("sit/with spaces", nil); err != nil {
		t.Fatalf("EmbedSituation failed: %v", err)
	}
	if _, err := client.EmbedSituationExport("sit/with spaces", nil); err != nil {
		t.Fatalf("EmbedSituationExport failed: %v", err)
	}
	if _, err := client.EmbedSituationIssues(map[string]string{"limit": "12"}); err != nil {
		t.Fatalf("EmbedSituationIssues failed: %v", err)
	}
	if _, err := client.EmbedSituationIssue("special/situations digest 22"); err != nil {
		t.Fatalf("EmbedSituationIssue failed: %v", err)
	}

	wantPaths := []string{
		"/v1/embed/situations",
		"/v1/embed/situations/feed",
		"/v1/embed/situations/feed.rss",
		"/v1/embed/situations/stats",
		"/v1/embed/situations/sit%2Fwith%20spaces",
		"/v1/embed/situations/sit%2Fwith%20spaces/export",
		"/v1/embed/situations/issues",
		"/v1/embed/situations/issues/special%2Fsituations%20digest%2022",
	}
	for i, want := range wantPaths {
		if (*captured)[i].Path != want {
			t.Fatalf("path %d = %q, want %q", i, (*captured)[i].Path, want)
		}
		if (*captured)[i].Method != http.MethodGet {
			t.Fatalf("method %d = %q, want GET", i, (*captured)[i].Method)
		}
	}
	query, err := url.ParseQuery((*captured)[0].Query)
	if err != nil {
		t.Fatalf("embed list query parse failed: %v", err)
	}
	if query.Get("limit") != "20" || query.Get("tickers") != "AAPL" {
		t.Fatalf("embed list query = %q, want limit/tickers", (*captured)[0].Query)
	}
	feedQuery, err := url.ParseQuery((*captured)[1].Query)
	if err != nil {
		t.Fatalf("embed feed query parse failed: %v", err)
	}
	if feedQuery.Get("limit") != "5" || feedQuery.Get("types") != "merger" {
		t.Fatalf("embed feed query = %q, want limit/types", (*captured)[1].Query)
	}
	issueQuery, err := url.ParseQuery((*captured)[6].Query)
	if err != nil {
		t.Fatalf("embed issue query parse failed: %v", err)
	}
	if issueQuery.Get("limit") != "12" {
		t.Fatalf("issue query = %q, want limit=12", (*captured)[6].Query)
	}
}

func TestPaidSituationHelpersRouteToAuthenticatedSurface(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	calls := []func() error{
		func() error {
			_, err := client.ListSituations(map[string]string{"types": "merger, tender_offer", "limit": "20"})
			return err
		},
		func() error {
			_, err := client.GetSituation("sit/with spaces", map[string]string{"enrich": "false"})
			return err
		},
		func() error {
			_, err := client.SituationsByForm("SC 13D", map[string]string{"tickers": "AAPL, MSFT"})
			return err
		},
		func() error {
			_, err := client.SituationsFeed(map[string]string{"tickers": "AAPL, MSFT"})
			return err
		},
		func() error {
			_, err := client.SituationsIssues(map[string]string{"limit": "12"})
			return err
		},
		func() error {
			_, err := client.SituationIssue("special/situations digest 22", nil)
			return err
		},
		func() error {
			_, err := client.SituationsCalendar(map[string]string{"statuses": "pending"})
			return err
		},
		func() error {
			_, err := client.SituationsStats(map[string]string{"window": "30d"})
			return err
		},
		func() error {
			_, err := client.SituationFilings("sit/with spaces", map[string]string{"limit": "10"})
			return err
		},
		func() error {
			_, err := client.SituationSummary("sit/with spaces", nil)
			return err
		},
		func() error {
			_, err := client.ExportSituation("sit/with spaces", nil)
			return err
		},
		func() error {
			_, err := client.UnderwriteSituation("sit/with spaces", nil)
			return err
		},
	}
	for i, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}

	want := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/situations"},
		{http.MethodGet, "/v1/situations/sit%2Fwith%20spaces"},
		{http.MethodGet, "/v1/situations/by-form/SC%2013D"},
		{http.MethodGet, "/v1/situations/feed"},
		{http.MethodGet, "/v1/situations/issues"},
		{http.MethodGet, "/v1/situations/issues/special%2Fsituations%20digest%2022"},
		{http.MethodGet, "/v1/situations/calendar"},
		{http.MethodGet, "/v1/situations/stats"},
		{http.MethodGet, "/v1/situations/sit%2Fwith%20spaces/filings"},
		{http.MethodGet, "/v1/situations/sit%2Fwith%20spaces/summary"},
		{http.MethodGet, "/v1/situations/sit%2Fwith%20spaces/export"},
		{http.MethodGet, "/v1/situations/sit%2Fwith%20spaces/underwriting-pack"},
	}
	for i, expected := range want {
		if (*captured)[i].Method != expected.method || (*captured)[i].Path != expected.path {
			t.Fatalf("request %d = %s %s, want %s %s", i, (*captured)[i].Method, (*captured)[i].Path, expected.method, expected.path)
		}
	}
	query, err := url.ParseQuery((*captured)[0].Query)
	if err != nil {
		t.Fatalf("list query parse failed: %v", err)
	}
	if query.Get("types") != "merger,tender_offer" || query.Get("limit") != "20" {
		t.Fatalf("list query = %q, want normalized types/limit", (*captured)[0].Query)
	}
}

func TestTypedSituationRequestBuildersRouteToAuthenticatedSurface(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.Situations.ListWithParams(SituationListParams{
		Types:        " merger, tender_offer ",
		Statuses:     "announced",
		MarketCap:    "large",
		Limit:        "25",
		ResponseMode: "agent",
	}); err != nil {
		t.Fatalf("ListWithParams failed: %v", err)
	}
	if _, err := client.Situations.FeedWithParams(SituationFeedParams{
		Types:      "merger",
		Categories: "merger_acquisition",
		Tickers:    "AAPL, MSFT",
		Country:    "US",
		Since:      "2026-07-01T00:00:00.000Z",
		Limit:      "10",
	}); err != nil {
		t.Fatalf("FeedWithParams failed: %v", err)
	}
	if _, err := client.Situations.FeedRSSWithParams(SituationFeedRSSParams{
		Types:      "merger",
		Categories: "merger_acquisition",
		Tickers:    "AAPL, MSFT",
		Country:    "US",
		Since:      "2026-07-01T00:00:00.000Z",
	}); err != nil {
		t.Fatalf("FeedRSSWithParams failed: %v", err)
	}
	if _, err := client.Situations.GetWithParams("sit/with spaces", SituationMemberParams{Enrich: "false"}); err != nil {
		t.Fatalf("GetWithParams failed: %v", err)
	}
	if _, err := client.Situations.FilingsWithParams("sit/with spaces", SituationMemberParams{Limit: "5"}); err != nil {
		t.Fatalf("FilingsWithParams failed: %v", err)
	}

	if (*captured)[0].Path != "/v1/situations" {
		t.Fatalf("list path = %q", (*captured)[0].Path)
	}
	listQuery, err := url.ParseQuery((*captured)[0].Query)
	if err != nil {
		t.Fatalf("list query parse failed: %v", err)
	}
	if listQuery.Get("types") != "merger,tender_offer" || listQuery.Get("market_cap") != "large" || listQuery.Get("response_mode") != "agent" {
		t.Fatalf("typed list query = %q, want OpenAPI parameter names", (*captured)[0].Query)
	}
	feedQuery, err := url.ParseQuery((*captured)[1].Query)
	if err != nil {
		t.Fatalf("feed query parse failed: %v", err)
	}
	if feedQuery.Get("categories") != "merger_acquisition" || feedQuery.Get("tickers") != "AAPL,MSFT" || feedQuery.Get("country") != "US" {
		t.Fatalf("typed feed query = %q, want normalized categories/tickers", (*captured)[1].Query)
	}
	rssQuery, err := url.ParseQuery((*captured)[2].Query)
	if err != nil {
		t.Fatalf("rss query parse failed: %v", err)
	}
	if rssQuery.Get("categories") != "merger_acquisition" || rssQuery.Get("tickers") != "AAPL,MSFT" || rssQuery.Get("country") != "US" || rssQuery.Get("limit") != "" {
		t.Fatalf("typed rss query = %q, want RSS-compatible parameters only", (*captured)[2].Query)
	}
	if (*captured)[3].Path != "/v1/situations/sit%2Fwith%20spaces" || (*captured)[4].Path != "/v1/situations/sit%2Fwith%20spaces/filings" {
		t.Fatalf("typed member paths = %q / %q", (*captured)[3].Path, (*captured)[4].Path)
	}
}

func TestWatchSituationsUsesWatchlistAliasSurface(t *testing.T) {
	captureClient, captured, closeServer := newCaptureClient(t)
	defer closeServer()
	client := NewBearerTokenClient("human_bearer")
	client.BaseURL = captureClient.BaseURL
	client.HTTPClient = captureClient.HTTPClient

	_, err := client.Situations.Watch(SituationWatchParams{
		Name:    "  Deals  ",
		Filters: map[string][]string{"types": {"merger"}, "tickers": {" AAPL "}},
		StartAt: "2026-07-13T00:00:00Z",
		Delivery: SituationWatchDelivery{
			Email: " desk@example.com ",
		},
	})
	if err != nil {
		t.Fatalf("WatchSituations failed: %v", err)
	}

	if (*captured)[0].Method != http.MethodPost || (*captured)[0].Path != "/v1/situations/watchlists" {
		t.Fatalf("watch request = %s %s, want POST /v1/situations/watchlists", (*captured)[0].Method, (*captured)[0].Path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte((*captured)[0].Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["query"] != "situations.watch" || body["searchMode"] != "situation" || body["name"] != "Deals" {
		t.Fatalf("watch body routing fields = %#v", body)
	}
	delivery := body["delivery"].(map[string]any)
	if delivery["type"] != "email" || delivery["config"].(map[string]any)["to"] != "desk@example.com" {
		t.Fatalf("delivery = %#v, want normalized email delivery", delivery)
	}
	filters := body["filters"].(map[string]any)
	if filters["types"].([]any)[0] != "merger" || filters["tickers"].([]any)[0] != "AAPL" {
		t.Fatalf("filters = %#v, want normalized filters", filters)
	}
}

func TestWatchSituationsRejectsDeliveryWithoutBearerOnlyAuth(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()
	client.APIKey = "test_api_key"
	client.BearerToken = "human_bearer"

	_, err := client.Situations.Watch(SituationWatchParams{
		Filters:  map[string][]string{"types": {"merger"}},
		Delivery: SituationWatchDelivery{Email: "desk@example.com"},
	})
	if err == nil || !strings.Contains(err.Error(), "bearer-authenticated client without an API key") {
		t.Fatalf("delivery mixed auth error = %v, want bearer-only validation", err)
	}
	if len(*captured) != 0 {
		t.Fatalf("delivery validation should not send a request, sent %d", len(*captured))
	}

	client.APIKey = ""
	client.BearerToken = ""
	_, err = client.Situations.Watch(SituationWatchParams{
		Filters:  map[string][]string{"types": {"merger"}},
		Delivery: SituationWatchDelivery{Email: "desk@example.com"},
	})
	if err == nil || !strings.Contains(err.Error(), "bearer-authenticated client without an API key") {
		t.Fatalf("delivery missing bearer error = %v, want bearer-only validation", err)
	}
	if len(*captured) != 0 {
		t.Fatalf("delivery validation should not send a request, sent %d", len(*captured))
	}
}

func TestWatchSituationsSupportsFilterOnlyApiKeySafeCreate(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	_, err := client.Situations.Watch(SituationWatchParams{
		Name:    "  Deals  ",
		Filters: map[string][]string{"types": {"merger"}, "tickers": {" AAPL "}},
	})
	if err != nil {
		t.Fatalf("WatchSituations filter-only failed: %v", err)
	}

	if (*captured)[0].Method != http.MethodPost || (*captured)[0].Path != "/v1/situations/watchlists" {
		t.Fatalf("watch request = %s %s, want POST /v1/situations/watchlists", (*captured)[0].Method, (*captured)[0].Path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte((*captured)[0].Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body["delivery"]; ok {
		t.Fatalf("filter-only watch must omit delivery: %#v", body)
	}
}

func TestSituationWatchlistsUseAliasSurface(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.Situations.Watchlists(map[string]string{"limit": "10", "cursor": "20"}); err != nil {
		t.Fatalf("Watchlists failed: %v", err)
	}
	if _, err := client.Situations.Watchlist("mon/with spaces"); err != nil {
		t.Fatalf("Watchlist failed: %v", err)
	}
	if _, err := client.Situations.CreateWatchlist(SituationWatchlistParams{
		Name:    "  Deals  ",
		Filters: map[string][]string{"types": {"merger"}, "tickers": {" AAPL "}},
		StartAt: "2026-07-13T00:00:00Z",
	}); err != nil {
		t.Fatalf("CreateWatchlist failed: %v", err)
	}
	if err := client.Situations.DeleteWatchlist("mon/with spaces"); err != nil {
		t.Fatalf("DeleteWatchlist failed: %v", err)
	}

	if (*captured)[0].Method != http.MethodGet || (*captured)[0].Path != "/v1/situations/watchlists" {
		t.Fatalf("watchlists request = %s %s, want GET /v1/situations/watchlists", (*captured)[0].Method, (*captured)[0].Path)
	}
	query, err := url.ParseQuery((*captured)[0].Query)
	if err != nil {
		t.Fatalf("watchlists query parse failed: %v", err)
	}
	if query.Get("limit") != "10" || query.Get("cursor") != "20" {
		t.Fatalf("watchlists query = %q, want limit/cursor", (*captured)[0].Query)
	}
	if (*captured)[1].Method != http.MethodGet || (*captured)[1].Path != "/v1/situations/watchlists/mon%2Fwith%20spaces" {
		t.Fatalf("watchlist request = %s %s, want escaped watchlist GET", (*captured)[1].Method, (*captured)[1].Path)
	}
	if (*captured)[2].Method != http.MethodPost || (*captured)[2].Path != "/v1/situations/watchlists" {
		t.Fatalf("create watchlist request = %s %s, want POST /v1/situations/watchlists", (*captured)[2].Method, (*captured)[2].Path)
	}
	if (*captured)[3].Method != http.MethodDelete || (*captured)[3].Path != "/v1/situations/watchlists/mon%2Fwith%20spaces" {
		t.Fatalf("delete watchlist request = %s %s, want escaped watchlist DELETE", (*captured)[3].Method, (*captured)[3].Path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte((*captured)[2].Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["query"] != "situations.watch" || body["searchMode"] != "situation" || body["name"] != "Deals" {
		t.Fatalf("watchlist body routing fields = %#v", body)
	}
	if _, ok := body["webhookUrl"]; ok {
		t.Fatalf("watchlist body must not include webhookUrl: %#v", body)
	}
	if _, ok := body["delivery"]; ok {
		t.Fatalf("watchlist body must omit delivery for filter-only creates: %#v", body)
	}
	filters := body["filters"].(map[string]any)
	if filters["types"].([]any)[0] != "merger" || filters["tickers"].([]any)[0] != "AAPL" {
		t.Fatalf("filters = %#v, want normalized filters", filters)
	}
}

func TestWatchSituationsValidatesFiltersAndDelivery(t *testing.T) {
	client, _, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.WatchSituations(SituationWatchParams{}); err == nil {
		t.Fatal("expected missing filter error")
	}
	if _, err := client.WatchSituations(SituationWatchParams{
		Filters:  map[string][]string{"types": {"not-a-type"}},
		Delivery: SituationWatchDelivery{OrganizationWebhook: true},
	}); err == nil {
		t.Fatal("expected invalid canonical type error")
	}
	if _, err := client.WatchSituations(SituationWatchParams{
		Filters:  map[string][]string{"subtypes": {"not-a-subtype"}},
		Delivery: SituationWatchDelivery{OrganizationWebhook: true},
	}); err == nil {
		t.Fatal("expected invalid canonical subtype error")
	}
	if _, err := client.WatchSituations(SituationWatchParams{
		Filters:  map[string][]string{"situationIds": {"not-a-situation-id"}},
		Delivery: SituationWatchDelivery{OrganizationWebhook: true},
	}); err == nil {
		t.Fatal("expected invalid situation id error")
	}
}

func TestContextWrappersRouteToCorePaths(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	ctx := context.Background()
	calls := []func() error{
		func() error { _, err := client.HealthWithContext(ctx); return err },
		func() error { _, err := client.MeWithContext(ctx); return err },
		func() error {
			_, err := client.ResolveEntityWithContext(ctx, map[string]string{"ticker": "AAPL"})
			return err
		},
		func() error {
			_, err := client.SearchEntitiesWithContext(ctx, map[string]string{"q": "apple"})
			return err
		},
		func() error {
			_, err := client.SearchFilingsWithContext(ctx, map[string]string{"ticker": "AAPL"})
			return err
		},
		func() error {
			_, err := client.SearchSectionsWithContext(ctx, map[string]string{"ticker": "AAPL", "q": "risk"})
			return err
		},
		func() error {
			_, err := client.SearchFulltextWithContext(ctx, map[string]string{"q": "going concern"})
			return err
		},
		func() error {
			_, err := client.SemanticSearchWithContext(ctx, map[string]string{"q": "revenue risk", "mode": "hybrid"})
			return err
		},
		func() error {
			_, err := client.LatestFilingWithContext(ctx, map[string]string{"ticker": "AAPL", "form": "10-K"})
			return err
		},
		func() error {
			_, err := client.LatestSectionWithContext(ctx, "item/1a risk", map[string]string{"ticker": "AAPL"})
			return err
		},
		func() error {
			_, err := client.FilingByAccessionWithContext(ctx, "0000320193/25 000079", map[string]string{"view": "agent"})
			return err
		},
		func() error {
			_, err := client.FilingSectionByAccessionWithContext(ctx, "0000320193/25 000079", "item/7 md&a", map[string]string{"mode": "compact"})
			return err
		},
		func() error {
			_, err := client.AllStatementsWithContext(ctx, map[string]string{"ticker": "AAPL"})
			return err
		},
		func() error {
			_, err := client.FactorHistoryWithContext(ctx, "MKT/US", map[string]string{"response_mode": "compact"})
			return err
		},
		func() error {
			_, err := client.FactorDashboardWithContext(ctx, map[string]string{"ticker": "AAPL"})
			return err
		},
		func() error {
			_, err := client.PortfolioAnalyzeWithContext(
				ctx,
				map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}},
				map[string]string{"response_mode": "compact"},
			)
			return err
		},
		func() error {
			_, err := client.PortfolioAttributionWithContext(
				ctx,
				map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}},
				map[string]string{"response_mode": "compact"},
			)
			return err
		},
		func() error {
			_, err := client.PortfolioHedgeWithContext(
				ctx,
				map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}},
				map[string]string{"response_mode": "compact"},
			)
			return err
		},
		func() error {
			_, err := client.PortfolioOptimizeWithContext(
				ctx,
				map[string]any{"holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}},
				map[string]string{"response_mode": "compact"},
			)
			return err
		},
		func() error {
			_, err := client.ModelFactorAnalysisWithContext(
				ctx,
				map[string]any{"model": map[string]any{"id": "draft"}, "holdings": []map[string]any{{"symbol": "AAPL", "weight": 1}}},
				map[string]string{"response_mode": "compact"},
			)
			return err
		},
		func() error { _, err := client.MCPInfoWithContext(ctx); return err },
		func() error {
			_, err := client.CallMCPToolWithContext(ctx, "filings.latest", map[string]any{"ticker": "AAPL"}, "ctx-test")
			return err
		},
	}

	for _, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("context wrapper call failed: %v", err)
		}
	}

	wantPaths := []string{
		"/healthz",
		"/v1/me",
		"/v1/entities/resolve",
		"/v1/entities",
		"/v1/filings",
		"/v1/sections/search",
		"/v1/search/fulltext",
		"/v1/search/semantic",
		"/v1/filings/latest",
		"/v1/filings/latest/sections/item%2F1a%20risk",
		"/v1/filings/0000320193%2F25%20000079",
		"/v1/filings/0000320193%2F25%20000079/sections/item%2F7%20md&a",
		"/v1/statements/all",
		"/v1/factors/history/MKT%2FUS",
		"/v1/factors/dashboard",
		"/v1/portfolio/analyze",
		"/v1/portfolio/attribution",
		"/v1/portfolio/hedge",
		"/v1/portfolio/optimize",
		"/v1/models/factor-analysis",
		"/mcp",
		"/mcp",
	}
	for i, want := range wantPaths {
		if (*captured)[i].Path != want {
			t.Fatalf("path %d = %q, want %q", i, (*captured)[i].Path, want)
		}
	}
	for _, i := range []int{15, 16, 17, 18, 19, 21} {
		if (*captured)[i].Method != http.MethodPost {
			t.Fatalf("method %d = %q, want POST", i, (*captured)[i].Method)
		}
	}
	if !strings.Contains((*captured)[15].Query, "response_mode=compact") {
		t.Fatalf("portfolio analyze query missing response mode: %q", (*captured)[15].Query)
	}
	if !strings.Contains((*captured)[21].Body, `"id":"ctx-test"`) {
		t.Fatalf("MCP tool body missing request id: %q", (*captured)[21].Body)
	}
}

func TestContextCancellationPreventsRequest(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.LatestFilingWithContext(ctx, map[string]string{"ticker": "AAPL"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(*captured) != 0 {
		t.Fatalf("captured %d requests, want none after canceled context", len(*captured))
	}
}

func TestNilContextFallsBackToBackground(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.HealthWithContext(nil); err != nil {
		t.Fatalf("HealthWithContext(nil) failed: %v", err)
	}
	if len(*captured) != 1 || (*captured)[0].Path != "/healthz" {
		t.Fatalf("captured = %#v, want one health request", *captured)
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
	if !strings.Contains((*captured)[7].Query, "response_mode=compact") {
		t.Fatalf("factor custom query missing response_mode: %q", (*captured)[7].Query)
	}
	if !strings.Contains((*captured)[10].Query, "response_mode=compact") {
		t.Fatalf("portfolio hedge query missing response_mode: %q", (*captured)[10].Query)
	}
	for i := 7; i < len(*captured); i++ {
		if (*captured)[i].Method != http.MethodPost {
			t.Fatalf("method %d = %q, want POST", i, (*captured)[i].Method)
		}
	}
	if strings.Contains((*captured)[10].Body, "response_mode") {
		t.Fatalf("portfolio hedge body should not contain response_mode: %q", (*captured)[10].Body)
	}
	if !strings.Contains((*captured)[10].Body, "constraints") {
		t.Fatalf("portfolio hedge body missing constraints: %q", (*captured)[10].Body)
	}
}

func TestTraceWrappersEscapeDynamicPathSegments(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.GetTrace("trace/with spaces"); err != nil {
		t.Fatalf("GetTrace failed: %v", err)
	}

	if (*captured)[0].Path != "/v1/traces/trace%2Fwith%20spaces" {
		t.Fatalf("path = %q, want escaped trace id", (*captured)[0].Path)
	}
}

func TestDilutionEventDetailEscapesDynamicPathSegment(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.DilutionEventDetail("event/with spaces", map[string]string{"view": "agent"}); err != nil {
		t.Fatalf("DilutionEventDetail failed: %v", err)
	}

	if (*captured)[0].Path != "/v1/dilution/events/event%2Fwith%20spaces" {
		t.Fatalf("path = %q, want escaped event id", (*captured)[0].Path)
	}
	if !strings.Contains((*captured)[0].Query, "view=agent") {
		t.Fatalf("dilution event detail query missing view: %q", (*captured)[0].Query)
	}
}

func TestModelPortfolioFactorViewEscapesDynamicPathSegment(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.ModelPortfolioFactorView("portfolio/team alpha", map[string]string{"view": "agent"}); err != nil {
		t.Fatalf("ModelPortfolioFactorView failed: %v", err)
	}

	if (*captured)[0].Path != "/v1/model-portfolios/portfolio%2Fteam%20alpha/factor-view" {
		t.Fatalf("path = %q, want escaped portfolio id", (*captured)[0].Path)
	}
	if !strings.Contains((*captured)[0].Query, "view=agent") {
		t.Fatalf("model portfolio factor view query missing view: %q", (*captured)[0].Query)
	}
}

func TestStockLoadingsEscapesDynamicPathSegment(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.StockLoadings("BRK/B class", map[string]string{"view": "agent"}); err != nil {
		t.Fatalf("StockLoadings failed: %v", err)
	}

	if (*captured)[0].Path != "/v1/stocks/BRK%2FB%20class/loadings" {
		t.Fatalf("path = %q, want escaped ticker", (*captured)[0].Path)
	}
	if !strings.Contains((*captured)[0].Query, "view=agent") {
		t.Fatalf("stock loadings query missing view: %q", (*captured)[0].Query)
	}
}

func TestDeleteApiKeyEscapesDynamicPathSegment(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if err := client.DeleteApiKey("key/with spaces"); err != nil {
		t.Fatalf("DeleteApiKey failed: %v", err)
	}

	if (*captured)[0].Method != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", (*captured)[0].Method)
	}
	if (*captured)[0].Path != "/v1/api_keys/key%2Fwith%20spaces" {
		t.Fatalf("path = %q, want escaped key id", (*captured)[0].Path)
	}
}

func TestFilingWrappersEscapeDynamicPathSegments(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.LatestSection("item/1a risk", map[string]string{"ticker": "AAPL"}); err != nil {
		t.Fatalf("LatestSection failed: %v", err)
	}
	if _, err := client.FilingByAccession("0000320193/25 000079", map[string]string{"view": "agent"}); err != nil {
		t.Fatalf("FilingByAccession failed: %v", err)
	}
	if _, err := client.FilingSectionByAccession("0000320193/25 000079", "item/7 md&a", map[string]string{"mode": "compact"}); err != nil {
		t.Fatalf("FilingSectionByAccession failed: %v", err)
	}

	wantPaths := []string{
		"/v1/filings/latest/sections/item%2F1a%20risk",
		"/v1/filings/0000320193%2F25%20000079",
		"/v1/filings/0000320193%2F25%20000079/sections/item%2F7%20md&a",
	}
	for i, want := range wantPaths {
		if (*captured)[i].Path != want {
			t.Fatalf("path %d = %q, want %q", i, (*captured)[i].Path, want)
		}
	}
	if !strings.Contains((*captured)[0].Query, "ticker=AAPL") {
		t.Fatalf("latest section query missing ticker: %q", (*captured)[0].Query)
	}
	if !strings.Contains((*captured)[2].Query, "mode=compact") {
		t.Fatalf("filing section query missing mode: %q", (*captured)[2].Query)
	}
}

func TestTypedFilingParamsBuildCanonicalQueries(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.LatestFiling(LatestFilingParams{
		Ticker:     " AAPL ",
		Form:       "10-K",
		FY:         "2025",
		View:       ResponseViewAgent,
		FilingYear: "2026",
	}.Params()); err != nil {
		t.Fatalf("LatestFiling failed: %v", err)
	}
	if _, err := client.LatestSection("item_1a", LatestSectionParams{
		Ticker:  "MSFT",
		Form:    "10-Q",
		Quarter: "Q2",
		Mode:    "compact",
		Extra:   map[string]string{"filing_year": "2026"},
	}.Params()); err != nil {
		t.Fatalf("LatestSection failed: %v", err)
	}

	latest, err := url.ParseQuery((*captured)[0].Query)
	if err != nil {
		t.Fatalf("parse latest query: %v", err)
	}
	if latest.Get("ticker") != "AAPL" || latest.Get("form") != "10-K" || latest.Get("fy") != "2025" || latest.Get("view") != "agent" {
		t.Fatalf("latest filing query = %q", (*captured)[0].Query)
	}
	section, err := url.ParseQuery((*captured)[1].Query)
	if err != nil {
		t.Fatalf("parse latest section query: %v", err)
	}
	if section.Get("ticker") != "MSFT" || section.Get("form") != "10-Q" || section.Get("quarter") != "Q2" || section.Get("mode") != "compact" || section.Get("filing_year") != "2026" {
		t.Fatalf("latest section query = %q", (*captured)[1].Query)
	}
}

func TestTypedSearchStatementAndFactorParamsBuildCanonicalQueries(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.SearchSections(SectionSearchParams{
		Query:      "going concern",
		Ticker:     "TSLA",
		Form:       "10-K",
		FilingYear: "2025",
		Limit:      "3",
		View:       ResponseViewAgent,
		Extra:      map[string]string{"cursor": "next-page"},
	}.Params()); err != nil {
		t.Fatalf("SearchSections failed: %v", err)
	}
	if _, err := client.AllStatements(AllStatementsParams{
		Ticker: "AAPL",
		Period: "quarterly",
		FYFrom: "2023",
		FYTo:   "2025",
		Limit:  "8",
	}.Params()); err != nil {
		t.Fatalf("AllStatements failed: %v", err)
	}
	if _, err := client.FactorHistory("VALUE", FactorHistoryParams{
		Range:        "1y",
		DateTo:       "2026-06-09",
		ResponseMode: "compact",
		Include:      "trust,series",
		Extra:        map[string]string{"format": "json"},
	}.Params()); err != nil {
		t.Fatalf("FactorHistory failed: %v", err)
	}

	sections, err := url.ParseQuery((*captured)[0].Query)
	if err != nil {
		t.Fatalf("parse section search query: %v", err)
	}
	if sections.Get("q") != "going concern" || sections.Get("ticker") != "TSLA" || sections.Get("form") != "10-K" || sections.Get("filing_year") != "2025" || sections.Get("limit") != "3" || sections.Get("view") != "agent" || sections.Get("cursor") != "next-page" {
		t.Fatalf("section search query = %q", (*captured)[0].Query)
	}
	statements, err := url.ParseQuery((*captured)[1].Query)
	if err != nil {
		t.Fatalf("parse statements query: %v", err)
	}
	if statements.Get("ticker") != "AAPL" || statements.Get("period") != "quarterly" || statements.Get("fy_from") != "2023" || statements.Get("fy_to") != "2025" || statements.Get("limit") != "8" {
		t.Fatalf("statements query = %q", (*captured)[1].Query)
	}
	history, err := url.ParseQuery((*captured)[2].Query)
	if err != nil {
		t.Fatalf("parse factor history query: %v", err)
	}
	if history.Get("range") != "1y" || history.Get("date_to") != "2026-06-09" || history.Get("response_mode") != "compact" || history.Get("include") != "trust,series" || history.Get("format") != "json" {
		t.Fatalf("factor history query = %q", (*captured)[2].Query)
	}
}

func TestTypedParamsOmitEmptyValuesAndAllowExtraOverrides(t *testing.T) {
	params := FactorHistoryParams{
		Range:        "1y",
		ResponseMode: "compact",
		Include:      " ",
		Extra: map[string]string{
			"response_mode": "standard",
			"include":       "trust",
			"ignored":       " ",
		},
	}.Params()

	if params["response_mode"] != "standard" {
		t.Fatalf("extra should override typed value, got %q", params["response_mode"])
	}
	if params["include"] != "trust" {
		t.Fatalf("extra non-empty value should be included, got %q", params["include"])
	}
	if _, ok := params["ignored"]; ok {
		t.Fatalf("blank extra value should be omitted")
	}
	if got := (LatestFilingParams{}).Params(); got != nil {
		t.Fatalf("empty params = %#v, want nil", got)
	}
}

func TestTypedAgentHelpersDecodeHighFrequencyResponses(t *testing.T) {
	latestClient, latestCaptured, closeLatest := newCaptureClientWithPayload(t, map[string]any{
		"object":          "filing",
		"ticker":          "AAPL",
		"form":            "10-K",
		"accessionNumber": "0000320193-25-000079",
		"filingDate":      "2025-10-31",
		"title":           "10-K",
		"filingUrl":       "https://www.sec.gov/aapl.htm",
		"requestId":       "req_filing",
	})
	defer closeLatest()

	filing, err := latestClient.LatestFilingAgent(map[string]string{
		"ticker": "AAPL",
		"form":   "10-K",
		"view":   "default",
	})
	if err != nil {
		t.Fatalf("LatestFilingAgent failed: %v", err)
	}
	if filing.Ticker == nil || *filing.Ticker != "AAPL" || filing.AccessionNumber != "0000320193-25-000079" || filing.FilingURL == nil || *filing.FilingURL != "https://www.sec.gov/aapl.htm" || filing.RequestID != "req_filing" {
		t.Fatalf("decoded filing = %#v", filing)
	}
	latestQuery, err := url.ParseQuery((*latestCaptured)[0].Query)
	if err != nil {
		t.Fatalf("parse latest filing query: %v", err)
	}
	if latestQuery.Get("ticker") != "AAPL" || latestQuery.Get("view") != "agent" {
		t.Fatalf("latest filing agent query = %q", (*latestCaptured)[0].Query)
	}

	sectionsClient, sectionsCaptured, closeSections := newCaptureClientWithPayload(t, map[string]any{
		"object":     "list",
		"hasMore":    true,
		"nextCursor": "cur_2",
		"requestId":  "req_sections",
		"data": []map[string]any{{
			"object":              "section",
			"ticker":              "AMD",
			"accessionNumber":     "0000002488-25-000001",
			"key":                 "item_1a",
			"startOffset":         1500,
			"endOffset":           3200,
			"snippet":             "Risk factor text",
			"accession":           "0000002488-25-000001",
			"section_key":         "item_1a",
			"char_start":          25,
			"char_end":            80,
			"highlighted_snippet": "**risk** factor text",
			"source_url":          "https://www.sec.gov/amd.htm",
		}},
	})
	defer closeSections()

	sections, err := sectionsClient.SearchSectionsAgent(SectionSearchParams{
		Query:  "risk",
		Ticker: "AMD",
		Limit:  "1",
	}.Params())
	if err != nil {
		t.Fatalf("SearchSectionsAgent failed: %v", err)
	}
	if !sections.HasMore || sections.NextCursor == nil || *sections.NextCursor != "cur_2" || sections.RequestID != "req_sections" || len(sections.Data) != 1 {
		t.Fatalf("decoded sections list = %#v", sections)
	}
	if sections.Data[0].SectionKey == nil || *sections.Data[0].SectionKey != "item_1a" || sections.Data[0].CharStart == nil || *sections.Data[0].CharStart != 25 || sections.Data[0].SourceURL == nil || *sections.Data[0].SourceURL != "https://www.sec.gov/amd.htm" {
		t.Fatalf("decoded section = %#v", sections.Data[0])
	}
	sectionsQuery, err := url.ParseQuery((*sectionsCaptured)[0].Query)
	if err != nil {
		t.Fatalf("parse sections query: %v", err)
	}
	if sectionsQuery.Get("q") != "risk" || sectionsQuery.Get("ticker") != "AMD" || sectionsQuery.Get("view") != "agent" {
		t.Fatalf("sections agent query = %q", (*sectionsCaptured)[0].Query)
	}
}

func TestTypedAgentHelpersDecodeEntityAndStatementResponses(t *testing.T) {
	entityClient, entityCaptured, closeEntity := newCaptureClientWithPayload(t, map[string]any{
		"object": "entity",
		"id":     "ent_aapl",
		"ticker": "AAPL",
		"cik":    "0000320193",
		"name":   "APPLE INC",
		"primaryIdentifiers": []map[string]any{
			{"type": "ticker", "value": "AAPL"},
			{"type": "cik", "value": "0000320193"},
		},
		"matchConfidence": 1,
		"matchBasis":      nil,
		"requestId":       "req_entity",
	})
	defer closeEntity()

	entity, err := entityClient.ResolveEntityAgent(map[string]string{"ticker": "AAPL"})
	if err != nil {
		t.Fatalf("ResolveEntityAgent failed: %v", err)
	}
	if entity.ID != "ent_aapl" || entity.Ticker == nil || *entity.Ticker != "AAPL" || entity.MatchConfidence == nil || *entity.MatchConfidence != 1 || entity.MatchBasis != nil || len(entity.PrimaryIdentifiers) != 2 || entity.RequestID != "req_entity" {
		t.Fatalf("decoded entity = %#v", entity)
	}
	entityQuery, err := url.ParseQuery((*entityCaptured)[0].Query)
	if err != nil {
		t.Fatalf("parse entity query: %v", err)
	}
	if entityQuery.Get("ticker") != "AAPL" || entityQuery.Get("view") != "agent" {
		t.Fatalf("entity agent query = %q", (*entityCaptured)[0].Query)
	}

	statementClient, statementCaptured, closeStatement := newCaptureClientWithPayload(t, map[string]any{
		"object":       "statement",
		"ticker":       "AAPL",
		"companyName":  "APPLE INC",
		"statementKey": "balance_sheet",
		"title":        "Consolidated Balance Sheets",
		"period":       "annual",
		"periods": []map[string]any{{
			"periodEnd": "2024-09-28",
			"filedAt":   "2024-10-31",
			"form":      "10-K",
			"fy":        2024,
			"fp":        "FY",
		}},
		"rows": []map[string]any{{
			"key":      "Assets",
			"tag":      "Assets",
			"taxonomy": "us-gaap",
			"label":    "Assets",
			"unit":     "USD",
			"values": []map[string]any{{
				"periodEnd":   "2024-09-28",
				"periodStart": nil,
				"value":       352755000000,
				"filedAt":     "2024-10-31",
				"form":        "10-Q",
				"fy":          2024,
				"fp":          "Q2",
				"periodBasis": "year_to_date",
				"discreteQuarter": map[string]any{
					"value":       1000000000,
					"basis":       "discrete_period",
					"reason":      "derived",
					"periodStart": "2024-03-31",
				},
			}},
		}},
		"sources": []map[string]any{{
			"source":          "sec",
			"sourceKind":      "company_facts",
			"accessionNumber": nil,
			"sourceUrl":       "https://data.sec.gov/api/xbrl/companyfacts/CIK0000320193.json",
		}},
		"requestId": "req_statement",
	})
	defer closeStatement()

	statement, err := statementClient.StatementAgent("balance/sheet", map[string]string{"ticker": "AAPL", "period": "annual"})
	if err != nil {
		t.Fatalf("StatementAgent failed: %v", err)
	}
	value := statement.Rows[0].Values[0]
	if statement.StatementKey != "balance_sheet" || value.Value == nil || *value.Value != 352755000000 || value.PeriodStart != nil || value.DiscreteQuarter == nil || value.DiscreteQuarter.Value == nil || *value.DiscreteQuarter.Value != 1000000000 || value.DiscreteQuarter.PeriodStart == nil || *value.DiscreteQuarter.PeriodStart != "2024-03-31" || statement.Sources[0].SourceKind != "company_facts" || statement.Sources[0].AccessionNumber != nil || statement.RequestID != "req_statement" {
		t.Fatalf("decoded statement = %#v", statement)
	}
	if (*statementCaptured)[0].Path != "/v1/statements/balance%2Fsheet" {
		t.Fatalf("statement path = %q", (*statementCaptured)[0].Path)
	}
	statementQuery, err := url.ParseQuery((*statementCaptured)[0].Query)
	if err != nil {
		t.Fatalf("parse statement query: %v", err)
	}
	if statementQuery.Get("ticker") != "AAPL" || statementQuery.Get("period") != "annual" || statementQuery.Get("view") != "agent" {
		t.Fatalf("statement agent query = %q", (*statementCaptured)[0].Query)
	}
}

func TestPaginateFilingsFollowsNextCursor(t *testing.T) {
	payloads := []map[string]any{
		{
			"object":     "list",
			"data":       []map[string]any{{"accessionNumber": "0001"}, {"accessionNumber": "0002"}},
			"nextCursor": "cur_2",
		},
		{
			"object":     "list",
			"data":       []map[string]any{{"accessionNumber": "0003"}},
			"nextCursor": nil,
		},
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, capturedRequest{Path: r.URL.EscapedPath(), Query: r.URL.RawQuery})
		w.Header().Set("content-type", "application/json")
		if len(payloads) == 0 {
			t.Fatalf("unexpected extra request: %s?%s", r.URL.EscapedPath(), r.URL.RawQuery)
		}
		payload := payloads[0]
		payloads = payloads[1:]
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	iter := client.PaginateFilings(map[string]string{"ticker": "AAPL", "form": "10-K", "limit": "2"})
	var accessions []string
	for iter.Next(context.Background()) {
		accessions = append(accessions, iter.Item()["accessionNumber"].(string))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if strings.Join(accessions, ",") != "0001,0002,0003" {
		t.Fatalf("accessions = %v", accessions)
	}
	if len(captured) != 2 {
		t.Fatalf("captured %d requests, want 2", len(captured))
	}
	first, _ := url.ParseQuery(captured[0].Query)
	second, _ := url.ParseQuery(captured[1].Query)
	if first.Get("ticker") != "AAPL" || first.Get("form") != "10-K" || first.Get("limit") != "2" || first.Get("cursor") != "" {
		t.Fatalf("first query = %q", captured[0].Query)
	}
	if second.Get("ticker") != "AAPL" || second.Get("form") != "10-K" || second.Get("limit") != "2" || second.Get("cursor") != "cur_2" {
		t.Fatalf("second query = %q", captured[1].Query)
	}
}

func TestPaginateSectionsAgentYieldsTypedRecordsAndStopsOnHasMoreFalse(t *testing.T) {
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, capturedRequest{Path: r.URL.EscapedPath(), Query: r.URL.RawQuery})
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":     "list",
			"hasMore":    false,
			"nextCursor": "high_water_mark",
			"data": []map[string]any{{
				"object":          "section",
				"ticker":          "AAPL",
				"accessionNumber": "0000320193-25-000079",
				"key":             "item_1a",
				"snippet":         "Risk factor text",
			}},
		})
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	iter := client.PaginateSectionsAgent(map[string]string{"ticker": "AAPL", "q": "risk", "limit": "1"})
	var snippets []string
	for iter.Next(context.Background()) {
		if iter.Item().Snippet == nil {
			t.Fatal("Snippet is nil")
		}
		snippets = append(snippets, *iter.Item().Snippet)
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if strings.Join(snippets, ",") != "Risk factor text" {
		t.Fatalf("snippets = %v", snippets)
	}
	if len(captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(captured))
	}
	query, _ := url.ParseQuery(captured[0].Query)
	if query.Get("ticker") != "AAPL" || query.Get("q") != "risk" || query.Get("limit") != "1" || query.Get("view") != "agent" || query.Get("cursor") != "" {
		t.Fatalf("query = %q", captured[0].Query)
	}
}

func TestPaginateFilingsStopsOnSnakeCaseHasMoreFalse(t *testing.T) {
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, capturedRequest{Path: r.URL.EscapedPath(), Query: r.URL.RawQuery})
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":      "list",
			"has_more":    false,
			"next_cursor": "high_water_mark",
			"data":        []map[string]any{{"accessionNumber": "0001"}},
		})
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	iter := client.PaginateFilings(map[string]string{"limit": "1"})
	var accessions []string
	for iter.Next(context.Background()) {
		accessions = append(accessions, iter.Item()["accessionNumber"].(string))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if strings.Join(accessions, ",") != "0001" {
		t.Fatalf("accessions = %v", accessions)
	}
	if len(captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(captured))
	}
}

func TestPageIteratorHonorsMaxItemsWithoutExtraFetch(t *testing.T) {
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = append(captured, capturedRequest{Path: r.URL.EscapedPath(), Query: r.URL.RawQuery})
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":     "list",
			"data":       []map[string]any{{"id": "sec_1"}, {"id": "sec_2"}},
			"nextCursor": "cur_2",
		})
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	iter := NewPageIterator(client.SearchSectionsWithContext, map[string]string{"q": "risk", "limit": "2"}, PaginationOptions[map[string]any]{
		MaxItems: 1,
		GetItems: mapPageItems,
	})
	var ids []string
	for iter.Next(context.Background()) {
		ids = append(ids, iter.Item()["id"].(string))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if strings.Join(ids, ",") != "sec_1" {
		t.Fatalf("ids = %v", ids)
	}
	if len(captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(captured))
	}
}

func TestPageIteratorRepeatedCursorReturnsError(t *testing.T) {
	client, _, closeServer := newCaptureClientWithPayload(t, map[string]any{
		"object":     "list",
		"data":       []map[string]any{{"id": "filing_1"}},
		"nextCursor": "cur_repeat",
	})
	defer closeServer()

	iter := client.PaginateFilings(map[string]string{"cursor": "cur_repeat", "limit": "1"})
	if !iter.Next(context.Background()) {
		t.Fatalf("Next returned false before yielding current page: %v", iter.Err())
	}
	if iter.Item()["id"] != "filing_1" {
		t.Fatalf("item = %#v", iter.Item())
	}
	if iter.Next(context.Background()) {
		t.Fatal("Next returned true after repeated cursor")
	}
	if iter.Err() == nil || !strings.Contains(iter.Err().Error(), "pagination cursor repeated") {
		t.Fatalf("Err = %v, want repeated cursor error", iter.Err())
	}
}

func TestPageIteratorStopsOnEmptyPageWithFreshCursor(t *testing.T) {
	client, captured, closeServer := newCaptureClientWithPayload(t, map[string]any{
		"object":     "list",
		"data":       []map[string]any{},
		"nextCursor": "cur_fresh",
	})
	defer closeServer()

	iter := client.PaginateFilings(map[string]string{"limit": "1"})
	if iter.Next(context.Background()) {
		t.Fatalf("Next returned an item for an empty page: %#v", iter.Item())
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(*captured))
	}
}

func TestPaginateFilingsWithOptionsCapsItems(t *testing.T) {
	client, captured, closeServer := newCaptureClientWithPayload(t, map[string]any{
		"object":     "list",
		"data":       []map[string]any{{"id": "filing_1"}, {"id": "filing_2"}},
		"nextCursor": "cur_2",
	})
	defer closeServer()

	iter := client.PaginateFilingsWithOptions(map[string]string{"ticker": "AAPL", "limit": "2"}, PaginationOptions[map[string]any]{
		MaxItems: 1,
	})
	var ids []string
	for iter.Next(context.Background()) {
		ids = append(ids, iter.Item()["id"].(string))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if strings.Join(ids, ",") != "filing_1" {
		t.Fatalf("ids = %v", ids)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(*captured))
	}
}

func TestPaginateFilingsWithOptionsCapsPages(t *testing.T) {
	client, captured, closeServer := newCaptureClientWithPayload(t, map[string]any{
		"object":     "list",
		"data":       []map[string]any{{"id": "filing_1"}},
		"nextCursor": "cur_2",
	})
	defer closeServer()

	iter := client.PaginateFilingsWithOptions(map[string]string{"ticker": "AAPL", "limit": "1"}, PaginationOptions[map[string]any]{
		MaxPages: 1,
	})
	var ids []string
	for iter.Next(context.Background()) {
		ids = append(ids, iter.Item()["id"].(string))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if strings.Join(ids, ",") != "filing_1" {
		t.Fatalf("ids = %v", ids)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(*captured))
	}
}

func TestPaginateFilingsWithZeroMaxItemsDoesNotFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected pagination request: %s?%s", r.URL.EscapedPath(), r.URL.RawQuery)
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	iter := client.PaginateFilingsWithOptions(map[string]string{"ticker": "AAPL"}, PaginationOptions[map[string]any]{
		MaxItems: 0,
	})
	if iter.Next(context.Background()) {
		t.Fatalf("Next returned an item with MaxItems=0: %#v", iter.Item())
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
}

func TestPaginateFilingsWithZeroMaxPagesDoesNotFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected pagination request: %s?%s", r.URL.EscapedPath(), r.URL.RawQuery)
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()

	iter := client.PaginateFilingsWithOptions(map[string]string{"ticker": "AAPL"}, PaginationOptions[map[string]any]{
		MaxPages: 0,
	})
	if iter.Next(context.Background()) {
		t.Fatalf("Next returned an item with MaxPages=0: %#v", iter.Item())
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
}

func TestPaginateFilingsWithUnlimitedSentinelFetches(t *testing.T) {
	client, captured, closeServer := newCaptureClientWithPayload(t, map[string]any{
		"object": "list",
		"data":   []map[string]any{{"id": "filing_1"}},
	})
	defer closeServer()

	iter := client.PaginateFilingsWithOptions(map[string]string{"ticker": "AAPL"}, PaginationOptions[map[string]any]{
		MaxPages: UnlimitedPaginationCap,
		MaxItems: UnlimitedPaginationCap,
	})
	var ids []string
	for iter.Next(context.Background()) {
		ids = append(ids, iter.Item()["id"].(string))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if strings.Join(ids, ",") != "filing_1" {
		t.Fatalf("ids = %v", ids)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d requests, want 1", len(*captured))
	}
}

func TestPageIteratorFetchReceivesCopiedParams(t *testing.T) {
	var calls int
	iter := NewPageIterator(func(ctx context.Context, params map[string]string) (map[string]any, error) {
		calls++
		if params["ticker"] != "AAPL" {
			t.Fatalf("ticker = %q, want AAPL", params["ticker"])
		}
		params["ticker"] = "MSFT"
		return map[string]any{
			"object": "list",
			"data":   []any{map[string]any{"id": "entity_1"}},
		}, nil
	}, map[string]string{"ticker": "AAPL"}, PaginationOptions[map[string]any]{
		MaxPages: UnlimitedPaginationCap,
		MaxItems: UnlimitedPaginationCap,
		GetItems: mapPageItems,
	})

	if !iter.Next(context.Background()) {
		t.Fatalf("Next returned false: %v", iter.Err())
	}
	if iter.Item()["id"] != "entity_1" {
		t.Fatalf("item = %#v", iter.Item())
	}
	if iter.Next(context.Background()) {
		t.Fatal("Next returned true after terminal page")
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestCallMCPToolBuildsToolsCallEnvelope(t *testing.T) {
	client, captured, closeServer := newCaptureClient(t)
	defer closeServer()

	if _, err := client.CallMCPTool("filings.latest", map[string]any{"ticker": "AAPL", "form": "10-K"}, "agent-test"); err != nil {
		t.Fatalf("CallMCPTool failed: %v", err)
	}

	if (*captured)[0].Method != http.MethodPost {
		t.Fatalf("method = %q, want POST", (*captured)[0].Method)
	}
	if (*captured)[0].Path != "/mcp" {
		t.Fatalf("path = %q, want /mcp", (*captured)[0].Path)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte((*captured)[0].Body), &body); err != nil {
		t.Fatalf("body was not JSON: %v", err)
	}
	if body["jsonrpc"] != "2.0" || body["id"] != "agent-test" || body["method"] != "tools/call" {
		t.Fatalf("unexpected JSON-RPC envelope: %#v", body)
	}
	params, ok := body["params"].(map[string]any)
	if !ok {
		t.Fatalf("params missing or wrong type: %#v", body["params"])
	}
	if params["name"] != "filings.latest" {
		t.Fatalf("tool name = %v, want filings.latest", params["name"])
	}
	arguments, ok := params["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("arguments missing or wrong type: %#v", params["arguments"])
	}
	if arguments["ticker"] != "AAPL" || arguments["form"] != "10-K" {
		t.Fatalf("unexpected arguments: %#v", arguments)
	}
}

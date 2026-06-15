package secapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ResponseView selects the response shape for endpoints that support compact
// or agent-oriented payloads.
type ResponseView string

const (
	ResponseViewDefault ResponseView = "default"
	ResponseViewCompact ResponseView = "compact"
	ResponseViewAgent   ResponseView = "agent"
)

type Client struct {
	APIKey     string
	BaseURL    string
	APIVersion string
	HTTPClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		BaseURL:    "https://api.secapi.ai",
		APIVersion: "2026-03-19",
		HTTPClient: http.DefaultClient,
	}
}

func NewSecApiClient(apiKey string) *Client {
	return NewClient(apiKey)
}

func firstParams(params []map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	return params[0]
}

func (c *Client) request(method string, pathname string, params map[string]string, body any) (map[string]any, error) {
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.secapi.ai"
	}
	u, err := url.Parse(baseURL + pathname)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	for key, value := range params {
		if value == "" {
			continue
		}
		query.Set(key, value)
	}
	u.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("secapi-version", c.APIVersion)
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 && res.StatusCode < 400 {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("sec api error: %d %v", res.StatusCode, decoded)
	}
	return decoded, nil
}

func (c *Client) Health() (map[string]any, error) {
	return c.request(http.MethodGet, "/healthz", nil, nil)
}

func (c *Client) Me() (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/me", nil, nil)
}

func (c *Client) ResolveEntity(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/entities/resolve", params, nil)
}

func (c *Client) SearchEntities(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/entities", params, nil)
}

func (c *Client) SearchFilings(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/filings", params, nil)
}

func (c *Client) SearchSections(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/sections/search", params, nil)
}

func (c *Client) SearchFulltext(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/search/fulltext", params, nil)
}

func (c *Client) SemanticSearch(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/search/semantic", params, nil)
}

func (c *Client) LatestFiling(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/filings/latest", params, nil)
}

func (c *Client) LatestSection(sectionKey string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/filings/latest/sections/"+sectionKey, params, nil)
}

func (c *Client) FilingByAccession(accessionNumber string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/filings/"+accessionNumber, params, nil)
}

func (c *Client) FilingSectionByAccession(accessionNumber string, sectionKey string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/filings/"+accessionNumber+"/sections/"+sectionKey, params, nil)
}

func (c *Client) AllStatements(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/statements/all", params, nil)
}

func (c *Client) Offerings(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/offerings", params, nil)
}

func (c *Client) MarketCalendar(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/calendar", params, nil)
}

func (c *Client) MarketEstimates(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/estimates", params, nil)
}

func (c *Client) MarketSnapshots(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/snapshots", params, nil)
}

func (c *Client) MarketBars(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/bars", params, nil)
}

func (c *Client) MarketCorporateActions(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/corporate-actions", params, nil)
}

func (c *Client) MarketReference(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/reference", params, nil)
}

func (c *Client) NewsStories(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/news/stories", params, nil)
}

func (c *Client) MacroIndicators(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/indicators", params, nil)
}

func (c *Client) MacroReleases(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/releases", params, nil)
}

func (c *Client) MacroCalendar(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/calendar", params, nil)
}

func (c *Client) MacroForecasts(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/forecasts", params, nil)
}

func (c *Client) MacroHighSignalPack(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/high-signal-pack", params, nil)
}

func (c *Client) MacroRegimes(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/regimes", params, nil)
}

func (c *Client) CompanyIncomeStatements(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/companies/income-statements", params, nil)
}

func (c *Client) CompanyBalanceSheets(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/companies/balance-sheets", params, nil)
}

func (c *Client) CompanyCashFlowStatements(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/companies/cash-flow-statements", params, nil)
}

func (c *Client) CompanyFinancials(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/companies/financials", params, nil)
}

func (c *Client) CompanyRatios(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/companies/ratios", params, nil)
}

func (c *Client) CompanyResolve(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/companies/resolve", params, nil)
}

func (c *Client) CompanySearch(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/companies/search", params, nil)
}

func (c *Client) FactorCatalog(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/catalog", params, nil)
}

func (c *Client) FactorReturns(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/returns", params, nil)
}

func (c *Client) FactorHistory(factorKey string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/history/"+url.PathEscape(factorKey), params, nil)
}

func (c *Client) FactorSparklines(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/sparklines", params, nil)
}

func (c *Client) FactorReturnsIntraday(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/returns/intraday", params, nil)
}

func (c *Client) FactorDashboard(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/dashboard", params, nil)
}

func (c *Client) FactorRegimePerformance(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/regime-performance", params, nil)
}

func (c *Client) FactorCorrelations(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/correlations", params, nil)
}

func (c *Client) FactorScreen(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/screen", params, nil)
}

func (c *Client) FactorExtremeMoves(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/extreme-moves", params, nil)
}

func (c *Client) FactorExtremePairs(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/extreme-pairs", params, nil)
}

func (c *Client) FactorValuations(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/valuations", params, nil)
}

func (c *Client) FactorValuationStocks(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/valuations/stocks", params, nil)
}

func (c *Client) FactorExposures(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/exposures", params, nil)
}

func (c *Client) FactorDecomposition(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/decomposition", params, nil)
}

func (c *Client) FactorRelatedStocks(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/related-stocks", params, nil)
}

func (c *Client) FactorSimilarityPack(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/similarity-pack", params, nil)
}

func (c *Client) FactorPairs(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/pairs", params, nil)
}

func (c *Client) FactorPairHistory(f1 string, f2 string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/pair-history/"+url.PathEscape(f1)+"/"+url.PathEscape(f2), params, nil)
}

func (c *Client) FactorBulkDownload(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/bulk-download", params, nil)
}

func (c *Client) FactorCustom(body any, params ...map[string]string) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/factors/custom", firstParams(params), body)
}

func (c *Client) StockLoadings(ticker string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/stocks/"+url.PathEscape(ticker)+"/loadings", params, nil)
}

func (c *Client) PortfolioAnalyze(body any, params ...map[string]string) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/portfolio/analyze", firstParams(params), body)
}

func (c *Client) PortfolioAttribution(body any, params ...map[string]string) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/portfolio/attribution", firstParams(params), body)
}

func (c *Client) ModelPortfolioFactorView(portfolioID string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/model-portfolios/"+url.PathEscape(portfolioID)+"/factor-view", params, nil)
}

func (c *Client) ModelFactorAnalysis(body any, params ...map[string]string) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/models/factor-analysis", firstParams(params), body)
}

func (c *Client) PortfolioOptimize(body any, params ...map[string]string) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/portfolio/optimize", firstParams(params), body)
}

func (c *Client) PortfolioHedge(body any, params ...map[string]string) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/portfolio/hedge", firstParams(params), body)
}

func (c *Client) PortfolioStressTest(body any, params ...map[string]string) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/portfolio/stress-test", firstParams(params), body)
}

func (c *Client) IntelligenceSecurity(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/intelligence/security", params, nil)
}

func (c *Client) IntelligenceCompany(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/intelligence/company", params, nil)
}

func (c *Client) IntelligenceCountryReport(body any) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/intelligence/country-report", nil, body)
}

func (c *Client) IntelligencePortfolio(body any) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/intelligence/portfolio", nil, body)
}

func (c *Client) IntelligenceQuery(body any) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/intelligence/query", nil, body)
}

func (c *Client) IntelligenceEarningsPreview(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/intelligence/earnings-preview", params, nil)
}

func (c *Client) IntelligenceWatchlist(body any) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/intelligence/watchlist", nil, body)
}

func (c *Client) IntelligenceFootnotesQuery(body any) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/intelligence/footnotes/query", nil, body)
}

func (c *Client) VolatilitySignal(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/signals/volatility", params, nil)
}

func (c *Client) MCPInfo() (map[string]any, error) {
	return c.request(http.MethodGet, "/mcp", nil, nil)
}

func (c *Client) DeleteApiKey(keyID string) error {
	_, err := c.request(http.MethodDelete, "/v1/api_keys/"+url.PathEscape(keyID), nil, nil)
	return err
}

func (c *Client) AnalyticsQuery(body any) (map[string]any, error) {
	return c.request(http.MethodPost, "/v1/analytics/query", nil, body)
}

func (c *Client) ListTraces(ids string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/traces", map[string]string{"ids": ids}, nil)
}

func (c *Client) GetTrace(traceID string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/traces/"+traceID, nil, nil)
}

func (c *Client) SegmentedRevenues(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/statements/segmented-revenues", params, nil)
}

func (c *Client) SegmentedFacts(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/statements/segmented-facts", params, nil)
}

func (c *Client) PensionBenefitSchedule(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/filings/pension-benefit-schedule", params, nil)
}

func (c *Client) ShareFloat(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/statements/share-float", params, nil)
}

func (c *Client) BoardComposition(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/board", params, nil)
}

func (c *Client) NportHoldings(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/funds/nport/holdings", params, nil)
}

func (c *Client) Insiders(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/insiders", params, nil)
}

func (c *Client) BeneficialOwnership(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/owners/13d-13g", params, nil)
}

func (c *Client) Compensation(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/compensation", params, nil)
}

func (c *Client) MaEvents(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/events/ma", params, nil)
}

func (c *Client) VotingResultsEvents(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/events/voting-results", params, nil)
}

// Dilution endpoints return financing, warrant, convertible, and related
// dilution data. Most accept ?view=agent for smaller response shapes.
func (c *Client) DilutionEvents(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/events", params, nil)
}

func (c *Client) DilutionEventDetail(eventID string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/events/"+url.PathEscape(eventID), params, nil)
}

func (c *Client) DilutionWarrants(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/warrants", params, nil)
}

func (c *Client) DilutionConvertibles(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/convertibles", params, nil)
}

func (c *Client) DilutionRofr(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/rofr", params, nil)
}

func (c *Client) DilutionLockups(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/lockups", params, nil)
}

func (c *Client) DilutionCashPosition(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/cash-position", params, nil)
}

func (c *Client) DilutionCorporateActions(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/corporate-actions", params, nil)
}

func (c *Client) DilutionNasdaqCompliance(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/nasdaq-compliance", params, nil)
}

func (c *Client) DilutionRatings(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/ratings", params, nil)
}

func (c *Client) DilutionReverseSplits(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/reverse-splits", params, nil)
}

// DilutionScore requires a ticker; the route returns 400 without one.
func (c *Client) DilutionScore(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/score", params, nil)
}

func (c *Client) DilutionShareFloatHistory(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/share-float-history", params, nil)
}

func (c *Client) DilutionCoverage(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/dilution/coverage", params, nil)
}

func (c *Client) Form144Filings(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/forms/144", params, nil)
}

func (c *Client) CompanySubsidiaries(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/companies/subsidiaries", params, nil)
}

func (c *Client) EnforcementActions(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/events/enforcement", params, nil)
}

func (c *Client) EarningsTranscripts(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/earnings/transcripts", params, nil)
}

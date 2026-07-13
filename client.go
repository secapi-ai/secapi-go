package secapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ResponseView selects the response shape for endpoints that support compact
// or agent-oriented payloads.
type ResponseView string

const (
	ResponseViewDefault ResponseView = "default"
	ResponseViewCompact ResponseView = "compact"
	ResponseViewAgent   ResponseView = "agent"
)

const DefaultHTTPTimeout = 30 * time.Second
const DefaultRetryInitialBackoff = 200 * time.Millisecond
const DefaultRetryMaxBackoff = 2 * time.Second
const DefaultRetryMaxRetries = 2
const SDKVersion = "1.0.1"
const sdkUserAgent = "secapi-go/" + SDKVersion

// UnlimitedPaginationCap disables a MaxPages or MaxItems cap in explicit
// PaginationOptions. Leave a sibling cap at zero only when setting the other cap.
// Set both caps to zero to intentionally fetch nothing.
const UnlimitedPaginationCap = -1

type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type AgentList[T any] struct {
	Object     string  `json:"object"`
	Data       []T     `json:"data"`
	HasMore    bool    `json:"hasMore"`
	NextCursor *string `json:"nextCursor"`
	RequestID  string  `json:"requestId"`
}

type PageFetchFunc func(context.Context, map[string]string) (map[string]any, error)

type PaginationOptions[T any] struct {
	MaxPages      int
	MaxItems      int
	GetItems      func(map[string]any) ([]T, error)
	GetNextCursor func(map[string]any) *string
}

func unlimitedPaginationOptions[T any]() PaginationOptions[T] {
	return PaginationOptions[T]{
		MaxPages: UnlimitedPaginationCap,
		MaxItems: UnlimitedPaginationCap,
	}
}

func normalizePaginationOptions[T any](options PaginationOptions[T]) PaginationOptions[T] {
	if options.MaxItems > 0 && options.MaxPages == 0 {
		options.MaxPages = UnlimitedPaginationCap
	}
	if options.MaxPages > 0 && options.MaxItems == 0 {
		options.MaxItems = UnlimitedPaginationCap
	}
	return options
}

type PageIterator[T any] struct {
	fetch       PageFetchFunc
	params      map[string]string
	options     PaginationOptions[T]
	buffer      []T
	item        T
	err         error
	pendingErr  error
	done        bool
	noMorePages bool
	pages       int
	yielded     int
	seen        map[string]struct{}
}

func NewPageIterator[T any](fetch PageFetchFunc, params map[string]string, options PaginationOptions[T]) *PageIterator[T] {
	nextParams := copyStringMap(params)
	options = normalizePaginationOptions(options)
	seen := map[string]struct{}{}
	if cursor := strings.TrimSpace(nextParams["cursor"]); cursor != "" {
		seen[cursor] = struct{}{}
	}
	return &PageIterator[T]{
		fetch:   fetch,
		params:  nextParams,
		options: options,
		seen:    seen,
	}
}

func (it *PageIterator[T]) Next(ctx context.Context) bool {
	if it.done || it.err != nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for len(it.buffer) == 0 {
		if it.pendingErr != nil {
			it.err = it.pendingErr
			it.pendingErr = nil
			return false
		}
		if it.noMorePages {
			it.done = true
			return false
		}
		if it.options.MaxItems >= 0 && it.yielded >= it.options.MaxItems {
			it.done = true
			return false
		}
		if it.options.MaxPages >= 0 && it.pages >= it.options.MaxPages {
			it.done = true
			return false
		}
		it.fetchNextPage(ctx)
		if it.done || it.err != nil {
			return false
		}
	}
	if it.options.MaxItems >= 0 && it.yielded >= it.options.MaxItems {
		it.done = true
		return false
	}
	it.item = it.buffer[0]
	it.buffer = it.buffer[1:]
	it.yielded++
	return true
}

func (it *PageIterator[T]) Item() T {
	return it.item
}

func (it *PageIterator[T]) Err() error {
	return it.err
}

type AgentFiling struct {
	Object          string  `json:"object"`
	Ticker          *string `json:"ticker"`
	Form            string  `json:"form"`
	AccessionNumber string  `json:"accessionNumber"`
	FilingDate      string  `json:"filingDate"`
	Title           string  `json:"title"`
	FilingURL       *string `json:"filingUrl"`
	RequestID       string  `json:"requestId"`
}

type AgentEntity struct {
	Object             string                  `json:"object"`
	ID                 string                  `json:"id"`
	Ticker             *string                 `json:"ticker"`
	CIK                *string                 `json:"cik"`
	Name               string                  `json:"name"`
	PrimaryIdentifiers []AgentEntityIdentifier `json:"primaryIdentifiers"`
	MatchConfidence    *float64                `json:"matchConfidence"`
	MatchBasis         *string                 `json:"matchBasis"`
	RequestID          string                  `json:"requestId"`
}

type AgentEntityIdentifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type AgentSection struct {
	Object             string  `json:"object"`
	Ticker             *string `json:"ticker"`
	AccessionNumber    *string `json:"accessionNumber"`
	Key                string  `json:"key"`
	StartOffset        *int64  `json:"startOffset"`
	EndOffset          *int64  `json:"endOffset"`
	Snippet            *string `json:"snippet"`
	Accession          *string `json:"accession"`
	SectionKey         *string `json:"section_key"`
	CharStart          *int64  `json:"char_start"`
	CharEnd            *int64  `json:"char_end"`
	HighlightedSnippet *string `json:"highlighted_snippet"`
	SourceURL          *string `json:"source_url"`
	CitationDegraded   *string `json:"_citation_degraded"`
}

type AgentStatement struct {
	Object       string                 `json:"object"`
	Ticker       *string                `json:"ticker"`
	CompanyName  string                 `json:"companyName"`
	StatementKey string                 `json:"statementKey"`
	Title        string                 `json:"title"`
	Period       string                 `json:"period"`
	PeriodBasis  *string                `json:"periodBasis"`
	Periods      []AgentStatementPeriod `json:"periods"`
	Rows         []AgentStatementRow    `json:"rows"`
	Sources      []AgentStatementSource `json:"sources"`
	RequestID    string                 `json:"requestId"`
}

type AgentStatementPeriod struct {
	PeriodEnd string   `json:"periodEnd"`
	FiledAt   *string  `json:"filedAt"`
	Form      *string  `json:"form"`
	FY        *float64 `json:"fy"`
	FP        *string  `json:"fp"`
}

type AgentStatementRow struct {
	Key      string                `json:"key"`
	Tag      string                `json:"tag"`
	Taxonomy string                `json:"taxonomy"`
	Label    string                `json:"label"`
	Unit     *string               `json:"unit"`
	Values   []AgentStatementValue `json:"values"`
}

type AgentStatementValue struct {
	PeriodEnd       string                `json:"periodEnd"`
	PeriodStart     *string               `json:"periodStart"`
	FiledAt         *string               `json:"filedAt"`
	Form            *string               `json:"form"`
	FY              *float64              `json:"fy"`
	FP              *string               `json:"fp"`
	Value           *float64              `json:"value"`
	PeriodBasis     *string               `json:"periodBasis"`
	DiscreteQuarter *AgentDiscreteQuarter `json:"discreteQuarter"`
}

type AgentDiscreteQuarter struct {
	Value       *float64 `json:"value"`
	Basis       string   `json:"basis"`
	Reason      string   `json:"reason"`
	PeriodStart *string  `json:"periodStart"`
}

type AgentStatementSource struct {
	Source          string  `json:"source"`
	SourceKind      string  `json:"sourceKind"`
	AccessionNumber *string `json:"accessionNumber"`
	SourceURL       string  `json:"sourceUrl"`
}

// LatestFilingParams provides typed query parameters for LatestFiling.
type LatestFilingParams struct {
	// Ticker selects an issuer by ticker, for example "AAPL".
	Ticker string
	// CIK selects an issuer by SEC CIK.
	CIK string
	// Form narrows the latest filing lookup, for example "10-K" or "10-Q".
	Form string
	// FP narrows the fiscal period, for example "FY" or "Q1".
	FP string
	// Quarter is accepted as an alias for FP by the API.
	Quarter string
	// FilingYear selects the filing calendar year.
	FilingYear string
	// FY selects the fiscal year.
	FY string
	// Year is accepted as an alias for FY by the API.
	Year string
	// View selects the response shape for endpoints that support ?view=.
	View ResponseView
	// Extra carries less common query parameters without waiting for an SDK release.
	Extra map[string]string
}

func (p LatestFilingParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("ticker", p.Ticker),
		param("cik", p.CIK),
		param("form", p.Form),
		param("fp", p.FP),
		param("quarter", p.Quarter),
		param("filing_year", p.FilingYear),
		param("fy", p.FY),
		param("year", p.Year),
		param("view", string(p.View)),
	)
}

// LatestSectionParams provides typed query parameters for LatestSection.
type LatestSectionParams struct {
	Ticker     string
	CIK        string
	Form       string
	FP         string
	Quarter    string
	FilingYear string
	FY         string
	Year       string
	// Mode selects compact or full section text.
	Mode  string
	Extra map[string]string
}

func (p LatestSectionParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("ticker", p.Ticker),
		param("cik", p.CIK),
		param("form", p.Form),
		param("fp", p.FP),
		param("quarter", p.Quarter),
		param("filing_year", p.FilingYear),
		param("fy", p.FY),
		param("year", p.Year),
		param("mode", p.Mode),
	)
}

// SectionSearchParams provides typed query parameters for SearchSections.
type SectionSearchParams struct {
	// Query is sent as q.
	Query   string
	Ticker  string
	CIK     string
	Form    string
	FP      string
	Quarter string
	// FilingID scopes search to a specific filing.
	FilingID   string
	FilingYear string
	FY         string
	Year       string
	Cursor     string
	Limit      string
	View       ResponseView
	Extra      map[string]string
}

func (p SectionSearchParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("q", p.Query),
		param("ticker", p.Ticker),
		param("cik", p.CIK),
		param("form", p.Form),
		param("fp", p.FP),
		param("quarter", p.Quarter),
		param("filing_id", p.FilingID),
		param("filing_year", p.FilingYear),
		param("fy", p.FY),
		param("year", p.Year),
		param("cursor", p.Cursor),
		param("limit", p.Limit),
		param("view", string(p.View)),
	)
}

// AllStatementsParams provides typed query parameters for AllStatements.
type AllStatementsParams struct {
	Ticker string
	CIK    string
	// Period is "annual" or "quarterly".
	Period string
	// FY selects a single fiscal year.
	FY string
	// FYFrom and FYTo select a fiscal-year range.
	FYFrom string
	FYTo   string
	Limit  string
	Extra  map[string]string
}

func (p AllStatementsParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("ticker", p.Ticker),
		param("cik", p.CIK),
		param("period", p.Period),
		param("fy", p.FY),
		param("fy_from", p.FYFrom),
		param("fy_to", p.FYTo),
		param("limit", p.Limit),
	)
}

// FactorHistoryParams provides typed query parameters for FactorHistory.
type FactorHistoryParams struct {
	Range        string
	Lookback     string
	Window       string
	DateFrom     string
	DateTo       string
	ResponseMode string
	Include      string
	Format       string
	Extra        map[string]string
}

func (p FactorHistoryParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("range", p.Range),
		param("lookback", p.Lookback),
		param("window", p.Window),
		param("date_from", p.DateFrom),
		param("date_to", p.DateTo),
		param("response_mode", p.ResponseMode),
		param("include", p.Include),
		param("format", p.Format),
	)
}

type Client struct {
	APIKey      string
	BearerToken string
	BaseURL     string
	APIVersion  string
	HTTPClient  *http.Client
	RetryConfig RetryConfig
	Entities    *EntityService
	Filings     *FilingService
	Sections    *SectionService
	Search      *SearchService
	Factors     *FactorService
}

// APIError is returned for non-2xx SEC API responses.
type APIError struct {
	StatusCode int
	Body       map[string]any
	RequestID  string
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	message := e.Message
	if message == "" {
		message = http.StatusText(e.StatusCode)
	}
	if e.Code != "" && e.RequestID != "" {
		return fmt.Sprintf("sec api error: status=%d code=%s request_id=%s message=%s", e.StatusCode, e.Code, e.RequestID, message)
	}
	if e.Code != "" {
		return fmt.Sprintf("sec api error: status=%d code=%s message=%s", e.StatusCode, e.Code, message)
	}
	if e.RequestID != "" {
		return fmt.Sprintf("sec api error: status=%d request_id=%s message=%s", e.StatusCode, e.RequestID, message)
	}
	return fmt.Sprintf("sec api error: status=%d message=%s", e.StatusCode, message)
}

// NewClient creates an API-key client. If apiKey is empty, it uses
// SECAPI_API_KEY (or the legacy OMNI_DATASTREAM_API_KEY) from the environment.
func NewClient(apiKey string) *Client {
	if strings.TrimSpace(apiKey) == "" {
		apiKey = firstEnv("SECAPI_API_KEY", "OMNI_DATASTREAM_API_KEY")
	}
	return newClient(apiKey, "")
}

// NewBearerTokenClient creates a bearer-token client. If bearerToken is empty,
// it uses SECAPI_BEARER_TOKEN (or the legacy OMNI_DATASTREAM_BEARER_TOKEN).
func NewBearerTokenClient(bearerToken string) *Client {
	if strings.TrimSpace(bearerToken) == "" {
		bearerToken = firstEnv("SECAPI_BEARER_TOKEN", "OMNI_DATASTREAM_BEARER_TOKEN")
	}
	return newClient("", strings.TrimSpace(bearerToken))
}

func newClient(apiKey string, bearerToken string) *Client {
	client := &Client{
		APIKey:      apiKey,
		BearerToken: bearerToken,
		BaseURL:     firstEnvOrDefault("https://api.secapi.ai", "SECAPI_BASE_URL", "SECAPI_API_BASE_URL", "OMNI_DATASTREAM_BASE_URL", "OMNI_DATASTREAM_API_BASE_URL"),
		APIVersion:  "2026-03-19",
		HTTPClient:  defaultHTTPClient(),
		RetryConfig: defaultRetryConfig(),
	}
	client.Entities = &EntityService{client: client}
	client.Filings = &FilingService{client: client}
	client.Sections = &SectionService{client: client}
	client.Search = &SearchService{client: client}
	client.Factors = &FactorService{client: client}
	return client
}

func NewSecApiClient(apiKey string) *Client {
	return NewClient(apiKey)
}

type EntityService struct {
	client *Client
}

func (s *EntityService) Resolve(params map[string]string) (map[string]any, error) {
	return s.client.ResolveEntity(params)
}

func (s *EntityService) ResolveWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.ResolveEntityWithContext(ctx, params)
}

func (s *EntityService) ResolveAgent(params map[string]string) (*AgentEntity, error) {
	return s.client.ResolveEntityAgent(params)
}

func (s *EntityService) ResolveAgentWithContext(ctx context.Context, params map[string]string) (*AgentEntity, error) {
	return s.client.ResolveEntityAgentWithContext(ctx, params)
}

func (s *EntityService) Search(params map[string]string) (map[string]any, error) {
	return s.client.SearchEntities(params)
}

func (s *EntityService) SearchWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.SearchEntitiesWithContext(ctx, params)
}

func (s *EntityService) Paginate(params map[string]string) *PageIterator[map[string]any] {
	return s.client.PaginateEntities(params)
}

func (s *EntityService) PaginateWithOptions(params map[string]string, options PaginationOptions[map[string]any]) *PageIterator[map[string]any] {
	return s.client.PaginateEntitiesWithOptions(params, options)
}

type FilingService struct {
	client *Client
}

func (s *FilingService) Search(params map[string]string) (map[string]any, error) {
	return s.client.SearchFilings(params)
}

func (s *FilingService) SearchWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.SearchFilingsWithContext(ctx, params)
}

func (s *FilingService) Paginate(params map[string]string) *PageIterator[map[string]any] {
	return s.client.PaginateFilings(params)
}

func (s *FilingService) PaginateWithOptions(params map[string]string, options PaginationOptions[map[string]any]) *PageIterator[map[string]any] {
	return s.client.PaginateFilingsWithOptions(params, options)
}

func (s *FilingService) Latest(params map[string]string) (map[string]any, error) {
	return s.client.LatestFiling(params)
}

func (s *FilingService) LatestWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.LatestFilingWithContext(ctx, params)
}

func (s *FilingService) LatestAgent(params map[string]string) (*AgentFiling, error) {
	return s.client.LatestFilingAgent(params)
}

func (s *FilingService) LatestAgentWithContext(ctx context.Context, params map[string]string) (*AgentFiling, error) {
	return s.client.LatestFilingAgentWithContext(ctx, params)
}

func (s *FilingService) ByAccession(accessionNumber string, params map[string]string) (map[string]any, error) {
	return s.client.FilingByAccession(accessionNumber, params)
}

func (s *FilingService) ByAccessionWithContext(ctx context.Context, accessionNumber string, params map[string]string) (map[string]any, error) {
	return s.client.FilingByAccessionWithContext(ctx, accessionNumber, params)
}

type SectionService struct {
	client *Client
}

func (s *SectionService) Search(params map[string]string) (map[string]any, error) {
	return s.client.SearchSections(params)
}

func (s *SectionService) SearchWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.SearchSectionsWithContext(ctx, params)
}

func (s *SectionService) SearchAgent(params map[string]string) (*AgentList[AgentSection], error) {
	return s.client.SearchSectionsAgent(params)
}

func (s *SectionService) SearchAgentWithContext(ctx context.Context, params map[string]string) (*AgentList[AgentSection], error) {
	return s.client.SearchSectionsAgentWithContext(ctx, params)
}

func (s *SectionService) Paginate(params map[string]string) *PageIterator[map[string]any] {
	return s.client.PaginateSections(params)
}

func (s *SectionService) PaginateWithOptions(params map[string]string, options PaginationOptions[map[string]any]) *PageIterator[map[string]any] {
	return s.client.PaginateSectionsWithOptions(params, options)
}

func (s *SectionService) PaginateAgent(params map[string]string) *PageIterator[AgentSection] {
	return s.client.PaginateSectionsAgent(params)
}

func (s *SectionService) PaginateAgentWithOptions(params map[string]string, options PaginationOptions[AgentSection]) *PageIterator[AgentSection] {
	return s.client.PaginateSectionsAgentWithOptions(params, options)
}

func (s *SectionService) Latest(sectionKey string, params map[string]string) (map[string]any, error) {
	return s.client.LatestSection(sectionKey, params)
}

func (s *SectionService) LatestWithContext(ctx context.Context, sectionKey string, params map[string]string) (map[string]any, error) {
	return s.client.LatestSectionWithContext(ctx, sectionKey, params)
}

func (s *SectionService) ByAccession(accessionNumber string, sectionKey string, params map[string]string) (map[string]any, error) {
	return s.client.FilingSectionByAccession(accessionNumber, sectionKey, params)
}

func (s *SectionService) ByAccessionWithContext(ctx context.Context, accessionNumber string, sectionKey string, params map[string]string) (map[string]any, error) {
	return s.client.FilingSectionByAccessionWithContext(ctx, accessionNumber, sectionKey, params)
}

type SearchService struct {
	client *Client
}

func (s *SearchService) Fulltext(params map[string]string) (map[string]any, error) {
	return s.client.SearchFulltext(params)
}

func (s *SearchService) FulltextWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.SearchFulltextWithContext(ctx, params)
}

func (s *SearchService) Semantic(params map[string]string) (map[string]any, error) {
	return s.client.SemanticSearch(params)
}

func (s *SearchService) SemanticWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.SemanticSearchWithContext(ctx, params)
}

type FactorService struct {
	client *Client
}

func (s *FactorService) Catalog(params map[string]string) (map[string]any, error) {
	return s.client.FactorCatalog(params)
}

func (s *FactorService) Returns(params map[string]string) (map[string]any, error) {
	return s.client.FactorReturns(params)
}

func (s *FactorService) History(factorKey string, params map[string]string) (map[string]any, error) {
	return s.client.FactorHistory(factorKey, params)
}

func (s *FactorService) HistoryWithContext(ctx context.Context, factorKey string, params map[string]string) (map[string]any, error) {
	return s.client.FactorHistoryWithContext(ctx, factorKey, params)
}

func (s *FactorService) Sparklines(params map[string]string) (map[string]any, error) {
	return s.client.FactorSparklines(params)
}

func (s *FactorService) ReturnsIntraday(params map[string]string) (map[string]any, error) {
	return s.client.FactorReturnsIntraday(params)
}

func (s *FactorService) Dashboard(params map[string]string) (map[string]any, error) {
	return s.client.FactorDashboard(params)
}

func (s *FactorService) DashboardWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.FactorDashboardWithContext(ctx, params)
}

func (s *FactorService) RegimePerformance(params map[string]string) (map[string]any, error) {
	return s.client.FactorRegimePerformance(params)
}

func (s *FactorService) Correlations(params map[string]string) (map[string]any, error) {
	return s.client.FactorCorrelations(params)
}

func (s *FactorService) Screen(params map[string]string) (map[string]any, error) {
	return s.client.FactorScreen(params)
}

func (s *FactorService) ExtremeMoves(params map[string]string) (map[string]any, error) {
	return s.client.FactorExtremeMoves(params)
}

func (s *FactorService) ExtremePairs(params map[string]string) (map[string]any, error) {
	return s.client.FactorExtremePairs(params)
}

func (s *FactorService) Valuations(params map[string]string) (map[string]any, error) {
	return s.client.FactorValuations(params)
}

func (s *FactorService) ValuationsWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.FactorValuationsWithContext(ctx, params)
}

func (s *FactorService) ValuationStocks(params map[string]string) (map[string]any, error) {
	return s.client.FactorValuationStocks(params)
}

func (s *FactorService) Exposures(params map[string]string) (map[string]any, error) {
	return s.client.FactorExposures(params)
}

func (s *FactorService) Decomposition(params map[string]string) (map[string]any, error) {
	return s.client.FactorDecomposition(params)
}

func (s *FactorService) RelatedStocks(params map[string]string) (map[string]any, error) {
	return s.client.FactorRelatedStocks(params)
}

func (s *FactorService) SimilarityPack(params map[string]string) (map[string]any, error) {
	return s.client.FactorSimilarityPack(params)
}

func (s *FactorService) Pairs(params map[string]string) (map[string]any, error) {
	return s.client.FactorPairs(params)
}

func (s *FactorService) PairHistory(f1 string, f2 string, params map[string]string) (map[string]any, error) {
	return s.client.FactorPairHistory(f1, f2, params)
}

func (s *FactorService) BulkDownload(params map[string]string) (map[string]any, error) {
	return s.client.FactorBulkDownload(params)
}

func (s *FactorService) Custom(body any, params ...map[string]string) (map[string]any, error) {
	return s.client.FactorCustom(body, params...)
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func firstEnvOrDefault(defaultValue string, names ...string) string {
	if value := firstEnv(names...); value != "" {
		return value
	}
	return defaultValue
}

func firstParams(params []map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	return params[0]
}

func decodeResponse[T any](body map[string]any) (*T, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var typed T
	if err := json.Unmarshal(payload, &typed); err != nil {
		return nil, err
	}
	return &typed, nil
}

func withAgentView(params map[string]string) map[string]string {
	next := make(map[string]string, len(params)+1)
	for key, value := range params {
		next[key] = value
	}
	next["view"] = string(ResponseViewAgent)
	return next
}

func copyStringMap(params map[string]string) map[string]string {
	if len(params) == 0 {
		return map[string]string{}
	}
	next := make(map[string]string, len(params))
	for key, value := range params {
		next[key] = value
	}
	return next
}

func param(key string, value string) [2]string {
	return [2]string{key, value}
}

func requestParams(extra map[string]string, pairs ...[2]string) map[string]string {
	params := make(map[string]string)
	for _, pair := range pairs {
		key := strings.TrimSpace(pair[0])
		value := strings.TrimSpace(pair[1])
		if key == "" || value == "" {
			continue
		}
		params[key] = value
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		params[key] = value
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func stringField(body map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := body[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boolField(body map[string]any, key string) (bool, bool) {
	value, ok := body[key]
	if !ok {
		return false, false
	}
	typed, ok := value.(bool)
	return typed, ok
}

func defaultNextCursor(body map[string]any) *string {
	if hasMore, ok := boolField(body, "hasMore"); ok && !hasMore {
		return nil
	}
	if hasMore, ok := boolField(body, "has_more"); ok && !hasMore {
		return nil
	}
	cursor := stringField(body, "nextCursor", "next_cursor")
	if cursor == "" {
		return nil
	}
	return &cursor
}

func mapPageItems(body map[string]any) ([]map[string]any, error) {
	for _, key := range []string{"data", "items", "results", "sections", "filings"} {
		raw, ok := body[key]
		if !ok {
			continue
		}
		values, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("pagination field %q is %T, want []any", key, raw)
		}
		items := make([]map[string]any, 0, len(values))
		for index, value := range values {
			item, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("pagination field %q item %d is %T, want object", key, index, value)
			}
			items = append(items, item)
		}
		return items, nil
	}
	return nil, nil
}

func agentListItems[T any](body map[string]any) ([]T, error) {
	list, err := decodeResponse[AgentList[T]](body)
	if err != nil {
		return nil, err
	}
	return list.Data, nil
}

func (it *PageIterator[T]) fetchNextPage(ctx context.Context) {
	page, err := it.fetch(ctx, copyStringMap(it.params))
	if err != nil {
		it.err = err
		return
	}
	it.pages++
	getItems := it.options.GetItems
	if getItems == nil {
		it.err = errors.New("pagination GetItems is required")
		return
	}
	items, err := getItems(page)
	if err != nil {
		it.err = err
		return
	}
	it.buffer = items

	getNextCursor := it.options.GetNextCursor
	if getNextCursor == nil {
		getNextCursor = defaultNextCursor
	}
	nextCursor := getNextCursor(page)
	if nextCursor == nil || strings.TrimSpace(*nextCursor) == "" {
		it.noMorePages = true
		return
	}
	cursor := strings.TrimSpace(*nextCursor)
	if _, ok := it.seen[cursor]; ok {
		it.pendingErr = fmt.Errorf("SEC API pagination cursor repeated: %s", cursor)
		return
	}
	if len(items) == 0 {
		it.noMorePages = true
		return
	}
	it.seen[cursor] = struct{}{}
	it.params = copyStringMap(it.params)
	it.params["cursor"] = cursor
}

func responseRequestID(res *http.Response, body map[string]any) string {
	if requestID := stringField(body, "requestId", "request_id"); requestID != "" {
		return requestID
	}
	for _, header := range []string{"Request-Id", "X-Request-Id", "X-Correlation-Id"} {
		if value := strings.TrimSpace(res.Header.Get(header)); value != "" {
			return value
		}
	}
	return ""
}

func apiError(res *http.Response, body map[string]any) *APIError {
	return &APIError{
		StatusCode: res.StatusCode,
		Body:       body,
		RequestID:  responseRequestID(res, body),
		Code:       stringField(body, "code", "errorCode"),
		Message:    stringField(body, "message", "error"),
	}
}

func isSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: DefaultHTTPTimeout}
}

func defaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     DefaultRetryMaxRetries,
		InitialBackoff: DefaultRetryInitialBackoff,
		MaxBackoff:     DefaultRetryMaxBackoff,
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTPClient()
}

func (c *Client) retryConfig() RetryConfig {
	cfg := c.RetryConfig
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.InitialBackoff < 0 {
		cfg.InitialBackoff = 0
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = cfg.InitialBackoff
	}
	return cfg
}

func isRetryableMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func shouldRetryResponse(method string, statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || (isRetryableMethod(method) && isRetryableStatus(statusCode))
}

func isRetryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryAfterDelay(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := time.Until(when)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}

func retryDelay(attempt int, cfg RetryConfig, res *http.Response) time.Duration {
	if res != nil {
		if delay, ok := retryAfterDelay(res.Header.Get("Retry-After")); ok {
			if cfg.MaxBackoff > 0 && delay > cfg.MaxBackoff {
				return cfg.MaxBackoff
			}
			return delay
		}
	}
	delay := cfg.InitialBackoff
	for i := 0; i < attempt; i++ {
		delay *= 2
		if cfg.MaxBackoff > 0 && delay > cfg.MaxBackoff {
			return cfg.MaxBackoff
		}
	}
	if cfg.MaxBackoff > 0 && delay > cfg.MaxBackoff {
		return cfg.MaxBackoff
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) request(method string, pathname string, params map[string]string, body any) (map[string]any, error) {
	return c.requestWithContext(context.Background(), method, pathname, params, body)
}

func (c *Client) requestWithContext(ctx context.Context, method string, pathname string, params map[string]string, body any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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

	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	cfg := c.retryConfig()
	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}

		req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("content-type", "application/json")
		req.Header.Set("accept", "application/json")
		req.Header.Set("secapi-version", c.APIVersion)
		req.Header.Set("user-agent", sdkUserAgent)
		if c.BearerToken != "" {
			req.Header.Set("authorization", "Bearer "+c.BearerToken)
		}
		if c.APIKey != "" {
			req.Header.Set("x-api-key", c.APIKey)
		}

		res, err := c.httpClient().Do(req)
		if err != nil {
			if isRetryableMethod(method) && ctx.Err() == nil && attempt < cfg.MaxRetries {
				if sleepErr := sleepContext(ctx, retryDelay(attempt, cfg, nil)); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, err
		}

		if shouldRetryResponse(method, res.StatusCode) && attempt < cfg.MaxRetries {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if err := sleepContext(ctx, retryDelay(attempt, cfg, res)); err != nil {
				return nil, err
			}
			continue
		}
		defer res.Body.Close()

		if res.StatusCode == http.StatusNoContent {
			return nil, nil
		}

		responsePayload, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		if len(responsePayload) == 0 && isSuccessStatus(res.StatusCode) {
			return nil, nil
		}
		if len(responsePayload) == 0 {
			return nil, apiError(res, nil)
		}
		var decoded map[string]any
		if err := json.Unmarshal(responsePayload, &decoded); err != nil {
			if !isSuccessStatus(res.StatusCode) {
				return nil, &APIError{
					StatusCode: res.StatusCode,
					RequestID:  responseRequestID(res, nil),
					Message:    strings.TrimSpace(string(responsePayload)),
				}
			}
			return nil, err
		}
		if !isSuccessStatus(res.StatusCode) {
			return nil, apiError(res, decoded)
		}
		return decoded, nil
	}
}

func (c *Client) Health() (map[string]any, error) {
	return c.HealthWithContext(context.Background())
}

func (c *Client) HealthWithContext(ctx context.Context) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/healthz", nil, nil)
}

func (c *Client) Me() (map[string]any, error) {
	return c.MeWithContext(context.Background())
}

func (c *Client) MeWithContext(ctx context.Context) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/me", nil, nil)
}

func (c *Client) ResolveEntity(params map[string]string) (map[string]any, error) {
	return c.ResolveEntityWithContext(context.Background(), params)
}

func (c *Client) ResolveEntityWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/entities/resolve", params, nil)
}

func (c *Client) ResolveEntityAgent(params map[string]string) (*AgentEntity, error) {
	return c.ResolveEntityAgentWithContext(context.Background(), params)
}

func (c *Client) ResolveEntityAgentWithContext(ctx context.Context, params map[string]string) (*AgentEntity, error) {
	body, err := c.ResolveEntityWithContext(ctx, withAgentView(params))
	if err != nil {
		return nil, err
	}
	return decodeResponse[AgentEntity](body)
}

func (c *Client) SearchEntities(params map[string]string) (map[string]any, error) {
	return c.SearchEntitiesWithContext(context.Background(), params)
}

func (c *Client) SearchEntitiesWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/entities", params, nil)
}

func (c *Client) PaginateEntities(params map[string]string) *PageIterator[map[string]any] {
	return c.PaginateEntitiesWithOptions(params, unlimitedPaginationOptions[map[string]any]())
}

func (c *Client) PaginateEntitiesWithOptions(params map[string]string, options PaginationOptions[map[string]any]) *PageIterator[map[string]any] {
	if options.GetItems == nil {
		options.GetItems = mapPageItems
	}
	return NewPageIterator(c.SearchEntitiesWithContext, params, options)
}

func (c *Client) SearchFilings(params map[string]string) (map[string]any, error) {
	return c.SearchFilingsWithContext(context.Background(), params)
}

func (c *Client) SearchFilingsWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/filings", params, nil)
}

func (c *Client) PaginateFilings(params map[string]string) *PageIterator[map[string]any] {
	return c.PaginateFilingsWithOptions(params, unlimitedPaginationOptions[map[string]any]())
}

func (c *Client) PaginateFilingsWithOptions(params map[string]string, options PaginationOptions[map[string]any]) *PageIterator[map[string]any] {
	if options.GetItems == nil {
		options.GetItems = mapPageItems
	}
	return NewPageIterator(c.SearchFilingsWithContext, params, options)
}

func (c *Client) SearchSections(params map[string]string) (map[string]any, error) {
	return c.SearchSectionsWithContext(context.Background(), params)
}

func (c *Client) SearchSectionsWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/sections/search", params, nil)
}

func (c *Client) PaginateSections(params map[string]string) *PageIterator[map[string]any] {
	return c.PaginateSectionsWithOptions(params, unlimitedPaginationOptions[map[string]any]())
}

func (c *Client) PaginateSectionsWithOptions(params map[string]string, options PaginationOptions[map[string]any]) *PageIterator[map[string]any] {
	if options.GetItems == nil {
		options.GetItems = mapPageItems
	}
	return NewPageIterator(c.SearchSectionsWithContext, params, options)
}

func (c *Client) SearchSectionsAgent(params map[string]string) (*AgentList[AgentSection], error) {
	return c.SearchSectionsAgentWithContext(context.Background(), params)
}

func (c *Client) SearchSectionsAgentWithContext(ctx context.Context, params map[string]string) (*AgentList[AgentSection], error) {
	body, err := c.SearchSectionsWithContext(ctx, withAgentView(params))
	if err != nil {
		return nil, err
	}
	return decodeResponse[AgentList[AgentSection]](body)
}

func (c *Client) PaginateSectionsAgent(params map[string]string) *PageIterator[AgentSection] {
	return c.PaginateSectionsAgentWithOptions(params, unlimitedPaginationOptions[AgentSection]())
}

func (c *Client) PaginateSectionsAgentWithOptions(params map[string]string, options PaginationOptions[AgentSection]) *PageIterator[AgentSection] {
	if options.GetItems == nil {
		options.GetItems = agentListItems[AgentSection]
	}
	return NewPageIterator(c.SearchSectionsWithContext, withAgentView(params), options)
}

func (c *Client) SearchFulltext(params map[string]string) (map[string]any, error) {
	return c.SearchFulltextWithContext(context.Background(), params)
}

func (c *Client) SearchFulltextWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/search/fulltext", params, nil)
}

func (c *Client) SemanticSearch(params map[string]string) (map[string]any, error) {
	return c.SemanticSearchWithContext(context.Background(), params)
}

func (c *Client) SemanticSearchWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/search/semantic", params, nil)
}

func (c *Client) LatestFiling(params map[string]string) (map[string]any, error) {
	return c.LatestFilingWithContext(context.Background(), params)
}

func (c *Client) LatestFilingWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/filings/latest", params, nil)
}

func (c *Client) LatestFilingAgent(params map[string]string) (*AgentFiling, error) {
	return c.LatestFilingAgentWithContext(context.Background(), params)
}

func (c *Client) LatestFilingAgentWithContext(ctx context.Context, params map[string]string) (*AgentFiling, error) {
	body, err := c.LatestFilingWithContext(ctx, withAgentView(params))
	if err != nil {
		return nil, err
	}
	return decodeResponse[AgentFiling](body)
}

func (c *Client) LatestSection(sectionKey string, params map[string]string) (map[string]any, error) {
	return c.LatestSectionWithContext(context.Background(), sectionKey, params)
}

func (c *Client) LatestSectionWithContext(ctx context.Context, sectionKey string, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/filings/latest/sections/"+url.PathEscape(sectionKey), params, nil)
}

func (c *Client) FilingByAccession(accessionNumber string, params map[string]string) (map[string]any, error) {
	return c.FilingByAccessionWithContext(context.Background(), accessionNumber, params)
}

func (c *Client) FilingByAccessionWithContext(ctx context.Context, accessionNumber string, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/filings/"+url.PathEscape(accessionNumber), params, nil)
}

func (c *Client) FilingSectionByAccession(accessionNumber string, sectionKey string, params map[string]string) (map[string]any, error) {
	return c.FilingSectionByAccessionWithContext(context.Background(), accessionNumber, sectionKey, params)
}

func (c *Client) FilingSectionByAccessionWithContext(ctx context.Context, accessionNumber string, sectionKey string, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/filings/"+url.PathEscape(accessionNumber)+"/sections/"+url.PathEscape(sectionKey), params, nil)
}

func (c *Client) AllStatements(params map[string]string) (map[string]any, error) {
	return c.AllStatementsWithContext(context.Background(), params)
}

func (c *Client) AllStatementsWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/statements/all", params, nil)
}

func (c *Client) Statement(statementKey string, params map[string]string) (map[string]any, error) {
	return c.StatementWithContext(context.Background(), statementKey, params)
}

func (c *Client) StatementWithContext(ctx context.Context, statementKey string, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/statements/"+url.PathEscape(statementKey), params, nil)
}

func (c *Client) StatementAgent(statementKey string, params map[string]string) (*AgentStatement, error) {
	return c.StatementAgentWithContext(context.Background(), statementKey, params)
}

func (c *Client) StatementAgentWithContext(ctx context.Context, statementKey string, params map[string]string) (*AgentStatement, error) {
	body, err := c.StatementWithContext(ctx, statementKey, withAgentView(params))
	if err != nil {
		return nil, err
	}
	return decodeResponse[AgentStatement](body)
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

func (c *Client) MarketUniverse(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/universe", params, nil)
}

func (c *Client) MarketReference(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/reference", params, nil)
}

func (c *Client) NewsStories(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/news/stories", params, nil)
}

func (c *Client) MacroSearch(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/search", params, nil)
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

func (c *Client) MacroCreditRatings(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/credit-ratings", params, nil)
}

func (c *Client) MacroCreditRating(country string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/macro/credit-ratings/"+url.PathEscape(country), nil, nil)
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
	return c.FactorHistoryWithContext(context.Background(), factorKey, params)
}

func (c *Client) FactorHistoryWithContext(ctx context.Context, factorKey string, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/factors/history/"+url.PathEscape(factorKey), params, nil)
}

func (c *Client) FactorSparklines(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/sparklines", params, nil)
}

func (c *Client) FactorReturnsIntraday(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/returns/intraday", params, nil)
}

func (c *Client) FactorDashboard(params map[string]string) (map[string]any, error) {
	return c.FactorDashboardWithContext(context.Background(), params)
}

func (c *Client) FactorDashboardWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/factors/dashboard", params, nil)
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
	return c.FactorValuationsWithContext(context.Background(), params)
}

func (c *Client) FactorValuationsWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/factors/valuations", params, nil)
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
	return c.PortfolioAnalyzeWithContext(context.Background(), body, params...)
}

func (c *Client) PortfolioAnalyzeWithContext(ctx context.Context, body any, params ...map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodPost, "/v1/portfolio/analyze", firstParams(params), body)
}

func (c *Client) PortfolioAttribution(body any, params ...map[string]string) (map[string]any, error) {
	return c.PortfolioAttributionWithContext(context.Background(), body, params...)
}

func (c *Client) PortfolioAttributionWithContext(ctx context.Context, body any, params ...map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodPost, "/v1/portfolio/attribution", firstParams(params), body)
}

func (c *Client) ModelPortfolioFactorView(portfolioID string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/model-portfolios/"+url.PathEscape(portfolioID)+"/factor-view", params, nil)
}

func (c *Client) ModelFactorAnalysis(body any, params ...map[string]string) (map[string]any, error) {
	return c.ModelFactorAnalysisWithContext(context.Background(), body, params...)
}

func (c *Client) ModelFactorAnalysisWithContext(ctx context.Context, body any, params ...map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodPost, "/v1/models/factor-analysis", firstParams(params), body)
}

func (c *Client) PortfolioOptimize(body any, params ...map[string]string) (map[string]any, error) {
	return c.PortfolioOptimizeWithContext(context.Background(), body, params...)
}

func (c *Client) PortfolioOptimizeWithContext(ctx context.Context, body any, params ...map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodPost, "/v1/portfolio/optimize", firstParams(params), body)
}

func (c *Client) PortfolioHedge(body any, params ...map[string]string) (map[string]any, error) {
	return c.PortfolioHedgeWithContext(context.Background(), body, params...)
}

func (c *Client) PortfolioHedgeWithContext(ctx context.Context, body any, params ...map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodPost, "/v1/portfolio/hedge", firstParams(params), body)
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
	return c.MCPInfoWithContext(context.Background())
}

func (c *Client) MCPInfoWithContext(ctx context.Context) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/mcp", nil, nil)
}

func (c *Client) CallMCPTool(toolName string, arguments map[string]any, id ...any) (map[string]any, error) {
	return c.CallMCPToolWithContext(context.Background(), toolName, arguments, id...)
}

func (c *Client) CallMCPToolWithContext(ctx context.Context, toolName string, arguments map[string]any, id ...any) (map[string]any, error) {
	requestID := any("secapi-go")
	if len(id) > 0 {
		requestID = id[0]
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	return c.requestWithContext(ctx, http.MethodPost, "/mcp", nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": arguments,
		},
	})
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
	return c.request(http.MethodGet, "/v1/traces/"+url.PathEscape(traceID), nil, nil)
}

func (c *Client) RequestDiagnostics(requestID string) (map[string]any, error) {
	return c.RequestDiagnosticsWithContext(context.Background(), requestID)
}

func (c *Client) RequestDiagnosticsWithContext(ctx context.Context, requestID string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/diagnostics/requests/"+url.PathEscape(requestID), nil, nil)
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

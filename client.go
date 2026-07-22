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
	"regexp"
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
const SDKVersion = "2.0.0"
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

type SituationWatchDelivery struct {
	Email               string
	OrganizationWebhook bool
}

type SituationWatchParams struct {
	Name     string
	Filters  map[string][]string
	StartAt  string
	Delivery SituationWatchDelivery
}

type SituationWatchlistParams struct {
	Name     string
	Filters  map[string][]string
	StartAt  string
	Delivery SituationWatchDelivery
}

type SituationListParams struct {
	Types         string
	Subtypes      string
	Statuses      string
	Tickers       string
	Forms         string
	Sectors       string
	MarketCap     string
	Country       string
	AnnouncedFrom string
	AnnouncedTo   string
	UpdatedFrom   string
	Enrich        string
	Cursor        string
	Limit         string
	ResponseMode  string
	Extra         map[string]string
}

func (p SituationListParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("types", p.Types),
		param("subtypes", p.Subtypes),
		param("statuses", p.Statuses),
		param("tickers", p.Tickers),
		param("forms", p.Forms),
		param("sectors", p.Sectors),
		param("market_cap", p.MarketCap),
		param("country", p.Country),
		param("announced_from", p.AnnouncedFrom),
		param("announced_to", p.AnnouncedTo),
		param("updated_from", p.UpdatedFrom),
		param("enrich", p.Enrich),
		param("cursor", p.Cursor),
		param("limit", p.Limit),
		param("response_mode", p.ResponseMode),
	)
}

type SituationFeedParams struct {
	Types      string
	Categories string
	Tickers    string
	Country    string
	Since      string
	Cursor     string
	Limit      string
	Extra      map[string]string
}

func (p SituationFeedParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("types", p.Types),
		param("categories", p.Categories),
		param("tickers", p.Tickers),
		param("country", p.Country),
		param("since", p.Since),
		param("cursor", p.Cursor),
		param("limit", p.Limit),
	)
}

type SituationFeedRSSParams struct {
	Types      string
	Categories string
	Tickers    string
	Country    string
	Since      string
	Extra      map[string]string
}

func (p SituationFeedRSSParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("types", p.Types),
		param("categories", p.Categories),
		param("tickers", p.Tickers),
		param("country", p.Country),
		param("since", p.Since),
	)
}

type SituationIssueListParams struct {
	Limit string
	Extra map[string]string
}

func (p SituationIssueListParams) Params() map[string]string {
	return requestParams(p.Extra, param("limit", p.Limit))
}

type SituationMemberParams struct {
	Enrich string
	Limit  string
	Cursor string
	Extra  map[string]string
}

func (p SituationMemberParams) Params() map[string]string {
	return requestParams(p.Extra,
		param("enrich", p.Enrich),
		param("limit", p.Limit),
		param("cursor", p.Cursor),
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
	Situations  *SituationService
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

func NewClient(apiKey string) *Client {
	if strings.TrimSpace(apiKey) == "" {
		apiKey = firstEnv("SECAPI_API_KEY", "OMNI_DATASTREAM_API_KEY")
	}
	return newClient(apiKey, firstEnv("SECAPI_BEARER_TOKEN", "OMNI_DATASTREAM_BEARER_TOKEN"))
}

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
	client.Situations = &SituationService{client: client}
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

func (s *FactorService) ExtremeMoves(params map[string]string) (map[string]any, error) {
	return s.client.FactorExtremeMoves(params)
}

func (s *FactorService) ExtremePairs(params map[string]string) (map[string]any, error) {
	return s.client.FactorExtremePairs(params)
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

type SituationService struct {
	client *Client
}

func (s *SituationService) List(params map[string]string) (map[string]any, error) {
	return s.client.ListSituations(params)
}

func (s *SituationService) ListWithParams(params SituationListParams) (map[string]any, error) {
	return s.client.ListSituations(params.Params())
}

func (s *SituationService) ListWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return s.client.ListSituationsWithContext(ctx, params)
}

func (s *SituationService) Get(situationID string, params map[string]string) (map[string]any, error) {
	return s.client.GetSituation(situationID, params)
}

func (s *SituationService) GetWithParams(situationID string, params SituationMemberParams) (map[string]any, error) {
	return s.client.GetSituation(situationID, params.Params())
}

func (s *SituationService) GetWithContext(ctx context.Context, situationID string, params map[string]string) (map[string]any, error) {
	return s.client.GetSituationWithContext(ctx, situationID, params)
}

func (s *SituationService) ByForm(form string, params map[string]string) (map[string]any, error) {
	return s.client.SituationsByForm(form, params)
}

func (s *SituationService) ByFormWithParams(form string, params SituationListParams) (map[string]any, error) {
	return s.client.SituationsByForm(form, params.Params())
}

func (s *SituationService) Feed(params map[string]string) (map[string]any, error) {
	return s.client.SituationsFeed(params)
}

func (s *SituationService) FeedWithParams(params SituationFeedParams) (map[string]any, error) {
	return s.client.SituationsFeed(params.Params())
}

func (s *SituationService) FeedRSS(params map[string]string) (string, error) {
	return s.client.SituationsFeedRSS(params)
}

func (s *SituationService) FeedRSSWithParams(params SituationFeedRSSParams) (string, error) {
	return s.client.SituationsFeedRSS(params.Params())
}

func (s *SituationService) Issues(params map[string]string) (map[string]any, error) {
	return s.client.SituationsIssues(params)
}

func (s *SituationService) IssuesWithParams(params SituationIssueListParams) (map[string]any, error) {
	return s.client.SituationsIssues(params.Params())
}

func (s *SituationService) Issue(issue string, params map[string]string) (map[string]any, error) {
	return s.client.SituationIssue(issue, params)
}

func (s *SituationService) Calendar(params map[string]string) (map[string]any, error) {
	return s.client.SituationsCalendar(params)
}

func (s *SituationService) Stats(params map[string]string) (map[string]any, error) {
	return s.client.SituationsStats(params)
}

func (s *SituationService) Performance(params map[string]string) (map[string]any, error) {
	return s.client.SituationsPerformance(params)
}

func (s *SituationService) Filings(situationID string, params map[string]string) (map[string]any, error) {
	return s.client.SituationFilings(situationID, params)
}

func (s *SituationService) FilingsWithParams(situationID string, params SituationMemberParams) (map[string]any, error) {
	return s.client.SituationFilings(situationID, params.Params())
}

func (s *SituationService) Summary(situationID string, params map[string]string) (map[string]any, error) {
	return s.client.SituationSummary(situationID, params)
}

func (s *SituationService) Export(situationID string, params map[string]string) (string, error) {
	return s.client.ExportSituation(situationID, params)
}

func (s *SituationService) Underwrite(situationID string, params map[string]string) (map[string]any, error) {
	return s.client.UnderwriteSituation(situationID, params)
}

func (s *SituationService) Watch(params SituationWatchParams) (map[string]any, error) {
	return s.client.WatchSituations(params)
}

func (s *SituationService) Watchlists(params map[string]string) (map[string]any, error) {
	return s.client.ListSituationWatchlists(params)
}

func (s *SituationService) Watchlist(monitorID string) (map[string]any, error) {
	return s.client.GetSituationWatchlist(monitorID)
}

func (s *SituationService) CreateWatchlist(params SituationWatchlistParams) (map[string]any, error) {
	return s.client.CreateSituationWatchlist(params)
}

func (s *SituationService) DeleteWatchlist(monitorID string) error {
	return s.client.DeleteSituationWatchlist(monitorID)
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

var situationIDPattern = regexp.MustCompile(`^sit_[a-f0-9]{20}$`)

var situationFilterKeys = map[string]struct{}{
	"situationIds": {},
	"types":        {},
	"subtypes":     {},
	"statuses":     {},
	"tickers":      {},
	"sectors":      {},
}

var situationTypes = map[string]struct{}{
	"merger": {}, "tender_offer": {}, "going_private": {}, "spin_off": {}, "divestiture": {},
	"activist_campaign": {}, "restructuring": {}, "bankruptcy": {}, "liquidation": {},
	"strategic_review": {}, "capital_return": {}, "capital_raise": {}, "spac": {}, "delisting": {},
	"relisting": {}, "litigation": {}, "management_change": {}, "domicile_change": {},
	"demutualization": {}, "other": {},
}

var situationSubtypes = map[string]struct{}{
	"definitive": {}, "preliminary": {}, "unsolicited": {}, "rumor_response": {}, "scheme_of_arrangement": {}, "spac_merger": {},
	"self_tender": {}, "third_party": {}, "exchange_offer": {}, "management_buyout": {}, "sponsor_buyout": {}, "squeeze_out": {},
	"spin_off": {}, "split_off": {}, "carve_out_ipo": {}, "asset_sale": {}, "joint_venture": {}, "carve_out": {}, "stake_disclosure": {},
	"proxy_contest": {}, "cooperation_agreement": {}, "settlement": {}, "debt_for_equity_swap": {}, "out_of_court": {}, "operational": {},
	"chapter_11": {}, "chapter_7": {}, "chapter_15": {}, "administration": {}, "prepackaged": {}, "emergence": {}, "plan_of_liquidation": {},
	"dissolution": {}, "formal_alternatives": {}, "sale_process": {}, "buyback_authorization": {}, "special_dividend": {}, "recapitalization": {},
	"rights_offering": {}, "public_offering": {}, "private_placement": {}, "pipe": {}, "atm_program": {}, "ipo": {}, "extension": {},
	"trust_liquidation": {}, "forced": {}, "voluntary": {}, "uplisting": {}, "otc_relisting": {}, "won": {}, "lost": {}, "settled": {},
	"ceo": {}, "cfo": {}, "chair": {}, "board": {}, "redomiciliation": {},
}

var situationStatuses = map[string]struct{}{
	"rumored": {}, "announced": {}, "pending": {}, "completed": {}, "terminated": {}, "expired": {},
}

func situationQueryParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	next := make(map[string]string, len(params))
	for key, value := range params {
		next[key] = strings.Join(splitSituationParam(value), ",")
	}
	return next
}

func splitSituationParam(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return []string{strings.TrimSpace(value)}
	}
	return values
}

func validateSituationWatchFilters(filters map[string][]string) (map[string][]string, error) {
	if len(filters) == 0 {
		return nil, errors.New("situations.watch requires at least one non-empty list filter")
	}
	normalized := make(map[string][]string, len(filters))
	for key, rawValues := range filters {
		if _, ok := situationFilterKeys[key]; !ok {
			return nil, fmt.Errorf("situations.watch has unsupported filter key: %s", key)
		}
		if len(rawValues) == 0 {
			return nil, fmt.Errorf("situations.watch filter %s must be a non-empty list", key)
		}
		values := make([]string, 0, len(rawValues))
		for _, rawValue := range rawValues {
			value := strings.TrimSpace(rawValue)
			if value == "" {
				return nil, fmt.Errorf("situations.watch filter %s cannot contain blank values", key)
			}
			values = append(values, value)
		}
		normalized[key] = values
	}
	if values := normalized["types"]; len(values) > 0 {
		if len(values) > 50 {
			return nil, errors.New("situations.watch types must be canonical situation types (maximum 50)")
		}
		for _, value := range values {
			if _, ok := situationTypes[value]; !ok {
				return nil, errors.New("situations.watch types must be canonical situation types (maximum 50)")
			}
		}
	}
	if values := normalized["subtypes"]; len(values) > 0 {
		if len(values) > 100 {
			return nil, errors.New("situations.watch subtypes must be canonical situation subtypes (maximum 100)")
		}
		for _, value := range values {
			if _, ok := situationSubtypes[value]; !ok {
				return nil, errors.New("situations.watch subtypes must be canonical situation subtypes (maximum 100)")
			}
		}
	}
	if values := normalized["statuses"]; len(values) > 0 {
		if len(values) > 10 {
			return nil, errors.New("situations.watch statuses must be canonical lifecycle statuses (maximum 10)")
		}
		for _, value := range values {
			if _, ok := situationStatuses[value]; !ok {
				return nil, errors.New("situations.watch statuses must be canonical lifecycle statuses (maximum 10)")
			}
		}
	}
	if values := normalized["situationIds"]; len(values) > 0 {
		if len(values) > 50 {
			return nil, errors.New("situations.watch situationIds must be canonical ids (maximum 50)")
		}
		for _, value := range values {
			if !situationIDPattern.MatchString(value) {
				return nil, errors.New("situations.watch situationIds must be canonical ids (maximum 50)")
			}
		}
	}
	if len(normalized["tickers"]) > 200 || len(normalized["sectors"]) > 200 {
		return nil, errors.New("situations.watch tickers and sectors allow at most 200 values")
	}
	return normalized, nil
}

func normalizeSituationWatchDelivery(delivery SituationWatchDelivery) (map[string]any, error) {
	if email := strings.TrimSpace(delivery.Email); email != "" {
		return map[string]any{"type": "email", "config": map[string]any{"to": email}}, nil
	}
	if delivery.OrganizationWebhook {
		return map[string]any{"type": "webhook", "config": map[string]any{"organizationEventFanout": true}}, nil
	}
	return nil, errors.New("situations.watch requires an email or organization webhook delivery destination")
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

func retryDelay(attempt int, cfg RetryConfig, res *http.Response, body map[string]any) time.Duration {
	if res != nil {
		if delay, ok := retryAfterDelay(res.Header.Get("Retry-After")); ok {
			if cfg.MaxBackoff > 0 && delay > cfg.MaxBackoff {
				return cfg.MaxBackoff
			}
			return delay
		}
	}
	if delay, ok := structuredRetryDelay(body); ok {
		if cfg.MaxBackoff > 0 && delay > cfg.MaxBackoff {
			return cfg.MaxBackoff
		}
		return delay
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

func structuredRetryDelay(body map[string]any) (time.Duration, bool) {
	for _, candidate := range retryDelayCandidateMaps(body) {
		if delay, ok := durationFromRetryField(candidate, "retryAfterMs", time.Millisecond); ok {
			return delay, true
		}
		if delay, ok := durationFromRetryField(candidate, "retry_after_ms", time.Millisecond); ok {
			return delay, true
		}
		if delay, ok := durationFromRetryField(candidate, "retryAfterSeconds", time.Second); ok {
			return delay, true
		}
		if delay, ok := durationFromRetryField(candidate, "retry_after_seconds", time.Second); ok {
			return delay, true
		}
	}
	return 0, false
}

func retryDelayCandidateMaps(body map[string]any) []map[string]any {
	if body == nil {
		return nil
	}
	candidates := []map[string]any{body}
	if details, ok := body["details"].(map[string]any); ok {
		candidates = append(candidates, details)
	}
	if errorObject, ok := body["error"].(map[string]any); ok {
		candidates = append(candidates, errorObject)
		if data, ok := errorObject["data"].(map[string]any); ok {
			candidates = append(candidates, data)
		}
	}
	return candidates
}

func durationFromRetryField(body map[string]any, key string, unit time.Duration) (time.Duration, bool) {
	value, ok := body[key]
	if !ok {
		return 0, false
	}
	var numeric float64
	switch typed := value.(type) {
	case float64:
		numeric = typed
	case int:
		numeric = float64(typed)
	case int64:
		numeric = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		numeric = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		numeric = parsed
	default:
		return 0, false
	}
	if numeric < 0 {
		return 0, false
	}
	return time.Duration(numeric * float64(unit)), true
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

func (c *Client) requestText(method string, pathname string, params map[string]string, body any) (string, error) {
	return c.requestTextWithContext(context.Background(), method, pathname, params, body)
}

func (c *Client) requestTextWithContext(ctx context.Context, method string, pathname string, params map[string]string, body any) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.secapi.ai"
	}
	u, err := url.Parse(baseURL + pathname)
	if err != nil {
		return "", err
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
			return "", err
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
			return "", err
		}
		req.Header.Set("content-type", "application/json")
		req.Header.Set("accept", "text/markdown, text/plain, application/json")
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
				if sleepErr := sleepContext(ctx, retryDelay(attempt, cfg, nil, nil)); sleepErr != nil {
					return "", sleepErr
				}
				continue
			}
			return "", err
		}

		responsePayload, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return "", err
		}
		var decoded map[string]any
		if len(responsePayload) > 0 {
			_ = json.Unmarshal(responsePayload, &decoded)
		}
		if shouldRetryResponse(method, res.StatusCode) && attempt < cfg.MaxRetries {
			if err := sleepContext(ctx, retryDelay(attempt, cfg, res, decoded)); err != nil {
				return "", err
			}
			continue
		}
		if !isSuccessStatus(res.StatusCode) {
			if decoded != nil {
				return "", apiError(res, decoded)
			}
			return "", &APIError{
				StatusCode: res.StatusCode,
				RequestID:  responseRequestID(res, nil),
				Message:    strings.TrimSpace(string(responsePayload)),
			}
		}
		return string(responsePayload), nil
	}
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
				if sleepErr := sleepContext(ctx, retryDelay(attempt, cfg, nil, nil)); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, err
		}

		responsePayload, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return nil, err
		}
		var decoded map[string]any
		var decodeErr error
		if len(responsePayload) > 0 {
			decodeErr = json.Unmarshal(responsePayload, &decoded)
		}
		if shouldRetryResponse(method, res.StatusCode) && attempt < cfg.MaxRetries {
			if err := sleepContext(ctx, retryDelay(attempt, cfg, res, decoded)); err != nil {
				return nil, err
			}
			continue
		}

		if res.StatusCode == http.StatusNoContent {
			return nil, nil
		}

		if len(responsePayload) == 0 && isSuccessStatus(res.StatusCode) {
			return nil, nil
		}
		if len(responsePayload) == 0 {
			return nil, apiError(res, nil)
		}
		if decodeErr != nil {
			if !isSuccessStatus(res.StatusCode) {
				return nil, &APIError{
					StatusCode: res.StatusCode,
					RequestID:  responseRequestID(res, nil),
					Message:    strings.TrimSpace(string(responsePayload)),
				}
			}
			return nil, decodeErr
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

// Public market data plane. These five routes are contract-gated public surface
// in services/datastream-api/src/lib/api-surface-registry.ts and are present in
// the published public OpenAPI. Market routes registered with internal-token or
// operator access deliberately have NO client method here — see
// docs/operations/sdk-mirror-publish-triage.md.
func (c *Client) MarketCalendar(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/market/calendar", params, nil)
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

func (c *Client) ListSituations(params map[string]string) (map[string]any, error) {
	return c.ListSituationsWithContext(context.Background(), params)
}

func (c *Client) ListSituationsWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/situations", situationQueryParams(params), nil)
}

func (c *Client) GetSituation(situationID string, params map[string]string) (map[string]any, error) {
	return c.GetSituationWithContext(context.Background(), situationID, params)
}

func (c *Client) GetSituationWithContext(ctx context.Context, situationID string, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/situations/"+url.PathEscape(situationID), params, nil)
}

func (c *Client) SituationsByForm(form string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/by-form/"+url.PathEscape(form), situationQueryParams(params), nil)
}

func (c *Client) SituationsFeed(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/feed", situationQueryParams(params), nil)
}

func (c *Client) SituationsFeedRSS(params map[string]string) (string, error) {
	return c.requestText(http.MethodGet, "/v1/situations/feed.rss", situationQueryParams(params), nil)
}

func (c *Client) SituationsIssues(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/issues", params, nil)
}

func (c *Client) SituationIssue(issue string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/issues/"+url.PathEscape(issue), params, nil)
}

func (c *Client) SituationsCalendar(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/calendar", situationQueryParams(params), nil)
}

func (c *Client) SituationsStats(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/stats", params, nil)
}

func (c *Client) SituationsPerformance(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/performance", situationQueryParams(params), nil)
}

func (c *Client) SituationFilings(situationID string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/"+url.PathEscape(situationID)+"/filings", params, nil)
}

func (c *Client) SituationSummary(situationID string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/"+url.PathEscape(situationID)+"/summary", params, nil)
}

func (c *Client) ExportSituation(situationID string, params map[string]string) (string, error) {
	return c.requestText(http.MethodGet, "/v1/situations/"+url.PathEscape(situationID)+"/export", params, nil)
}

func (c *Client) UnderwriteSituation(situationID string, params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/"+url.PathEscape(situationID)+"/underwriting-pack", params, nil)
}

func (c *Client) WatchSituations(params SituationWatchParams) (map[string]any, error) {
	filters, err := validateSituationWatchFilters(params.Filters)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = "Special Situations watch"
	}
	body := map[string]any{
		"name":       name,
		"query":      "situations.watch",
		"searchMode": "situation",
		"filters":    filters,
	}
	if strings.TrimSpace(params.Delivery.Email) != "" || params.Delivery.OrganizationWebhook {
		if err := c.requireBearerOnlySituationDelivery(); err != nil {
			return nil, err
		}
		delivery, err := normalizeSituationWatchDelivery(params.Delivery)
		if err != nil {
			return nil, err
		}
		body["delivery"] = delivery
	}
	if startAt := strings.TrimSpace(params.StartAt); startAt != "" {
		body["startAt"] = startAt
	}
	return c.request(http.MethodPost, "/v1/situations/watchlists", nil, body)
}

func (c *Client) ListSituationWatchlists(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/watchlists", params, nil)
}

func (c *Client) GetSituationWatchlist(monitorID string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/situations/watchlists/"+url.PathEscape(monitorID), nil, nil)
}

func (c *Client) CreateSituationWatchlist(params SituationWatchlistParams) (map[string]any, error) {
	filters, err := validateSituationWatchFilters(params.Filters)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = "Special Situations watch"
	}
	body := map[string]any{
		"name":       name,
		"query":      "situations.watch",
		"searchMode": "situation",
		"filters":    filters,
	}
	if strings.TrimSpace(params.Delivery.Email) != "" || params.Delivery.OrganizationWebhook {
		if err := c.requireBearerOnlySituationDelivery(); err != nil {
			return nil, err
		}
		delivery, err := normalizeSituationWatchDelivery(params.Delivery)
		if err != nil {
			return nil, err
		}
		body["delivery"] = delivery
	}
	if startAt := strings.TrimSpace(params.StartAt); startAt != "" {
		body["startAt"] = startAt
	}
	return c.request(http.MethodPost, "/v1/situations/watchlists", nil, body)
}

func (c *Client) DeleteSituationWatchlist(monitorID string) error {
	_, err := c.request(http.MethodDelete, "/v1/situations/watchlists/"+url.PathEscape(monitorID), nil, nil)
	return err
}

func (c *Client) requireBearerOnlySituationDelivery() error {
	if strings.TrimSpace(c.BearerToken) == "" || strings.TrimSpace(c.APIKey) != "" {
		return errors.New("situations.watch delivery activation requires a bearer-authenticated client without an API key")
	}
	return nil
}

func (c *Client) EmbedSituations(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/embed/situations", params, nil)
}

func (c *Client) EmbedSituationsWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/embed/situations", params, nil)
}

func (c *Client) EmbedSituationsFeed(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/embed/situations/feed", params, nil)
}

func (c *Client) EmbedSituationsFeedRSS(params map[string]string) (string, error) {
	return c.requestText(http.MethodGet, "/v1/embed/situations/feed.rss", params, nil)
}

func (c *Client) EmbedSituationsStats(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/embed/situations/stats", params, nil)
}

func (c *Client) EmbedSituation(situationID string, params map[string]string) (map[string]any, error) {
	return c.EmbedSituationWithContext(context.Background(), situationID, params)
}

func (c *Client) EmbedSituationWithContext(ctx context.Context, situationID string, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/embed/situations/"+url.PathEscape(situationID), params, nil)
}

func (c *Client) EmbedSituationExport(situationID string, params map[string]string) (string, error) {
	return c.requestText(http.MethodGet, "/v1/embed/situations/"+url.PathEscape(situationID)+"/export", params, nil)
}

func (c *Client) EmbedSituationIssues(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/embed/situations/issues", params, nil)
}

func (c *Client) EmbedSituationIssuesWithContext(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/embed/situations/issues", params, nil)
}

func (c *Client) EmbedSituationIssue(issue string) (map[string]any, error) {
	return c.EmbedSituationIssueWithContext(context.Background(), issue)
}

func (c *Client) EmbedSituationIssueWithContext(ctx context.Context, issue string) (map[string]any, error) {
	return c.requestWithContext(ctx, http.MethodGet, "/v1/embed/situations/issues/"+url.PathEscape(issue), nil, nil)
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

func (c *Client) FactorExtremeMoves(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/extreme-moves", params, nil)
}

func (c *Client) FactorExtremePairs(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/factors/extreme-pairs", params, nil)
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

func (c *Client) IntelligenceEarningsPreview(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/intelligence/earnings-preview", params, nil)
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
	return c.request(http.MethodGet, "/v1/diagnostics/requests/"+url.PathEscape(requestID), nil, nil)
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

func (c *Client) EarningsTranscripts(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/earnings/transcripts", params, nil)
}

func (c *Client) EnforcementActions(params map[string]string) (map[string]any, error) {
	return c.request(http.MethodGet, "/v1/events/enforcement", params, nil)
}

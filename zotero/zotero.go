package zotero

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// LibraryType represents the type of Zotero library (user or group)
type LibraryType string

const (
	LibraryTypeUser  LibraryType = "users"
	LibraryTypeGroup LibraryType = "groups"
)

// Client represents a Zotero API client
type Client struct {
	BaseURL      string
	LibraryID    string
	LibraryType  LibraryType
	APIKey       string
	Locale       string
	Timeout      time.Duration
	RateLimit    time.Duration
	RetryConfig  *RetryConfig
	httpClient   *http.Client
	rateLimiter  *rate.Limiter
	preserveJSON bool
	logger       *log.Logger
}

// APIError describes an unsuccessful Zotero API response. It deliberately
// retains the response status and headers so callers can handle concurrency
// failures such as 412 without parsing an error string.
type APIError struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	apiKey     string
}

func (e *APIError) Error() string {
	body := strings.TrimSpace(string(e.Body))
	if e.apiKey != "" {
		body = strings.ReplaceAll(body, e.apiKey, "[REDACTED]")
	}
	if len(body) > 4096 {
		body = body[:4096] + "..."
	}
	return fmt.Sprintf("Zotero API error: status %d: %s", e.StatusCode, body)
}

// Request performs one API request. A path beginning with one of the
// absolute API prefixes is resolved directly below BaseURL. Other paths are
// library-relative and are resolved below /users/{LibraryID} or
// /groups/{LibraryID}. Query parameters are encoded by net/url; body and
// headers are copied and never mutated by the client.
func (c *Client) Request(ctx context.Context, method, path string, query url.Values, body []byte, headers http.Header) ([]byte, http.Header, error) {
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		if err := c.checkMutationAllowed(); err != nil {
			return nil, nil, err
		}
	}
	result, err := c.request(ctx, method, path, query, body, headers, true)
	return result.body, result.headers, err
}

// All retrieves a JSON array a page at a time (100 records per request). It
// returns an error if a server repeats an identical page, which prevents an
// ignored start parameter from causing an endless loop.
func (c *Client) All(ctx context.Context, path string, query url.Values) ([]map[string]any, error) {
	q := cloneValues(query)
	q.Set("limit", "100")
	start := 0
	if s := q.Get("start"); s != "" {
		if n, err := strconv.Atoi(s); err != nil || n < 0 {
			return nil, fmt.Errorf("invalid start query value %q", s)
		} else {
			start = n
		}
	}
	var all []map[string]any
	seen := make(map[string]struct{})
	for {
		q.Set("start", strconv.Itoa(start))
		body, _, err := c.Request(ctx, http.MethodGet, path, q, nil, nil)
		if err != nil {
			return all, err
		}
		if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
			return all, fmt.Errorf("decode page at start %d: expected JSON array, got null", start)
		}
		hash := sha256.Sum256(body)
		digest := hex.EncodeToString(hash[:])
		if _, ok := seen[digest]; ok {
			return all, fmt.Errorf("pagination repeated page at start %d", start)
		}
		seen[digest] = struct{}{}
		var page []map[string]any
		if err := json.Unmarshal(body, &page); err != nil {
			return all, fmt.Errorf("decode page at start %d: %w", start, err)
		}
		all = append(all, page...)
		if len(page) < 100 {
			return all, nil
		}
		start += len(page)
	}
}

type requestResult struct {
	body       []byte
	headers    http.Header
	statusCode int
}

// RetryConfig defines retry behavior for failed requests
type RetryConfig struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	Jitter          bool
}

// ClientOption is a function that configures a Client
type ClientOption func(*Client)

// NewClient creates a new Zotero API client with the given library ID, library type, and options
func NewClient(libraryID string, libraryType LibraryType, opts ...ClientOption) *Client {
	client := &Client{
		BaseURL:      "https://api.zotero.org",
		LibraryID:    libraryID,
		LibraryType:  libraryType,
		Locale:       "en-US",
		Timeout:      30 * time.Second,
		RateLimit:    time.Second,
		httpClient:   &http.Client{},
		preserveJSON: false,
		logger:       log.New(io.Discard, "", 0),
	}

	for _, opt := range opts {
		opt(client)
	}

	// Configure HTTP client timeout
	client.httpClient.Timeout = client.Timeout

	// Configure rate limiter if rate limit is set
	if client.RateLimit > 0 {
		client.rateLimiter = rate.NewLimiter(rate.Every(client.RateLimit), 1)
	}

	return client
}

// WithAPIKey sets the API key for authentication
func WithAPIKey(apiKey string) ClientOption {
	return func(c *Client) {
		c.APIKey = apiKey
	}
}

// WithBaseURL sets a custom base URL (e.g., for local Zotero server)
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.BaseURL = normalizeBaseURL(baseURL)
	}
}

func normalizeBaseURL(baseURL string) string {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return baseURL
	}
	host := strings.ToLower(u.Hostname())
	if (host == "localhost" || host == "127.0.0.1" || host == "::1") && u.Port() == "23119" && u.Path == "" {
		u.Path = "/api"
		return u.String()
	}
	return baseURL
}

// WithLocale sets the localization for the client
func WithLocale(locale string) ClientOption {
	return func(c *Client) {
		c.Locale = locale
	}
}

// WithTimeout sets the HTTP request timeout
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.Timeout = timeout
	}
}

// WithRateLimit sets the rate limit for API requests
func WithRateLimit(rateLimit time.Duration) ClientOption {
	return func(c *Client) {
		c.RateLimit = rateLimit
	}
}

// WithRetry sets the retry configuration for failed requests
func WithRetry(config RetryConfig) ClientOption {
	return func(c *Client) {
		c.RetryConfig = &config
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithPreserveJSON sets whether to preserve JSON order
func WithPreserveJSON(preserve bool) ClientOption {
	return func(c *Client) {
		c.preserveJSON = preserve
	}
}

// WithLogger sets a custom logger for the client
func WithLogger(logger *log.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

// joinWithOR joins string slices with OR operator (||)
func joinWithOR(values []string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for i := 1; i < len(values); i++ {
		result += " || " + values[i]
	}
	return result
}

// buildQueryString constructs URL query parameters
func (c *Client) buildQueryString(params *QueryParams) string {
	if params == nil {
		return ""
	}

	values := url.Values{}

	if params.Limit > 0 {
		values.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Start > 0 {
		values.Set("start", strconv.Itoa(params.Start))
	}
	if params.Sort != "" {
		values.Set("sort", params.Sort)
	}
	if params.Format != "" {
		values.Set("format", params.Format)
	}
	if params.Include != "" {
		values.Set("include", params.Include)
	}
	if params.Style != "" {
		values.Set("style", params.Style)
	}
	if params.Q != "" {
		values.Set("q", params.Q)
	}
	if params.QMode != "" {
		values.Set("qmode", params.QMode)
	}
	if params.Since > 0 {
		values.Set("since", strconv.Itoa(params.Since))
	}

	// Tags: Join multiple tags with OR operator (||)
	if len(params.Tag) > 0 {
		values.Set("tag", joinWithOR(params.Tag))
	}

	// ItemKeys: Join with comma separator (up to 50 items)
	if len(params.ItemKey) > 0 {
		itemKeyValue := params.ItemKey[0]
		for i := 1; i < len(params.ItemKey); i++ {
			itemKeyValue += "," + params.ItemKey[i]
		}
		values.Set("itemKey", itemKeyValue)
	}

	// ItemTypes: Join multiple item types with OR operator (||)
	if len(params.ItemType) > 0 {
		values.Set("itemType", joinWithOR(params.ItemType))
	}

	for k, v := range params.Extra {
		values.Set(k, v)
	}

	if query := values.Encode(); query != "" {
		return "?" + query
	}
	return ""
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for k, values := range in {
		out[k] = append([]string(nil), values...)
	}
	return out
}

func cloneHeaders(in http.Header) http.Header {
	out := make(http.Header, len(in))
	for k, values := range in {
		out[k] = append([]string(nil), values...)
	}
	return out
}

var absolutePrefixes = []string{
	"/users/", "/groups/", "/keys/", "/itemTypes", "/itemFields",
	"/itemTypeFields", "/itemTypeCreatorTypes", "/creatorFields", "/items/new",
}

func isAbsoluteAPIPath(path string) bool {
	for _, prefix := range absolutePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (c *Client) resolveURL(path string, query url.Values) (string, error) {
	base, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.Fragment != "" || base.RawQuery != "" {
		return "", fmt.Errorf("invalid base URL %q", c.BaseURL)
	}
	for _, segment := range strings.Split(base.Path, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid base URL %q", c.BaseURL)
		}
	}
	p, err := url.Parse(path)
	if err != nil || p.IsAbs() || p.Host != "" || p.Fragment != "" {
		return "", fmt.Errorf("invalid API path %q", path)
	}
	if p.Opaque != "" || strings.Contains(p.Path, "\\") {
		return "", fmt.Errorf("invalid API path %q", path)
	}
	for _, segment := range strings.Split(p.Path, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid API path %q", path)
		}
	}
	if !strings.HasPrefix(p.Path, "/") {
		p.Path = "/" + p.Path
	}
	if !isAbsoluteAPIPath(p.Path) {
		p.Path = "/" + string(c.LibraryType) + "/" + url.PathEscape(c.LibraryID) + p.Path
	}
	base.Path = strings.TrimRight(base.Path, "/") + p.Path
	base.RawQuery = p.Query().Encode()
	if query != nil {
		merged := base.Query()
		for key, values := range query {
			merged[key] = append([]string(nil), values...)
		}
		base.RawQuery = merged.Encode()
	}
	return base.String(), nil
}

func (c *Client) waitRate(ctx context.Context) error {
	if c.rateLimiter == nil {
		return nil
	}
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter error: %w", err)
	}
	return nil
}

func (c *Client) retryDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if backoff := parseBackoff(resp.Header.Get("Backoff")); backoff > 0 {
			return c.capBackoff(backoff)
		}
		retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
			return c.capBackoff(time.Duration(seconds) * time.Second)
		}
		if when, err := http.ParseTime(retryAfter); err == nil {
			if delay := time.Until(when); delay > 0 {
				return c.capBackoff(delay)
			}
			return 0
		}
	}
	config := c.RetryConfig
	if config == nil {
		return 0
	}
	interval := config.InitialInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	if config.MaxInterval > 0 && interval > config.MaxInterval {
		interval = config.MaxInterval
	}
	for i := 0; i < attempt; i++ {
		multiplier := config.Multiplier
		if multiplier <= 0 {
			multiplier = 2
		}
		interval = time.Duration(float64(interval) * multiplier)
		if config.MaxInterval > 0 && interval > config.MaxInterval {
			interval = config.MaxInterval
		}
	}
	return interval
}

func parseBackoff(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (c *Client) capBackoff(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	max := time.Minute
	if c.RetryConfig != nil && c.RetryConfig.MaxInterval > 0 && c.RetryConfig.MaxInterval < max {
		max = c.RetryConfig.MaxInterval
	}
	if delay > max {
		return max
	}
	return delay
}

func responseBackoff(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	if delay := parseBackoff(resp.Header.Get("Backoff")); delay > 0 {
		return delay
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusInternalServerError ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func (c *Client) request(ctx context.Context, method, path string, query url.Values, body []byte, headers http.Header, allowRetry bool) (requestResult, error) {
	if err := c.waitRate(ctx); err != nil {
		return requestResult{}, err
	}
	urlStr, err := c.resolveURL(path, query)
	if err != nil {
		return requestResult{}, err
	}
	maxAttempts := 1
	if allowRetry && method == http.MethodGet && c.RetryConfig != nil && c.RetryConfig.MaxAttempts > 1 {
		maxAttempts = c.RetryConfig.MaxAttempts
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		reqHeaders := cloneHeaders(headers)
		reqHeaders.Set("Zotero-API-Version", "3")
		if c.APIKey != "" {
			reqHeaders.Set("Zotero-API-Key", c.APIKey)
		}
		req, err := http.NewRequestWithContext(ctx, method, urlStr, bytes.NewReader(body))
		if err != nil {
			return requestResult{}, fmt.Errorf("error creating request: %w", err)
		}
		req.Header = reqHeaders
		client := *c.httpClient
		initial, _ := url.Parse(urlStr)
		originalRedirect := c.httpClient.CheckRedirect
		client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if initial != nil && (!strings.EqualFold(next.URL.Host, initial.Host) || (initial.Scheme == "https" && next.URL.Scheme != "https")) {
				next.Header.Del("Zotero-API-Key")
			}
			if originalRedirect != nil {
				return originalRedirect(next, via)
			}
			return nil
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("error executing request: %w", err)
			if attempt+1 < maxAttempts {
				if err := sleepContext(ctx, c.retryDelay(attempt, nil)); err != nil {
					return requestResult{}, err
				}
				continue
			}
			return requestResult{}, lastErr
		}
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		result := requestResult{body: respBody, headers: resp.Header.Clone(), statusCode: resp.StatusCode}
		if readErr != nil {
			return result, fmt.Errorf("error reading response body: %w", readErr)
		}
		if resp.StatusCode >= 400 {
			apiErr := &APIError{StatusCode: resp.StatusCode, Headers: resp.Header.Clone(), Body: respBody, apiKey: c.APIKey}
			if method == http.MethodGet && retryableStatus(resp.StatusCode) && attempt+1 < maxAttempts {
				lastErr = apiErr
				if err := sleepContext(ctx, c.retryDelay(attempt, resp)); err != nil {
					return result, err
				}
				continue
			}
			return result, apiErr
		}
		if delay := c.capBackoff(responseBackoff(resp)); delay > 0 {
			if err := sleepContext(ctx, delay); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	return requestResult{}, lastErr
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

// doRequest retains the original typed-client helper while routing all reads
// through Request, including status and response headers on errors.
func (c *Client) doRequest(ctx context.Context, method, path string, params *QueryParams) ([]byte, *http.Response, error) {
	query := queryValues(params)
	result, err := c.request(ctx, method, path, query, nil, nil, true)
	resp := &http.Response{StatusCode: result.statusCode, Header: result.headers}
	return result.body, resp, err
}

func queryValues(params *QueryParams) url.Values {
	values := url.Values{}
	if params == nil {
		return values
	}
	if params.Limit > 0 {
		values.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Start > 0 {
		values.Set("start", strconv.Itoa(params.Start))
	}
	if params.Sort != "" {
		values.Set("sort", params.Sort)
	}
	if params.Format != "" {
		values.Set("format", params.Format)
	}
	if params.Include != "" {
		values.Set("include", params.Include)
	}
	if params.Style != "" {
		values.Set("style", params.Style)
	}
	if params.Q != "" {
		values.Set("q", params.Q)
	}
	if params.QMode != "" {
		values.Set("qmode", params.QMode)
	}
	if params.Since > 0 {
		values.Set("since", strconv.Itoa(params.Since))
	}
	if len(params.Tag) > 0 {
		values.Set("tag", joinWithOR(params.Tag))
	}
	if len(params.ItemKey) > 0 {
		values.Set("itemKey", strings.Join(params.ItemKey, ","))
	}
	if len(params.ItemType) > 0 {
		values.Set("itemType", joinWithOR(params.ItemType))
	}
	for k, v := range params.Extra {
		values.Set(k, v)
	}
	return values
}

func (c *Client) localMutationBlocked() bool {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "localhost" || host == "127.0.0.1" || host == "::1") && u.Port() == "23119" && strings.HasPrefix(u.Path, "/api")
}

func (c *Client) checkMutationAllowed() error {
	if c.localMutationBlocked() {
		return fmt.Errorf("mutating Zotero Desktop API requests are disabled; use the Web API for writes")
	}
	return nil
}

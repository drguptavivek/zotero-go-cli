package zotero

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestPathResolutionAndResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/123/items" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "a b" {
			t.Errorf("query = %q", r.URL.Query().Get("q"))
		}
		w.Header().Set("X-Test", "present")
		w.Write([]byte(`[{"key":"A"}]`))
	}))
	defer server.Close()
	c := NewClient("123", LibraryTypeUser, WithBaseURL(server.URL), WithRateLimit(0))
	body, headers, err := c.Request(context.Background(), http.MethodGet, "/items", url.Values{"q": {"a b"}}, nil, nil)
	if err != nil || string(body) != `[{"key":"A"}]` || headers.Get("X-Test") != "present" {
		t.Fatalf("Request() = %q, %v, headers=%v", body, err, headers)
	}
}

func TestRequestRejectsUnsafeBaseAndPaths(t *testing.T) {
	for _, base := range []string{"ftp://example.test", "https://user:pass@example.test", "https://example.test/#fragment"} {
		c := NewClient("123", LibraryTypeUser, WithBaseURL(base), WithRateLimit(0))
		if _, _, err := c.Request(context.Background(), http.MethodGet, "/items", nil, nil, nil); err == nil {
			t.Errorf("base %q was accepted", base)
		}
	}
	c := NewClient("123", LibraryTypeUser, WithBaseURL("https://example.test"), WithRateLimit(0))
	for _, path := range []string{"https://evil.test/items", "/../items", `/items\\secret`, "//evil.test/items"} {
		if _, _, err := c.Request(context.Background(), http.MethodGet, path, nil, nil, nil); err == nil {
			t.Errorf("unsafe path %q was accepted", path)
		}
	}
}

func TestLocalBaseURLDefaultsToAPIPath(t *testing.T) {
	c := NewClient("123", LibraryTypeUser, WithBaseURL("http://localhost:23119"))
	if c.BaseURL != "http://localhost:23119/api" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
}

func TestAllPaginatesAndGuardsRepeatedPages(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		if call == 1 {
			w.Write([]byte(makePage(100, start)))
			return
		}
		w.Write([]byte(`[ {"key":"last"} ]`))
	}))
	defer server.Close()
	c := NewClient("123", LibraryTypeUser, WithBaseURL(server.URL), WithRateLimit(0))
	items, err := c.All(context.Background(), "/items", nil)
	if err != nil || len(items) != 101 || calls.Load() != 2 {
		t.Fatalf("All() len=%d calls=%d err=%v", len(items), calls.Load(), err)
	}

	var repeatCalls atomic.Int32
	repeat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repeatCalls.Add(1)
		w.Write([]byte(makePage(100, 0)))
	}))
	defer repeat.Close()
	c = NewClient("123", LibraryTypeUser, WithBaseURL(repeat.URL), WithRateLimit(0))
	_, err = c.All(context.Background(), "/items", nil)
	if err == nil || !strings.Contains(err.Error(), "repeated page") || repeatCalls.Load() != 2 {
		t.Fatalf("repeated page err=%v calls=%d", err, repeatCalls.Load())
	}
}

func TestAllRejectsNullResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("null")) }))
	defer server.Close()
	c := NewClient("123", LibraryTypeUser, WithBaseURL(server.URL), WithRateLimit(0))
	if _, err := c.All(context.Background(), "/items", nil); err == nil || !strings.Contains(err.Error(), "expected JSON array") {
		t.Fatalf("null response error = %v", err)
	}
}

func makePage(n, offset int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"key":"%d"}`, offset+i)
	}
	b.WriteByte(']')
	return b.String()
}

func TestRequestRetriesGETOnlyAndExposesAPIError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("busy"))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	c := NewClient("123", LibraryTypeUser, WithBaseURL(server.URL), WithRateLimit(0), WithRetry(RetryConfig{MaxAttempts: 2, InitialInterval: time.Millisecond, Multiplier: 1}))
	if _, _, err := c.Request(context.Background(), http.MethodGet, "/items", nil, nil, nil); err != nil {
		t.Fatalf("GET retry error: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("GET calls = %d", calls.Load())
	}

	calls.Store(0)
	_, _, err := c.Request(context.Background(), http.MethodPost, "/items", nil, []byte(`{}`), nil)
	if err == nil || calls.Load() != 1 {
		t.Fatalf("POST retry behavior err=%v calls=%d", err, calls.Load())
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "etag-value")
		w.WriteHeader(http.StatusPreconditionFailed)
		w.Write([]byte("version conflict"))
	}))
	defer bad.Close()
	c = NewClient("123", LibraryTypeUser, WithBaseURL(bad.URL), WithRateLimit(0))
	_, headers, err := c.Request(context.Background(), http.MethodGet, "/items", nil, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusPreconditionFailed || apiErr.Headers.Get("ETag") != "etag-value" || headers.Get("ETag") != "etag-value" {
		t.Fatalf("APIError=%#v headers=%v err=%v", apiErr, headers, err)
	}
}

func TestAPIErrorRedactsAPIKeyInErrorText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("request included secret-key"))
	}))
	defer server.Close()
	c := NewClient("123", LibraryTypeUser, WithBaseURL(server.URL), WithAPIKey("secret-key"), WithRateLimit(0))
	_, _, err := c.Request(context.Background(), http.MethodGet, "/items", nil, nil, nil)
	if err == nil || strings.Contains(err.Error(), "secret-key") || !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("redacted API error = %v", err)
	}
}

func TestRequestRedirectDoesNotLeakAPIKeyAcrossHosts(t *testing.T) {
	var leaked atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Zotero-API-Key") != "" {
			leaked.Store(true)
		}
		w.Write([]byte(`[]`))
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/items", http.StatusFound)
	}))
	defer source.Close()
	c := NewClient("123", LibraryTypeUser, WithBaseURL(source.URL), WithAPIKey("secret-key"), WithRateLimit(0))
	if _, _, err := c.Request(context.Background(), http.MethodGet, "/items", nil, nil, nil); err != nil {
		t.Fatalf("redirected request error: %v", err)
	}
	if leaked.Load() {
		t.Fatal("API key leaked to redirected host")
	}
}

func TestRequestRedirectLimitAndHTTPSDowngrade(t *testing.T) {
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer loop.Close()
	c := NewClient("123", LibraryTypeUser, WithBaseURL(loop.URL), WithRateLimit(0))
	if _, _, err := c.Request(context.Background(), http.MethodGet, "/items", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect loop error = %v", err)
	}

	var downgradeKey atomic.Bool
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme == "https" {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://example.test/items"}}, Body: http.NoBody, Request: r}, nil
		}
		if r.Header.Get("Zotero-API-Key") != "" {
			downgradeKey.Store(true)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`[]`)), Request: r}, nil
	})
	c = NewClient("123", LibraryTypeUser, WithBaseURL("https://example.test"), WithAPIKey("secret"), WithHTTPClient(&http.Client{Transport: transport}), WithRateLimit(0))
	if _, _, err := c.Request(context.Background(), http.MethodGet, "/items", nil, nil, nil); err != nil {
		t.Fatalf("downgrade request error = %v", err)
	}
	if downgradeKey.Load() {
		t.Fatal("API key leaked on HTTPS downgrade")
	}
}

func TestRequestHonorsBackoffOnSuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Backoff", "1")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	c := NewClient("123", LibraryTypeUser, WithBaseURL(server.URL), WithRateLimit(0), WithRetry(RetryConfig{MaxInterval: time.Millisecond}))
	started := time.Now()
	if _, _, err := c.Request(context.Background(), http.MethodGet, "/items", nil, nil, nil); err != nil {
		t.Fatalf("backoff request error = %v", err)
	}
	if time.Since(started) < time.Millisecond {
		t.Fatal("successful response returned before configured backoff")
	}
}

func TestDownloadLocalFileRedirectAndAtomicFailure(t *testing.T) {
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "paper.pdf")
	if err := os.WriteFile(source, []byte("pdf bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urlPath := filepath.ToSlash(source)
		if !strings.HasPrefix(urlPath, "/") {
			urlPath = "/" + urlPath
		}
		fileURL := &url.URL{Scheme: "file", Path: urlPath}
		w.Header().Set("Location", fileURL.String())
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "out.pdf")
	c := NewClient("123", LibraryTypeUser, WithBaseURL(server.URL), WithRateLimit(0))
	if err := c.Download(context.Background(), "ATTACH", destination); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	got, _ := os.ReadFile(destination)
	if string(got) != "pdf bytes" {
		t.Fatalf("downloaded %q", got)
	}

	old := filepath.Join(t.TempDir(), "existing.pdf")
	if err := os.WriteFile(old, []byte("keep"), 0640); err != nil {
		t.Fatal(err)
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("failed"))
	}))
	defer failing.Close()
	c = NewClient("123", LibraryTypeUser, WithBaseURL(failing.URL), WithRateLimit(0))
	if err := c.Download(context.Background(), "ATTACH", old); err == nil {
		t.Fatal("expected failed download")
	}
	kept, _ := os.ReadFile(old)
	if string(kept) != "keep" {
		t.Fatalf("destination changed after failure: %q", kept)
	}
}

func TestDownloadRejectsFileRedirectFromRemoteBase(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"file:///tmp/secret.pdf"}},
			Body:       http.NoBody,
			Request:    r,
		}, nil
	})
	c := NewClient("123", LibraryTypeUser, WithBaseURL("https://remote.example/api"), WithHTTPClient(&http.Client{Transport: transport}), WithRateLimit(0))
	err := c.Download(context.Background(), "ATTACH", filepath.Join(t.TempDir(), "out.pdf"))
	if err == nil || !strings.Contains(err.Error(), "non-local") {
		t.Fatalf("remote file redirect err=%v", err)
	}
}

func TestLocalDesktopMutationsAreRejected(t *testing.T) {
	client := NewClient("123", LibraryTypeUser,
		WithBaseURL("http://localhost:23119/api"), WithRateLimit(0))
	_, err := client.CreateItems(context.Background(), []Item{{Data: ItemData{ItemType: ItemTypeBook, Title: "blocked"}}})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("CreateItems() error = %v", err)
	}
	if _, _, err := client.Request(context.Background(), http.MethodPost, "/items", nil, []byte(`{}`), nil); err == nil {
		t.Fatal("generic mutation should be rejected")
	}
}

func TestUploadAttachmentUsesEncodedAuthorizationAndRegistersUpload(t *testing.T) {
	var authBody url.Values
	var registered string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/users/123/items":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"success":{"0":"ATTACH"},"failed":{},"unchanged":{}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/users/123/items/ATTACH/file":
			body, _ := io.ReadAll(r.Body)
			if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
				parsed, _ := url.ParseQuery(string(body))
				if parsed.Get("md5") != "" {
					authBody = parsed
					w.Header().Set("Last-Modified-Version", "7")
					fmt.Fprintf(w, `{"exists":0,"url":%q,"params":{"policy":"p","signature":"s"},"upload":"token-7"}`, server.URL+"/upload")
					return
				}
				registered = parsed.Get("upload")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/upload":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/users/123/items/ATTACH":
			w.Write([]byte(`{"key":"ATTACH","version":7,"data":{"itemType":"attachment","title":"a&b.pdf"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	input := filepath.Join(t.TempDir(), "a&b.pdf")
	if err := os.WriteFile(input, []byte("pdf"), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewClient("123", LibraryTypeUser, WithBaseURL(server.URL), WithRateLimit(0))
	item, err := c.UploadAttachment(context.Background(), "", input, "a&b.pdf", "application/pdf")
	if err != nil {
		t.Fatalf("UploadAttachment() error = %v", err)
	}
	if item.Key != "ATTACH" || authBody.Get("filename") != "a&b.pdf" || registered != "token-7" {
		t.Fatalf("item=%#v auth=%v registered=%q", item, authBody, registered)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

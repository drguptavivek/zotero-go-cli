package zotero

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Epistemic-Technology/zotero/internal/atomicfile"
)

// QueryParams represents optional parameters for API requests
type QueryParams struct {
	Limit    int               // Maximum number of results (default 100)
	Start    int               // Starting index for results
	Sort     string            // Field to sort by (dateAdded, dateModified, title, creator, itemType, etc.)
	Format   string            // Response format (atom, bib, json, keys, versions, etc.)
	Include  string            // Additional data to include (data, bib, citation, etc.)
	Style    string            // Citation style for bib/citation formats
	Q        string            // Quick search query
	QMode    string            // Quick search mode (titleCreatorYear, everything)
	Tag      []string          // Filter by tag(s)
	ItemKey  []string          // Filter by item key(s)
	ItemType []string          // Filter by item type(s); prefix with "-" to exclude (e.g., "-annotation")
	Since    int               // Return only objects modified since version
	Extra    map[string]string // Additional query parameters
}

// Items retrieves all library items
func (c *Client) Items(ctx context.Context, params *QueryParams) ([]Item, error) {
	body, _, err := c.doRequest(ctx, http.MethodGet, "/items", params)
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("error unmarshaling items: %w", err)
	}

	return items, nil
}

// Top retrieves top-level library items (no parent items)
func (c *Client) Top(ctx context.Context, params *QueryParams) ([]Item, error) {
	body, _, err := c.doRequest(ctx, http.MethodGet, "/items/top", params)
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("error unmarshaling items: %w", err)
	}

	return items, nil
}

// Item retrieves a specific item by key
func (c *Client) Item(ctx context.Context, itemKey string, params *QueryParams) (*Item, error) {
	path := fmt.Sprintf("/items/%s", itemKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var item Item
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("error unmarshaling item: %w", err)
	}

	return &item, nil
}

// Children retrieves child items of a specific item
func (c *Client) Children(ctx context.Context, itemKey string, params *QueryParams) ([]Item, error) {
	path := fmt.Sprintf("/items/%s/children", itemKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("error unmarshaling items: %w", err)
	}

	return items, nil
}

// Trash retrieves items in the trash
func (c *Client) Trash(ctx context.Context, params *QueryParams) ([]Item, error) {
	body, _, err := c.doRequest(ctx, http.MethodGet, "/items/trash", params)
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("error unmarshaling items: %w", err)
	}

	return items, nil
}

// Collections retrieves all library collections
func (c *Client) Collections(ctx context.Context, params *QueryParams) ([]Collection, error) {
	body, _, err := c.doRequest(ctx, http.MethodGet, "/collections", params)
	if err != nil {
		return nil, err
	}

	var collections []Collection
	if err := json.Unmarshal(body, &collections); err != nil {
		return nil, fmt.Errorf("error unmarshaling collections: %w", err)
	}

	return collections, nil
}

// CollectionsTop retrieves top-level collections
func (c *Client) CollectionsTop(ctx context.Context, params *QueryParams) ([]Collection, error) {
	body, _, err := c.doRequest(ctx, http.MethodGet, "/collections/top", params)
	if err != nil {
		return nil, err
	}

	var collections []Collection
	if err := json.Unmarshal(body, &collections); err != nil {
		return nil, fmt.Errorf("error unmarshaling collections: %w", err)
	}

	return collections, nil
}

// Collection retrieves a specific collection by key
func (c *Client) Collection(ctx context.Context, collectionKey string, params *QueryParams) (*Collection, error) {
	path := fmt.Sprintf("/collections/%s", collectionKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var collection Collection
	if err := json.Unmarshal(body, &collection); err != nil {
		return nil, fmt.Errorf("error unmarshaling collection: %w", err)
	}

	return &collection, nil
}

// CollectionsSub retrieves subcollections of a specific collection
func (c *Client) CollectionsSub(ctx context.Context, collectionKey string, params *QueryParams) ([]Collection, error) {
	path := fmt.Sprintf("/collections/%s/collections", collectionKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var collections []Collection
	if err := json.Unmarshal(body, &collections); err != nil {
		return nil, fmt.Errorf("error unmarshaling collections: %w", err)
	}

	return collections, nil
}

// CollectionItems retrieves items from a specific collection
func (c *Client) CollectionItems(ctx context.Context, collectionKey string, params *QueryParams) ([]Item, error) {
	path := fmt.Sprintf("/collections/%s/items", collectionKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("error unmarshaling items: %w", err)
	}

	return items, nil
}

// CollectionItemsTop retrieves top-level items from a specific collection
func (c *Client) CollectionItemsTop(ctx context.Context, collectionKey string, params *QueryParams) ([]Item, error) {
	path := fmt.Sprintf("/collections/%s/items/top", collectionKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("error unmarshaling items: %w", err)
	}

	return items, nil
}

// Searches retrieves all saved searches
func (c *Client) Searches(ctx context.Context, params *QueryParams) ([]Search, error) {
	body, _, err := c.doRequest(ctx, http.MethodGet, "/searches", params)
	if err != nil {
		return nil, err
	}

	var searches []Search
	if err := json.Unmarshal(body, &searches); err != nil {
		return nil, fmt.Errorf("error unmarshaling searches: %w", err)
	}

	return searches, nil
}

// Search retrieves a specific saved search by key
func (c *Client) Search(ctx context.Context, searchKey string, params *QueryParams) (*Search, error) {
	path := fmt.Sprintf("/searches/%s", searchKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var search Search
	if err := json.Unmarshal(body, &search); err != nil {
		return nil, fmt.Errorf("error unmarshaling search: %w", err)
	}

	return &search, nil
}

// TagsResponse represents the response from the tags endpoint
type TagsResponse struct {
	Tag      string `json:"tag"`
	NumItems int    `json:"numItems,omitempty"`
	Type     int    `json:"type,omitempty"`
	Meta     Meta   `json:"meta,omitempty"`
	Links    Links  `json:"links,omitempty"`
}

// Tags retrieves all library tags
func (c *Client) Tags(ctx context.Context, params *QueryParams) ([]TagsResponse, error) {
	body, _, err := c.doRequest(ctx, http.MethodGet, "/tags", params)
	if err != nil {
		return nil, err
	}

	var tags []TagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("error unmarshaling tags: %w", err)
	}

	return tags, nil
}

// ItemTags retrieves tags for a specific item
func (c *Client) ItemTags(ctx context.Context, itemKey string, params *QueryParams) ([]Tag, error) {
	path := fmt.Sprintf("/items/%s/tags", itemKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var tags []Tag
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("error unmarshaling tags: %w", err)
	}

	return tags, nil
}

// CollectionTags retrieves tags for items in a specific collection
func (c *Client) CollectionTags(ctx context.Context, collectionKey string, params *QueryParams) ([]TagsResponse, error) {
	path := fmt.Sprintf("/collections/%s/tags", collectionKey)
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var tags []TagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("error unmarshaling tags: %w", err)
	}

	return tags, nil
}

// Groups retrieves groups the current user belongs to (requires user library type)
func (c *Client) Groups(ctx context.Context, params *QueryParams) ([]Group, error) {
	if c.LibraryType != LibraryTypeUser {
		return nil, fmt.Errorf("groups() requires user library type")
	}

	path := fmt.Sprintf("/users/%s/groups", url.PathEscape(c.LibraryID))
	body, _, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var groups []Group
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("error unmarshaling groups: %w", err)
	}

	return groups, nil
}

// NumItems returns the total count of library items
func (c *Client) NumItems(ctx context.Context) (int, error) {
	params := &QueryParams{
		Limit:  1,
		Format: "json",
	}

	_, resp, err := c.doRequest(ctx, http.MethodGet, "/items", params)
	if err != nil {
		return 0, err
	}

	totalResults := resp.Header.Get("Total-Results")
	if totalResults == "" {
		return 0, fmt.Errorf("Total-Results header not found")
	}

	count, err := strconv.Atoi(totalResults)
	if err != nil {
		return 0, fmt.Errorf("error parsing Total-Results: %w", err)
	}

	return count, nil
}

// LastModifiedVersion returns the library's last modified version
func (c *Client) LastModifiedVersion(ctx context.Context) (int, error) {
	_, resp, err := c.doRequest(ctx, http.MethodGet, "/items", &QueryParams{Limit: 1})
	if err != nil {
		return 0, err
	}

	version := resp.Header.Get("Last-Modified-Version")
	if version == "" {
		return 0, fmt.Errorf("Last-Modified-Version header not found")
	}

	v, err := strconv.Atoi(version)
	if err != nil {
		return 0, fmt.Errorf("error parsing Last-Modified-Version: %w", err)
	}

	return v, nil
}

// Deleted retrieves deleted content since a specific version
func (c *Client) Deleted(ctx context.Context, since int) (*DeletedContent, error) {
	params := &QueryParams{
		Since: since,
	}

	body, _, err := c.doRequest(ctx, http.MethodGet, "/deleted", params)
	if err != nil {
		return nil, err
	}

	var deleted DeletedContent
	if err := json.Unmarshal(body, &deleted); err != nil {
		return nil, fmt.Errorf("error unmarshaling deleted content: %w", err)
	}

	return &deleted, nil
}

// Download streams an attachment to destination using an atomic replacement.
// Zotero Desktop may redirect to a file:// URL; that redirect is accepted only
// when BaseURL is a loopback local API. API keys are never sent to file URLs or
// to a different HTTP host.
func (c *Client) Download(ctx context.Context, itemKey, destination string) error {
	if strings.TrimSpace(itemKey) == "" {
		return fmt.Errorf("item key is required")
	}
	if destination == "" {
		return fmt.Errorf("destination is required")
	}
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".zotero-download-*")
	if err != nil {
		return fmt.Errorf("create temporary destination: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if info, statErr := os.Stat(destination); statErr == nil {
		_ = tmp.Chmod(info.Mode().Perm())
	}
	resp, localFile, err := c.downloadResponse(ctx, itemKey)
	if err != nil {
		tmp.Close()
		return err
	}
	if localFile != "" {
		respFile, openErr := os.Open(localFile)
		if openErr != nil {
			tmp.Close()
			return fmt.Errorf("open local attachment: %w", openErr)
		}
		_, err = io.Copy(tmp, respFile)
		respFile.Close()
	} else {
		_, err = io.Copy(tmp, resp.Body)
		resp.Body.Close()
	}
	if err != nil {
		tmp.Close()
		return fmt.Errorf("write attachment: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary destination: %w", err)
	}
	if err := atomicfile.Replace(tmpName, destination); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	return nil
}

func (c *Client) downloadResponse(ctx context.Context, itemKey string) (*http.Response, string, error) {
	current, err := c.resolveURL(fmt.Sprintf("/items/%s/file", url.PathEscape(itemKey)), nil)
	if err != nil {
		return nil, "", err
	}
	baseURL, _ := url.Parse(current)
	baseHost := strings.ToLower(baseURL.Host)
	baseScheme := strings.ToLower(baseURL.Scheme)
	baseLocal := c.isLoopbackBase()
	carryKey := true
	for redirects := 0; redirects < 10; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return nil, "", fmt.Errorf("create download request: %w", err)
		}
		req.Header.Set("Zotero-API-Version", "3")
		if carryKey && c.APIKey != "" {
			req.Header.Set("Zotero-API-Key", c.APIKey)
		}
		client := *c.httpClient
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := client.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("download request: %w", err)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := resp.Header.Get("Location")
			resp.Body.Close()
			if location == "" {
				return nil, "", fmt.Errorf("download redirect missing Location header")
			}
			next, err := url.Parse(location)
			if err != nil {
				return nil, "", fmt.Errorf("invalid download redirect: %w", err)
			}
			prior, _ := url.Parse(current)
			next = prior.ResolveReference(next)
			if next.Scheme == "file" {
				if !baseLocal {
					return nil, "", fmt.Errorf("refusing file redirect from non-local Zotero API")
				}
				if next.Host != "" && !strings.EqualFold(next.Host, "localhost") {
					return nil, "", fmt.Errorf("refusing file redirect to host %q", next.Host)
				}
				localPath := next.Path
				if runtime.GOOS == "windows" && strings.HasPrefix(localPath, "/") && len(localPath) > 2 && localPath[2] == ':' {
					localPath = strings.TrimPrefix(localPath, "/")
				}
				return nil, filepath.FromSlash(localPath), nil
			}
			if next.Scheme != "http" && next.Scheme != "https" {
				return nil, "", fmt.Errorf("unsupported download redirect scheme %q", next.Scheme)
			}
			carryKey = strings.EqualFold(next.Host, baseHost) && !(baseScheme == "https" && strings.ToLower(next.Scheme) != "https")
			current = next.String()
			continue
		}
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, "", &APIError{StatusCode: resp.StatusCode, Headers: resp.Header.Clone(), Body: body, apiKey: c.APIKey}
		}
		return resp, "", nil
	}
	return nil, "", fmt.Errorf("too many download redirects")
}

func (c *Client) isLoopbackBase() bool {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// File downloads the raw file content of an attachment item.
func (c *Client) File(ctx context.Context, itemKey string) ([]byte, error) {
	resp, localFile, err := c.downloadResponse(ctx, itemKey)
	if err != nil {
		return nil, err
	}
	if localFile != "" {
		return os.ReadFile(localFile)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Dump is a convenience wrapper around File() that writes an attachment to disk
// If filename is empty, it will fetch the item to determine the stored filename
// If path is empty, it writes to the current working directory
// Returns the full path to the written file
func (c *Client) Dump(ctx context.Context, itemKey string, filename string, path string) (string, error) {
	// If no filename provided, fetch the item to get the stored filename
	if filename == "" {
		item, err := c.Item(ctx, itemKey, nil)
		if err != nil {
			return "", fmt.Errorf("error fetching item to determine filename: %w", err)
		}

		// Try to get filename from item data
		if item.Data.Filename != "" {
			filename = item.Data.Filename
		} else if item.Data.Title != "" {
			// Fall back to title if filename not available
			filename = item.Data.Title
		} else {
			// Last resort: use the item key
			filename = itemKey
		}
	}

	filename = filepath.Base(filename)
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		return "", fmt.Errorf("filename is empty")
	}
	var fullPath string
	if path != "" {
		fullPath = filepath.Join(path, filename)
	} else {
		fullPath = filename
	}
	if err := c.Download(ctx, itemKey, fullPath); err != nil {
		return "", fmt.Errorf("error downloading file: %w", err)
	}
	return fullPath, nil
}

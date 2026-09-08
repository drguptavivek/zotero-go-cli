package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/Epistemic-Technology/zotero/zotero"
)

// rawClient is the small client surface used by workflows. It is deliberately
// structural: the core package can implement these methods without importing
// this internal package.
type rawClient interface {
	All(context.Context, string, url.Values) ([]map[string]any, error)
	Download(context.Context, string, string) error
}

type clientResolver struct {
	client rawClient
	mu     sync.Mutex
	scoped map[string][]Record
}

func adaptClient(client *zotero.Client) (Resolver, error) {
	if client == nil {
		return nil, fmt.Errorf("client factory returned nil client")
	}
	raw, ok := any(client).(rawClient)
	if !ok {
		return nil, fmt.Errorf("zotero client lacks workflow methods All and Download")
	}
	return &clientResolver{client: raw, scoped: make(map[string][]Record)}, nil
}

func (r *clientResolver) Search(ctx context.Context, query string, collectionKeys []string) ([]Record, error) {
	query = NormalizeTitle(query)
	if query == "" {
		return nil, fmt.Errorf("search query has no searchable letters or numbers")
	}
	var out []Record
	paths := []string{"/items"}
	if len(collectionKeys) > 0 {
		snapshot, err := r.scopedSnapshot(ctx, collectionKeys)
		if err != nil {
			return nil, err
		}
		local := filterSnapshot(snapshot, query)
		if len(local) > 0 {
			return local, nil
		}
		paths = paths[:0]
		for _, key := range collectionKeys {
			paths = append(paths, "/collections/"+url.PathEscape(key)+"/items")
		}
	}
	for _, path := range paths {
		search := func(candidate string) ([]Record, error) {
			values := url.Values{"q": []string{candidate}, "qmode": []string{"titleCreatorYear"}, "limit": []string{"100"}}
			items, err := r.client.All(ctx, path, values)
			if err != nil {
				return nil, err
			}
			records := make([]Record, 0, len(items))
			for _, raw := range items {
				record := recordFromMap(raw)
				if isParentRecord(record) {
					records = appendUnique(records, record)
				}
			}
			return records, nil
		}
		records, err := search(query)
		if err != nil {
			return nil, err
		}
		out = appendUnique(out, records...)
	}
	return out, nil
}

func (r *clientResolver) scopedSnapshot(ctx context.Context, keys []string) ([]Record, error) {
	var out []Record
	for _, key := range keys {
		r.mu.Lock()
		cached, ok := r.scoped[key]
		r.mu.Unlock()
		if !ok {
			rows, err := r.client.All(ctx, "/collections/"+url.PathEscape(key)+"/items", url.Values{"limit": []string{"100"}})
			if err != nil {
				return nil, err
			}
			rowsRecords := make([]Record, 0, len(rows))
			for _, row := range rows {
				record := recordFromMap(row)
				if isParentRecord(record) {
					rowsRecords = append(rowsRecords, record)
				}
			}
			r.mu.Lock()
			r.scoped[key] = rowsRecords
			r.mu.Unlock()
			cached = rowsRecords
		}
		out = appendUnique(out, cached...)
	}
	return out, nil
}

func filterSnapshot(records []Record, query string) []Record {
	want := strings.Fields(NormalizeTitle(query))
	if len(want) == 0 {
		return nil
	}
	var out []Record
	for _, record := range records {
		title := NormalizeTitle(record.Title)
		if title == NormalizeTitle(query) || strings.Contains(title, NormalizeTitle(query)) {
			out = appendUnique(out, record)
			continue
		}
		all := true
		for _, term := range want {
			if !strings.Contains(title, term) {
				all = false
				break
			}
		}
		if all {
			out = appendUnique(out, record)
		}
	}
	return out
}

func (r *clientResolver) Collections(ctx context.Context) ([]CollectionInfo, error) {
	rows, err := r.client.All(ctx, "/collections", url.Values{"limit": []string{"100"}})
	if err != nil {
		return nil, err
	}
	out := make([]CollectionInfo, 0, len(rows))
	for _, row := range rows {
		data := mapValue(row, "data")
		parent := ""
		if p, ok := data["parentCollection"].(string); ok {
			parent = p
		}
		meta := mapValue(row, "meta")
		out = append(out, CollectionInfo{Key: stringValue(row, "key", data), Name: stringValue(data, "name", row), Parent: parent, NumItems: intValue(meta, "numItems"), NumCollections: intValue(meta, "numCollections")})
	}
	return out, nil
}

func (r *clientResolver) CollectionItems(ctx context.Context, key string) ([]Record, error) {
	rows, err := r.client.All(ctx, "/collections/"+url.PathEscape(key)+"/items", url.Values{"limit": []string{"100"}})
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		record := recordFromMap(row)
		if isParentRecord(record) {
			out = append(out, record)
		}
	}
	return out, nil
}

func (r *clientResolver) PDFStatus(ctx context.Context, key string, mode PDFMode) ([]PDFStatus, error) {
	rows, err := r.client.All(ctx, "/items/"+url.PathEscape(key)+"/children", url.Values{"limit": []string{"100"}})
	if err != nil {
		return nil, err
	}
	var out []PDFStatus
	for _, row := range rows {
		data := mapValue(row, "data")
		contentType := strings.ToLower(stringValue(data, "contentType", row))
		filename := stringValue(data, "filename", row)
		if contentType != "application/pdf" && !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
			continue
		}
		linkMode := stringValue(data, "linkMode", row)
		status := PDFStatus{ParentKey: key, Key: stringValue(row, "key", data), Filename: filename, ContentType: contentType, LinkMode: linkMode, Present: linkMode != "linked_url", Status: "present"}
		if linkMode == "linked_url" {
			status.Status = "linked"
			out = append(out, status)
			continue
		}
		if mode == PDFVerify {
			dest, err := os.CreateTemp("", "zotero-workflow-*.pdf")
			if err != nil {
				status.Status, status.Error = "inaccessible", err.Error()
			} else {
				name := dest.Name()
				_ = dest.Close()
				err = r.client.Download(ctx, status.Key, name)
				if err != nil {
					status.Status, status.Error = "inaccessible", err.Error()
				} else {
					file, openErr := os.Open(name)
					var signature [5]byte
					var readErr error
					if openErr == nil {
						_, readErr = io.ReadFull(file, signature[:])
					}
					if openErr != nil {
						status.Status, status.Error = "inaccessible", openErr.Error()
					} else if readErr != nil {
						_ = file.Close()
						status.Status, status.Error = "inaccessible", readErr.Error()
					} else if string(signature[:]) != "%PDF-" {
						_ = file.Close()
						status.Status, status.Error = "invalid", "downloaded file does not have a PDF signature"
					} else {
						_ = file.Close()
						status.Verified = true
					}
				}
				_ = os.Remove(name)
			}
		}
		out = append(out, status)
	}
	if len(out) == 0 {
		return []PDFStatus{{ParentKey: key, Status: "absent", Present: false}}, nil
	}
	return out, nil
}

func recordFromMap(row map[string]any) Record {
	data := mapValue(row, "data")
	library := mapValue(row, "library")
	links := mapValue(row, "links")
	uri := stringValue(mapValue(links, "alternate"), "href", mapValue(links, "self"))
	libraryID, libraryType := stringValue(library, "id", library), stringValue(library, "type", library)
	libraryIdentity := libraryID
	if libraryType != "" {
		libraryIdentity = libraryType + ":" + libraryID
	}
	doi := stringValue(data, "DOI", data)
	if doi == "" {
		doi = stringValue(data, "doi", data)
	}
	r := Record{Key: stringValue(row, "key", data), Library: libraryIdentity, URI: uri, Title: stringValue(data, "title", row), DOI: cleanDOI(doi), ItemType: stringValue(data, "itemType", row)}
	if date := stringValue(data, "date", data); date != "" {
		r.Year = metadataYear(date)
	}
	if creators, ok := data["creators"].([]any); ok {
		for _, value := range creators {
			if c, ok := value.(map[string]any); ok {
				name := strings.TrimSpace(stringValue(c, "name", c))
				if name == "" {
					name = strings.TrimSpace(stringValue(c, "lastName", c) + " " + stringValue(c, "firstName", c))
				}
				if name != "" {
					r.Authors = append(r.Authors, name)
				}
			}
		}
	}
	return r
}

func isParentRecord(r Record) bool {
	switch strings.ToLower(r.ItemType) {
	case "attachment", "note", "annotation", "highlight", "image":
		return false
	default:
		return true
	}
}

var metadataYearPattern = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)

func metadataYear(value string) int {
	match := metadataYearPattern.FindString(value)
	if match == "" {
		return 0
	}
	var year int
	_, _ = fmt.Sscan(match, &year)
	return year
}

func mapValue(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func intValue(m map[string]any, key string) int {
	switch value := m[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		var n int
		_, _ = fmt.Sscan(string(value), &n)
		return n
	default:
		return 0
	}
}

func stringValue(primary map[string]any, key string, fallback map[string]any) string {
	if value, ok := primary[key]; ok {
		switch typed := value.(type) {
		case string:
			return typed
		case float64:
			return fmt.Sprintf("%.0f", typed)
		case json.Number:
			return typed.String()
		}
	}
	if value, ok := fallback[key]; ok {
		switch typed := value.(type) {
		case string:
			return typed
		case float64:
			return fmt.Sprintf("%.0f", typed)
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

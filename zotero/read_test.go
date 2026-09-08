package zotero

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// setupMockServer creates a test HTTP server that serves fixture data
func setupMockServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	server := httptest.NewServer(handler)
	client := NewClient("12345", LibraryTypeUser,
		WithBaseURL(server.URL),
		WithAPIKey("test-key"),
		WithRateLimit(0), // Disable rate limiting for tests
	)
	return server, client
}

// loadFixture loads a test fixture file
func loadFixture(t *testing.T, filename string) []byte {
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatalf("failed to load fixture %s: %v", filename, err)
	}
	return data
}

func TestItems(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/items" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Zotero-API-Key") != "test-key" {
			t.Error("API key not set in request")
		}
		if r.Header.Get("Zotero-API-Version") != "3" {
			t.Error("API version not set correctly")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	items, err := client.Items(context.Background(), nil)
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
	if items[0].Key != "ABCD1234" {
		t.Errorf("items[0].Key = %v, want ABCD1234", items[0].Key)
	}
}

func TestItemsWithParams(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("limit") != "10" {
			t.Errorf("limit = %v, want 10", query.Get("limit"))
		}
		if query.Get("start") != "20" {
			t.Errorf("start = %v, want 20", query.Get("start"))
		}
		if query.Get("sort") != "title" {
			t.Errorf("sort = %v, want title", query.Get("sort"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	params := &QueryParams{
		Limit: 10,
		Start: 20,
		Sort:  "title",
	}
	items, err := client.Items(context.Background(), params)
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestTop(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/items/top" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	items, err := client.Top(context.Background(), nil)
	if err != nil {
		t.Fatalf("Top() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestItem(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/items/ABCD1234" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "item.json"))
	})
	defer server.Close()

	item, err := client.Item(context.Background(), "ABCD1234", nil)
	if err != nil {
		t.Fatalf("Item() error = %v", err)
	}

	if item.Key != "ABCD1234" {
		t.Errorf("item.Key = %v, want ABCD1234", item.Key)
	}
	if item.Data.Title != "Test Book Title" {
		t.Errorf("item.Data.Title = %v, want Test Book Title", item.Data.Title)
	}
}

func TestChildren(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/items/ABCD1234/children" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	items, err := client.Children(context.Background(), "ABCD1234", nil)
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestTrash(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/items/trash" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	items, err := client.Trash(context.Background(), nil)
	if err != nil {
		t.Fatalf("Trash() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestCollections(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/collections" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "collections.json"))
	})
	defer server.Close()

	collections, err := client.Collections(context.Background(), nil)
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	if len(collections) != 2 {
		t.Errorf("len(collections) = %v, want 2", len(collections))
	}
	if collections[0].Key != "COLL1234" {
		t.Errorf("collections[0].Key = %v, want COLL1234", collections[0].Key)
	}
}

func TestCollectionsTop(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/collections/top" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "collections.json"))
	})
	defer server.Close()

	collections, err := client.CollectionsTop(context.Background(), nil)
	if err != nil {
		t.Fatalf("CollectionsTop() error = %v", err)
	}

	if len(collections) != 2 {
		t.Errorf("len(collections) = %v, want 2", len(collections))
	}
}

func TestCollection(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/collections/COLL1234" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		// Return the first collection from collections.json
		data := loadFixture(t, "collections.json")
		var collections []Collection
		json.Unmarshal(data, &collections)
		collectionData, _ := json.Marshal(collections[0])
		w.Write(collectionData)
	})
	defer server.Close()

	collection, err := client.Collection(context.Background(), "COLL1234", nil)
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if collection.Key != "COLL1234" {
		t.Errorf("collection.Key = %v, want COLL1234", collection.Key)
	}
	if collection.Data.Name != "Test Collection" {
		t.Errorf("collection.Data.Name = %v, want Test Collection", collection.Data.Name)
	}
}

func TestCollectionsSub(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/collections/COLL1234/collections" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "collections.json"))
	})
	defer server.Close()

	collections, err := client.CollectionsSub(context.Background(), "COLL1234", nil)
	if err != nil {
		t.Fatalf("CollectionsSub() error = %v", err)
	}

	if len(collections) != 2 {
		t.Errorf("len(collections) = %v, want 2", len(collections))
	}
}

func TestCollectionItems(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/collections/COLL1234/items" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	items, err := client.CollectionItems(context.Background(), "COLL1234", nil)
	if err != nil {
		t.Fatalf("CollectionItems() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestCollectionItemsTop(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/collections/COLL1234/items/top" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	items, err := client.CollectionItemsTop(context.Background(), "COLL1234", nil)
	if err != nil {
		t.Fatalf("CollectionItemsTop() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestTags(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "tags.json"))
	})
	defer server.Close()

	tags, err := client.Tags(context.Background(), nil)
	if err != nil {
		t.Fatalf("Tags() error = %v", err)
	}

	if len(tags) != 2 {
		t.Errorf("len(tags) = %v, want 2", len(tags))
	}
	if tags[0].Tag != "test" {
		t.Errorf("tags[0].Tag = %v, want test", tags[0].Tag)
	}
}

func TestGroups(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/groups" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "groups.json"))
	})
	defer server.Close()

	groups, err := client.Groups(context.Background(), nil)
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}

	if len(groups) != 1 {
		t.Errorf("len(groups) = %v, want 1", len(groups))
	}
	if groups[0].Name != "smart_cities" {
		t.Errorf("groups[0].Name = %v, want smart_cities", groups[0].Name)
	}
}

func TestGroupsWithGroupLibrary(t *testing.T) {
	client := NewClient("12345", LibraryTypeGroup)
	_, err := client.Groups(context.Background(), nil)
	if err == nil {
		t.Error("Groups() with group library should return error")
	}
}

func TestNumItems(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Total-Results", "42")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})
	defer server.Close()

	count, err := client.NumItems(context.Background())
	if err != nil {
		t.Fatalf("NumItems() error = %v", err)
	}

	if count != 42 {
		t.Errorf("count = %v, want 42", count)
	}
}

func TestLastModifiedVersion(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified-Version", "100")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})
	defer server.Close()

	version, err := client.LastModifiedVersion(context.Background())
	if err != nil {
		t.Fatalf("LastModifiedVersion() error = %v", err)
	}

	if version != 100 {
		t.Errorf("version = %v, want 100", version)
	}
}

func TestDeleted(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("since") != "50" {
			t.Errorf("since = %v, want 50", query.Get("since"))
		}

		w.Header().Set("Content-Type", "application/json")
		deletedData := DeletedContent{
			Items:       []string{"ITEM1", "ITEM2"},
			Collections: []string{"COLL1"},
			Searches:    []string{},
			Tags:        []string{"oldtag"},
		}
		data, _ := json.Marshal(deletedData)
		w.Write(data)
	})
	defer server.Close()

	deleted, err := client.Deleted(context.Background(), 50)
	if err != nil {
		t.Fatalf("Deleted() error = %v", err)
	}

	if len(deleted.Items) != 2 {
		t.Errorf("len(deleted.Items) = %v, want 2", len(deleted.Items))
	}
	if deleted.Items[0] != "ITEM1" {
		t.Errorf("deleted.Items[0] = %v, want ITEM1", deleted.Items[0])
	}
	if len(deleted.Collections) != 1 {
		t.Errorf("len(deleted.Collections) = %v, want 1", len(deleted.Collections))
	}
}

func TestAPIError(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Item not found"}`))
	})
	defer server.Close()

	_, err := client.Item(context.Background(), "NOTFOUND", nil)
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestContextCancellation(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// This handler should not be reached
		t.Error("request should have been cancelled")
	})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Items(ctx, nil)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestItemsWithItemTypeFilter(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		itemType := query.Get("itemType")
		if itemType != "journalArticle" {
			t.Errorf("itemType = %v, want journalArticle", itemType)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	params := &QueryParams{
		ItemType: []string{"journalArticle"},
	}
	items, err := client.Items(context.Background(), params)
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestItemsWithExcludeItemType(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		itemType := query.Get("itemType")
		if itemType != "-annotation" {
			t.Errorf("itemType = %v, want -annotation", itemType)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	params := &QueryParams{
		ItemType: []string{"-annotation"},
	}
	items, err := client.Items(context.Background(), params)
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestItemsWithMultipleItemTypes(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		itemType := query.Get("itemType")
		// Multiple item types should be joined with " || "
		expectedItemType := "journalArticle || book || -annotation"
		if itemType != expectedItemType {
			t.Errorf("itemType = %v, want %v", itemType, expectedItemType)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "items.json"))
	})
	defer server.Close()

	params := &QueryParams{
		ItemType: []string{"journalArticle", "book", "-annotation"},
	}
	items, err := client.Items(context.Background(), params)
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	if len(items) != 2 {
		t.Errorf("len(items) = %v, want 2", len(items))
	}
}

func TestFile(t *testing.T) {
	expectedContent := []byte("This is a test PDF file content")

	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/12345/items/ABCD1234/file" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Write(expectedContent)
	})
	defer server.Close()

	content, err := client.File(context.Background(), "ABCD1234")
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}

	if string(content) != string(expectedContent) {
		t.Errorf("content = %v, want %v", string(content), string(expectedContent))
	}
}

func TestFileNotFound(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "File not found"}`))
	})
	defer server.Close()

	_, err := client.File(context.Background(), "NOTFOUND")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestDump(t *testing.T) {
	expectedContent := []byte("This is a test PDF file content")

	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/12345/items/ABCD1234" {
			// Return item metadata
			w.Header().Set("Content-Type", "application/json")
			item := Item{
				Key: "ABCD1234",
				Data: ItemData{
					ItemType: "attachment",
					Title:    "Test PDF",
					Filename: "test.pdf",
				},
			}
			data, _ := json.Marshal(item)
			w.Write(data)
		} else if r.URL.Path == "/users/12345/items/ABCD1234/file" {
			// Return file content
			w.Header().Set("Content-Type", "application/pdf")
			w.Write(expectedContent)
		} else {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})
	defer server.Close()

	// Test with explicit filename and path
	fullPath, err := client.Dump(context.Background(), "ABCD1234", "custom.pdf", tmpDir)
	if err != nil {
		t.Fatalf("Dump() error = %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "custom.pdf")
	if fullPath != expectedPath {
		t.Errorf("fullPath = %v, want %v", fullPath, expectedPath)
	}

	// Verify file was written correctly
	content, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("error reading dumped file: %v", err)
	}
	if string(content) != string(expectedContent) {
		t.Errorf("file content = %v, want %v", string(content), string(expectedContent))
	}
}

func TestDumpWithAutoFilename(t *testing.T) {
	expectedContent := []byte("This is a test PDF file content")

	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/12345/items/ABCD1234" {
			// Return item metadata
			w.Header().Set("Content-Type", "application/json")
			item := Item{
				Key: "ABCD1234",
				Data: ItemData{
					ItemType: "attachment",
					Title:    "Test PDF",
					Filename: "auto-filename.pdf",
				},
			}
			data, _ := json.Marshal(item)
			w.Write(data)
		} else if r.URL.Path == "/users/12345/items/ABCD1234/file" {
			// Return file content
			w.Header().Set("Content-Type", "application/pdf")
			w.Write(expectedContent)
		} else {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})
	defer server.Close()

	// Test with auto-detected filename
	fullPath, err := client.Dump(context.Background(), "ABCD1234", "", tmpDir)
	if err != nil {
		t.Fatalf("Dump() error = %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "auto-filename.pdf")
	if fullPath != expectedPath {
		t.Errorf("fullPath = %v, want %v", fullPath, expectedPath)
	}

	// Verify file was written correctly
	content, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("error reading dumped file: %v", err)
	}
	if string(content) != string(expectedContent) {
		t.Errorf("file content = %v, want %v", string(content), string(expectedContent))
	}
}

func TestDumpWithTitleFallback(t *testing.T) {
	expectedContent := []byte("This is a test PDF file content")

	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/12345/items/ABCD1234" {
			// Return item metadata without filename
			w.Header().Set("Content-Type", "application/json")
			item := Item{
				Key: "ABCD1234",
				Data: ItemData{
					ItemType: "attachment",
					Title:    "Fallback Title",
					Filename: "", // No filename, should fall back to title
				},
			}
			data, _ := json.Marshal(item)
			w.Write(data)
		} else if r.URL.Path == "/users/12345/items/ABCD1234/file" {
			// Return file content
			w.Header().Set("Content-Type", "application/pdf")
			w.Write(expectedContent)
		} else {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})
	defer server.Close()

	// Test with auto-detected filename (should fall back to title)
	fullPath, err := client.Dump(context.Background(), "ABCD1234", "", tmpDir)
	if err != nil {
		t.Fatalf("Dump() error = %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "Fallback Title")
	if fullPath != expectedPath {
		t.Errorf("fullPath = %v, want %v", fullPath, expectedPath)
	}

	// Verify file was written correctly
	content, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("error reading dumped file: %v", err)
	}
	if string(content) != string(expectedContent) {
		t.Errorf("file content = %v, want %v", string(content), string(expectedContent))
	}
}

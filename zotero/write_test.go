package zotero

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateItems(t *testing.T) {
	tests := []struct {
		name            string
		items           []Item
		responseCode    int
		responseBody    string
		expectError     bool
		expectedSuccess int
	}{
		{
			name: "create single item",
			items: []Item{
				{
					Data: ItemData{
						ItemType: ItemTypeBook,
						Title:    "Test Book",
					},
				},
			},
			responseCode:    http.StatusOK,
			responseBody:    `{"success": {"0": "ABCD1234"}, "unchanged": {}, "failed": {}}`,
			expectError:     false,
			expectedSuccess: 1,
		},
		{
			name: "create multiple items",
			items: []Item{
				{Data: ItemData{ItemType: ItemTypeBook, Title: "Book 1"}},
				{Data: ItemData{ItemType: ItemTypeJournalArticle, Title: "Article 1"}},
			},
			responseCode:    http.StatusOK,
			responseBody:    `{"success": {"0": "ABCD1234", "1": "EFGH5678"}, "unchanged": {}, "failed": {}}`,
			expectError:     false,
			expectedSuccess: 2,
		},
		{
			name:         "no items provided",
			items:        []Item{},
			responseCode: http.StatusOK,
			expectError:  true,
		},
		{
			name: "too many items",
			items: func() []Item {
				items := make([]Item, 51)
				for i := range items {
					items[i] = Item{Data: ItemData{ItemType: ItemTypeBook}}
				}
				return items
			}(),
			responseCode: http.StatusOK,
			expectError:  true,
		},
		{
			name: "API error",
			items: []Item{
				{Data: ItemData{ItemType: ItemTypeBook}},
			},
			responseCode: http.StatusBadRequest,
			responseBody: `{"error": "Invalid request"}`,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/users/12345/items" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.responseCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := NewClient("12345", LibraryTypeUser,
				WithBaseURL(server.URL),
				WithAPIKey("test-key"),
			)

			resp, err := client.CreateItems(context.Background(), tt.items)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if resp == nil {
				t.Error("expected response, got nil")
				return
			}

			if len(resp.Success) != tt.expectedSuccess {
				t.Errorf("expected %d successful items, got %d", tt.expectedSuccess, len(resp.Success))
			}
		})
	}
}

func TestUpdateItem(t *testing.T) {
	tests := []struct {
		name         string
		item         *Item
		responseCode int
		expectError  bool
	}{
		{
			name: "update item",
			item: &Item{
				Key:     "ABCD1234",
				Version: 5,
				Data: ItemData{
					Key:      "ABCD1234",
					Version:  5,
					ItemType: ItemTypeBook,
					Title:    "Updated Title",
				},
			},
			responseCode: http.StatusNoContent,
			expectError:  false,
		},
		{
			name:         "nil item",
			item:         nil,
			responseCode: http.StatusOK,
			expectError:  true,
		},
		{
			name: "missing key",
			item: &Item{
				Version: 5,
				Data: ItemData{
					ItemType: ItemTypeBook,
				},
			},
			responseCode: http.StatusOK,
			expectError:  true,
		},
		{
			name: "API error",
			item: &Item{
				Key:     "ABCD1234",
				Version: 5,
				Data:    ItemData{ItemType: ItemTypeBook},
			},
			responseCode: http.StatusBadRequest,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("expected PATCH, got %s", r.Method)
				}
				if tt.item != nil && tt.item.Key != "" {
					expectedPath := "/users/12345/items/" + tt.item.Key
					if r.URL.Path != expectedPath {
						t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
					}

					versionHeader := r.Header.Get("If-Unmodified-Since-Version")
					if versionHeader == "" {
						t.Error("expected If-Unmodified-Since-Version header")
					}
				}
				w.WriteHeader(tt.responseCode)
			}))
			defer server.Close()

			client := NewClient("12345", LibraryTypeUser,
				WithBaseURL(server.URL),
				WithAPIKey("test-key"),
			)

			err := client.UpdateItem(context.Background(), tt.item)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestUpdateItems(t *testing.T) {
	tests := []struct {
		name         string
		items        []Item
		responseCode int
		responseBody string
		expectError  bool
	}{
		{
			name: "update multiple items",
			items: []Item{
				{Key: "ABCD1234", Version: 5, Data: ItemData{Key: "ABCD1234", Version: 5, ItemType: ItemTypeBook}},
				{Key: "EFGH5678", Version: 3, Data: ItemData{Key: "EFGH5678", Version: 3, ItemType: ItemTypeJournalArticle}},
			},
			responseCode: http.StatusOK,
			responseBody: `{"success": {"ABCD1234": "ABCD1234", "EFGH5678": "EFGH5678"}, "unchanged": {}, "failed": {}}`,
			expectError:  false,
		},
		{
			name:         "no items",
			items:        []Item{},
			responseCode: http.StatusOK,
			expectError:  true,
		},
		{
			name: "item missing version",
			items: []Item{
				{Key: "ABCD1234", Data: ItemData{Key: "ABCD1234", ItemType: ItemTypeBook}},
			},
			responseCode: http.StatusOK,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				w.WriteHeader(tt.responseCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := NewClient("12345", LibraryTypeUser,
				WithBaseURL(server.URL),
				WithAPIKey("test-key"),
			)

			_, err := client.UpdateItems(context.Background(), tt.items)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeleteItem(t *testing.T) {
	tests := []struct {
		name         string
		itemKey      string
		version      int
		responseCode int
		expectError  bool
	}{
		{
			name:         "delete item",
			itemKey:      "ABCD1234",
			version:      5,
			responseCode: http.StatusNoContent,
			expectError:  false,
		},
		{
			name:         "missing key",
			itemKey:      "",
			version:      5,
			responseCode: http.StatusOK,
			expectError:  true,
		},
		{
			name:         "missing version",
			itemKey:      "ABCD1234",
			version:      0,
			responseCode: http.StatusOK,
			expectError:  true,
		},
		{
			name:         "API error",
			itemKey:      "ABCD1234",
			version:      5,
			responseCode: http.StatusNotFound,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				if tt.itemKey != "" {
					expectedPath := "/users/12345/items/" + tt.itemKey
					if r.URL.Path != expectedPath {
						t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
					}
				}
				w.WriteHeader(tt.responseCode)
			}))
			defer server.Close()

			client := NewClient("12345", LibraryTypeUser,
				WithBaseURL(server.URL),
				WithAPIKey("test-key"),
			)

			err := client.DeleteItem(context.Background(), tt.itemKey, tt.version)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeleteItems(t *testing.T) {
	tests := []struct {
		name         string
		itemKeys     []string
		version      int
		responseCode int
		expectError  bool
	}{
		{
			name:         "delete multiple items",
			itemKeys:     []string{"ABCD1234", "EFGH5678"},
			version:      10,
			responseCode: http.StatusNoContent,
			expectError:  false,
		},
		{
			name:         "no items",
			itemKeys:     []string{},
			version:      10,
			responseCode: http.StatusOK,
			expectError:  true,
		},
		{
			name: "too many items",
			itemKeys: func() []string {
				keys := make([]string, 51)
				for i := range keys {
					keys[i] = "KEY" + string(rune(i))
				}
				return keys
			}(),
			version:      10,
			responseCode: http.StatusOK,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				w.WriteHeader(tt.responseCode)
			}))
			defer server.Close()

			client := NewClient("12345", LibraryTypeUser,
				WithBaseURL(server.URL),
				WithAPIKey("test-key"),
			)

			err := client.DeleteItems(context.Background(), tt.itemKeys, tt.version)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCreateCollections(t *testing.T) {
	tests := []struct {
		name        string
		collections []Collection
		expectError bool
	}{
		{
			name: "create single collection",
			collections: []Collection{
				{Data: CollectionData{Name: "Test Collection"}},
			},
			expectError: false,
		},
		{
			name:        "no collections",
			collections: []Collection{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"success": {"0": "COLL1234"}, "unchanged": {}, "failed": {}}`))
			}))
			defer server.Close()

			client := NewClient("12345", LibraryTypeUser,
				WithBaseURL(server.URL),
				WithAPIKey("test-key"),
			)

			_, err := client.CreateCollections(context.Background(), tt.collections)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestUpdateCollection(t *testing.T) {
	collection := &Collection{
		Key:     "COLL1234",
		Version: 3,
		Data: CollectionData{
			Key:     "COLL1234",
			Version: 3,
			Name:    "Updated Collection",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("12345", LibraryTypeUser,
		WithBaseURL(server.URL),
		WithAPIKey("test-key"),
	)

	err := client.UpdateCollection(context.Background(), collection)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeleteCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("12345", LibraryTypeUser,
		WithBaseURL(server.URL),
		WithAPIKey("test-key"),
	)

	err := client.DeleteCollection(context.Background(), "COLL1234", 5)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateSearches(t *testing.T) {
	searches := []Search{
		{
			Data: SearchData{
				Name: "Test Search",
				Conditions: []SearchCondition{
					{Condition: "title", Operator: "contains", Value: "test"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": {"0": "SRCH1234"}, "unchanged": {}, "failed": {}}`))
	}))
	defer server.Close()

	client := NewClient("12345", LibraryTypeUser,
		WithBaseURL(server.URL),
		WithAPIKey("test-key"),
	)

	resp, err := client.CreateSearches(context.Background(), searches)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Error("expected response, got nil")
	}
}

func TestUpdateSearch(t *testing.T) {
	search := &Search{
		Key:     "SRCH1234",
		Version: 2,
		Data: SearchData{
			Key:     "SRCH1234",
			Version: 2,
			Name:    "Updated Search",
			Conditions: []SearchCondition{
				{Condition: "title", Operator: "contains", Value: "updated"},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("12345", LibraryTypeUser,
		WithBaseURL(server.URL),
		WithAPIKey("test-key"),
	)

	err := client.UpdateSearch(context.Background(), search)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeleteSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("12345", LibraryTypeUser,
		WithBaseURL(server.URL),
		WithAPIKey("test-key"),
	)

	err := client.DeleteSearch(context.Background(), "SRCH1234", 3)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// Return existing item
			item := Item{
				Key:     "ABCD1234",
				Version: 5,
				Data: ItemData{
					Key:      "ABCD1234",
					Version:  5,
					ItemType: ItemTypeBook,
					Title:    "Test Book",
					Tags:     []Tag{{Tag: "existing"}},
				},
			}
			json.NewEncoder(w).Encode(item)
		} else if r.Method == http.MethodPatch {
			// Update item
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := NewClient("12345", LibraryTypeUser,
		WithBaseURL(server.URL),
		WithAPIKey("test-key"),
	)

	err := client.AddTags(context.Background(), "ABCD1234", "new-tag", "another-tag")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeleteTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("12345", LibraryTypeUser,
		WithBaseURL(server.URL),
		WithAPIKey("test-key"),
	)

	err := client.DeleteTags(context.Background(), 10, "tag1", "tag2")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWriteResponseParsing(t *testing.T) {
	responseJSON := `{
		"success": {
			"0": "ABCD1234",
			"1": "EFGH5678"
		},
		"unchanged": {
			"2": "IJKL9012"
		},
		"failed": {
			"3": {
				"code": 400,
				"message": "Invalid item type"
			}
		}
	}`

	var resp WriteResponse
	err := json.Unmarshal([]byte(responseJSON), &resp)
	if err != nil {
		t.Fatalf("error unmarshaling response: %v", err)
	}

	if len(resp.Success) != 2 {
		t.Errorf("expected 2 successful items, got %d", len(resp.Success))
	}
	if len(resp.Unchanged) != 1 {
		t.Errorf("expected 1 unchanged item, got %d", len(resp.Unchanged))
	}
	if len(resp.Failed) != 1 {
		t.Errorf("expected 1 failed item, got %d", len(resp.Failed))
	}

	if failed, ok := resp.Failed["3"]; ok {
		if failed.Code != 400 {
			t.Errorf("expected code 400, got %d", failed.Code)
		}
		if failed.Message != "Invalid item type" {
			t.Errorf("expected message 'Invalid item type', got '%s'", failed.Message)
		}
	} else {
		t.Error("expected failed item with key '3'")
	}
}

func TestDefaultAttachmentFilenameHandlesWindowsPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "unix", path: "/tmp/papers/report.pdf", want: "report.pdf"},
		{name: "windows", path: `C:\Users\Vivek\papers\report.pdf`, want: "report.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultAttachmentFilename(tt.path); got != tt.want {
				t.Fatalf("defaultAttachmentFilename(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

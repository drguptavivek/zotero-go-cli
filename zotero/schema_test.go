package zotero

import (
	"context"
	"net/http"
	"testing"
)

func TestItemTypes(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/itemTypes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "itemtypes.json"))
	})
	defer server.Close()

	itemTypes, err := client.ItemTypes(context.Background(), "")
	if err != nil {
		t.Fatalf("ItemTypes() error = %v", err)
	}

	if len(itemTypes) != 4 {
		t.Errorf("len(itemTypes) = %v, want 4", len(itemTypes))
	}
	if itemTypes[1].ItemType != "book" {
		t.Errorf("itemTypes[1].ItemType = %v, want book", itemTypes[1].ItemType)
	}
	if itemTypes[1].Localized != "Book" {
		t.Errorf("itemTypes[1].Localized = %v, want Book", itemTypes[1].Localized)
	}
}

func TestItemTypesWithLocale(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("locale") != "de-DE" {
			t.Errorf("locale = %v, want de-DE", query.Get("locale"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "itemtypes.json"))
	})
	defer server.Close()

	_, err := client.ItemTypes(context.Background(), "de-DE")
	if err != nil {
		t.Fatalf("ItemTypes() error = %v", err)
	}
}

func TestItemFields(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/itemFields" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "itemfields.json"))
	})
	defer server.Close()

	fields, err := client.ItemFields(context.Background(), "")
	if err != nil {
		t.Fatalf("ItemFields() error = %v", err)
	}

	if len(fields) != 4 {
		t.Errorf("len(fields) = %v, want 4", len(fields))
	}
	if fields[0].Field != "title" {
		t.Errorf("fields[0].Field = %v, want title", fields[0].Field)
	}
	if fields[0].Localized != "Title" {
		t.Errorf("fields[0].Localized = %v, want Title", fields[0].Localized)
	}
}

func TestItemTypeFields(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/itemTypeFields" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("itemType") != "book" {
			t.Errorf("itemType = %v, want book", query.Get("itemType"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "itemtypefields.json"))
	})
	defer server.Close()

	fields, err := client.ItemTypeFields(context.Background(), "book", "")
	if err != nil {
		t.Fatalf("ItemTypeFields() error = %v", err)
	}

	if len(fields) != 4 {
		t.Errorf("len(fields) = %v, want 4", len(fields))
	}
	if fields[1].Field != "publisher" {
		t.Errorf("fields[1].Field = %v, want publisher", fields[1].Field)
	}
}

func TestItemTypeFieldsWithConstant(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("itemType") != "book" {
			t.Errorf("itemType = %v, want book", query.Get("itemType"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "itemtypefields.json"))
	})
	defer server.Close()

	// Test using the constant
	_, err := client.ItemTypeFields(context.Background(), ItemTypeBook, "")
	if err != nil {
		t.Fatalf("ItemTypeFields() error = %v", err)
	}
}

func TestItemTypeCreatorTypes(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/itemTypeCreatorTypes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("itemType") != "journalArticle" {
			t.Errorf("itemType = %v, want journalArticle", query.Get("itemType"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "creatortypes.json"))
	})
	defer server.Close()

	creatorTypes, err := client.ItemTypeCreatorTypes(context.Background(), ItemTypeJournalArticle, "")
	if err != nil {
		t.Fatalf("ItemTypeCreatorTypes() error = %v", err)
	}

	if len(creatorTypes) != 3 {
		t.Errorf("len(creatorTypes) = %v, want 3", len(creatorTypes))
	}
	if creatorTypes[0].CreatorType != "author" {
		t.Errorf("creatorTypes[0].CreatorType = %v, want author", creatorTypes[0].CreatorType)
	}
	if creatorTypes[0].Localized != "Author" {
		t.Errorf("creatorTypes[0].Localized = %v, want Author", creatorTypes[0].Localized)
	}
}

func TestCreatorFields(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/creatorFields" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "creatorfields.json"))
	})
	defer server.Close()

	fields, err := client.CreatorFields(context.Background(), "")
	if err != nil {
		t.Fatalf("CreatorFields() error = %v", err)
	}

	if len(fields) != 3 {
		t.Errorf("len(fields) = %v, want 3", len(fields))
	}
	if fields[0].Field != "firstName" {
		t.Errorf("fields[0].Field = %v, want firstName", fields[0].Field)
	}
}

func TestNewItemTemplate(t *testing.T) {
	server, client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/items/new" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("itemType") != "book" {
			t.Errorf("itemType = %v, want book", query.Get("itemType"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(loadFixture(t, "itemtemplate.json"))
	})
	defer server.Close()

	template, err := client.NewItemTemplate(context.Background(), ItemTypeBook)
	if err != nil {
		t.Fatalf("NewItemTemplate() error = %v", err)
	}

	if template == nil {
		t.Fatal("template is nil")
	}
	if template["itemType"] != "book" {
		t.Errorf("template[itemType] = %v, want book", template["itemType"])
	}
	if template["title"] != "" {
		t.Errorf("template[title] = %v, want empty string", template["title"])
	}
}

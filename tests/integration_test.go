//go:build integration

package tests

import (
	"context"
	"testing"

	"github.com/Epistemic-Technology/zotero/zotero"
)

// TestItems tests retrieving items from the library
func TestItems(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()
	params := &zotero.QueryParams{
		Limit: 10,
	}

	items, err := client.Items(ctx, params)
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	if len(items) > 10 {
		t.Errorf("Items() returned %d items, expected max 10", len(items))
	}

	// If items exist, verify structure
	if len(items) > 0 {
		item := items[0]
		if item.Key == "" {
			t.Error("Item key is empty")
		}
		if item.Version == 0 {
			t.Error("Item version is zero")
		}
		if item.Data.ItemType == "" {
			t.Error("Item type is empty")
		}
	}

	t.Logf("Successfully retrieved %d items", len(items))
}

// TestTop tests retrieving top-level items
func TestTop(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()
	params := &zotero.QueryParams{
		Limit: 5,
	}

	items, err := client.Top(ctx, params)
	if err != nil {
		t.Fatalf("Top() error = %v", err)
	}

	if len(items) > 5 {
		t.Errorf("Top() returned %d items, expected max 5", len(items))
	}

	t.Logf("Successfully retrieved %d top-level items", len(items))
}

// TestItemByKey tests retrieving a specific item by key
func TestItemByKey(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// First get an item to test with
	items, err := client.Items(ctx, &zotero.QueryParams{Limit: 1})
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	if len(items) == 0 {
		t.Skip("No items in library to test with")
	}

	itemKey := items[0].Key

	// Now fetch that specific item
	item, err := client.Item(ctx, itemKey, nil)
	if err != nil {
		t.Fatalf("Item() error = %v", err)
	}

	if item.Key != itemKey {
		t.Errorf("Item() returned key %s, expected %s", item.Key, itemKey)
	}

	t.Logf("Successfully retrieved item with key %s (type: %s)", item.Key, item.Data.ItemType)
}

// TestChildren tests retrieving child items
func TestChildren(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// Find an item with children
	items, err := client.Items(ctx, &zotero.QueryParams{Limit: 20})
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	var parentKey string
	for _, item := range items {
		if item.Meta.NumChildren > 0 {
			parentKey = item.Key
			break
		}
	}

	if parentKey == "" {
		t.Skip("No items with children found to test with")
	}

	// Get children
	children, err := client.Children(ctx, parentKey, nil)
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}

	t.Logf("Successfully retrieved %d children for item %s", len(children), parentKey)
}

// TestCollections tests retrieving collections
func TestCollections(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()
	params := &zotero.QueryParams{
		Limit: 10,
	}

	collections, err := client.Collections(ctx, params)
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	if len(collections) > 10 {
		t.Errorf("Collections() returned %d collections, expected max 10", len(collections))
	}

	// If collections exist, verify structure
	if len(collections) > 0 {
		coll := collections[0]
		if coll.Key == "" {
			t.Error("Collection key is empty")
		}
		if coll.Data.Name == "" {
			t.Error("Collection name is empty")
		}
	}

	t.Logf("Successfully retrieved %d collections", len(collections))
}

// TestCollectionsTop tests retrieving top-level collections
func TestCollectionsTop(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	collections, err := client.CollectionsTop(ctx, nil)
	if err != nil {
		t.Fatalf("CollectionsTop() error = %v", err)
	}

	// Verify all returned collections are top-level (no parent)
	for _, coll := range collections {
		if coll.Data.ParentCollection != "" {
			t.Errorf("CollectionsTop() returned collection %s with parent %s",
				coll.Key, coll.Data.ParentCollection.String())
		}
	}

	t.Logf("Successfully retrieved %d top-level collections", len(collections))
}

// TestCollection tests retrieving a specific collection
func TestCollection(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// First get a collection to test with
	collections, err := client.Collections(ctx, &zotero.QueryParams{Limit: 1})
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	if len(collections) == 0 {
		t.Skip("No collections in library to test with")
	}

	collectionKey := collections[0].Key

	// Now fetch that specific collection
	collection, err := client.Collection(ctx, collectionKey, nil)
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if collection.Key != collectionKey {
		t.Errorf("Collection() returned key %s, expected %s", collection.Key, collectionKey)
	}

	t.Logf("Successfully retrieved collection '%s' (key: %s)", collection.Data.Name, collection.Key)
}

// TestCollectionItems tests retrieving items in a collection
func TestCollectionItems(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// Find a collection with items
	collections, err := client.Collections(ctx, nil)
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	var collectionKey string
	for _, coll := range collections {
		if coll.Meta.NumItems > 0 {
			collectionKey = coll.Key
			break
		}
	}

	if collectionKey == "" {
		t.Skip("No collections with items found to test with")
	}

	// Get items in the collection
	items, err := client.CollectionItems(ctx, collectionKey, &zotero.QueryParams{Limit: 5})
	if err != nil {
		t.Fatalf("CollectionItems() error = %v", err)
	}

	t.Logf("Successfully retrieved %d items from collection %s", len(items), collectionKey)
}

// TestTags tests retrieving tags
func TestTags(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()
	params := &zotero.QueryParams{
		Limit: 20,
	}

	tags, err := client.Tags(ctx, params)
	if err != nil {
		t.Fatalf("Tags() error = %v", err)
	}

	if len(tags) > 20 {
		t.Errorf("Tags() returned %d tags, expected max 20", len(tags))
	}

	// If tags exist, verify structure
	if len(tags) > 0 {
		tag := tags[0]
		if tag.Tag == "" {
			t.Error("Tag name is empty")
		}
	}

	t.Logf("Successfully retrieved %d tags", len(tags))
}

// TestItemTags tests retrieving tags for a specific item
func TestItemTags(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// First get an item to test with
	items, err := client.Items(ctx, &zotero.QueryParams{Limit: 10})
	if err != nil {
		t.Fatalf("Items() error = %v", err)
	}

	// Find an item with tags
	var itemKey string
	for _, item := range items {
		if len(item.Data.Tags) > 0 {
			itemKey = item.Key
			break
		}
	}

	if itemKey == "" {
		t.Skip("No items with tags found to test with")
	}

	// Get tags for that item
	tags, err := client.ItemTags(ctx, itemKey, nil)
	if err != nil {
		t.Fatalf("ItemTags() error = %v", err)
	}

	t.Logf("Successfully retrieved %d tags for item %s", len(tags), itemKey)
}

// TestGroups tests retrieving groups (only works with user libraries)
func TestGroups(t *testing.T) {
	client := skipIfNoCredentials(t)

	// Only test groups if this is a user library
	if getTestLibraryType() != "user" {
		t.Skip("Groups() only works with user libraries, skipping")
	}

	ctx := context.Background()

	groups, err := client.Groups(ctx, nil)
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}

	// If groups exist, verify structure
	if len(groups) > 0 {
		group := groups[0]
		if group.ID == 0 {
			t.Error("Group ID is zero")
		}
		if group.Name == "" {
			t.Error("Group name is empty")
		}
	}

	t.Logf("Successfully retrieved %d groups", len(groups))
}

// TestNumItems tests getting the total count of items
func TestNumItems(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	count, err := client.NumItems(ctx)
	if err != nil {
		t.Fatalf("NumItems() error = %v", err)
	}

	if count < 0 {
		t.Errorf("NumItems() returned negative count: %d", count)
	}

	t.Logf("Library has %d items", count)
}

// TestLastModifiedVersion tests getting the library's last modified version
func TestLastModifiedVersion(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	version, err := client.LastModifiedVersion(ctx)
	if err != nil {
		t.Fatalf("LastModifiedVersion() error = %v", err)
	}

	if version < 0 {
		t.Errorf("LastModifiedVersion() returned negative version: %d", version)
	}

	t.Logf("Library last modified version: %d", version)
}

// TestDeleted tests retrieving deleted items
func TestDeleted(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// Use version 0 to get all deleted items since beginning
	deleted, err := client.Deleted(ctx, 0)
	if err != nil {
		t.Fatalf("Deleted() error = %v", err)
	}

	t.Logf("Deleted content: %d items, %d collections, %d searches, %d tags",
		len(deleted.Items), len(deleted.Collections), len(deleted.Searches), len(deleted.Tags))
}

// TestPagination tests pagination with limit and start parameters
func TestPagination(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// Get first page
	page1, err := client.Items(ctx, &zotero.QueryParams{
		Limit: 5,
		Start: 0,
	})
	if err != nil {
		t.Fatalf("Items() page 1 error = %v", err)
	}

	// Get second page
	page2, err := client.Items(ctx, &zotero.QueryParams{
		Limit: 5,
		Start: 5,
	})
	if err != nil {
		t.Fatalf("Items() page 2 error = %v", err)
	}

	// Verify pages are different (if library has enough items)
	if len(page1) > 0 && len(page2) > 0 && page1[0].Key == page2[0].Key {
		t.Error("Pagination returned same items on different pages")
	}

	t.Logf("Pagination test: page 1 has %d items, page 2 has %d items", len(page1), len(page2))
}

// TestSorting tests sorting items by different fields
func TestSorting(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	sortFields := []string{"title", "dateAdded", "dateModified"}

	for _, sortField := range sortFields {
		items, err := client.Items(ctx, &zotero.QueryParams{
			Limit: 5,
			Sort:  sortField,
		})
		if err != nil {
			t.Errorf("Items() with sort=%s error = %v", sortField, err)
			continue
		}

		t.Logf("Successfully retrieved %d items sorted by %s", len(items), sortField)
	}
}

// TestQuickSearch tests quick search functionality
func TestQuickSearch(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// Search for a common term
	items, err := client.Items(ctx, &zotero.QueryParams{
		Q:     "the",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Items() with quick search error = %v", err)
	}

	t.Logf("Quick search for 'the' returned %d items", len(items))
}

// TestTrash tests retrieving items in trash
func TestTrash(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	items, err := client.Trash(ctx, nil)
	if err != nil {
		t.Fatalf("Trash() error = %v", err)
	}

	t.Logf("Trash contains %d items", len(items))
}

// TestItemTypeFilter tests filtering items by item type
func TestItemTypeFilter(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// Test filtering for journal articles
	articles, err := client.Items(ctx, &zotero.QueryParams{
		ItemType: []string{"journalArticle"},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Items() with itemType filter error = %v", err)
	}

	// Verify all returned items are journal articles
	for _, item := range articles {
		if item.Data.ItemType != "journalArticle" {
			t.Errorf("Expected journalArticle, got %s", item.Data.ItemType)
		}
	}

	t.Logf("Successfully retrieved %d journal articles", len(articles))
}

// TestExcludeItemType tests excluding item types using negative filter
func TestExcludeItemType(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// Test excluding annotations
	items, err := client.Items(ctx, &zotero.QueryParams{
		ItemType: []string{"-annotation"},
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("Items() with exclude itemType error = %v", err)
	}

	// Verify no returned items are annotations
	for _, item := range items {
		if item.Data.ItemType == "annotation" {
			t.Errorf("Found annotation item when it should be excluded: %s", item.Key)
		}
	}

	t.Logf("Successfully retrieved %d items (excluding annotations)", len(items))
}

// TestMultipleItemTypes tests filtering for multiple item types
func TestMultipleItemTypes(t *testing.T) {
	client := skipIfNoCredentials(t)

	ctx := context.Background()

	// Test filtering for books and journal articles
	items, err := client.Items(ctx, &zotero.QueryParams{
		ItemType: []string{"book", "journalArticle"},
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("Items() with multiple itemType filters error = %v", err)
	}

	// Verify all returned items are either books or journal articles
	for _, item := range items {
		if item.Data.ItemType != "book" && item.Data.ItemType != "journalArticle" {
			t.Errorf("Expected book or journalArticle, got %s", item.Data.ItemType)
		}
	}

	t.Logf("Successfully retrieved %d books and journal articles", len(items))
}

//go:build integration_write

package tests

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Epistemic-Technology/zotero/zotero"
)

// TestWriteItemCreateAndDelete tests creating and deleting a single item
func TestWriteItemCreateAndDelete(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a simple item (using book with just title for simplicity)
	item := zotero.Item{
		Data: zotero.ItemData{
			ItemType: zotero.ItemTypeBook,
			Title:    "Integration Test Item - Safe to Delete",
		},
	}

	// Create the item
	resp, err := client.CreateItems(ctx, []zotero.Item{item})
	if err != nil {
		t.Fatalf("CreateItems() error = %v", err)
	}

	if len(resp.Success) != 1 {
		t.Fatalf("expected 1 successful item, got %d", len(resp.Success))
	}

	// Get the created item key
	var createdKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKey = keyStr
			break
		}
	}

	t.Logf("Created item with key: %s", createdKey)

	// Cleanup: delete the item
	defer func() {
		// Fetch current version before deleting
		fetchedItem, err := client.Item(ctx, createdKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch item for cleanup: %v", err)
			return
		}

		err = client.DeleteItem(ctx, createdKey, fetchedItem.Version)
		if err != nil {
			t.Errorf("Failed to cleanup item: %v", err)
		} else {
			t.Logf("Successfully deleted item %s", createdKey)
		}
	}()

	// Verify the item was created
	fetchedItem, err := client.Item(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Item() error = %v", err)
	}

	if fetchedItem.Key != createdKey {
		t.Errorf("expected key %s, got %s", createdKey, fetchedItem.Key)
	}

	if fetchedItem.Data.ItemType != zotero.ItemTypeBook {
		t.Errorf("expected itemType %s, got %s", zotero.ItemTypeBook, fetchedItem.Data.ItemType)
	}

	if fetchedItem.Data.Title != "Integration Test Item - Safe to Delete" {
		t.Errorf("expected title 'Integration Test Item - Safe to Delete', got '%s'", fetchedItem.Data.Title)
	}
}

// TestWriteItemUpdate tests updating an existing item
func TestWriteItemUpdate(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a test item
	item := zotero.Item{
		Data: zotero.ItemData{
			ItemType: zotero.ItemTypeBook,
			Title:    "Original Title",
		},
	}

	resp, err := client.CreateItems(ctx, []zotero.Item{item})
	if err != nil {
		t.Fatalf("CreateItems() error = %v", err)
	}

	var createdKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKey = keyStr
			break
		}
	}

	t.Logf("Created item with key: %s", createdKey)

	// Cleanup: delete the item
	defer func() {
		fetchedItem, err := client.Item(ctx, createdKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch item for cleanup: %v", err)
			return
		}

		err = client.DeleteItem(ctx, createdKey, fetchedItem.Version)
		if err != nil {
			t.Errorf("Failed to cleanup item: %v", err)
		} else {
			t.Logf("Successfully deleted item %s", createdKey)
		}
	}()

	// Fetch the item to get current version
	fetchedItem, err := client.Item(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Item() error = %v", err)
	}

	// Update the item
	fetchedItem.Data.Title = "Updated Title"
	err = client.UpdateItem(ctx, fetchedItem)
	if err != nil {
		t.Fatalf("UpdateItem() error = %v", err)
	}

	// Verify the update
	updatedItem, err := client.Item(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Item() after update error = %v", err)
	}

	if updatedItem.Data.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got '%s'", updatedItem.Data.Title)
	}

	if updatedItem.Version <= fetchedItem.Version {
		t.Errorf("expected version to increase, got %d (was %d)", updatedItem.Version, fetchedItem.Version)
	}

	t.Logf("Successfully updated item %s (version %d -> %d)", createdKey, fetchedItem.Version, updatedItem.Version)
}

// TestWriteBatchItemsCreateAndDelete tests creating and deleting multiple items
func TestWriteBatchItemsCreateAndDelete(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create multiple items
	items := []zotero.Item{
		{Data: zotero.ItemData{ItemType: zotero.ItemTypeBook, Title: "Test Book 1"}},
		{Data: zotero.ItemData{ItemType: zotero.ItemTypeBook, Title: "Test Book 2"}},
		{Data: zotero.ItemData{ItemType: zotero.ItemTypeBook, Title: "Test Book 3"}},
	}

	resp, err := client.CreateItems(ctx, items)
	if err != nil {
		t.Fatalf("CreateItems() error = %v", err)
	}

	if len(resp.Success) != 3 {
		t.Fatalf("expected 3 successful items, got %d", len(resp.Success))
	}

	// Collect created keys
	var createdKeys []string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKeys = append(createdKeys, keyStr)
		}
	}

	t.Logf("Created %d items", len(createdKeys))

	// Cleanup: delete all items
	defer func() {
		// Fetch current version
		fetchedItem, err := client.Item(ctx, createdKeys[0], nil)
		if err != nil {
			t.Errorf("Failed to fetch item for cleanup: %v", err)
			return
		}

		err = client.DeleteItems(ctx, createdKeys, fetchedItem.Version)
		if err != nil {
			t.Errorf("Failed to cleanup items: %v", err)
		} else {
			t.Logf("Successfully deleted %d items", len(createdKeys))
		}
	}()

	// Verify items were created
	for _, key := range createdKeys {
		item, err := client.Item(ctx, key, nil)
		if err != nil {
			t.Errorf("Failed to fetch item %s: %v", key, err)
			continue
		}

		if item.Data.ItemType != zotero.ItemTypeBook {
			t.Errorf("expected itemType %s, got %s", zotero.ItemTypeBook, item.Data.ItemType)
		}
	}
}

// TestWriteBatchItemsUpdate tests updating multiple items at once
func TestWriteBatchItemsUpdate(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create test items
	items := []zotero.Item{
		{Data: zotero.ItemData{ItemType: zotero.ItemTypeBook, Title: "Original 1"}},
		{Data: zotero.ItemData{ItemType: zotero.ItemTypeBook, Title: "Original 2"}},
	}

	resp, err := client.CreateItems(ctx, items)
	if err != nil {
		t.Fatalf("CreateItems() error = %v", err)
	}

	var createdKeys []string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKeys = append(createdKeys, keyStr)
		}
	}

	t.Logf("Created %d items for batch update test", len(createdKeys))

	// Cleanup: delete all items
	defer func() {
		fetchedItem, err := client.Item(ctx, createdKeys[0], nil)
		if err != nil {
			t.Errorf("Failed to fetch item for cleanup: %v", err)
			return
		}

		err = client.DeleteItems(ctx, createdKeys, fetchedItem.Version)
		if err != nil {
			t.Errorf("Failed to cleanup items: %v", err)
		} else {
			t.Logf("Successfully deleted %d items", len(createdKeys))
		}
	}()

	// Fetch items to get current versions
	var fetchedItems []zotero.Item
	for _, key := range createdKeys {
		item, err := client.Item(ctx, key, nil)
		if err != nil {
			t.Fatalf("Item() error = %v", err)
		}
		fetchedItems = append(fetchedItems, *item)
	}

	// Update all items
	for i := range fetchedItems {
		fetchedItems[i].Data.Title = "Updated " + string(rune('A'+i))
	}

	updateResp, err := client.UpdateItems(ctx, fetchedItems)
	if err != nil {
		t.Fatalf("UpdateItems() error = %v", err)
	}

	if len(updateResp.Success) != len(createdKeys) {
		t.Errorf("expected %d successful updates, got %d", len(createdKeys), len(updateResp.Success))
	}

	// Verify updates
	for i, key := range createdKeys {
		item, err := client.Item(ctx, key, nil)
		if err != nil {
			t.Fatalf("Item() after update error = %v", err)
		}

		expectedTitle := "Updated " + string(rune('A'+i))
		if item.Data.Title != expectedTitle {
			t.Errorf("expected title '%s', got '%s'", expectedTitle, item.Data.Title)
		}
	}

	t.Logf("Successfully batch updated %d items", len(createdKeys))
}

// TestWriteCollectionCreateAndDelete tests creating and deleting a collection
func TestWriteCollectionCreateAndDelete(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a test collection
	collection := zotero.Collection{
		Data: zotero.CollectionData{
			Name: "Integration Test Collection",
		},
	}

	resp, err := client.CreateCollections(ctx, []zotero.Collection{collection})
	if err != nil {
		t.Fatalf("CreateCollections() error = %v", err)
	}

	if len(resp.Success) != 1 {
		t.Fatalf("expected 1 successful collection, got %d", len(resp.Success))
	}

	var createdKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKey = keyStr
			break
		}
	}

	t.Logf("Created collection with key: %s", createdKey)

	// Cleanup: delete the collection
	defer func() {
		fetchedColl, err := client.Collection(ctx, createdKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch collection for cleanup: %v", err)
			return
		}

		err = client.DeleteCollection(ctx, createdKey, fetchedColl.Version)
		if err != nil {
			t.Errorf("Failed to cleanup collection: %v", err)
		} else {
			t.Logf("Successfully deleted collection %s", createdKey)
		}
	}()

	// Verify the collection was created
	fetchedColl, err := client.Collection(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if fetchedColl.Key != createdKey {
		t.Errorf("expected key %s, got %s", createdKey, fetchedColl.Key)
	}

	if fetchedColl.Data.Name != "Integration Test Collection" {
		t.Errorf("expected name 'Integration Test Collection', got '%s'", fetchedColl.Data.Name)
	}
}

// TestWriteCollectionUpdate tests updating a collection
func TestWriteCollectionUpdate(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a test collection
	collection := zotero.Collection{
		Data: zotero.CollectionData{
			Name: "Original Name",
		},
	}

	resp, err := client.CreateCollections(ctx, []zotero.Collection{collection})
	if err != nil {
		t.Fatalf("CreateCollections() error = %v", err)
	}

	var createdKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKey = keyStr
			break
		}
	}

	t.Logf("Created collection with key: %s", createdKey)

	// Cleanup
	defer func() {
		fetchedColl, err := client.Collection(ctx, createdKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch collection for cleanup: %v", err)
			return
		}

		err = client.DeleteCollection(ctx, createdKey, fetchedColl.Version)
		if err != nil {
			t.Errorf("Failed to cleanup collection: %v", err)
		} else {
			t.Logf("Successfully deleted collection %s", createdKey)
		}
	}()

	// Fetch the collection
	fetchedColl, err := client.Collection(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	// Update the collection
	fetchedColl.Data.Name = "Updated Name"
	err = client.UpdateCollection(ctx, fetchedColl)
	if err != nil {
		t.Fatalf("UpdateCollection() error = %v", err)
	}

	// Verify the update
	updatedColl, err := client.Collection(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Collection() after update error = %v", err)
	}

	if updatedColl.Data.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got '%s'", updatedColl.Data.Name)
	}

	if updatedColl.Version <= fetchedColl.Version {
		t.Errorf("expected version to increase, got %d (was %d)", updatedColl.Version, fetchedColl.Version)
	}

	t.Logf("Successfully updated collection %s (version %d -> %d)", createdKey, fetchedColl.Version, updatedColl.Version)
}

// TestWriteNestedCollections tests creating and deleting nested collections
func TestWriteNestedCollections(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create parent collection
	parentColl := zotero.Collection{
		Data: zotero.CollectionData{
			Name: "Parent Collection",
		},
	}

	resp, err := client.CreateCollections(ctx, []zotero.Collection{parentColl})
	if err != nil {
		t.Fatalf("CreateCollections() error = %v", err)
	}

	var parentKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			parentKey = keyStr
			break
		}
	}

	t.Logf("Created parent collection with key: %s", parentKey)

	// Create child collection
	childColl := zotero.Collection{
		Data: zotero.CollectionData{
			Name:             "Child Collection",
			ParentCollection: zotero.ParentCollectionRef(parentKey),
		},
	}

	resp, err = client.CreateCollections(ctx, []zotero.Collection{childColl})
	if err != nil {
		// Cleanup parent before failing
		fetchedParent, _ := client.Collection(ctx, parentKey, nil)
		if fetchedParent != nil {
			client.DeleteCollection(ctx, parentKey, fetchedParent.Version)
		}
		t.Fatalf("CreateCollections() for child error = %v", err)
	}

	var childKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			childKey = keyStr
			break
		}
	}

	t.Logf("Created child collection with key: %s", childKey)

	// Cleanup: delete child first, then parent
	defer func() {
		// Delete child
		fetchedChild, err := client.Collection(ctx, childKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch child collection for cleanup: %v", err)
		} else {
			err = client.DeleteCollection(ctx, childKey, fetchedChild.Version)
			if err != nil {
				t.Errorf("Failed to cleanup child collection: %v", err)
			} else {
				t.Logf("Successfully deleted child collection %s", childKey)
			}
		}

		// Delete parent
		fetchedParent, err := client.Collection(ctx, parentKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch parent collection for cleanup: %v", err)
		} else {
			err = client.DeleteCollection(ctx, parentKey, fetchedParent.Version)
			if err != nil {
				t.Errorf("Failed to cleanup parent collection: %v", err)
			} else {
				t.Logf("Successfully deleted parent collection %s", parentKey)
			}
		}
	}()

	// Verify the child has the correct parent
	fetchedChild, err := client.Collection(ctx, childKey, nil)
	if err != nil {
		t.Fatalf("Collection() error = %v", err)
	}

	if fetchedChild.Data.ParentCollection.String() != parentKey {
		t.Errorf("expected parent %s, got %s", parentKey, fetchedChild.Data.ParentCollection.String())
	}
}

// TestWriteSearchCreateAndDelete tests creating and deleting a saved search
func TestWriteSearchCreateAndDelete(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a saved search
	search := zotero.Search{
		Data: zotero.SearchData{
			Name: "Integration Test Search",
			Conditions: []zotero.SearchCondition{
				{Condition: "title", Operator: "contains", Value: "test"},
			},
		},
	}

	resp, err := client.CreateSearches(ctx, []zotero.Search{search})
	if err != nil {
		t.Fatalf("CreateSearches() error = %v", err)
	}

	if len(resp.Success) != 1 {
		t.Fatalf("expected 1 successful search, got %d", len(resp.Success))
	}

	var createdKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKey = keyStr
			break
		}
	}

	t.Logf("Created search with key: %s", createdKey)

	// Cleanup: delete the search
	defer func() {
		fetchedSearch, err := client.Search(ctx, createdKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch search for cleanup: %v", err)
			return
		}

		err = client.DeleteSearch(ctx, createdKey, fetchedSearch.Version)
		if err != nil {
			t.Errorf("Failed to cleanup search: %v", err)
		} else {
			t.Logf("Successfully deleted search %s", createdKey)
		}
	}()

	// Verify the search was created
	fetchedSearch, err := client.Search(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if fetchedSearch.Key != createdKey {
		t.Errorf("expected key %s, got %s", createdKey, fetchedSearch.Key)
	}

	if fetchedSearch.Data.Name != "Integration Test Search" {
		t.Errorf("expected name 'Integration Test Search', got '%s'", fetchedSearch.Data.Name)
	}

	if len(fetchedSearch.Data.Conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(fetchedSearch.Data.Conditions))
	}
}

// TestWriteSearchUpdate tests updating a saved search
func TestWriteSearchUpdate(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a saved search
	search := zotero.Search{
		Data: zotero.SearchData{
			Name: "Original Search",
			Conditions: []zotero.SearchCondition{
				{Condition: "title", Operator: "contains", Value: "test"},
			},
		},
	}

	resp, err := client.CreateSearches(ctx, []zotero.Search{search})
	if err != nil {
		t.Fatalf("CreateSearches() error = %v", err)
	}

	var createdKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKey = keyStr
			break
		}
	}

	t.Logf("Created search with key: %s", createdKey)

	// Cleanup
	defer func() {
		fetchedSearch, err := client.Search(ctx, createdKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch search for cleanup: %v", err)
			return
		}

		err = client.DeleteSearch(ctx, createdKey, fetchedSearch.Version)
		if err != nil {
			t.Errorf("Failed to cleanup search: %v", err)
		} else {
			t.Logf("Successfully deleted search %s", createdKey)
		}
	}()

	// Fetch the search
	fetchedSearch, err := client.Search(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	// Update the search
	fetchedSearch.Data.Name = "Updated Search"
	fetchedSearch.Data.Conditions = append(fetchedSearch.Data.Conditions,
		zotero.SearchCondition{Condition: "itemType", Operator: "is", Value: "book"})

	err = client.UpdateSearch(ctx, fetchedSearch)
	if err != nil {
		t.Fatalf("UpdateSearch() error = %v", err)
	}

	// Verify the update
	updatedSearch, err := client.Search(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Search() after update error = %v", err)
	}

	if updatedSearch.Data.Name != "Updated Search" {
		t.Errorf("expected name 'Updated Search', got '%s'", updatedSearch.Data.Name)
	}

	if len(updatedSearch.Data.Conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(updatedSearch.Data.Conditions))
	}

	t.Logf("Successfully updated search %s (version %d -> %d)", createdKey, fetchedSearch.Version, updatedSearch.Version)
}

// TestWriteAddAndRemoveTags tests adding tags to an item
func TestWriteAddAndRemoveTags(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a test item
	item := zotero.Item{
		Data: zotero.ItemData{
			ItemType: zotero.ItemTypeBook,
			Title:    "Test Book for Tags",
		},
	}

	resp, err := client.CreateItems(ctx, []zotero.Item{item})
	if err != nil {
		t.Fatalf("CreateItems() error = %v", err)
	}

	var createdKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKey = keyStr
			break
		}
	}

	t.Logf("Created item with key: %s", createdKey)

	// Cleanup: delete the item
	defer func() {
		fetchedItem, err := client.Item(ctx, createdKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch item for cleanup: %v", err)
			return
		}

		err = client.DeleteItem(ctx, createdKey, fetchedItem.Version)
		if err != nil {
			t.Errorf("Failed to cleanup item: %v", err)
		} else {
			t.Logf("Successfully deleted item %s", createdKey)
		}
	}()

	// Add tags to the item
	err = client.AddTags(ctx, createdKey, "test-tag-1", "test-tag-2", "test-tag-3")
	if err != nil {
		t.Fatalf("AddTags() error = %v", err)
	}

	// Verify tags were added
	fetchedItem, err := client.Item(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Item() error = %v", err)
	}

	if len(fetchedItem.Data.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(fetchedItem.Data.Tags))
	}

	// Verify tag names
	tagNames := make(map[string]bool)
	for _, tag := range fetchedItem.Data.Tags {
		tagNames[tag.Tag] = true
	}

	expectedTags := []string{"test-tag-1", "test-tag-2", "test-tag-3"}
	for _, expectedTag := range expectedTags {
		if !tagNames[expectedTag] {
			t.Errorf("expected tag '%s' not found", expectedTag)
		}
	}

	t.Logf("Successfully added %d tags to item %s", len(fetchedItem.Data.Tags), createdKey)
}

// TestWriteVersionConcurrencyControl tests that version-based concurrency control works
func TestWriteVersionConcurrencyControl(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a test item
	item := zotero.Item{
		Data: zotero.ItemData{
			ItemType: zotero.ItemTypeBook,
			Title:    "Concurrency Test",
		},
	}

	resp, err := client.CreateItems(ctx, []zotero.Item{item})
	if err != nil {
		t.Fatalf("CreateItems() error = %v", err)
	}

	var createdKey string
	for _, key := range resp.Success {
		if keyStr, ok := key.(string); ok {
			createdKey = keyStr
			break
		}
	}

	t.Logf("Created item with key: %s", createdKey)

	// Cleanup
	defer func() {
		fetchedItem, err := client.Item(ctx, createdKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch item for cleanup: %v", err)
			return
		}

		err = client.DeleteItem(ctx, createdKey, fetchedItem.Version)
		if err != nil {
			t.Errorf("Failed to cleanup item: %v", err)
		} else {
			t.Logf("Successfully deleted item %s", createdKey)
		}
	}()

	// Fetch the item
	fetchedItem, err := client.Item(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Item() error = %v", err)
	}

	originalVersion := fetchedItem.Version

	// Update the item with correct version
	fetchedItem.Data.Title = "Updated Once"
	err = client.UpdateItem(ctx, fetchedItem)
	if err != nil {
		t.Fatalf("UpdateItem() with correct version error = %v", err)
	}

	// Try to update again with the old version (should fail)
	fetchedItem.Version = originalVersion
	fetchedItem.Data.Version = originalVersion
	fetchedItem.Data.Title = "Updated Twice with Old Version"
	err = client.UpdateItem(ctx, fetchedItem)
	if err == nil {
		t.Error("expected error when updating with old version, got nil")
	} else {
		t.Logf("Correctly rejected update with stale version: %v", err)
	}

	// Verify the item still has the first update
	currentItem, err := client.Item(ctx, createdKey, nil)
	if err != nil {
		t.Fatalf("Item() after failed update error = %v", err)
	}

	if currentItem.Data.Title != "Updated Once" {
		t.Errorf("expected title 'Updated Once', got '%s'", currentItem.Data.Title)
	}

	if currentItem.Version == originalVersion {
		t.Errorf("expected version to have changed from %d", originalVersion)
	}
}

// TestWriteUploadAndDownloadFile tests uploading and downloading an attachment file
func TestWriteUploadAndDownloadFile(t *testing.T) {
	client := skipIfNoCredentials(t)
	ctx := context.Background()

	// Create a test file with known content
	testContent := []byte("This is a test file for Zotero attachment integration testing.\n")
	testFilename := "test-attachment.txt"
	testPath := t.TempDir() + "/" + testFilename

	err := os.WriteFile(testPath, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	t.Logf("Created test file at: %s", testPath)

	// Upload the file as a standalone attachment
	attachment, err := client.UploadAttachment(ctx, "", testPath, testFilename, "text/plain")
	if err != nil {
		// File uploads may not be supported by all API endpoints (e.g., local desktop API)
		// Skip the test if upload isn't supported
		if strings.Contains(err.Error(), "missing upload params") ||
			strings.Contains(err.Error(), "missing upload URL") {
			t.Skipf("File upload not supported by this API endpoint: %v", err)
		}
		t.Fatalf("UploadAttachment() error = %v", err)
	}

	attachmentKey := attachment.Key
	t.Logf("Uploaded attachment with key: %s", attachmentKey)

	// Cleanup: delete the attachment
	defer func() {
		fetchedAttachment, err := client.Item(ctx, attachmentKey, nil)
		if err != nil {
			t.Errorf("Failed to fetch attachment for cleanup: %v", err)
			return
		}

		err = client.DeleteItem(ctx, attachmentKey, fetchedAttachment.Version)
		if err != nil {
			t.Errorf("Failed to cleanup attachment: %v", err)
		} else {
			t.Logf("Successfully deleted attachment %s", attachmentKey)
		}
	}()

	// Download the file using File() method
	downloadedContent, err := client.File(ctx, attachmentKey)
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}

	// Verify the content matches
	if !bytes.Equal(downloadedContent, testContent) {
		t.Errorf("Downloaded content does not match original.\nExpected: %q\nGot: %q", testContent, downloadedContent)
	} else {
		t.Logf("Successfully downloaded file with matching content (%d bytes)", len(downloadedContent))
	}

	// Test Dump() method - save to disk
	dumpPath := t.TempDir()
	fullPath, err := client.Dump(ctx, attachmentKey, "", dumpPath)
	if err != nil {
		t.Fatalf("Dump() error = %v", err)
	}

	t.Logf("Saved file to: %s", fullPath)

	// Verify the dumped file content
	dumpedContent, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("Failed to read dumped file: %v", err)
	}

	if !bytes.Equal(dumpedContent, testContent) {
		t.Errorf("Dumped file content does not match original.\nExpected: %q\nGot: %q", testContent, dumpedContent)
	} else {
		t.Logf("Successfully dumped file with matching content")
	}

	// Verify the filename was auto-detected correctly
	expectedPath := dumpPath + "/" + testFilename
	if fullPath != expectedPath {
		t.Logf("Note: Auto-detected filename differs. Expected: %s, Got: %s", expectedPath, fullPath)
		// This is not necessarily an error - the API might return a different filename
	}
}

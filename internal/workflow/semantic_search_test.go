package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type semanticResolver struct {
	collections []CollectionInfo
	items       map[string][]Record
}

func (r semanticResolver) Search(context.Context, string, []string) ([]Record, error) {
	return nil, nil
}
func (r semanticResolver) Collections(context.Context) ([]CollectionInfo, error) {
	return r.collections, nil
}
func (r semanticResolver) CollectionItems(_ context.Context, key string) ([]Record, error) {
	return r.items[key], nil
}
func (r semanticResolver) PDFStatus(context.Context, string, PDFMode) ([]PDFStatus, error) {
	return nil, nil
}

func TestParseSemanticQuery(t *testing.T) {
	topic, collection, natural, err := ParseSemanticQuery("search india burden articles in glaucoam collection")
	if err != nil || topic != "india burden articles" || collection != "glaucoam" || !natural {
		t.Fatalf("parsed = %q %q %v err=%v", topic, collection, natural, err)
	}
	topic, collection, natural, err = ParseSemanticQuery("india burden")
	if err != nil || topic != "india burden" || collection != "" || natural {
		t.Fatalf("plain = %q %q %v", topic, collection, natural)
	}
}

func TestResolveSemanticCollectionTypoAndAmbiguity(t *testing.T) {
	collections := []CollectionInfo{{Key: "G", Name: "Glaucoma"}, {Key: "I", Name: "India", Parent: "G"}}
	got, err := resolveCollectionQuery("glaucoam", collections)
	if err != nil || len(got) != 1 || got[0].Key != "G" {
		t.Fatalf("typo = %#v err=%v", got, err)
	}
	ambiguous := append(collections, CollectionInfo{Key: "G2", Name: "Glaucoma"})
	if _, err := resolveCollectionQuery("glaucoma", ambiguous); err == nil {
		t.Fatal("expected ambiguous typo")
	}
}

func TestSemanticSearchFiltersOutOfScopeAndReportsLimit(t *testing.T) {
	resolver := semanticResolver{
		collections: []CollectionInfo{{Key: "G", Name: "Glaucoma"}, {Key: "I", Name: "India", Parent: "G"}},
		items:       map[string][]Record{"G": {{Key: "IN", Library: "user:1", Title: "In scope"}}, "I": {{Key: "IN2", Library: "user:1", Title: "India scope"}}},
	}
	invoke := func(context.Context, string, map[string]any) (json.RawMessage, error) {
		return json.RawMessage(`{"content":[{"type":"text","text":"{\"results\":[{\"itemKey\":\"IN\",\"libraryKey\":\"user\",\"title\":\"In scope\"},{\"itemKey\":\"OUT\",\"libraryKey\":\"user\",\"title\":\"Out of scope\"}]}"}]}`), nil
	}
	report, err := SemanticSearch(context.Background(), resolver, invoke, "india burden in glaucoma collection", SemanticSearchOptions{MaxResults: 2, Mode: "hybrid", Granularity: "papers"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 1 || report.Results[0].ItemKey != "IN" || report.ExcludedOutOfScope != 1 {
		t.Fatalf("report = %#v", report)
	}
	if report.Completeness != "not exhaustive" || !report.PotentiallyTruncated {
		t.Fatalf("limits = %#v", report)
	}
	if !strings.Contains(report.LimitNote, "no pagination") {
		t.Errorf("limit note = %q", report.LimitNote)
	}
}

func TestSemanticSearchRejectsBadInputs(t *testing.T) {
	resolver := semanticResolver{}
	invoke := func(context.Context, string, map[string]any) (json.RawMessage, error) { return nil, nil }
	if _, err := SemanticSearch(context.Background(), resolver, invoke, "topic", SemanticSearchOptions{MaxResults: 101, Mode: "hybrid", Granularity: "papers"}); err == nil {
		t.Fatal("expected max error")
	}
	if _, err := SemanticSearch(context.Background(), resolver, invoke, "topic in missing collection", SemanticSearchOptions{MaxResults: 10, Mode: "hybrid", Granularity: "papers"}); err == nil {
		t.Fatal("expected collection error")
	}
	if _, err := parseSemanticResults(json.RawMessage(`{"content":[{"type":"text","text":"{}"}]}`)); err == nil {
		t.Fatal("expected missing results array error")
	}
}

func TestSemanticScopeSafetyAndQueryContext(t *testing.T) {
	r := semanticResolver{collections: []CollectionInfo{{Key: "G", Name: "3 Glaucoma"}, {Key: "I", Name: "India", Parent: "G"}}, items: map[string][]Record{"I": {{Key: "IN", Library: "user:1"}}}}
	invoke := func(_ context.Context, _ string, args map[string]any) (json.RawMessage, error) {
		if !strings.Contains(args["query"].(string), "glaucoma") {
			t.Errorf("collection topic lost: %v", args)
		}
		return json.RawMessage(`{"structuredContent":{"results":[{"itemKey":"IN","libraryKey":"user"},{"itemKey":"IN","libraryKey":"group:1"},{"itemKey":"IN"}]}}`), nil
	}
	opts := SemanticSearchOptions{CollectionPath: "3 Glaucoma", MaxResults: 100, Mode: "hybrid", Granularity: "papers"}
	got, err := SemanticSearch(context.Background(), r, invoke, "India burden", opts)
	if err != nil || len(got.Results) != 1 || got.ExcludedOutOfScope != 2 {
		t.Fatalf("scope=%+v err=%v", got, err)
	}
	opts.IncludeSubcollectionsSet = true
	opts.IncludeSubcollections = false
	got, err = SemanticSearch(context.Background(), r, invoke, "India burden", opts)
	if err != nil || len(got.Results) != 0 {
		t.Fatalf("empty scope leaked: %+v err=%v", got, err)
	}
}

func TestSemanticRejectsMissingLibraryIdentity(t *testing.T) {
	for _, pair := range [][2]string{{"", "user"}, {"user:1", ""}, {"group:1", "group"}, {"group:12", "group:1"}} {
		if sameLibraryKey(pair[0], pair[1]) {
			t.Fatalf("accepted unverifiable identity %v", pair)
		}
	}
}

func TestTypoMustNotSilentlyChoosePartialCollection(t *testing.T) {
	cs := []CollectionInfo{{Key: "G", Name: "3 Glaucoma"}, {Key: "B", Name: "Glaucoam Blindness", Parent: "G"}}
	if _, err := resolveCollectionQuery("glaucoam", cs); err == nil {
		t.Fatal("typo silently selected narrower partial name")
	}
	got, err := resolveCollectionQuery("3 Glaucoma", cs)
	if err != nil || len(got) != 1 || got[0].Key != "G" {
		t.Fatalf("explicit scope failed %v %v", got, err)
	}
}

func TestExplicitBreadcrumbDisambiguatesRepeatedLeaf(t *testing.T) {
	cs := []CollectionInfo{{Key: "G", Name: "3 Glaucoma"}, {Key: "P", Name: "PROs", Parent: "G"}, {Key: "N", Name: "Glaucoma", Parent: "P"}}
	if _, err := resolveCollectionQuery("glaucoma", cs); err == nil {
		t.Fatal("bare name should not silently choose nested exact leaf")
	}
	got, err := resolveCollectionQuery("3 Glaucoma > PROs > Glaucoma", cs)
	if err != nil || len(got) != 1 || got[0].Key != "N" {
		t.Fatalf("full breadcrumb failed %v %v", got, err)
	}
}

func TestBareRootNameCannotHideDuplicateLeaf(t *testing.T) {
	cs := []CollectionInfo{{Key: "G", Name: "Glaucoma"}, {Key: "N", Name: "Glaucoma", Parent: "G"}}
	if _, err := resolveCollectionQuery("Glaucoma", cs); err == nil {
		t.Fatal("bare root silently hides duplicate leaf")
	}
}

func TestRequestedSemanticLibraryIsEnforced(t *testing.T) {
	r := semanticResolver{collections: []CollectionInfo{{Key: "G", Name: "3 Glaucoma"}}, items: map[string][]Record{"G": {{Key: "K", Library: "user:1"}}}}
	invoke := func(context.Context, string, map[string]any) (json.RawMessage, error) {
		return json.RawMessage(`{"structuredContent":{"results":[{"itemKey":"K","libraryKey":"user"}]}}`), nil
	}
	opts := SemanticSearchOptions{CollectionPath: "3 Glaucoma", MaxResults: 100, LibraryKey: "user:999"}
	if _, err := SemanticSearch(context.Background(), r, invoke, "burden", opts); err == nil {
		t.Fatal("invalid explicit personal ID accepted")
	}
	opts.LibraryKey = "group:999"
	got, err := SemanticSearch(context.Background(), r, invoke, "burden", opts)
	if err != nil || len(got.Results) != 0 {
		t.Fatalf("requested group mismatch passed: %+v %v", got, err)
	}
	opts.LibraryKey = "user"
	got, err = SemanticSearch(context.Background(), r, invoke, "burden", opts)
	if err != nil || len(got.Results) != 1 {
		t.Fatalf("valid user alias failed: %+v %v", got, err)
	}
}

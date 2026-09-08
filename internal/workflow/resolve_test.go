package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeResolver struct {
	searches    []string
	results     map[string][]Record
	collections []CollectionInfo
	pdfErr      error
}

type pdfByKeyResolver struct {
	results map[string][]Record
	pdf     map[string][]PDFStatus
}

func (r pdfByKeyResolver) Search(context.Context, string, []string) ([]Record, error) {
	return r.results["title"], nil
}
func (r pdfByKeyResolver) Collections(context.Context) ([]CollectionInfo, error)     { return nil, nil }
func (r pdfByKeyResolver) CollectionItems(context.Context, string) ([]Record, error) { return nil, nil }
func (r pdfByKeyResolver) PDFStatus(_ context.Context, key string, _ PDFMode) ([]PDFStatus, error) {
	return r.pdf[key], nil
}

func (f *fakeResolver) Search(_ context.Context, query string, _ []string) ([]Record, error) {
	f.searches = append(f.searches, query)
	if stringsHas(query, "error") {
		return nil, errors.New("search failed")
	}
	return append([]Record(nil), f.results[query]...), nil
}
func (f *fakeResolver) Collections(context.Context) ([]CollectionInfo, error) {
	return f.collections, nil
}
func (f *fakeResolver) CollectionItems(context.Context, string) ([]Record, error) { return nil, nil }
func (f *fakeResolver) PDFStatus(context.Context, string, PDFMode) ([]PDFStatus, error) {
	if f.pdfErr != nil {
		return nil, f.pdfErr
	}
	return []PDFStatus{{Status: "present", Present: true, Verified: true}}, nil
}
func stringsHas(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestResolveReferencesExactAndConflictAndCache(t *testing.T) {
	r := &fakeResolver{results: map[string][]Record{
		"Known Title":    {{Key: "A", Title: "Known Title", DOI: "10.1/x", Year: 2024, Authors: []string{"Doe J"}}},
		"Conflict Title": {{Key: "B", Title: "Conflict Title", DOI: "10.1/other", Year: 2024}},
	}}
	citations := []Citation{{Number: 1, Title: "Known Title", DOI: "10.1/x"}, {Number: 2, Title: "Conflict Title", DOI: "10.1/wrong"}, {Number: 3, Title: "Known Title", DOI: "10.1/x"}}
	report := ResolveReferences(context.Background(), r, citations, ResolveOptions{})
	if report.References[0].Status != "matched" || report.References[0].MatchBasis != "doi" {
		t.Errorf("first = %#v", report.References[0])
	}
	if report.References[1].Status != "conflict" {
		t.Errorf("conflict = %#v", report.References[1])
	}
	if len(r.searches) != 2 {
		t.Errorf("searches = %v, want 2 due to cache", r.searches)
	}
}

func TestResolveCollectionPathNeverEscapesBranch(t *testing.T) {
	collections := []CollectionInfo{{Key: "A", Name: "3 Glaucoma", Parent: ""}, {Key: "B", Name: "Burden", Parent: "A"}, {Key: "C", Name: "India", Parent: "B"}, {Key: "X", Name: "India", Parent: ""}}
	c, err := ResolveCollectionPath("Glaucoma > burden > India", collections)
	if err != nil || c.Key != "C" {
		t.Fatalf("collection = %#v, err=%v", c, err)
	}
	if _, err := ResolveCollectionPath("Burden > India", collections); err == nil {
		t.Fatal("expected missing top-level path")
	}
}

func TestResolveReferencesAmbiguousAndPDFFailureAreExplicit(t *testing.T) {
	r := &fakeResolver{results: map[string][]Record{
		"Duplicate": {{Key: "A", Title: "Duplicate", Year: 2024}, {Key: "B", Title: "Duplicate", Year: 2024}},
		"PDF paper": {{Key: "P", Title: "PDF paper", Year: 2024}},
	}, pdfErr: errors.New("404 attachment")}
	report := ResolveReferences(context.Background(), r, []Citation{{Number: 4, Title: "Duplicate", Year: 2024}, {Number: 5, Title: "PDF paper", Year: 2024}}, ResolveOptions{PDFMode: PDFVerify})
	if report.References[0].Status != "ambiguous" || len(report.References[0].Candidates) != 2 {
		t.Fatalf("ambiguous = %#v", report.References[0])
	}
	if report.References[1].Status != "matched" || report.References[1].Error != "404 attachment" {
		t.Fatalf("pdf failure = %#v", report.References[1])
	}
}

func TestDescendantCollectionKeys(t *testing.T) {
	collections := []CollectionInfo{{Key: "A", Parent: ""}, {Key: "B", Parent: "A"}, {Key: "C", Parent: "B"}, {Key: "X", Parent: ""}}
	got := DescendantCollectionKeys("A", collections)
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Fatalf("keys = %#v", got)
	}
}

func TestFindCollectionsPartialBreadcrumbs(t *testing.T) {
	collections := []CollectionInfo{
		{Key: "G", Name: "3 Glaucoma"}, {Key: "B", Name: "1.Burden", Parent: "G"}, {Key: "I1", Name: "India", Parent: "B"},
		{Key: "O", Name: "Other"}, {Key: "I2", Name: "India", Parent: "O"},
	}
	got, err := FindCollections("glaucoma > burden > india", collections)
	if err != nil || len(got) != 1 || got[0].Key != "I1" || got[0].Path != "3 Glaucoma > 1.Burden > India" {
		t.Fatalf("matches = %#v, err=%v", got, err)
	}
	single, err := FindCollections("India", collections)
	if err != nil || len(single) != 2 || single[0].Key != "I1" || single[1].Key != "I2" {
		t.Fatalf("single matches = %#v, err=%v", single, err)
	}
}

func TestFindCollectionsRejectsCyclesAndOrphans(t *testing.T) {
	if _, err := FindCollections("x", []CollectionInfo{{Key: "A", Name: "A", Parent: "B"}, {Key: "B", Name: "B", Parent: "A"}}); err == nil {
		t.Fatal("expected cycle error")
	}
	if _, err := FindCollections("x", []CollectionInfo{{Key: "A", Name: "A", Parent: "missing"}}); err == nil {
		t.Fatal("expected orphan error")
	}
}

func TestIdentityRequiresEvidenceAndHandlesAuthorForms(t *testing.T) {
	if got, _, status := identify(Citation{Title: "Same"}, []Record{{Key: "A", Title: "Same"}}); got != nil || status != "unresolved" {
		t.Fatalf("no evidence = %#v %s", got, status)
	}
	if got, _, status := identify(Citation{Title: "Same", Authors: []string{"Gupta V"}}, []Record{{Key: "A", Title: "Same", Authors: []string{"Gupta Vivek"}}}); got == nil || status != "matched" {
		t.Fatalf("author evidence = %#v %s", got, status)
	}
	if got, _, status := identify(Citation{Title: "Same", Authors: []string{"Gupta V"}, Year: 2021}, []Record{{Key: "B", Title: "Same", Authors: []string{"Sharma R"}, Year: 2021}}); got != nil || status != "unresolved" {
		t.Fatalf("author mismatch = %#v %s", got, status)
	}
}

func TestParentRecordFilter(t *testing.T) {
	for _, itemType := range []string{"attachment", "note", "annotation"} {
		if isParentRecord(Record{ItemType: itemType}) {
			t.Errorf("%s should be filtered", itemType)
		}
	}
	if !isParentRecord(Record{ItemType: "journalArticle"}) {
		t.Error("journal article should remain")
	}
}

func TestPDFFallbackOnlyUsesEquivalentDuplicate(t *testing.T) {
	resolver := pdfByKeyResolver{
		results: map[string][]Record{"title": {
			{Key: "PRIMARY", Library: "user:1", Title: "Same title", Authors: []string{"Gupta V"}, Year: 2024},
			{Key: "UNRELATED", Library: "user:1", Title: "Different title", Authors: []string{"Other A"}, Year: 2024},
			{Key: "DUPLICATE", Library: "user:1", Title: "Same title"},
		}},
		pdf: map[string][]PDFStatus{
			"PRIMARY":   {{ParentKey: "PRIMARY", Status: "absent"}},
			"UNRELATED": {{ParentKey: "UNRELATED", Status: "present", Verified: true}},
			"DUPLICATE": {{ParentKey: "DUPLICATE", Status: "present", Verified: true}},
		},
	}
	report := ResolveReferences(context.Background(), resolver, []Citation{{Number: 1, Title: "Same title", Authors: []string{"Gupta V"}, Year: 2024}}, ResolveOptions{PDFMode: PDFVerify})
	if len(report.References) != 1 || report.References[0].MatchKey != "PRIMARY" {
		t.Fatalf("match = %#v", report.References)
	}
	if len(report.References[0].PDF) != 1 || report.References[0].PDF[0].ParentKey != "PRIMARY" {
		t.Fatalf("unrelated pdf replaced match: %#v", report.References[0].PDF)
	}
	if !sameCitationIdentity(Citation{Title: "Same title", Authors: []string{"Gupta V"}, Year: 2024}, Record{Title: "Same title", Authors: []string{"Gupta Vivek"}, Year: 2024}) {
		t.Fatal("equivalent duplicate identity rejected")
	}
}

func TestOutputSafetyAndAtomicEmit(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "citations.txt")
	output := filepath.Join(dir, "report.json")
	if err := os.WriteFile(input, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if samePath(input, output) {
		t.Fatal("different paths considered equal")
	}
	if !samePath(input, input) {
		t.Fatal("same path not detected")
	}
	if err := emit(output, map[string]string{"ok": "yes"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || len(data) == 0 {
		t.Fatalf("report output missing: %v", err)
	}
}

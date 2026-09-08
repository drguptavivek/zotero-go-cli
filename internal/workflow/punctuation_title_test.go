package workflow

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeTitlePunctuationVariants(t *testing.T) {
	variants := []string{
		"AI-based screening—India",
		"AI–based screening‑India",
		"AI based screening India",
		"AI ‘based’ screening: India?",
		"AI-based screening: India",
		"AI based screening India",
	}
	want := NormalizeTitle(variants[0])
	for _, variant := range variants[1:] {
		if got := NormalizeTitle(variant); got != want {
			t.Errorf("NormalizeTitle(%q) = %q, want %q", variant, got, want)
		}
	}
	if got := NormalizeTitle("Patient's outcomes"); got != "patients outcomes" {
		t.Errorf("apostrophe normalization = %q", got)
	}
}

func TestParseTitlePreservesSourceAndIdentityFields(t *testing.T) {
	input := "1. Gupta V. AI-based screening—India? Ophthalmology Today. 2024;10(2):1-4. doi:10.1234/AI-India."
	citations, err := ParseInput(strings.NewReader(input))
	if err != nil || len(citations) != 1 {
		t.Fatalf("citations = %#v err=%v", citations, err)
	}
	got := citations[0]
	if got.Title != "AI-based screening—India?" {
		t.Errorf("title = %q", got.Title)
	}
	if got.DOI != "10.1234/AI-India" || len(got.Authors) != 1 || got.Authors[0] != "Gupta V" {
		t.Errorf("identity = %#v", got)
	}
	if got.Raw == "" || got.Raw == got.Title {
		t.Errorf("raw citation was not preserved: %q", got.Raw)
	}
}

func TestPublishedNoDOIReferenceTitlesParseWithAuthorYearEvidence(t *testing.T) {
	input := `1. Broor S, et al. Demographic Shift of Influenza A(H1N1)pdm09 during and after Pandemic, Rural India. Emerg Infect Dis. 2012;18(9):1-2.
2. Yadav K, et al. Revitalizing Rural Health Care Delivery: Can Rural Health Practitioners be the Answer? Indian J Community Med. 2009;34(2):1-3.`
	citations, err := ParseInput(strings.NewReader(input))
	if err != nil || len(citations) != 2 {
		t.Fatalf("citations = %#v err=%v", citations, err)
	}
	if citations[0].Title != "Demographic Shift of Influenza A(H1N1)pdm09 during and after Pandemic, Rural India" || citations[0].DOI != "" || citations[0].Year != 2012 {
		t.Errorf("first = %#v", citations[0])
	}
	if citations[1].Title != "Revitalizing Rural Health Care Delivery: Can Rural Health Practitioners be the Answer?" || citations[1].DOI != "" || citations[1].Year != 2009 {
		t.Errorf("second = %#v", citations[1])
	}
}

type punctuationRawClient struct {
	calls []string
}

func (f *punctuationRawClient) All(_ context.Context, _ string, query url.Values) ([]map[string]any, error) {
	f.calls = append(f.calls, query.Get("q"))
	if query.Get("q") != NormalizeTitle("AI-based screening—India?") {
		return []map[string]any{}, nil
	}
	return []map[string]any{{
		"key":     "PUNCT123",
		"library": map[string]any{"type": "user", "id": 123},
		"data":    map[string]any{"itemType": "journalArticle", "title": "AI-based screening—India?", "date": "2024"},
	}}, nil
}

func (f *punctuationRawClient) Download(context.Context, string, string) error { return nil }

func TestSearchStripsPunctuationBeforeFirstRequest(t *testing.T) {
	fake := &punctuationRawClient{}
	resolver := &clientResolver{client: fake, scoped: make(map[string][]Record)}
	got, err := resolver.Search(context.Background(), "AI-based screening—India?", nil)
	if err != nil || len(got) != 1 || got[0].Key != "PUNCT123" {
		t.Fatalf("records = %#v err=%v", got, err)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "ai based screening india" {
		t.Errorf("search calls = %#v", fake.calls)
	}
}

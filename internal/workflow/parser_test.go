package workflow

import (
	"strings"
	"testing"
)

func TestParseVancouverMultilineAndAbbreviation(t *testing.T) {
	input := `1. Gupta V, Vashist P, Patil A, et al. Glaucoma burden in India: a review. Indian Journal of Ophthalmology. 2024;72(1):1-5. doi:10.1234/example.
2) Doe J. Direct anterior vs. posterior approach in hip arthroplasty: a systematic review. Journal of Tests. 2025;4:20.
   continuation on another line.`
	got, err := ParseInput(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
		t.Fatalf("entries = %#v", got)
	}
	if got[0].Title != "Glaucoma burden in India: a review" {
		t.Errorf("title 1 = %q", got[0].Title)
	}
	if got[1].Title != "Direct anterior vs. posterior approach in hip arthroplasty: a systematic review" {
		t.Errorf("title 2 = %q", got[1].Title)
	}
	if got[0].DOI != "10.1234/example" {
		t.Errorf("doi = %q", got[0].DOI)
	}
}

func TestParseJSONPreservesDuplicates(t *testing.T) {
	got, err := ParseInput(strings.NewReader(`[{"number":7,"title":"One","doi":"https://doi.org/10.1234/a"},{"title":"One"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Number != 7 || got[1].Number != 2 {
		t.Fatalf("entries = %#v", got)
	}
	if got[0].DOI != "10.1234/a" {
		t.Errorf("doi = %q", got[0].DOI)
	}
}

func TestNormalizeTitle(t *testing.T) {
	if got := NormalizeTitle("Éffective refractive-error: coverage!"); got != "éffective refractive error coverage" {
		t.Errorf("normalized = %q", got)
	}
}

func TestParseAuthorListWithSingleNameAuthor(t *testing.T) {
	got, err := ParseInput(strings.NewReader("109. Krishnan A, Gupta V, Ritvik, Nongkynrih B, Thakur J. How to Effectively Monitor and Evaluate NCD Programmes in India. Indian J Community Med. 2011;36(Suppl 1):S57-62."))
	if err != nil || len(got) != 1 || got[0].Title != "How to Effectively Monitor and Evaluate NCD Programmes in India" {
		t.Fatalf("parsed = %#v err=%v", got, err)
	}
}

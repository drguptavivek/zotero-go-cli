package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPunctuationNeverDropsNaturalCollectionScope(t *testing.T) {
	for _, suffix := range []string{"?", "!", ".", "?!", "？", "。"} {
		topic, path, natural, err := ParseSemanticQuery("search for India—burden articles in “3 Glaucoma” collection" + suffix)
		if err != nil || !natural || path != "3 glaucoma" || !strings.Contains(topic, "India—burden") {
			t.Fatalf("suffix %q lost scope: %q %q %v %v", suffix, topic, path, natural, err)
		}
	}
}

func TestCollectionPunctuationVariants(t *testing.T) {
	cs := []CollectionInfo{{Key: "G", Name: "3 Glaucoma"}, {Key: "B", Name: "1.Burden—India?", Parent: "G"}}
	for _, name := range []string{"Burden-India", "Burden–India?", "Burden‑India", "Burden India", "“Burden—India?”"} {
		got, err := resolveCollectionQuery("3 Glaucoma > "+name, cs)
		if err != nil || len(got) != 1 || got[0].Key != "B" {
			t.Fatalf("%q: %v %v", name, got, err)
		}
	}
}

func TestCollectionNumberingWithUnicodeDashes(t *testing.T) {
	for _, name := range []string{"1–Burden", "1—Burden", "1‑Burden", "1. Burden"} {
		if got := normalizeCollectionName(name); got != "burden" {
			t.Fatalf("%q => %q", name, got)
		}
	}
}

func TestSemanticSearchSendsPunctuationFreeTopic(t *testing.T) {
	invoke := func(_ context.Context, _ string, args map[string]any) (json.RawMessage, error) {
		if args["query"] != "rural health care can practitioners be the answer" {
			t.Fatalf("query=%q", args["query"])
		}
		return json.RawMessage(`{"structuredContent":{"results":[]}}`), nil
	}
	_, err := SemanticSearch(context.Background(), semanticResolver{}, invoke, "Rural—health care: can practitioners be the answer?", SemanticSearchOptions{MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
}

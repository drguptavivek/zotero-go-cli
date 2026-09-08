package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Epistemic-Technology/zotero/internal/semantic"
	"github.com/Epistemic-Technology/zotero/zotero"
	"github.com/spf13/cobra"
)

type SemanticResult struct {
	ItemKey       string         `json:"itemKey"`
	LibraryKey    string         `json:"libraryKey,omitempty"`
	Title         string         `json:"title,omitempty"`
	Authors       string         `json:"authors,omitempty"`
	Year          int            `json:"year,omitempty"`
	Score         float64        `json:"score,omitempty"`
	SemanticScore float64        `json:"semanticScore,omitempty"`
	KeywordScore  float64        `json:"keywordScore,omitempty"`
	Source        string         `json:"source,omitempty"`
	MatchedChunk  map[string]any `json:"matchedChunk,omitempty"`
	Links         map[string]any `json:"links,omitempty"`
}

type SemanticReport struct {
	Query                string           `json:"query"`
	CollectionQuery      string           `json:"collectionQuery,omitempty"`
	CollectionPath       string           `json:"collectionPath,omitempty"`
	CollectionKeys       []string         `json:"collectionKeys,omitempty"`
	Results              []SemanticResult `json:"results"`
	RequestedMax         int              `json:"requestedMax"`
	RetrievedCandidates  int              `json:"retrievedCandidates"`
	ScopeFiltered        int              `json:"scopeFiltered,omitempty"`
	ExcludedOutOfScope   int              `json:"excludedOutOfScope,omitempty"`
	PotentiallyTruncated bool             `json:"potentiallyTruncated"`
	Completeness         string           `json:"completeness"`
	LimitNote            string           `json:"limitNote"`
}

type SemanticSearchOptions struct {
	CollectionPath           string
	MaxResults               int
	Mode                     string
	MinSimilarity            *float64
	Granularity              string
	LibraryKey               string
	IncludeSubcollections    bool
	IncludeSubcollectionsSet bool
}

type semanticInvoker func(context.Context, string, map[string]any) (json.RawMessage, error)

var naturalCollectionQuery = regexp.MustCompile(`(?is)^\s*(.*?)\s+in\s+(.+?)\s+collection\s*[?!.？！。]*\s*$`)

// ParseSemanticQuery recognizes the deliberately small natural-language form
// "<topic> in <collection> collection". Explicit --collection-path takes
// precedence at the command layer.
func ParseSemanticQuery(input string) (topic, collection string, natural bool, err error) {
	input = unquoteSearchText(strings.TrimSpace(input))
	if input == "" {
		return "", "", false, errors.New("semantic query is empty")
	}
	match := naturalCollectionQuery.FindStringSubmatch(input)
	if match == nil {
		return input, "", false, nil
	}
	topic, collection = strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
	topic = stripSearchPrefix(topic)
	collection = unquoteSearchText(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(collection), "the ")))
	if topic == "" || collection == "" {
		return "", "", false, errors.New("semantic query needs a topic and collection")
	}
	return topic, collection, true, nil
}

// Unwrap only paired quotation marks, preserving apostrophes within names.
func unquoteSearchText(s string) string {
	for _, pair := range [][2]string{{"\"", "\""}, {"'", "'"}, {"“", "”"}, {"‘", "’"}} {
		if strings.HasPrefix(s, pair[0]) && strings.HasSuffix(s, pair[1]) && len(s) >= len(pair[0])+len(pair[1]) {
			return strings.TrimSpace(s[len(pair[0]) : len(s)-len(pair[1])])
		}
	}
	return s
}

func stripSearchPrefix(topic string) string {
	lower := strings.ToLower(topic)
	for _, prefix := range []string{"search for ", "search ", "find "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(topic[len(prefix):])
		}
	}
	return topic
}

// SemanticSearch calls ZOTseek and, when scoped, verifies every returned item
// against a locally fetched collection snapshot before returning it.
func SemanticSearch(ctx context.Context, resolver Resolver, invoke semanticInvoker, input string, opts SemanticSearchOptions) (SemanticReport, error) {
	topic, naturalCollection, natural, err := ParseSemanticQuery(input)
	if err != nil {
		return SemanticReport{}, err
	}
	topic = NormalizeTitle(topic)
	if topic == "" {
		return SemanticReport{}, errors.New("semantic query has no searchable letters or numbers")
	}
	collectionQuery := strings.TrimSpace(opts.CollectionPath)
	if collectionQuery == "" && natural {
		collectionQuery = naturalCollection
	}
	if opts.MaxResults <= 0 || opts.MaxResults > 100 {
		return SemanticReport{}, fmt.Errorf("max results must be between 1 and 100")
	}
	if opts.LibraryKey != "" && opts.LibraryKey != "user" && !regexp.MustCompile(`^group:[1-9][0-9]*$`).MatchString(opts.LibraryKey) {
		return SemanticReport{}, errors.New("library key must be user or group:GROUP_ID")
	}
	args := map[string]any{"query": topic, "max_results": opts.MaxResults, "mode": opts.Mode, "granularity": opts.Granularity}
	if opts.MinSimilarity != nil {
		args["min_similarity"] = *opts.MinSimilarity
	}
	if opts.LibraryKey != "" {
		args["library_key"] = opts.LibraryKey
	}

	var scope []Record
	var collectionKeys []string
	var collectionPath string
	if collectionQuery != "" {
		collections, err := resolver.Collections(ctx)
		if err != nil {
			return SemanticReport{}, err
		}
		matches, err := resolveCollectionQuery(collectionQuery, collections)
		if err != nil {
			return SemanticReport{}, err
		}
		if len(matches) != 1 {
			return SemanticReport{}, fmt.Errorf("collection query %q is ambiguous (%d matches)", collectionQuery, len(matches))
		}
		rootKey := matches[0].Key
		includeDescendants := opts.IncludeSubcollections
		if !opts.IncludeSubcollectionsSet {
			includeDescendants = true
		}
		if includeDescendants {
			collectionKeys = DescendantCollectionKeys(rootKey, collections)
		} else {
			collectionKeys = []string{rootKey}
		}
		collectionPath = matches[0].Path
		// Keep collection context in global ranking before filtering membership.
		for _, part := range strings.Split(collectionPath, " > ") {
			part = normalizeCollectionName(part)
			if part != "" && !strings.Contains(strings.ToLower(topic), part) {
				topic += " " + part
			}
		}
		args["query"] = topic
		for _, key := range collectionKeys {
			items, err := resolver.CollectionItems(ctx, key)
			if err != nil {
				return SemanticReport{}, fmt.Errorf("fetch collection %s: %w", key, err)
			}
			scope = appendUnique(scope, items...)
		}
	}

	raw, err := invoke(ctx, "search", args)
	if err != nil {
		return SemanticReport{}, err
	}
	results, err := parseSemanticResults(raw)
	if err != nil {
		return SemanticReport{}, err
	}
	report := SemanticReport{Query: topic, CollectionQuery: collectionQuery, CollectionPath: collectionPath, CollectionKeys: collectionKeys, Results: make([]SemanticResult, 0), RequestedMax: opts.MaxResults, RetrievedCandidates: len(results), PotentiallyTruncated: len(results) >= opts.MaxResults, Completeness: "not exhaustive", LimitNote: fmt.Sprintf("ZOTseek search is capped at %d candidates and has no pagination; collection filtering happens after retrieval, so out-of-scope candidates can consume the cap. Results are ranked discovery, not an exhaustive collection listing.", opts.MaxResults)}
	if collectionQuery == "" {
		for _, result := range results {
			if opts.LibraryKey == "" || result.LibraryKey == opts.LibraryKey {
				report.Results = append(report.Results, result)
			} else {
				report.ExcludedOutOfScope++
			}
		}
		return report, nil
	}
	report.ScopeFiltered = len(scope)
	if len(scope) == 0 {
		report.ExcludedOutOfScope = len(results)
		return report, nil
	}
	seen := make(map[string]bool)
	for _, result := range results {
		if opts.LibraryKey != "" && result.LibraryKey != opts.LibraryKey {
			report.ExcludedOutOfScope++
			continue
		}
		var matches []Record
		for _, item := range scope {
			if item.Key != "" && item.Key == result.ItemKey && sameLibraryKey(item.Library, result.LibraryKey) {
				matches = append(matches, item)
			}
		}
		if len(matches) == 1 {
			identity := matches[0].Library + "\x00" + matches[0].Key
			if opts.Granularity == "passages" || !seen[identity] {
				report.Results = append(report.Results, result)
				seen[identity] = true
			}
		} else {
			report.ExcludedOutOfScope++
		}
	}
	return report, nil
}

func resolveCollectionQuery(query string, collections []CollectionInfo) ([]CollectionMatch, error) {
	trimmedQuery := strings.TrimSpace(query)
	// An unqualified name must not hide a second matching leaf elsewhere.
	if !strings.Contains(trimmedQuery, ">") && normalizeCollectionName(trimmedQuery) == strings.ToLower(trimmedQuery) {
		var paths []string
		for _, c := range collections {
			if normalizeCollectionName(c.Name) == normalizeCollectionName(trimmedQuery) {
				paths = append(paths, canonicalCollectionPath(c, collections))
			}
		}
		if len(paths) > 1 {
			sort.Strings(paths)
			return nil, fmt.Errorf("collection query %q is ambiguous: %s", query, strings.Join(paths, "; "))
		}
	}
	var rawMatches []CollectionMatch
	for _, c := range collections {
		if strings.EqualFold(strings.TrimSpace(canonicalCollectionPath(c, collections)), trimmedQuery) {
			path := canonicalCollectionPath(c, collections)
			rawMatches = append(rawMatches, CollectionMatch{Key: c.Key, Name: c.Name, Path: path, Parent: c.Parent, NumItems: c.NumItems, NumCollections: c.NumCollections})
		}
	}
	if len(rawMatches) == 1 {
		return rawMatches, nil
	}
	if len(rawMatches) > 1 {
		paths := make([]string, len(rawMatches))
		for i := range rawMatches {
			paths[i] = rawMatches[i].Path
		}
		return nil, fmt.Errorf("collection query %q is ambiguous: %s", query, strings.Join(paths, "; "))
	}
	exact, err := FindCollections(query, collections)
	if err != nil {
		return nil, err
	}
	parts := splitCollectionPath(query)
	if len(parts) == 0 {
		return nil, errors.New("collection query is empty")
	}
	// Resolve a typo only when one credible candidate is strictly closest.
	type scored struct {
		match CollectionMatch
		score int
	}
	var scoredMatches []scored
	for _, c := range collections {
		paths := strings.Split(canonicalCollectionPath(c, collections), " > ")
		if len(parts) > len(paths) {
			continue
		}
		if len(parts) == 1 {
			name := normalizeCollectionName(paths[len(paths)-1])
			d := levenshtein(name, normalizeCollectionName(parts[0]))
			if d <= typoThreshold(len(name)) {
				scoredMatches = append(scoredMatches, scored{CollectionMatch{Key: c.Key, Name: c.Name, Path: strings.Join(paths, " > "), Parent: c.Parent, NumItems: c.NumItems, NumCollections: c.NumCollections}, d})
			}
			continue
		}
		for start := 0; start+len(parts) <= len(paths); start++ {
			total, ok := 0, true
			for i, part := range parts {
				d := levenshtein(normalizeCollectionName(paths[start+i]), normalizeCollectionName(part))
				if d > typoThreshold(len(paths[start+i])) {
					ok = false
					break
				}
				total += d
			}
			if ok {
				scoredMatches = append(scoredMatches, scored{CollectionMatch{Key: c.Key, Name: c.Name, Path: strings.Join(paths, " > "), Parent: c.Parent, NumItems: c.NumItems, NumCollections: c.NumCollections}, total})
				break
			}
		}
	}
	if len(exact) > 0 {
		candidates := append([]CollectionMatch{}, exact...)
		seen := map[string]bool{}
		for _, match := range candidates {
			seen[match.Key] = true
		}
		for _, candidate := range scoredMatches {
			if !seen[candidate.match.Key] {
				candidates = append(candidates, candidate.match)
				seen[candidate.match.Key] = true
			}
		}
		if len(candidates) == 1 {
			return candidates, nil
		}
		paths := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			paths = append(paths, candidate.Path)
		}
		sort.Strings(paths)
		return nil, fmt.Errorf("collection query %q is ambiguous: %s", query, strings.Join(paths, "; "))
	}
	if len(scoredMatches) == 0 {
		return nil, fmt.Errorf("collection %q not found", query)
	}
	sort.Slice(scoredMatches, func(i, j int) bool {
		if scoredMatches[i].score == scoredMatches[j].score {
			return scoredMatches[i].match.Key < scoredMatches[j].match.Key
		}
		return scoredMatches[i].score < scoredMatches[j].score
	})
	if len(scoredMatches) > 1 && scoredMatches[0].score == scoredMatches[1].score {
		return nil, fmt.Errorf("collection query %q is ambiguous between %q and %q", query, scoredMatches[0].match.Path, scoredMatches[1].match.Path)
	}
	return []CollectionMatch{scoredMatches[0].match}, nil
}

func canonicalCollectionPath(c CollectionInfo, all []CollectionInfo) string {
	byKey := make(map[string]CollectionInfo, len(all))
	for _, item := range all {
		byKey[item.Key] = item
	}
	var parts []string
	seen := map[string]bool{}
	for c.Key != "" {
		if seen[c.Key] {
			return ""
		}
		seen[c.Key] = true
		parts = append([]string{c.Name}, parts...)
		if c.Parent == "" {
			break
		}
		parent, ok := byKey[c.Parent]
		if !ok {
			return ""
		}
		c = parent
	}
	return strings.Join(parts, " > ")
}

func typoThreshold(length int) int {
	if length < 5 {
		return 1
	}
	if length < 9 {
		return 2
	}
	return 3
}

func levenshtein(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ra := range a {
		curr := make([]int, len(b)+1)
		curr[0] = i + 1
		for j, rb := range b {
			cost := 0
			if ra != rb {
				cost = 1
			}
			curr[j+1] = minInt(curr[j]+1, prev[j+1]+1, prev[j]+cost)
		}
		prev = curr
	}
	return prev[len(b)]
}
func minInt(values ...int) int {
	out := values[0]
	for _, value := range values[1:] {
		if value < out {
			out = value
		}
	}
	return out
}

func sameLibraryKey(recordLibrary, resultLibrary string) bool {
	if resultLibrary == "" || recordLibrary == "" {
		return false
	}
	if recordLibrary == resultLibrary {
		return true
	}
	return resultLibrary == "user" && strings.HasPrefix(recordLibrary, "user:")
}

func parseSemanticResults(raw json.RawMessage) ([]SemanticResult, error) {
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured map[string]any `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse ZOTseek response: %w", err)
	}
	var payload json.RawMessage
	if len(envelope.Structured) > 0 {
		payload, _ = json.Marshal(envelope.Structured)
	}
	if len(payload) == 0 {
		for _, block := range envelope.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				payload = json.RawMessage(block.Text)
				break
			}
		}
	}
	if len(payload) == 0 {
		return nil, errors.New("ZOTseek result has no structured search content")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, fmt.Errorf("parse ZOTseek search JSON: %w", err)
	}
	resultsRaw, ok := fields["results"]
	if !ok || len(resultsRaw) == 0 || string(resultsRaw) == "null" || resultsRaw[0] != '[' {
		return nil, errors.New("ZOTseek search result missing results array")
	}
	var body struct {
		Results []SemanticResult `json:"results"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, fmt.Errorf("parse ZOTseek search JSON: %w", err)
	}
	return body.Results, nil
}

func NewSemanticCommand(factory func() (*zotero.Client, error)) *cobra.Command {
	var endpoint, protocol, output, collectionPath, mode, granularity, libraryKey string
	var maxResults int
	var minSimilarity float64
	var includeSubcollections bool
	var timeout time.Duration
	cmd := &cobra.Command{Use: "semantic-search QUERY", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if maxResults < 1 || maxResults > 100 {
			return errors.New("--max-results must be between 1 and 100")
		}
		if mode != "hybrid" && mode != "semantic" && mode != "keyword" {
			return errors.New("--mode must be hybrid, semantic, or keyword")
		}
		if granularity != "papers" && granularity != "passages" {
			return errors.New("--granularity must be papers or passages")
		}
		if minSimilarity < 0 || minSimilarity > 1 || math.IsNaN(minSimilarity) || math.IsInf(minSimilarity, 0) {
			return errors.New("--min-similarity must be finite and between 0 and 1")
		}
		if timeout <= 0 {
			return errors.New("--timeout must be positive")
		}
		client, err := factory()
		if err != nil {
			return err
		}
		resolver, err := adaptClient(client)
		if err != nil {
			return err
		}
		if endpoint == "" {
			endpoint = os.Getenv("ZOTSEEK_MCP_URL")
			if endpoint == "" {
				endpoint = "http://localhost:23119/zotseek/mcp"
			}
		}
		if protocol == "" {
			protocol = os.Getenv("ZOTSEEK_MCP_PROTOCOL")
			if protocol == "" {
				protocol = "2025-03-26"
			}
		}
		mcp := &semantic.Client{Endpoint: endpoint, Protocol: protocol}
		invoke := func(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
			return mcp.Call(ctx, name, args)
		}
		opts := SemanticSearchOptions{CollectionPath: collectionPath, MaxResults: maxResults, Mode: mode, Granularity: granularity, LibraryKey: libraryKey}
		if cmd.Flags().Changed("min-similarity") {
			opts.MinSimilarity = &minSimilarity
		}
		opts.IncludeSubcollections = includeSubcollections
		opts.IncludeSubcollectionsSet = cmd.Flags().Changed("include-subcollections")
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		report, err := SemanticSearch(ctx, resolver, invoke, args[0], opts)
		if err != nil {
			return err
		}
		return emitTo(cmd.OutOrStdout(), output, report)
	}}
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "ZOTseek MCP endpoint")
	cmd.Flags().StringVar(&protocol, "protocol", "", "MCP protocol version")
	cmd.Flags().StringVar(&output, "output", "-", "JSON report path, or - for stdout")
	cmd.Flags().StringVar(&collectionPath, "collection-path", "", "restrict to a collection breadcrumb")
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "ZOTseek candidate cap")
	cmd.Flags().StringVar(&mode, "mode", "hybrid", "ZOTseek mode: hybrid, semantic, or keyword")
	cmd.Flags().StringVar(&granularity, "granularity", "papers", "papers or passages")
	cmd.Flags().StringVar(&libraryKey, "library-key", "", "ZOTseek library key")
	cmd.Flags().Float64Var(&minSimilarity, "min-similarity", 0, "minimum semantic similarity (0-1)")
	cmd.Flags().BoolVar(&includeSubcollections, "include-subcollections", true, "include descendant collections")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "overall semantic search timeout")
	return cmd
}

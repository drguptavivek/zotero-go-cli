package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Epistemic-Technology/zotero/internal/atomicfile"
	"github.com/Epistemic-Technology/zotero/zotero"
	"github.com/spf13/cobra"
)

// Record is the metadata needed to identify a parent reference. DOI and Year
// are kept outside the upstream client's historical ItemData shape.
type Record struct {
	Key         string   `json:"key"`
	Library     string   `json:"library,omitempty"`
	URI         string   `json:"uri,omitempty"`
	Title       string   `json:"title"`
	DOI         string   `json:"doi,omitempty"`
	Authors     []string `json:"authors,omitempty"`
	Year        int      `json:"year,omitempty"`
	ItemType    string   `json:"itemType,omitempty"`
	Collections []string `json:"collections,omitempty"`
}

type CollectionInfo struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	Parent         string `json:"parent,omitempty"`
	NumItems       int    `json:"numItems,omitempty"`
	NumCollections int    `json:"numCollections,omitempty"`
}

type PDFMode string

const (
	PDFNone     PDFMode = "none"
	PDFMetadata PDFMode = "metadata"
	PDFVerify   PDFMode = "verify"
)

type PDFStatus struct {
	ParentKey   string `json:"parentKey,omitempty"`
	Key         string `json:"key"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	LinkMode    string `json:"linkMode,omitempty"`
	Present     bool   `json:"present"`
	Verified    bool   `json:"verified,omitempty"`
	Status      string `json:"status"` // present, absent, inaccessible, invalid
	Error       string `json:"error,omitempty"`
}

// Resolver is the narrow workflow contract. The concrete Zotero client is
// adapted by the CLI layer; tests can use a deterministic in-memory resolver.
type Resolver interface {
	Search(ctx context.Context, query string, collectionKeys []string) ([]Record, error)
	Collections(ctx context.Context) ([]CollectionInfo, error)
	CollectionItems(ctx context.Context, collectionKey string) ([]Record, error)
	PDFStatus(ctx context.Context, key string, mode PDFMode) ([]PDFStatus, error)
}

type ResolveOptions struct {
	CollectionKeys        []string
	IncludeSubcollections bool
	PDFMode               PDFMode
	Progress              func(number int, status string)
}

type ReferenceResult struct {
	Number     int         `json:"number"`
	Citation   Citation    `json:"citation"`
	Status     string      `json:"status"` // matched, unresolved, ambiguous, conflict, error
	MatchKey   string      `json:"matchKey,omitempty"`
	MatchBasis string      `json:"matchBasis,omitempty"`
	Candidates []Record    `json:"candidates,omitempty"`
	PDF        []PDFStatus `json:"pdf,omitempty"`
	Error      string      `json:"error,omitempty"`
}

type Report struct {
	References []ReferenceResult `json:"references"`
}

// ResolveReferences performs one title-first search per citation. Results are
// cached by normalized query for this invocation, while duplicate input rows
// remain separate in the report.
func ResolveReferences(ctx context.Context, resolver Resolver, citations []Citation, opts ResolveOptions) Report {
	queries := make(map[string][]Record)
	pdfCache := make(map[string][]PDFStatus)
	report := Report{References: make([]ReferenceResult, 0, len(citations))}
	appendResult := func(result ReferenceResult) {
		report.References = append(report.References, result)
		if opts.Progress != nil {
			opts.Progress(result.Number, result.Status)
		}
	}
	for _, citation := range citations {
		result := ReferenceResult{Number: citation.Number, Citation: citation}
		if strings.TrimSpace(citation.Title) == "" {
			result.Status = "error"
			result.Error = "citation has no title"
			appendResult(result)
			continue
		}
		query := NormalizeTitle(citation.Title)
		candidates, ok := queries[query]
		if !ok {
			var err error
			candidates, err = resolver.Search(ctx, citation.Title, opts.CollectionKeys)
			if err != nil {
				result.Status, result.Error = "error", err.Error()
				appendResult(result)
				continue
			}
			queries[query] = candidates
		}
		match, basis, status := identify(citation, candidates)
		if match == nil && status == "unresolved" {
			// Fallback is deliberately narrow: a title fragment can recover
			// punctuation or truncation differences without semantic drift.
			fallback := titleFragment(citation.Title)
			if fallback != query {
				fallbackKey := NormalizeTitle(fallback)
				fallbackCandidates, cached := queries[fallbackKey]
				if cached {
					candidates = appendUnique(candidates, fallbackCandidates...)
					match, basis, status = identify(citation, candidates)
					if match != nil {
						goto matched
					}
				} else {
					more, err := resolver.Search(ctx, fallback, opts.CollectionKeys)
					if err != nil {
						result.Status, result.Error = "error", err.Error()
						result.Candidates = candidates
						appendResult(result)
						continue
					}
					queries[fallbackKey] = more
					candidates = appendUnique(candidates, more...)
				}
				match, basis, status = identify(citation, candidates)
			}
		}
	matched:
		result.Candidates = candidates
		result.Status, result.MatchBasis = status, basis
		if match != nil {
			result.MatchKey = match.Key
			if opts.PDFMode != PDFNone {
				pdf, err := cachedPDF(ctx, resolver, match, opts.PDFMode, pdfCache)
				if err != nil {
					result.Error = err.Error()
				} else {
					result.PDF = pdf
					if !pdfAvailable(pdf, opts.PDFMode) {
						// A duplicate metadata candidate may own the actual file. Try
						// each identity separately before reporting absence.
						for i := range candidates {
							candidate := &candidates[i]
							if candidate.Key == match.Key && candidate.Library == match.Library {
								continue
							}
							if !sameCitationIdentity(citation, *candidate) {
								continue
							}
							alternate, altErr := cachedPDF(ctx, resolver, candidate, opts.PDFMode, pdfCache)
							if altErr == nil && pdfAvailable(alternate, opts.PDFMode) {
								// Keep the bibliographic match key stable. ParentKey on
								// the returned PDF status identifies the fallback owner.
								result.PDF = alternate
								break
							}
						}
					}
				}
			}
		}
		appendResult(result)
	}
	return report
}

func cachedPDF(ctx context.Context, resolver Resolver, record *Record, mode PDFMode, cache map[string][]PDFStatus) ([]PDFStatus, error) {
	cacheKey := record.Library + "\x00" + record.Key
	if pdf, ok := cache[cacheKey]; ok {
		return pdf, nil
	}
	pdf, err := resolver.PDFStatus(ctx, record.Key, mode)
	if err == nil {
		cache[cacheKey] = pdf
	}
	return pdf, err
}

func pdfAvailable(statuses []PDFStatus, mode PDFMode) bool {
	for _, status := range statuses {
		if status.Status == "present" && (mode != PDFVerify || status.Verified) {
			return true
		}
	}
	return false
}

func sameCitationIdentity(c Citation, candidate Record) bool {
	if NormalizeTitle(c.Title) != NormalizeTitle(candidate.Title) {
		return false
	}
	if c.DOI != "" {
		return candidate.DOI != "" && strings.EqualFold(cleanDOI(c.DOI), cleanDOI(candidate.DOI))
	}
	return authorsYearAgree(c, candidate)
}

func identify(c Citation, candidates []Record) (*Record, string, string) {
	if len(candidates) == 0 {
		return nil, "", "unresolved"
	}
	var exactDOI []*Record
	var exactTitle []*Record
	for i := range candidates {
		r := &candidates[i]
		if c.DOI != "" && r.DOI != "" && strings.EqualFold(cleanDOI(c.DOI), cleanDOI(r.DOI)) {
			exactDOI = append(exactDOI, r)
			continue
		}
		if c.DOI != "" && r.DOI != "" && strings.EqualFold(cleanDOI(c.DOI), cleanDOI(r.DOI)) == false && NormalizeTitle(c.Title) == NormalizeTitle(r.Title) {
			// Same title with a conflicting DOI must never be silently accepted.
			return nil, "", "conflict"
		}
		if NormalizeTitle(c.Title) == NormalizeTitle(r.Title) && authorsYearAgree(c, *r) {
			exactTitle = append(exactTitle, r)
		}
	}
	if len(exactDOI) == 1 {
		return exactDOI[0], "doi", "matched"
	}
	if len(exactDOI) > 1 {
		return nil, "", "ambiguous"
	}
	if len(exactTitle) == 1 {
		return exactTitle[0], "title-authors-year", "matched"
	}
	if len(exactTitle) > 1 {
		return nil, "", "ambiguous"
	}
	return nil, "", "unresolved"
}

func authorsYearAgree(c Citation, r Record) bool {
	evidence := false
	if c.Year != 0 && r.Year != 0 && c.Year != r.Year {
		return false
	}
	if c.Year != 0 && r.Year != 0 {
		evidence = true
	}
	if len(c.Authors) > 0 && len(r.Authors) > 0 {
		left, right := authorAliases(c.Authors[0]), authorAliases(r.Authors[0])
		authorMatch := false
		for alias := range left {
			if right[alias] {
				authorMatch = true
			}
		}
		if !authorMatch {
			return false
		}
		evidence = true
	}
	return evidence
}

func authorAliases(s string) map[string]bool {
	words := strings.Fields(strings.ToLower(strings.Trim(s, ".,;")))
	aliases := make(map[string]bool)
	if len(words) == 0 {
		return aliases
	}
	for i := range words {
		words[i] = NormalizeTitle(words[i])
	}
	if len(words) == 1 {
		aliases["surname:"+words[0]] = true
		return aliases
	}
	first, last := words[0], words[len(words)-1]
	if len(first) <= 2 && len(last) > 2 {
		aliases["surname:"+last] = true
	} else if len(last) <= 2 && len(first) > 2 {
		aliases["surname:"+first] = true
	} else {
		// Vancouver commonly uses surname-first. For two full words retain
		// the surname-first alias, plus an order-independent full-name alias
		// so "Vivek Gupta" and "Gupta Vivek" match only as the same pair.
		aliases["surname:"+first] = true
		pair := []string{first, last}
		sort.Strings(pair)
		aliases["full:"+strings.Join(pair, " ")] = true
	}
	return aliases
}

func titleFragment(title string) string {
	words := strings.Fields(title)
	if len(words) <= 8 {
		return title
	}
	return strings.Join(words[:8], " ")
}

func appendUnique(base []Record, add ...Record) []Record {
	seen := make(map[string]bool, len(base)+len(add))
	for _, r := range base {
		seen[r.Library+"\x00"+r.Key] = true
	}
	for _, r := range add {
		identity := r.Library + "\x00" + r.Key
		if !seen[identity] {
			base, seen[identity] = append(base, r), true
		}
	}
	return base
}

// ResolveCollectionPath resolves a hierarchy without allowing a matching name
// from another branch to escape the requested path.
func ResolveCollectionPath(path string, collections []CollectionInfo) (CollectionInfo, error) {
	parts := splitCollectionPath(path)
	if len(parts) == 0 {
		return CollectionInfo{}, errors.New("collection path is empty")
	}
	var parent string
	var current CollectionInfo
	for _, part := range parts {
		var matches []CollectionInfo
		for _, c := range collections {
			if c.Parent == parent && normalizeCollectionName(c.Name) == normalizeCollectionName(part) {
				matches = append(matches, c)
			}
		}
		if len(matches) != 1 {
			if len(matches) == 0 {
				return CollectionInfo{}, fmt.Errorf("collection %q not found under %q", part, parent)
			}
			return CollectionInfo{}, fmt.Errorf("collection %q is ambiguous under %q", part, parent)
		}
		current, parent = matches[0], matches[0].Key
	}
	return current, nil
}

type CollectionMatch struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	Parent         string `json:"parent,omitempty"`
	NumItems       int    `json:"numItems,omitempty"`
	NumCollections int    `json:"numCollections,omitempty"`
}

// FindCollections returns all deterministic partial matches. A single query
// term matches a leaf name; a breadcrumb query matches an ordered contiguous
// sequence of ancestor names after numeric ordering prefixes are removed.
// Cycles and orphaned parents are errors rather than silently truncated paths.
func FindCollections(query string, collections []CollectionInfo) ([]CollectionMatch, error) {
	parts := splitCollectionPath(query)
	if len(parts) == 0 {
		return nil, errors.New("collection query is empty")
	}
	byKey := make(map[string]CollectionInfo, len(collections))
	for _, c := range collections {
		if c.Key == "" {
			return nil, errors.New("collection has empty key")
		}
		if _, exists := byKey[c.Key]; exists {
			return nil, fmt.Errorf("duplicate collection key %q", c.Key)
		}
		byKey[c.Key] = c
	}
	pathFor := make(map[string][]string, len(collections))
	var build func(string, map[string]bool) ([]string, error)
	build = func(key string, visiting map[string]bool) ([]string, error) {
		if path, ok := pathFor[key]; ok {
			return path, nil
		}
		c, ok := byKey[key]
		if !ok {
			return nil, fmt.Errorf("orphan collection parent %q", key)
		}
		if visiting[key] {
			return nil, fmt.Errorf("collection cycle detected at %q", key)
		}
		visiting[key] = true
		var path []string
		if c.Parent != "" {
			parent, err := build(c.Parent, visiting)
			if err != nil {
				return nil, err
			}
			path = append(path, parent...)
		}
		path = append(path, c.Name)
		delete(visiting, key)
		pathFor[key] = path
		return path, nil
	}
	var matches []CollectionMatch
	for _, c := range collections {
		path, err := build(c.Key, map[string]bool{})
		if err != nil {
			return nil, err
		}
		normalized := make([]string, len(path))
		for i := range path {
			normalized[i] = normalizeCollectionName(path[i])
		}
		matched := false
		if len(parts) == 1 {
			matched = normalized[len(normalized)-1] == normalizeCollectionName(parts[0]) || strings.Contains(normalized[len(normalized)-1], normalizeCollectionName(parts[0]))
		} else {
			want := make([]string, len(parts))
			for i := range parts {
				want[i] = normalizeCollectionName(parts[i])
			}
			for start := 0; start+len(want) <= len(normalized); start++ {
				matched = true
				for i := range want {
					if !strings.Contains(normalized[start+i], want[i]) {
						matched = false
						break
					}
				}
				if matched {
					break
				}
			}
		}
		if matched {
			labels := make([]string, len(path))
			copy(labels, path)
			matches = append(matches, CollectionMatch{Key: c.Key, Name: c.Name, Path: strings.Join(labels, " > "), Parent: c.Parent, NumItems: c.NumItems, NumCollections: c.NumCollections})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if strings.ToLower(matches[i].Path) == strings.ToLower(matches[j].Path) {
			return matches[i].Key < matches[j].Key
		}
		return strings.ToLower(matches[i].Path) < strings.ToLower(matches[j].Path)
	})
	return matches, nil
}

// DescendantCollectionKeys returns root first and then all descendants. It is
// intentionally based on parent keys, so a same-named collection elsewhere in
// the library cannot leak into a scoped search.
func DescendantCollectionKeys(root string, collections []CollectionInfo) []string {
	keys := []string{root}
	seen := map[string]bool{root: true}
	for changed := true; changed; {
		changed = false
		for _, c := range collections {
			if seen[c.Parent] && !seen[c.Key] {
				seen[c.Key] = true
				keys = append(keys, c.Key)
				changed = true
			}
		}
	}
	return keys
}

func splitCollectionPath(path string) []string {
	var out []string
	for _, p := range strings.Split(path, ">") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizeCollectionName(name string) string {
	name = unquoteSearchText(strings.TrimSpace(name))
	name = regexp.MustCompile(`^\s*\d+(?:[.)_\p{Pd}]|\s)+`).ReplaceAllString(name, "")
	return NormalizeTitle(name)
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

// NewCommand adds workflow commands to a root Cobra command. The adapter is
// intentionally injected so command tests do not need a live Zotero library.
func NewCommand(factory func() (*zotero.Client, error)) *cobra.Command {
	root := &cobra.Command{Use: "workflow", SilenceUsage: true}
	var output, collectionKey, collectionPath, pdf string
	var progress bool
	var includeSubs bool
	resolve := &cobra.Command{
		Use:   "resolve-references INPUT",
		Short: "Resolve supplied citations one by one",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if pdf != string(PDFNone) && pdf != string(PDFMetadata) && pdf != string(PDFVerify) {
				return fmt.Errorf("invalid --pdf value %q (want none, metadata, or verify)", pdf)
			}
			if output != "-" && samePath(args[0], output) {
				return errors.New("--output must not overwrite the citation input")
			}
			var r io.Reader = os.Stdin
			if args[0] != "-" {
				f, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer f.Close()
				r = f
			}
			citations, err := ParseInput(r)
			if err != nil {
				return err
			}
			client, err := factory()
			if err != nil {
				return err
			}
			resolver, err := adaptClient(client)
			if err != nil {
				return err
			}
			opts := ResolveOptions{IncludeSubcollections: includeSubs, PDFMode: PDFMode(pdf)}
			if progress {
				opts.Progress = func(number int, status string) { _, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[%d] %s\n", number, status) }
			}
			if collectionKey != "" && collectionPath != "" {
				return errors.New("--collection-key and --collection-path are mutually exclusive")
			}
			if collectionPath != "" {
				c, err := ResolveCollectionPathFromResolver(cmd.Context(), resolver, collectionPath)
				if err != nil {
					return err
				}
				collectionKey = c.Key
			}
			if collectionKey != "" {
				opts.CollectionKeys = []string{collectionKey}
				if includeSubs {
					collections, err := resolver.Collections(cmd.Context())
					if err != nil {
						return err
					}
					opts.CollectionKeys = DescendantCollectionKeys(collectionKey, collections)
				}
			}
			report := ResolveReferences(cmd.Context(), resolver, citations, opts)
			if err := emitTo(cmd.OutOrStdout(), output, report); err != nil {
				return err
			}
			if reportHasErrors(report) {
				return errors.New("reference report contains errors")
			}
			return nil
		},
	}
	resolve.Flags().StringVar(&output, "output", "-", "JSON report path, or - for stdout")
	resolve.Flags().StringVar(&collectionKey, "collection-key", "", "restrict searches to a collection")
	resolve.Flags().StringVar(&collectionPath, "collection-path", "", "restrict searches to a > separated collection path")
	resolve.Flags().BoolVar(&includeSubs, "include-subcollections", false, "include descendants of the selected collection")
	resolve.Flags().StringVar(&pdf, "pdf", string(PDFNone), "PDF mode: none, metadata, or verify")
	resolve.Flags().BoolVar(&progress, "progress", false, "write one status line per citation to stderr")
	root.AddCommand(resolve)
	root.AddCommand(collectionResolveCommand(factory))
	root.AddCommand(collectionFindCommand(factory))
	root.AddCommand(pdfStatusCommand(factory))
	root.AddCommand(NewSemanticCommand(factory))
	return root
}

func collectionFindCommand(factory func() (*zotero.Client, error)) *cobra.Command {
	return &cobra.Command{Use: "collection-find QUERY", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, err := factory()
		if err != nil {
			return err
		}
		resolver, err := adaptClient(client)
		if err != nil {
			return err
		}
		collections, err := resolver.Collections(cmd.Context())
		if err != nil {
			return err
		}
		matches, err := FindCollections(args[0], collections)
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), matches)
	}}
}

func ResolveCollectionPathFromResolver(ctx context.Context, resolver Resolver, path string) (CollectionInfo, error) {
	collections, err := resolver.Collections(ctx)
	if err != nil {
		return CollectionInfo{}, err
	}
	return ResolveCollectionPath(path, collections)
}

func collectionResolveCommand(factory func() (*zotero.Client, error)) *cobra.Command {
	return &cobra.Command{Use: "collection-resolve PATH", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, err := factory()
		if err != nil {
			return err
		}
		r, err := adaptClient(client)
		if err != nil {
			return err
		}
		c, err := ResolveCollectionPathFromResolver(cmd.Context(), r, args[0])
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), c)
	}}
}

func pdfStatusCommand(factory func() (*zotero.Client, error)) *cobra.Command {
	return &cobra.Command{Use: "pdf-status KEY...", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, err := factory()
		if err != nil {
			return err
		}
		r, err := adaptClient(client)
		if err != nil {
			return err
		}
		all := make([]PDFStatus, 0)
		for _, key := range args {
			s, err := r.PDFStatus(cmd.Context(), key, PDFVerify)
			if err != nil {
				return err
			}
			all = append(all, s...)
		}
		return writeJSON(cmd.OutOrStdout(), all)
	}}
}

func emit(path string, value any) error {
	return emitTo(os.Stdout, path, value)
}

func emitTo(stdout io.Writer, path string, value any) error {
	if path == "" || path == "-" {
		return writeJSON(stdout, value)
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	f, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() { _ = os.Remove(tmp) }
	if err := writeJSON(f, value); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := atomicfile.Replace(tmp, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func samePath(a, b string) bool {
	if a == "-" || b == "-" {
		return false
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if real, err := filepath.EvalSymlinks(aa); err == nil {
		aa = real
	}
	if real, err := filepath.EvalSymlinks(bb); err == nil {
		bb = real
	}
	return aa == bb
}

func reportHasErrors(report Report) bool {
	for _, result := range report.References {
		if result.Status == "error" || result.Error != "" {
			return true
		}
	}
	return false
}

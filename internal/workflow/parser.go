package workflow

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Citation is one reference supplied to resolve-references. Number and Raw are
// retained so a report can be joined back to the user's original list.
type Citation struct {
	Number  int      `json:"number,omitempty"`
	Raw     string   `json:"raw,omitempty"`
	Title   string   `json:"title"`
	DOI     string   `json:"doi,omitempty"`
	Authors []string `json:"authors,omitempty"`
	Year    int      `json:"year,omitempty"`
}

var (
	numberedCitation = regexp.MustCompile(`^\s*(\d+)\s*[.)]\s*(.*)$`)
	doiPattern       = regexp.MustCompile(`(?i)\b10\.\d{4,9}/[-._;()/:a-z0-9]+`)
	yearPattern      = regexp.MustCompile(`\b(19|20)\d{2}\b`)
)

// ParseInput accepts a numbered Vancouver list, a plain one-reference-per-line
// list, or an explicit JSON array. Numbering and duplicate entries are kept.
func ParseInput(r io.Reader) ([]Citation, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read citation input: %w", err)
	}
	text := strings.TrimSpace(string(b))
	if text == "" {
		return nil, errors.New("citation input is empty")
	}
	if strings.HasPrefix(text, "[") {
		return parseJSON(text)
	}
	parsed, err := parseVancouver(text)
	return parsed, err
}

func parseJSON(text string) ([]Citation, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("parse citation JSON: %w", err)
	}
	out := make([]Citation, 0, len(raw))
	for i, value := range raw {
		var c Citation
		if err := json.Unmarshal(value, &c); err == nil && c.Title != "" {
			if c.Number == 0 {
				c.Number = i + 1
			}
			if c.Raw == "" {
				c.Raw = c.Title
			}
			c.DOI = cleanDOI(c.DOI)
			out = append(out, c)
			continue
		}
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			return nil, fmt.Errorf("citation %d must be an object or string: %w", i+1, err)
		}
		parsed := parseOne(s, i+1)
		if parsed.Title == "" {
			return nil, fmt.Errorf("citation %d has no title", i+1)
		}
		out = append(out, parsed)
	}
	return out, nil
}

func parseVancouver(text string) ([]Citation, error) {
	type entry struct {
		n    int
		text string
	}
	var entries []entry
	var current strings.Builder
	currentNumber := 0
	var lines []string
	var hasNumbered bool
	scanner := bufio.NewScanner(strings.NewReader(text))
	number := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if match := numberedCitation.FindStringSubmatch(line); match != nil {
			hasNumbered = true
			if current.Len() > 0 {
				entries = append(entries, entry{n: currentNumber, text: current.String()})
				current.Reset()
			}
			number, _ = strconv.Atoi(match[1])
			currentNumber = number
			current.WriteString(match[2])
			continue
		}
		if current.Len() == 0 {
			number++
			currentNumber = number
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse citation input: %w", err)
	}
	if !hasNumbered {
		out := make([]Citation, 0, len(lines))
		for i, line := range lines {
			out = append(out, parseOne(line, i+1))
		}
		return out, nil
	}
	if current.Len() > 0 {
		entries = append(entries, entry{n: currentNumber, text: current.String()})
	}
	out := make([]Citation, 0, len(entries))
	for i, item := range entries {
		n := item.n
		if n == 0 {
			n = i + 1
		}
		out = append(out, parseOne(item.text, n))
	}
	return out, nil
}

func parseOne(raw string, number int) Citation {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "-"))
	c := Citation{Number: number, Raw: raw}
	if m := doiPattern.FindString(raw); m != "" {
		c.DOI = cleanDOI(m)
	}
	if m := yearPattern.FindStringSubmatch(raw); m != nil {
		c.Year, _ = strconv.Atoi(m[0])
	}

	// The author/title boundary normally follows "et al." or the final author
	// initials. Do not split at abbreviations such as "vs." in the title.
	start := authorTitleBoundary(raw)
	body := raw
	if start >= 0 {
		body = strings.TrimSpace(raw[start:])
		c.Authors = parseAuthors(raw[:start])
	}
	c.Title = extractTitle(body)
	if c.Title == "" {
		c.Title = strings.Trim(strings.TrimSpace(body), ".")
	}
	return c
}

func authorTitleBoundary(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '.' || !unicode.IsSpace(rune(s[i+1])) {
			continue
		}
		prefix := strings.TrimSpace(s[:i])
		if strings.Contains(strings.ToLower(prefix), "et al") || looksLikeAuthorList(prefix) || looksLikeSingleAuthor(prefix) {
			return i + 2
		}
	}
	return -1
}

func looksLikeSingleAuthor(s string) bool {
	words := strings.Fields(s)
	if len(words) < 2 || len(words) > 4 {
		return false
	}
	last := strings.Trim(words[len(words)-1], ".,;")
	return len(last) <= 3
}

func looksLikeAuthorList(s string) bool {
	if !strings.Contains(s, ",") {
		return false
	}
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		words := strings.Fields(part)
		if len(words) == 0 || (len(words) > 1 && len(words[len(words)-1]) > 3) {
			return false
		}
	}
	return true
}

func parseAuthors(s string) []string {
	var out []string
	for _, part := range strings.Split(strings.TrimSpace(s), ",") {
		part = strings.TrimSpace(strings.TrimSuffix(part, "."))
		if part == "" || strings.EqualFold(part, "et al") {
			continue
		}
		out = append(out, part)
	}
	return out
}

func extractTitle(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	// Locate a sentence boundary whose remaining citation contains the year.
	// A lowercase token after the period marks an abbreviation (e.g. "vs.").
	for i := 0; i+2 < len(body); i++ {
		if (body[i] != '.' && body[i] != '?' && body[i] != '!') || !unicode.IsSpace(rune(body[i+1])) {
			continue
		}
		next := strings.TrimSpace(body[i+1:])
		if next == "" || unicode.IsLower(rune(next[0])) {
			continue
		}
		if yearPattern.MatchString(next) || yearPattern.MatchString(next[:min(len(next), 240)]) {
			end := i
			if body[i] == '?' || body[i] == '!' {
				end = i + 1
			}
			candidate := strings.TrimSpace(body[:end])
			if candidate != "" {
				return strings.Trim(candidate, ".")
			}
		}
	}
	// If no journal/year boundary was found, use the first clear sentence.
	for i := 0; i+2 < len(body); i++ {
		if body[i] == '.' && unicode.IsSpace(rune(body[i+1])) {
			next := strings.TrimSpace(body[i+1:])
			if next != "" && !unicode.IsLower(rune(next[0])) {
				return strings.TrimSpace(body[:i])
			}
		}
	}
	return strings.Trim(body, ".")
}

func cleanDOI(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s, "https://doi.org/"), "http://doi.org/"), "doi:")
	return strings.Trim(s, " .;,)]}>")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// NormalizeTitle creates a conservative comparison key. It intentionally
// removes punctuation and diacritics only through Unicode letter/number rules.
func NormalizeTitle(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if r == '\'' || r == '\u2018' || r == '\u2019' {
			// Apostrophes are orthographic marks, not word boundaries: "patient's"
			// and "patients" should share a conservative lookup key.
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space && b.Len() > 0 {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

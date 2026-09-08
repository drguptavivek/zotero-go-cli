// Package docx validates Zotero citation fields in DOCX/OOXML packages.
//
// The validator is deliberately read-only. It uses only the standard library
// for ZIP and XML parsing so it can run in the portable zot-go binary.
package docx

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const zoteroPrefix = "ADDIN ZOTERO_ITEM CSL_CITATION"

var (
	uriRE       = regexp.MustCompile(`^https?://(?:www\.)?zotero\.org/(users/(?:local/[^/]+|[0-9]+)|groups/[0-9]+)/items/([^/?#]+)`)
	addinRE     = regexp.MustCompile(`\bADDIN\s+ZOTERO_[A-Za-z0-9_]+`)
	citationRE  = regexp.MustCompile(`\bADDIN\s+ZOTERO_ITEM\s+CSL_CITATION\b`)
	itemKeyRE   = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)
	requiredZIP = map[string]bool{"[Content_Types].xml": true, "_rels/.rels": true, "word/document.xml": true}
)

type fieldState struct {
	instruction []string
	visible     []string
	separate    bool
}

type simpleState struct {
	instruction string
	visible     []string
}

func localName(s string) string {
	if i := strings.LastIndexByte(s, '}'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func attr(start xml.StartElement, want string) string {
	for _, a := range start.Attr {
		if localName(a.Name.Local) == want {
			return a.Value
		}
	}
	return ""
}

func joinTrim(v []string) string { return strings.TrimSpace(strings.Join(v, "")) }

func fingerprint(v any) string {
	var b []byte
	if s, ok := v.(string); ok {
		b = []byte(s)
	} else {
		b = canonicalJSON(v)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func canonicalJSON(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'})
}

func zoteroMarker(instruction string) (string, string, bool) {
	if m := citationRE.FindStringIndex(instruction); m != nil {
		return zoteroPrefix, strings.TrimSpace(instruction[m[1]:]), true
	}
	if m := addinRE.FindStringIndex(instruction); m != nil {
		command := instruction[m[0]:m[1]]
		return command, strings.TrimSpace(instruction[m[1]:]), true
	}
	return "", "", false
}

func opaquePayloadFingerprint(payload string) string {
	trimmed := strings.TrimLeft(payload, " \t\r\n")
	dec := json.NewDecoder(strings.NewReader(trimmed))
	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		return fingerprint(payload)
	}
	offset := dec.InputOffset()
	suffix := strings.TrimSpace(trimmed[offset:])
	return fingerprint(map[string]any{"json": parsed, "suffix": suffix})
}

func fieldSummary(instruction, visible, part, command, payload string, citation any, citationOK bool) map[string]any {
	fieldType := "other"
	if command == zoteroPrefix {
		fieldType = "citation"
	}
	m := map[string]any{"part": part, "fieldType": fieldType, "command": command, "visibleText": visible}
	if citationOK {
		m["payloadFingerprint"] = fingerprint(citation)
	} else {
		m["payloadFingerprint"] = opaquePayloadFingerprint(payload)
	}
	return m
}

func citationData(data any, part, visible string, errs *[]string) map[string]any {
	result := map[string]any{
		"part": part, "citationID": nil, "noteIndex": nil, "itemCount": 0,
		"itemKeys": []any{}, "itemURIs": []any{}, "citationItems": []any{},
		"libraryNamespaces": []any{}, "embeddedItemDataCount": 0, "visibleText": visible,
	}
	obj, ok := data.(map[string]any)
	if !ok {
		*errs = append(*errs, part+": citation JSON must be an object")
		return result
	}
	if id, ok := obj["citationID"].(string); !ok || strings.TrimSpace(id) == "" {
		*errs = append(*errs, part+": citationID is missing or empty")
	} else {
		result["citationID"] = id
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		*errs = append(*errs, part+": properties must be an object")
	} else {
		n := props["noteIndex"]
		valid := false
		switch x := n.(type) {
		case json.Number:
			if i, e := strconv.ParseInt(string(x), 10, 64); e == nil && i >= 0 {
				result["noteIndex"], valid = i, true
			}
		case float64: // retained for callers that construct data directly
			if x >= 0 && x == float64(int64(x)) {
				result["noteIndex"], valid = int64(x), true
			}
		}
		if !valid {
			*errs = append(*errs, part+": properties.noteIndex must be a non-negative integer")
		}
	}
	items, ok := obj["citationItems"].([]any)
	if !ok || len(items) == 0 {
		*errs = append(*errs, part+": citationItems must be a non-empty array")
		return result
	}
	result["itemCount"] = len(items)
	itemKeys := result["itemKeys"].([]any)
	itemURIs := result["itemURIs"].([]any)
	namespaces := result["libraryNamespaces"].([]any)
	parsedItems := result["citationItems"].([]any)
	embedded := 0
	for i, raw := range items {
		itemResult := map[string]any{"id": nil, "uris": []any{}, "uriItemKeys": []any{}, "libraryNamespaces": []any{}, "hasItemData": false}
		item, ok := raw.(map[string]any)
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: citationItems[%d] must be an object", part, i+1))
			parsedItems = append(parsedItems, itemResult)
			continue
		}
		id := item["id"]
		idOK := false
		switch x := id.(type) {
		case string:
			idOK = strings.TrimSpace(x) != ""
		case json.Number:
			idOK = strings.TrimSpace(string(x)) != ""
		case float64:
			idOK = true
		}
		if !idOK {
			*errs = append(*errs, fmt.Sprintf("%s: citationItems[%d].id is missing", part, i+1))
		} else {
			itemResult["id"] = id
			itemKeys = append(itemKeys, id)
		}
		uris, ok := item["uris"].([]any)
		validURIs := ok && len(uris) > 0
		if validURIs {
			for _, u := range uris {
				if s, ok := u.(string); !ok || s == "" {
					validURIs = false
					break
				}
			}
		}
		if !validURIs {
			*errs = append(*errs, fmt.Sprintf("%s: citationItems[%d].uris must be a non-empty string array", part, i+1))
		} else {
			itemResult["uris"] = uris
			for _, u := range uris {
				s := u.(string)
				itemURIs = append(itemURIs, s)
				if m := uriRE.FindStringSubmatch(s); m != nil {
					itemResult["libraryNamespaces"] = append(itemResult["libraryNamespaces"].([]any), m[1])
					itemResult["uriItemKeys"] = append(itemResult["uriItemKeys"].([]any), m[2])
					namespaces = append(namespaces, m[1])
				}
			}
		}
		if d, ok := item["itemData"].(map[string]any); ok && len(d) > 0 {
			itemResult["hasItemData"] = true
			embedded++
		}
		itemResult["libraryNamespaces"] = uniqueSorted(itemResult["libraryNamespaces"].([]any))
		itemResult["uriItemKeys"] = uniqueSorted(itemResult["uriItemKeys"].([]any))
		parsedItems = append(parsedItems, itemResult)
	}
	result["itemKeys"], result["itemURIs"], result["citationItems"] = itemKeys, itemURIs, parsedItems
	result["libraryNamespaces"], result["embeddedItemDataCount"] = uniqueSorted(namespaces), embedded
	if schema, ok := obj["schema"].(string); !ok || schema == "" {
		*errs = append(*errs, part+": schema is missing")
	}
	return result
}

func uniqueSorted(v []any) []any {
	seen := map[string]bool{}
	for _, x := range v {
		if s, ok := x.(string); ok {
			seen[s] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for s := range seen {
		keys = append(keys, s)
	}
	sort.Strings(keys)
	out := make([]any, len(keys))
	for i, s := range keys {
		out[i] = s
	}
	return out
}

func extractFields(xmlBytes []byte, part string, errs *[]string, all *[]any) []any {
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	stack := []fieldState{}
	simples := []simpleState{}
	elements := []string{}
	outside := []string{}
	fields := []any{}
	process := func(instruction, visible string) {
		command, payload, ok := zoteroMarker(instruction)
		if !ok {
			return
		}
		var parsed any
		parsedOK := false
		if command == zoteroPrefix {
			jd := json.NewDecoder(strings.NewReader(payload))
			jd.UseNumber()
			if err := jd.Decode(&parsed); err != nil {
				*errs = append(*errs, fmt.Sprintf("%s: invalid Zotero citation JSON: %v", part, err))
			} else {
				var extra any
				if e := jd.Decode(&extra); e == io.EOF {
					parsedOK = true
				} else if e != nil {
					*errs = append(*errs, fmt.Sprintf("%s: invalid Zotero citation JSON: %v", part, e))
				} else {
					*errs = append(*errs, fmt.Sprintf("%s: invalid Zotero citation JSON: trailing data", part))
				}
			}
			if parsedOK {
				v := citationData(parsed, part, visible, errs)
				v["payloadFingerprint"] = fingerprint(parsed)
				fields = append(fields, v)
			}
		}
		if all != nil {
			entry := fieldSummary(instruction, visible, part, command, payload, parsed, parsedOK && command == zoteroPrefix)
			*all = append(*all, entry)
		}
		if command != zoteroPrefix {
			if command == "ADDIN ZOTERO_ITEM" {
				*errs = append(*errs, fmt.Sprintf("%s: unsupported Zotero ITEM field instruction", part))
			}
		}
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			*errs = append(*errs, fmt.Sprintf("%s: invalid XML: %v", part, err))
			return fields
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := localName(t.Name.Local)
			elements = append(elements, n)
			if n == "tab" && len(stack) > 0 {
				for i := range stack {
					if stack[i].separate {
						stack[i].visible = append(stack[i].visible, "\t")
					}
				}
			}
			if (n == "br" || n == "cr") && len(stack) > 0 {
				for i := range stack {
					if stack[i].separate {
						stack[i].visible = append(stack[i].visible, "\n")
					}
				}
			}
			if n == "tab" && len(simples) > 0 {
				for i := range simples {
					simples[i].visible = append(simples[i].visible, "\t")
				}
			}
			if (n == "br" || n == "cr") && len(simples) > 0 {
				for i := range simples {
					simples[i].visible = append(simples[i].visible, "\n")
				}
			}
			if n == "fldChar" {
				switch attr(t, "fldCharType") {
				case "begin":
					stack = append(stack, fieldState{})
				case "separate":
					if len(stack) > 0 {
						stack[len(stack)-1].separate = true
					}
				case "end":
					if len(stack) > 0 {
						f := stack[len(stack)-1]
						stack = stack[:len(stack)-1]
						instruction := joinTrim(f.instruction)
						if _, _, ok := zoteroMarker(instruction); ok {
							if !f.separate {
								*errs = append(*errs, fmt.Sprintf("%s: Zotero field has no separate marker", part))
							}
							process(instruction, strings.Join(f.visible, ""))
						}
					}
				}
			}
			if n == "fldSimple" {
				simples = append(simples, simpleState{instruction: attr(t, "instr")})
			}
		case xml.CharData:
			s := string(t)
			current := ""
			if len(elements) > 0 {
				current = elements[len(elements)-1]
			}
			if current == "instrText" {
				if len(stack) > 0 {
					stack[len(stack)-1].instruction = append(stack[len(stack)-1].instruction, s)
				} else {
					outside = append(outside, s)
				}
			} else if len(stack) > 0 {
				for i := range stack {
					if stack[i].separate {
						stack[i].visible = append(stack[i].visible, s)
					}
				}
			}
			if len(simples) > 0 {
				for i := range simples {
					simples[i].visible = append(simples[i].visible, s)
				}
			}
		case xml.EndElement:
			n := localName(t.Name.Local)
			if n == "fldSimple" && len(simples) > 0 {
				f := simples[len(simples)-1]
				simples = simples[:len(simples)-1]
				process(strings.TrimSpace(f.instruction), strings.Join(f.visible, ""))
			}
			if len(elements) > 0 {
				elements = elements[:len(elements)-1]
			}
		case xml.ProcInst, xml.Directive, xml.Comment:
		}
	}
	for _, f := range stack {
		if command, _, ok := zoteroMarker(joinTrim(f.instruction)); ok {
			_ = command
			*errs = append(*errs, fmt.Sprintf("%s: incomplete Zotero field (missing end marker)", part))
		}
	}
	if command, _, ok := zoteroMarker(strings.TrimSpace(strings.Join(outside, ""))); ok {
		_ = command
		*errs = append(*errs, fmt.Sprintf("%s: incomplete Zotero instruction outside a field", part))
	}
	return fields
}

func inspect(pathname string) map[string]any {
	r := map[string]any{
		"path": pathname, "xmlParts": 0, "zoteroFields": []any{}, "allZoteroFields": []any{},
		"errors": []any{}, "warnings": []any{},
		"portability": map[string]any{"libraryNamespaces": []any{}, "mixedLibraryNamespaces": false, "citationItemCount": 0, "itemsWithEmbeddedData": 0, "itemsWithoutEmbeddedData": 0, "embeddedDataCoverage": "not-applicable", "unrecognizedItemURIs": []any{}},
	}
	errs := []any{}
	r["errors"] = errs
	info, err := os.Stat(pathname)
	if err != nil || !info.Mode().IsRegular() {
		r["errors"] = append(errs, "file does not exist or is not a regular file")
		return r
	}
	packageReader, err := zip.OpenReader(pathname)
	if err != nil {
		r["errors"] = append(errs, "not a readable DOCX/ZIP package: "+err.Error())
		return r
	}
	defer packageReader.Close()
	seen := map[string]bool{}
	names := make([]string, 0, len(packageReader.File))
	for _, member := range packageReader.File {
		name := member.Name
		names = append(names, name)
		if seen[name] {
			r["errors"] = append(r["errors"].([]any), "ZIP package contains duplicate part names")
		}
		seen[name] = true
		clean := path.Clean(name)
		unsafe := strings.HasPrefix(name, "/") || clean == ".." || strings.HasPrefix(clean, "../")
		for _, p := range strings.Split(strings.Trim(name, "/"), "/") {
			if p == ".." {
				unsafe = true
			}
		}
		if unsafe {
			r["errors"] = append(r["errors"].([]any), "unsafe ZIP part path: "+name)
		}
	}
	for required := range requiredZIP {
		if !seen[required] {
			names = append(names, required)
		}
	}
	missing := []string{}
	for required := range requiredZIP {
		if !seen[required] {
			missing = append(missing, required)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		r["errors"] = append(r["errors"].([]any), "missing required OOXML part(s): "+strings.Join(missing, ", "))
	}
	all := []any{}
	fields := []any{}
	for _, member := range packageReader.File {
		if member.FileInfo().IsDir() {
			continue
		}
		b, readErr := readZipMember(member)
		if readErr != nil {
			r["errors"] = append(r["errors"].([]any), fmt.Sprintf("%s: could not read part: %v", member.Name, readErr))
			continue
		}
		if !strings.HasSuffix(member.Name, ".xml") && !strings.HasSuffix(member.Name, ".rels") {
			continue
		}
		r["xmlParts"] = r["xmlParts"].(int) + 1
		if xmlErr := validXML(b); xmlErr != nil {
			r["errors"] = append(r["errors"].([]any), fmt.Sprintf("%s: invalid XML: %v", member.Name, xmlErr))
			continue
		}
		if strings.HasPrefix(member.Name, "word/") && strings.HasSuffix(member.Name, ".xml") {
			fieldErrs := []string{}
			partFields := extractFields(b, member.Name, &fieldErrs, &all)
			for _, fieldErr := range fieldErrs {
				r["errors"] = append(r["errors"].([]any), fieldErr)
			}
			fields = append(fields, partFields...)
		}
	}
	r["zoteroFields"], r["allZoteroFields"] = fields, all
	// Re-run extraction errors through a string slice so inspect stays JSON-safe.
	// extractFields appends to a local slice; XML-level errors are already in r.
	citationIDs := map[string]bool{}
	duplicates := map[string]bool{}
	for _, raw := range fields {
		f := raw.(map[string]any)
		if id, ok := f["citationID"].(string); ok && id != "" {
			if citationIDs[id] {
				duplicates[id] = true
			}
			citationIDs[id] = true
		}
	}
	if len(duplicates) > 0 {
		ids := make([]string, 0, len(duplicates))
		for id := range duplicates {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		r["errors"] = append(r["errors"].([]any), "duplicate citationID value(s): "+strings.Join(ids, ", "))
	}
	// Aggregate portability information from validated citation fields.
	namespaceSet := map[string]bool{}
	unrecognizedSet := map[string]bool{}
	itemCount, withData := 0, 0
	for _, raw := range fields {
		f := raw.(map[string]any)
		for _, iraw := range f["citationItems"].([]any) {
			item := iraw.(map[string]any)
			itemCount++
			if item["hasItemData"] == true {
				withData++
			}
			for _, n := range item["libraryNamespaces"].([]any) {
				namespaceSet[n.(string)] = true
			}
			for _, u := range item["uris"].([]any) {
				s := u.(string)
				if uriRE.FindStringSubmatch(s) == nil {
					unrecognizedSet[s] = true
				}
			}
		}
	}
	namespaces := make([]any, 0, len(namespaceSet))
	for n := range namespaceSet {
		namespaces = append(namespaces, n)
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].(string) < namespaces[j].(string) })
	unrecognized := make([]any, 0, len(unrecognizedSet))
	for u := range unrecognizedSet {
		unrecognized = append(unrecognized, u)
	}
	sort.Slice(unrecognized, func(i, j int) bool { return unrecognized[i].(string) < unrecognized[j].(string) })
	if len(unrecognized) > 0 {
		vals := make([]string, len(unrecognized))
		for i, v := range unrecognized {
			vals[i] = v.(string)
		}
		r["warnings"] = append(r["warnings"].([]any), "unrecognized Zotero item URI format(s): "+strings.Join(vals, ", "))
	}
	coverage := "not-applicable"
	if itemCount > 0 {
		if withData == itemCount {
			coverage = "complete"
		} else if withData == 0 {
			coverage = "none"
		} else {
			coverage = "partial"
		}
	}
	r["portability"] = map[string]any{"libraryNamespaces": namespaces, "mixedLibraryNamespaces": len(namespaces) > 1, "citationItemCount": itemCount, "itemsWithEmbeddedData": withData, "itemsWithoutEmbeddedData": itemCount - withData, "embeddedDataCoverage": coverage, "unrecognizedItemURIs": unrecognized}
	r["allZoteroFieldCount"] = len(all)
	return r
}

func readZipMember(member *zip.File) ([]byte, error) {
	r, err := member.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func validXML(b []byte) error {
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		if _, err := d.Token(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}
}

type options struct {
	minimumFields, expectedFieldCount, expectedIncrease *int
	preserve, validateKeys                              bool
	expectCitationIDs, expectItemKeys, expectVisible    []string
}

func applyExpectations(result map[string]any, baseline map[string]any, opt options) {
	errs := result["errors"].([]any)
	fields := result["zoteroFields"].([]any)
	count := len(fields)
	if opt.minimumFields != nil && count < *opt.minimumFields {
		errs = append(errs, fmt.Sprintf("expected at least %d Zotero field(s), found %d", *opt.minimumFields, count))
	}
	if opt.expectedFieldCount != nil && count != *opt.expectedFieldCount {
		errs = append(errs, fmt.Sprintf("expected exactly %d Zotero field(s), found %d", *opt.expectedFieldCount, count))
	}
	if opt.expectedIncrease != nil {
		if baseline == nil {
			errs = append(errs, "--expected-increase requires --baseline")
		} else {
			increase := count - len(baseline["zoteroFields"].([]any))
			if increase != *opt.expectedIncrease {
				errs = append(errs, fmt.Sprintf("expected Zotero field count to increase by %d, observed %d", *opt.expectedIncrease, increase))
			}
		}
	}
	if opt.preserve {
		if baseline == nil {
			errs = append(errs, "--preserve-baseline-citations requires --baseline")
		} else {
			baselineFields := baseline["zoteroFields"].([]any)
			baselineAll := baseline["allZoteroFields"].([]any)
			baseByID, currentByID := map[string]map[string]any{}, map[string]map[string]any{}
			for _, raw := range baselineFields {
				f := raw.(map[string]any)
				if id, ok := f["citationID"].(string); ok && id != "" {
					baseByID[id] = f
				}
			}
			for _, raw := range fields {
				f := raw.(map[string]any)
				if id, ok := f["citationID"].(string); ok && id != "" {
					currentByID[id] = f
				}
			}
			baselineHas := len(baselineAll) > 0
			if !baselineHas {
				errs = append(errs, "baseline contains no valid Zotero citation fields or other Zotero fields; cannot establish preservation")
			}
			missing, changedURI, changedPayload, changedVisible := []string{}, []string{}, []string{}, []string{}
			for id := range baseByID {
				if _, ok := currentByID[id]; !ok {
					missing = append(missing, id)
					continue
				}
				b, c := baseByID[id], currentByID[id]
				if !sameSlice(b["itemURIs"].([]any), c["itemURIs"].([]any)) {
					changedURI = append(changedURI, id)
				}
				if b["payloadFingerprint"] != c["payloadFingerprint"] {
					changedPayload = append(changedPayload, id)
				}
				if b["visibleText"] != c["visibleText"] {
					changedVisible = append(changedVisible, id)
				}
			}
			sort.Strings(missing)
			sort.Strings(changedURI)
			sort.Strings(changedPayload)
			sort.Strings(changedVisible)
			if len(missing) > 0 {
				errs = append(errs, "baseline citationID value(s) missing: "+strings.Join(missing, ", "))
			}
			if len(changedURI) > 0 {
				errs = append(errs, "baseline citation item URI set changed for citationID value(s): "+strings.Join(changedURI, ", "))
			}
			if len(changedPayload) > 0 {
				errs = append(errs, "baseline citation payload changed for citationID value(s): "+strings.Join(changedPayload, ", "))
			}
			if len(changedVisible) > 0 {
				errs = append(errs, "baseline citation visible text changed for citationID value(s): "+strings.Join(changedVisible, ", "))
			}
			baseSig, curSig := map[string]int{}, map[string]int{}
			for _, raw := range baselineFields {
				f := raw.(map[string]any)
				baseSig[sig(f["payloadFingerprint"], f["visibleText"])]++
			}
			for _, raw := range fields {
				f := raw.(map[string]any)
				curSig[sig(f["payloadFingerprint"], f["visibleText"])]++
			}
			preserved := 0
			for k, n := range baseSig {
				if curSig[k] < n {
					preserved += curSig[k]
				} else {
					preserved += n
				}
			}
			baseAll, curAll := map[string]int{}, map[string]int{}
			for _, raw := range baselineAll {
				f := raw.(map[string]any)
				baseAll[sig(f["fieldType"], f["command"], f["payloadFingerprint"], f["visibleText"])]++
			}
			for _, raw := range result["allZoteroFields"].([]any) {
				f := raw.(map[string]any)
				curAll[sig(f["fieldType"], f["command"], f["payloadFingerprint"], f["visibleText"])]++
			}
			preservedAll := 0
			for k, n := range baseAll {
				if curAll[k] < n {
					preservedAll += curAll[k]
				} else {
					preservedAll += n
				}
			}
			if preservedAll != len(baselineAll) {
				errs = append(errs, fmt.Sprintf("baseline Zotero field payload/text count changed: preserved %d/%d", preservedAll, len(baselineAll)))
			}
			result["preservation"] = map[string]any{"requested": true, "baselineCitationCount": len(baselineFields), "preservedCitationCount": preserved, "missingCitationIDs": missing, "changedItemURICitationIDs": changedURI, "changedPayloadCitationIDs": changedPayload, "changedVisibleTextCitationIDs": changedVisible, "baselineAllZoteroFieldCount": len(baselineAll), "preservedAllZoteroFieldCount": preservedAll, "passed": baselineHas && len(missing) == 0 && len(changedURI) == 0 && len(changedPayload) == 0 && len(changedVisible) == 0 && preservedAll == len(baselineAll)}
		}
	}
	if opt.validateKeys {
		for _, raw := range fields {
			f := raw.(map[string]any)
			for i, iraw := range f["citationItems"].([]any) {
				item := iraw.(map[string]any)
				id, ok := item["id"].(string)
				if !ok || strings.TrimSpace(id) == "" || isDigits(id) || !itemKeyRE.MatchString(id) {
					continue
				}
				var mismatch []string
				for _, u := range item["uriItemKeys"].([]any) {
					if u.(string) != id {
						mismatch = append(mismatch, u.(string))
					}
				}
				if len(mismatch) > 0 {
					errs = append(errs, fmt.Sprintf("%s: citationItems[%d].id %q does not match recognized Zotero URI key(s): %s", f["part"], i+1, id, strings.Join(mismatch, ", ")))
				}
			}
		}
	}
	citationIDs := map[string]bool{}
	itemValues := []string{}
	visible := ""
	for _, raw := range fields {
		f := raw.(map[string]any)
		if id, ok := f["citationID"].(string); ok {
			citationIDs[id] = true
		}
		for _, v := range f["itemKeys"].([]any) {
			itemValues = append(itemValues, fmt.Sprint(v))
		}
		for _, v := range f["itemURIs"].([]any) {
			itemValues = append(itemValues, v.(string))
		}
		visible += f["visibleText"].(string) + "\n"
	}
	for _, want := range opt.expectCitationIDs {
		if !citationIDs[want] {
			errs = append(errs, "expected citationID not found: "+want)
		}
	}
	for _, want := range opt.expectItemKeys {
		found := false
		for _, v := range itemValues {
			if v == want || strings.HasSuffix(strings.TrimRight(v, "/"), "/items/"+want) {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, "expected Zotero item key not found: "+want)
		}
	}
	for _, want := range opt.expectVisible {
		if !strings.Contains(visible, want) {
			errs = append(errs, "expected visible citation text not found: "+want)
		}
	}
	result["errors"] = errs
}

func sameSlice(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	ca, cb := map[string]int{}, map[string]int{}
	for _, v := range a {
		ca[fmt.Sprint(v)]++
	}
	for _, v := range b {
		cb[fmt.Sprint(v)]++
	}
	if len(ca) != len(cb) {
		return false
	}
	for k, v := range ca {
		if cb[k] != v {
			return false
		}
	}
	return true
}
func sig(v ...any) string { b, _ := json.Marshal(v); return string(b) }
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func public(v any) any {
	switch x := v.(type) {
	case map[string]any:
		o := map[string]any{}
		for k, val := range x {
			if !strings.HasSuffix(k, "Fingerprint") {
				o[k] = public(val)
			}
		}
		return o
	case []any:
		o := make([]any, len(x))
		for i, val := range x {
			o[i] = public(val)
		}
		return o
	default:
		return v
	}
}

// ValidationError is returned when a document is readable but fails one or
// more validation gates. Callers can inspect the already-emitted report.
type ValidationError struct{ Errors []string }

func (e *ValidationError) Error() string { return strings.Join(e.Errors, "\n") }

func NewCommand() *cobra.Command {
	var baseline, docxPath string
	var minimum, exact, increase int
	var preserve, validateKeys, jsonOut bool
	var expectIDs, expectKeys, expectVisible []string
	root := &cobra.Command{Use: "docx", Short: "Validate Zotero citation fields in DOCX files", SilenceUsage: true, SilenceErrors: true}
	validate := &cobra.Command{Use: "validate FILE", Short: "Validate a DOCX file", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		docxPath = args[0]
		result := inspect(docxPath)
		var base map[string]any
		if baseline != "" {
			base = inspect(baseline)
			if be, ok := base["errors"].([]any); ok {
				for _, e := range be {
					result["errors"] = append(result["errors"].([]any), "baseline: "+fmt.Sprint(e))
				}
			}
		}
		opt := options{preserve: preserve, validateKeys: validateKeys, expectCitationIDs: expectIDs, expectItemKeys: expectKeys, expectVisible: expectVisible}
		if cmd.Flags().Changed("minimum-fields") {
			opt.minimumFields = &minimum
		}
		if cmd.Flags().Changed("expected-field-count") {
			opt.expectedFieldCount = &exact
		}
		if cmd.Flags().Changed("expected-increase") {
			opt.expectedIncrease = &increase
		}
		applyExpectations(result, base, opt)
		if base != nil {
			result["baseline"] = map[string]any{"path": baseline, "zoteroFieldCount": len(base["zoteroFields"].([]any)), "fieldCountIncrease": len(result["zoteroFields"].([]any)) - len(base["zoteroFields"].([]any))}
		}
		result["zoteroFieldCount"] = len(result["zoteroFields"].([]any))
		result["valid"] = len(result["errors"].([]any)) == 0
		out := cmd.OutOrStdout()
		if jsonOut {
			b, e := json.MarshalIndent(public(result), "", "  ")
			if e != nil {
				return e
			}
			if _, err := fmt.Fprintln(out, string(b)); err != nil {
				return err
			}
		} else {
			valid := result["valid"].(bool)
			status := "INVALID"
			if valid {
				status = "OK"
			}
			fmt.Fprintf(out, "%s: %s\nXML/relationships parts parsed: %d\nZotero fields: %d\nAll Zotero fields: %d\n", status, docxPath, result["xmlParts"], result["zoteroFieldCount"], result["allZoteroFieldCount"])
			p := result["portability"].(map[string]any)
			ns := p["libraryNamespaces"].([]any)
			names := "none"
			if len(ns) > 0 {
				v := make([]string, len(ns))
				for i, x := range ns {
					v[i] = x.(string)
				}
				names = strings.Join(v, ", ")
			}
			fmt.Fprintf(out, "Library namespaces: %s\nEmbedded itemData: %d/%d citation item(s)\n", names, p["itemsWithEmbeddedData"], p["citationItemCount"])
			for _, raw := range result["zoteroFields"].([]any) {
				f := raw.(map[string]any)
				fmt.Fprintf(out, "- %s: citationID=%q; items=%d; visible=%q\n", f["part"], f["citationID"], f["itemCount"], f["visibleText"])
			}
			if psv, ok := result["preservation"].(map[string]any); ok {
				outcome := "failed"
				if psv["passed"] == true {
					outcome = "passed"
				}
				fmt.Fprintf(out, "Baseline citation preservation: %s (%d/%d)\n", outcome, psv["preservedCitationCount"], psv["baselineCitationCount"])
			}
			for _, w := range result["warnings"].([]any) {
				fmt.Fprintln(cmd.ErrOrStderr(), "WARNING:", w)
			}
			for _, e := range result["errors"].([]any) {
				fmt.Fprintln(cmd.ErrOrStderr(), "ERROR:", e)
			}
		}
		if result["valid"] != true {
			es := []string{}
			for _, e := range result["errors"].([]any) {
				es = append(es, fmt.Sprint(e))
			}
			return &ValidationError{Errors: es}
		}
		return nil
	}}
	validate.Flags().StringVar(&baseline, "baseline", "", "Original DOCX for count comparison")
	validate.Flags().IntVar(&minimum, "minimum-fields", 0, "")
	validate.Flags().IntVar(&exact, "expected-field-count", 0, "")
	validate.Flags().IntVar(&increase, "expected-increase", 0, "")
	validate.Flags().BoolVar(&preserve, "preserve-baseline-citations", false, "Require baseline citation payloads, visible text, and Zotero fields to survive unchanged")
	validate.Flags().BoolVar(&validateKeys, "validate-key-ids", false, "Validate newly generated key-shaped citation item IDs against recognized URI keys")
	validate.Flags().StringArrayVar(&expectIDs, "expect-citation-id", nil, "")
	validate.Flags().StringArrayVar(&expectKeys, "expect-item-key", nil, "")
	validate.Flags().StringArrayVar(&expectVisible, "expect-visible-text", nil, "")
	validate.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	root.AddCommand(validate)
	return root
}

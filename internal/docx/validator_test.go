package docx

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testWordNS = `http://schemas.openxmlformats.org/wordprocessingml/2006/main`

func citationField(id string, keys []string, visible, namespace string, embedded bool) string {
	items := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		item := map[string]any{"id": key, "uris": []string{"http://zotero.org/" + namespace + "/items/" + key}}
		if embedded {
			item["itemData"] = map[string]any{"id": key, "type": "article-journal", "title": key}
		}
		items = append(items, item)
	}
	data := map[string]any{
		"citationID": id, "properties": map[string]any{"noteIndex": 0},
		"citationItems": items, "schema": "schema",
	}
	b, _ := json.Marshal(data)
	return citationPayloadField(string(b), visible)
}

func citationPayloadField(payload, visible string) string {
	payload = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(payload)
	return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ADDIN ZOTERO_ITEM CSL_CITATION ` + payload + `</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>` + visible + `</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
}

func citationFieldSplit(payload, visible string) string {
	b, _ := json.Marshal(map[string]any{"citationID": "split", "properties": map[string]any{"noteIndex": 0}, "citationItems": []any{map[string]any{"id": "ABCD1234", "uris": []string{"http://zotero.org/users/local/OWNER/items/ABCD1234"}}}, "schema": "schema"})
	p := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(" ADDIN ZOTERO_ITEM CSL_CITATION " + string(b))
	_ = payload
	i := strings.Index(p, `CSL_CITATION`)
	return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve">` + p[:i] + `</w:instrText><w:instrText xml:space="preserve">` + p[i:] + `</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>` + visible + `</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
}

func zoteroOther(command, payload, visible string) string {
	payload = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(payload)
	return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ADDIN ZOTERO_` + command + ` ` + payload + `</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>` + visible + `</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
}

func simpleField(payload, visible string) string {
	b, _ := json.Marshal(map[string]any{"citationID": "simple", "properties": map[string]any{"noteIndex": 0}, "citationItems": []any{map[string]any{"id": "ABCD1234", "uris": []string{"http://zotero.org/users/local/OWNER/items/ABCD1234"}}}, "schema": "schema"})
	return `<w:fldSimple w:instr=" ADDIN ZOTERO_ITEM CSL_CITATION ` + string(b) + `"><w:r><w:t>` + visible + `</w:t></w:r></w:fldSimple>`
}

func writeTestDocx(t *testing.T, name, fields string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	document := `<w:document xmlns:w="` + testWordNS + `"><w:body><w:p>` + fields + `</w:p><w:sectPr/></w:body></w:document>`
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		"word/document.xml":   document,
	}
	for part, content := range parts {
		w, e := z.Create(part)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(content)); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInspectSplitFieldsAndPortability(t *testing.T) {
	p := writeTestDocx(t, "split.docx", citationField("one", []string{"ABCD1234"}, "(A)", "users/111", true)+citationFieldSplit("", "(B)"))
	r := inspect(p)
	if len(r["errors"].([]any)) != 0 {
		t.Fatalf("unexpected errors: %v", r["errors"])
	}
	if got := len(r["zoteroFields"].([]any)); got != 2 {
		t.Fatalf("fields=%d", got)
	}
	pinfo := r["portability"].(map[string]any)
	if pinfo["embeddedDataCoverage"] != "partial" || pinfo["mixedLibraryNamespaces"] != true {
		t.Fatalf("portability=%v", pinfo)
	}
}

func TestPreservationIncludesBibliographyAndForeignFields(t *testing.T) {
	base := writeTestDocx(t, "base.docx", citationField("foreign", []string{"ABCD1234"}, "(A)", "users/111", false)+zoteroOther("BIBL", `{"uncited":[]}`, "References")+zoteroOther("CUSTOM", "opaque-payload", "custom"))
	edited := writeTestDocx(t, "edited.docx", citationField("foreign", []string{"ABCD1234"}, "(A)", "users/111", false)+zoteroOther("BIBL", `{"uncited":[]}`, "References")+zoteroOther("CUSTOM", "opaque-payload", "custom"))
	r := inspect(edited)
	b := inspect(base)
	applyExpectations(r, b, options{preserve: true})
	if len(r["errors"].([]any)) != 0 {
		t.Fatalf("preservation errors: %v", r["errors"])
	}
	p := r["preservation"].(map[string]any)
	if p["preservedAllZoteroFieldCount"] != 3 || p["passed"] != true {
		t.Fatalf("preservation=%v", p)
	}
	missing := writeTestDocx(t, "missing-custom.docx", citationField("foreign", []string{"ABCD1234"}, "(A)", "users/111", false)+zoteroOther("BIBL", `{"uncited":[]}`, "References"))
	r = inspect(missing)
	applyExpectations(r, b, options{preserve: true})
	if len(r["errors"].([]any)) == 0 || !strings.Contains(r["errors"].([]any)[0].(string), "baseline Zotero field payload/text count changed") {
		t.Fatalf("expected foreign-field preservation failure: %v", r["errors"])
	}
}

func TestPreservationDetectsPayloadAndVisibleTextChanges(t *testing.T) {
	base := writeTestDocx(t, "base.docx", citationField("field", []string{"ABCD1234"}, "(A)", "users/111", true))
	edited := writeTestDocx(t, "edited.docx", citationField("field", []string{"ABCD1234"}, "(changed)", "users/111", false))
	r, b := inspect(edited), inspect(base)
	applyExpectations(r, b, options{preserve: true})
	joined := fmtErrors(r["errors"].([]any))
	if !strings.Contains(joined, "baseline citation payload changed") || !strings.Contains(joined, "baseline citation visible text changed") {
		t.Fatalf("errors=%s", joined)
	}
}

func TestValidateKeyIDsAndDamagedXML(t *testing.T) {
	p := writeTestDocx(t, "keys.docx", citationField("field", []string{"ABCD1234"}, "(A)", "users/111", false))
	r := inspect(p)
	applyExpectations(r, nil, options{validateKeys: true})
	if len(r["errors"].([]any)) != 0 {
		t.Fatalf("unexpected key errors: %v", r["errors"])
	}
	bad := writeTestDocx(t, "bad.docx", `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> ADDIN ZOTERO_ITEM CSL_CITATION {</w:instrText></w:r>`)
	r = inspect(bad)
	if len(r["errors"].([]any)) == 0 || !strings.Contains(fmtErrors(r["errors"].([]any)), "incomplete Zotero field") {
		t.Fatalf("expected damaged field error: %v", r["errors"])
	}
	_ = simpleField
}

func fmtErrors(v []any) string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = x.(string)
	}
	return strings.Join(out, "\n")
}

func TestCommandJSON(t *testing.T) {
	p := writeTestDocx(t, "command.docx", citationField("field", []string{"ABCD1234"}, "(A)", "users/111", false))
	cmd := NewCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"validate", p, "--json", "--minimum-fields", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("output: %v", err)
	}
	if report["valid"] != true {
		t.Fatalf("report=%v", report)
	}
}

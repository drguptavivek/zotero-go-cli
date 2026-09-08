package cli

import (
	"bytes"
	"encoding/json"
	"github.com/Epistemic-Technology/zotero/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommandTreeIsLazyAndComplete(t *testing.T) {
	c := NewCommand()
	var out, err bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&err)
	c.SetArgs([]string{"--help"})
	if e := c.Execute(); e != nil {
		t.Fatal(e)
	}
	if out.Len() == 0 {
		t.Fatal("help output is empty")
	}
	for _, path := range [][]string{{"items", "list"}, {"items", "get"}, {"collections", "items"}, {"files", "download"}, {"fulltext", "get"}, {"util", "item-template"}, {"configure", "set"}, {"zotseek"}, {"workflow"}} {
		cur := c
		for _, name := range path {
			var next bool
			for _, child := range cur.Commands() {
				if child.Name() == name {
					cur = child
					next = true
					break
				}
			}
			if !next {
				t.Fatalf("missing command %v", path)
			}
		}
	}
}

func TestPyzoteroLeafCommandParity(t *testing.T) {
	c := NewCommand()
	want := []string{
		"items/list", "items/get", "items/children", "items/count", "items/versions", "items/create", "items/update", "items/delete", "items/add-tags", "items/add-doi", "items/bib", "items/citation", "items/deleted",
		"collections/list", "collections/get", "collections/subcollections", "collections/all", "collections/items", "collections/item-count", "collections/versions", "collections/create", "collections/update", "collections/delete", "collections/add-item", "collections/remove-item", "collections/tags",
		"tags/list", "tags/list-for-item", "tags/delete", "files/download", "files/upload", "files/upload-batch", "search/list", "search/create", "search/delete", "fulltext/get", "fulltext/list-new", "fulltext/set", "groups/list", "util/key-info", "util/last-modified-version", "util/item-types", "util/item-fields", "util/item-type-fields", "util/item-template", "configure/setup", "configure/set", "configure/get", "configure/list-profiles", "configure/current-profile",
	}
	for _, path := range want {
		cur := c
		for _, name := range strings.Split(path, "/") {
			var found bool
			for _, child := range cur.Commands() {
				if child.Name() == name {
					cur = child
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing pyzotero command %s", path)
			}
		}
		if cur.RunE == nil && cur.Run == nil {
			t.Fatalf("command %s has no implementation", path)
		}
	}
}

func TestManifestFlagsAreRegistered(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	rootPath := filepath.Join(filepath.Dir(file), "..", "..", "compatibility", "pyzotero-cli-1.0.0.json")
	raw, e := os.ReadFile(rootPath)
	if e != nil {
		t.Fatal(e)
	}
	var entries []struct {
		Path   []string `json:"path"`
		Params []struct {
			Opts []string `json:"opts"`
		} `json:"params"`
	}
	if e = json.Unmarshal(raw, &entries); e != nil {
		t.Fatal(e)
	}
	root := NewCommand()
	for _, entry := range entries {
		if len(entry.Path) == 0 {
			continue
		}
		cmd := root
		for _, name := range entry.Path {
			found := false
			for _, child := range cmd.Commands() {
				if child.Name() == name {
					cmd = child
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("manifest command missing: %s", strings.Join(entry.Path, "/"))
			}
		}
		usage := strings.Fields(cmd.Use)
		usageArgs := map[string]bool{}
		for _, token := range usage[1:] {
			if strings.HasPrefix(token, "--") {
				continue
			}
			usageArgs[strings.ToLower(strings.Trim(token, "[]..."))] = true
		}
		for _, param := range entry.Params {
			for _, opt := range param.Opts {
				if !strings.HasPrefix(opt, "--") {
					if strings.HasPrefix(opt, "-") {
						continue
					}
					arg := strings.ToLower(strings.Trim(opt, "[]..."))
					if !usageArgs[arg] {
						t.Errorf("%s missing positional %s (Use=%q)", strings.Join(entry.Path, "/"), opt, cmd.Use)
					}
					continue
				}
				name := strings.TrimPrefix(opt, "--")
				if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil {
					t.Errorf("%s missing flag --%s", strings.Join(entry.Path, "/"), name)
				}
			}
		}
	}
}

func runHTTPCommand(t *testing.T, handler http.HandlerFunc, args ...string) (string, int) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("ZOTERO_BASE_URL", server.URL)
	t.Setenv("ZOTERO_API_KEY", "test-secret")
	t.Setenv("ZOTERO_USE_LOCAL", "false")
	cmd := NewCommand()
	var out, err bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&err)
	cmd.SetArgs(append([]string{"--api-key", "test-secret", "--library-id", "123", "--library-type", "user"}, args...))
	e := cmd.Execute()
	if e != nil {
		t.Logf("command error: %v stderr=%q", e, err.String())
		return out.String(), 1
	}
	return out.String(), 0
}

func TestItemsListUsesExactAPIQueryAndOutput(t *testing.T) {
	out, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/123/items" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if got := r.URL.Query()["tag"]; len(got) != 2 || got[0] != "ai" || got[1] != "glaucoma" {
			t.Errorf("tags=%v", got)
		}
		if r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("q") != "glaucoma" || r.URL.Query().Get("qmode") != "everything" {
			t.Errorf("query=%v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"key":"ABC","data":{"title":"Test"}}]`))
	}, "items", "list", "--limit", "2", "--filter-tag", "ai", "--filter-tag", "glaucoma", "--query", "glaucoma", "--qmode", "everything")
	if status != 0 || !strings.Contains(out, "ABC") {
		t.Fatalf("status=%d output=%q", status, out)
	}
}

func TestKeysOutputExtractsJSONMapKeys(t *testing.T) {
	out, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"key":"ABC","data":{"title":"Found"}}]`))
	}, "items", "list", "--output", "keys")
	if status != 0 || strings.TrimSpace(out) != "ABC" {
		t.Fatalf("status=%d keys=%q", status, out)
	}
}

func TestWriteCarriesVersionAndBatchBody(t *testing.T) {
	tmp := t.TempDir() + "/item.json"
	if e := os.WriteFile(tmp, []byte(`{"title":"Updated"}`), 0600); e != nil {
		t.Fatal(e)
	}
	_, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/users/123/items/ABC" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type=%q", r.Header.Get("Content-Type"))
		}
		if got := r.Header.Get("If-Unmodified-Since-Version"); got != "42" {
			t.Errorf("version=%q", got)
		}
		var m map[string]any
		if e := json.NewDecoder(r.Body).Decode(&m); e != nil {
			t.Error(e)
		}
		if m["title"] != "Updated" {
			t.Errorf("body=%v", m)
		}
		w.WriteHeader(http.StatusNoContent)
	}, "items", "update", "ABC", "--from-json", tmp, "--last-modified", "42")
	if status != 0 {
		t.Fatalf("update status=%d", status)
	}
}

func TestPartialWriteFailureReturnsNonZero(t *testing.T) {
	_, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"failed":{"0":{"code":400,"message":"invalid"}}}`))
	}, "items", "create", "--from-json", `{"title":"bad"}`)
	if status == 0 {
		t.Fatal("partial write failure returned success")
	}
}

func TestFulltextSetAndLocalWriteGuard(t *testing.T) {
	tmp := t.TempDir() + "/fulltext.json"
	if e := os.WriteFile(tmp, []byte(`{"content":"hello","indexedPages":1,"totalPages":1}`), 0600); e != nil {
		t.Fatal(e)
	}
	seen := false
	_, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		seen = true
		if r.Method != "PUT" || r.URL.Path != "/users/123/items/ATT/fulltext" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}, "fulltext", "set", "ATT", "--from-json", tmp)
	if status != 0 || !seen {
		t.Fatalf("status=%d seen=%v", status, seen)
	}
	t.Setenv("ZOTERO_USE_LOCAL", "true")
	cmd := NewCommand()
	cmd.SetArgs([]string{"--library-id", "123", "--local", "fulltext", "set", "ATT", "--from-json", tmp})
	if e := cmd.Execute(); e == nil {
		t.Fatal("local write was allowed")
	}
}

func TestBibliographyFormatsArePassedToZotero(t *testing.T) {
	out, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "bibtex" {
			t.Errorf("format=%q", r.URL.Query().Get("format"))
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("@article{ABC}\n"))
	}, "items", "get", "ABC", "--output", "bibtex")
	if status != 0 || !strings.Contains(out, "@article{ABC}") {
		t.Fatalf("status=%d output=%q", status, out)
	}
	out, status = runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "bib" {
			t.Errorf("format=%q", r.URL.Query().Get("format"))
		}
		_, _ = w.Write([]byte("<p>Bibliography</p>"))
	}, "items", "bib", "ABC")
	if status != 0 || !strings.Contains(out, "Bibliography") {
		t.Fatalf("status=%d output=%q", status, out)
	}
}

func TestReadLeafRoutes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		path string
	}{
		{"items/list", []string{"items", "list"}, "/users/123/items"}, {"items/get", []string{"items", "get", "ABC"}, "/users/123/items"}, {"items/children", []string{"items", "children", "ABC"}, "/users/123/items/ABC/children"}, {"items/count", []string{"items", "count"}, "/users/123/items"}, {"items/versions", []string{"items", "versions"}, "/users/123/items"}, {"items/deleted", []string{"items", "deleted", "--since", "1"}, "/users/123/deleted"},
		{"collections/list", []string{"collections", "list"}, "/users/123/collections"}, {"collections/get", []string{"collections", "get", "ABC"}, "/users/123/collections/ABC"}, {"collections/subcollections", []string{"collections", "subcollections", "ABC"}, "/users/123/collections/ABC/collections"}, {"collections/all", []string{"collections", "all"}, "/users/123/collections"}, {"collections/items", []string{"collections", "items", "ABC"}, "/users/123/collections/ABC/items"}, {"collections/item-count", []string{"collections", "item-count", "ABC"}, "/users/123/collections/ABC/items"}, {"collections/versions", []string{"collections", "versions"}, "/users/123/collections"}, {"collections/tags", []string{"collections", "tags", "ABC"}, "/users/123/collections/ABC/tags"},
		{"tags/list", []string{"tags", "list"}, "/users/123/tags"}, {"tags/list-for-item", []string{"tags", "list-for-item", "ABC"}, "/users/123/items/ABC/tags"}, {"search/list", []string{"search", "list"}, "/users/123/searches"}, {"fulltext/get", []string{"fulltext", "get", "ABC"}, "/users/123/items/ABC/fulltext"}, {"fulltext/list-new", []string{"fulltext", "list-new", "--since", "1"}, "/users/123/fulltext"}, {"groups/list", []string{"groups", "list"}, "/users/123/groups"},
		{"util/key-info", []string{"util", "key-info"}, "/keys/test-secret"}, {"util/last-modified-version", []string{"util", "last-modified-version"}, "/users/123/items"}, {"util/item-types", []string{"util", "item-types"}, "/itemTypes"}, {"util/item-fields", []string{"util", "item-fields"}, "/itemFields"}, {"util/item-type-fields", []string{"util", "item-type-fields", "book"}, "/itemTypeFields"}, {"util/item-template", []string{"util", "item-template", "book"}, "/items/new"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			seen := false
			_, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
				seen = true
				if r.URL.Path != tc.path {
					t.Errorf("path=%s want=%s", r.URL.Path, tc.path)
				}
				if tc.name == "items/count" {
					w.Header().Set("Total-Results", "0")
				}
				if tc.name == "util/last-modified-version" {
					w.Header().Set("Last-Modified-Version", "1")
				}
				if tc.name == "files/download" {
					_, _ = w.Write([]byte("pdf"))
					return
				}
				if tc.name == "util/item-template" {
					_, _ = w.Write([]byte(`{}`))
					return
				}
				_, _ = w.Write([]byte(`[]`))
			}, tc.args...)
			if status != 0 || !seen {
				t.Fatalf("status=%d seen=%v", status, seen)
			}
		})
	}
}

func TestCollectionParentCountAndCreateContract(t *testing.T) {
	out, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/123/collections" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("parentCollection") != "ROOT" {
			t.Errorf("parent=%v", r.URL.Query())
		}
		_, _ = w.Write([]byte(`[]`))
	}, "collections", "all", "--parent-collection-id", "ROOT")
	if status != 0 || out == "" {
		t.Fatalf("all status=%d out=%q", status, out)
	}
	_, status = runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/users/123/collections/C/items" {
			t.Errorf("count request=%s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Total-Results", "27")
		_, _ = w.Write([]byte(`[]`))
	}, "collections", "item-count", "C")
	if status != 0 {
		t.Fatalf("count status=%d", status)
	}
	_, status = runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		var body []map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		if len(body) != 1 || body[0]["parentCollection"] != "ROOT" {
			t.Errorf("create body=%v", body)
		}
		_, _ = w.Write([]byte(`{"success":{"0":"K"}}`))
	}, "collections", "create", "--name", "Child", "--parent-id", "ROOT")
	if status != 0 {
		t.Fatalf("create status=%d", status)
	}
}

func TestSuccessfulMalformedJSONResponsesReturnErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "items versions", args: []string{"items", "versions"}},
		{name: "collection item count", args: []string{"collections", "item-count", "COLL"}},
		{name: "collection versions", args: []string{"collections", "versions"}},
		{name: "collection create", args: []string{"collections", "create", "--name", "Child"}},
		{name: "search create", args: []string{"search", "create", "--name", "Saved", "--conditions-json", "[]"}},
		{name: "fulltext list", args: []string{"fulltext", "list-new", "--since", "1"}},
		{name: "groups list", args: []string{"groups", "list"}},
		{name: "key info", args: []string{"util", "key-info"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, status := runHTTPCommand(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid JSON"))
			}, tc.args...)
			if status == 0 {
				t.Fatalf("malformed successful response returned status 0")
			}
		})
	}
}

func TestConfigureSetupWritesCompatibleProfile(t *testing.T) {
	path := t.TempDir() + "/config.ini"
	t.Setenv("ZOT_CONFIG_FILE", path)
	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("123\nuser\nsecret\nfalse\nen-US\n"))
	cmd.SetArgs([]string{"configure", "setup", "--profile", "book"})
	if e := cmd.Execute(); e != nil {
		t.Fatal(e)
	}
	f, e := config.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	sec := f.Sections["profile.book"]
	if sec["library_id"] != "123" || sec["api_key"] != "secret" || f.Active() != "book" {
		t.Fatalf("profile=%v active=%q", sec, f.Active())
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatal("setup output leaked API key")
	}
}

func TestDetectContentTypeUsesExtension(t *testing.T) {
	if got := detectContentType("paper.pdf"); got != "application/pdf" {
		t.Fatalf("pdf type=%q", got)
	}
	if got := detectContentType("scan.unknownext"); got != "application/octet-stream" {
		t.Fatalf("unknown type=%q", got)
	}
}

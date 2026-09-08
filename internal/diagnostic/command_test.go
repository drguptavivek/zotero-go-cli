package diagnostic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Epistemic-Technology/zotero/zotero"
)

func TestStrictAndOptionalFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	for _, strict := range []bool{false, true} {
		c := NewCommand(func() (*zotero.Client, error) { return nil, fmt.Errorf("bad configuration with secret") })
		var b bytes.Buffer
		c.SetOut(&b)
		c.SetErr(&bytes.Buffer{})
		args := []string{"--json", "--zotseek-endpoint", server.URL}
		if strict {
			args = append(args, "--strict")
		}
		c.SetArgs(args)
		e := c.Execute()
		if (e != nil) != strict {
			t.Fatalf("strict=%v err=%v", strict, e)
		}
		var result struct {
			Ready bool `json:"ready"`
		}
		if e := json.Unmarshal(b.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		if result.Ready == strict {
			t.Fatal("wrong readiness")
		}
		if bytes.Contains(b.Bytes(), []byte("secret")) {
			t.Fatal("sensitive error leaked")
		}
	}
}
func TestSkipDoesNotBuildClient(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer s.Close()
	c := NewCommand(func() (*zotero.Client, error) { t.Fatal("skip called client"); return nil, nil })
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--skip-local", "--require-zotseek", "--zotseek-endpoint", s.URL})
	if e := c.Execute(); e == nil {
		t.Fatal("required MCP failure ignored")
	}
}

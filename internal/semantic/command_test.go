package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoveryCallSSE(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
			return
		}
		methods = append(methods, q.Method)
		if q.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "test-session" {
			t.Error("session not preserved")
		}
		if q.Method == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		var result any
		switch q.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "test-session")
			result = map[string]any{"protocolVersion": "2025-03-26"}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "search", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			if q.Params["name"] != "search" {
				t.Error("wrong tool")
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "found"}}}
		default:
			t.Errorf("unexpected method %s", q.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": keepalive\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
		fmt.Fprintf(w, "data: %s\n\n", b)
	}))
	defer server.Close()
	c := &Client{Endpoint: server.URL}
	result, err := c.Call(context.Background(), "search", map[string]any{"query": "glaucoma"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), "found") {
		t.Fatal(string(result))
	}
	if strings.Join(methods, ",") != "initialize,notifications/initialized,tools/list,tools/call" {
		t.Fatal(methods)
	}
}
func TestUnknownToolNotInvoked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		json.NewDecoder(r.Body).Decode(&q)
		if q["method"] != "tools/list" {
			t.Errorf("unexpected invocation %v", q["method"])
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": map[string]any{"tools": []any{}}})
	}))
	defer server.Close()
	c := &Client{Endpoint: server.URL, Protocol: "2026-07-28"}
	_, err := c.Call(context.Background(), "missing", map[string]any{})
	if err == nil {
		t.Fatal("missing tool accepted")
	}
}
func TestMismatchedResponseAndToolErrors(t *testing.T) {
	for _, body := range []string{`{"id":999,"result":{}}`, `{"id":1,"error":{"code":-1,"message":"failed"}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c := &Client{Endpoint: server.URL, Protocol: "2026-07-28"}
		if _, err := c.Tools(context.Background()); err == nil {
			t.Fatal("invalid response accepted")
		}
		server.Close()
	}
}
func TestToolErrorReturnedWithPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		json.NewDecoder(r.Body).Decode(&q)
		result := map[string]any{"tools": []any{map[string]any{"name": "search", "inputSchema": map[string]any{"type": "object"}}}}
		if q["method"] == "tools/call" {
			result = map[string]any{"isError": true, "content": []any{}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": result})
	}))
	defer server.Close()
	c := &Client{Endpoint: server.URL, Protocol: "2026-07-28"}
	b, e := c.Call(context.Background(), "search", map[string]any{})
	if e == nil || len(b) == 0 {
		t.Fatalf("payload=%s err=%v", b, e)
	}
}
func TestRepeatedCursorFails(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		json.NewDecoder(r.Body).Decode(&q)
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": map[string]any{"tools": []any{}, "nextCursor": "same"}})
	}))
	defer s.Close()
	c := &Client{Endpoint: s.URL, Protocol: "2026-07-28"}
	if _, e := c.Tools(context.Background()); e == nil {
		t.Fatal("cursor loop accepted")
	}
}

func TestStatelessMetadataAndNoSession(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		json.NewDecoder(r.Body).Decode(&q)
		if r.Header.Get("Mcp-Session-Id") != "" {
			t.Error("stateless session sent")
		}
		if r.Header.Get("Mcp-Method") != q["method"] {
			t.Error("method header missing")
		}
		p := q["params"].(map[string]any)
		meta, ok := p["_meta"].(map[string]any)
		if !ok || meta["io.modelcontextprotocol/protocolVersion"] != "2026-07-28" {
			t.Error("missing modern metadata")
		}
		result := map[string]any{"tools": []any{map[string]any{"name": "search", "inputSchema": map[string]any{"type": "object"}}}}
		if q["method"] == "tools/call" {
			if r.Header.Get("Mcp-Name") != "search" {
				t.Error("name header missing")
			}
			result = map[string]any{"content": []any{}}
		}
		w.Header().Set("Mcp-Session-Id", "must-not-reuse")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": result})
	}))
	defer s.Close()
	c := &Client{Endpoint: s.URL, Protocol: "2026-07-28"}
	if _, e := c.Call(context.Background(), "search", map[string]any{}); e != nil {
		t.Fatal(e)
	}
}
func TestUnsupportedSchemasAndInteraction(t *testing.T) {
	if !hasCustomHeader(map[string]any{"properties": map[string]any{"x": map[string]any{"x-mcp-header": "X"}}}) {
		t.Fatal("nested custom header missed")
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		json.NewDecoder(r.Body).Decode(&q)
		result := map[string]any{"tools": []any{map[string]any{"name": "search", "inputSchema": map[string]any{"type": "object"}}}}
		if q["method"] == "tools/call" {
			result = map[string]any{"content": []any{}, "resultType": "input_required"}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q["id"], "result": result})
	}))
	defer s.Close()
	c := &Client{Endpoint: s.URL, Protocol: "2026-07-28"}
	if _, e := c.Call(context.Background(), "search", map[string]any{}); e == nil {
		t.Fatal("unsupported interaction accepted")
	}
}
func TestConfiguredResponseLimit(t *testing.T) {
	t.Setenv("ZOTSEEK_MCP_MAX_RESPONSE_BYTES", "20")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	}))
	defer s.Close()
	c := &Client{Endpoint: s.URL, Protocol: "2026-07-28"}
	if _, e := c.Tools(context.Background()); e == nil {
		t.Fatal("limit ignored")
	}
}

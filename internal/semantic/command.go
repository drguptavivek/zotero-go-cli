// Package semantic provides explicit MCP discovery and invocation for ZOTseek.
package semantic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

const maxResponse = 8 << 20

type Client struct {
	Endpoint    string
	Protocol    string
	HTTP        *http.Client
	session     string
	nextID      int
	initialized bool
}

func (c *Client) request(ctx context.Context, method string, params any, notification bool) (json.RawMessage, error) {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid MCP HTTP endpoint")
	}
	limit := maxResponse
	if raw := os.Getenv("ZOTSEEK_MCP_MAX_RESPONSE_BYTES"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n <= 0 || n > 1<<30 {
			return nil, fmt.Errorf("ZOTSEEK_MCP_MAX_RESPONSE_BYTES must be a positive integer <= 1 GiB")
		}
		limit = n
	}
	copied := map[string]any{}
	if p, ok := params.(map[string]any); ok {
		for k, v := range p {
			copied[k] = v
		}
	}
	if c.Protocol == "2026-07-28" {
		copied["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": c.Protocol, "io.modelcontextprotocol/clientInfo": map[string]string{"name": "zotero-go-cli", "version": "0.1.0-rc.2"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	}
	payload := map[string]any{"jsonrpc": "2.0", "method": method, "params": copied}
	c.nextID++
	id := c.nextID
	if !notification {
		payload["id"] = id
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.Protocol != "" {
		req.Header.Set("MCP-Protocol-Version", c.Protocol)
	}
	if c.Protocol == "2026-07-28" {
		req.Header.Set("Mcp-Method", method)
		if method == "tools/call" {
			if name, ok := copied["name"].(string); ok {
				req.Header.Set("Mcp-Name", headerValue(name))
			}
		}
	}
	if c.session != "" && c.Protocol != "2026-07-28" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP HTTP status %d", resp.StatusCode)
	}
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" && c.Protocol != "2026-07-28" {
		c.session = s
	}
	if notification {
		return nil, nil
	}
	limited := io.LimitReader(resp.Body, int64(limit)+1)
	decode := func(b []byte) (json.RawMessage, bool, error) {
		if !utf8.Valid(b) {
			return nil, false, fmt.Errorf("invalid UTF-8 in MCP response")
		}
		var envelope struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			ID      json.RawMessage `json:"id"`
			Result  json.RawMessage `json:"result"`
			Error   json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(b, &envelope); err != nil {
			return nil, false, fmt.Errorf("invalid MCP JSON: %w", err)
		}
		if envelope.JSONRPC != "2.0" {
			return nil, false, fmt.Errorf("malformed JSON-RPC version")
		}
		if envelope.Method != "" && len(envelope.ID) == 0 {
			return nil, false, nil
		}
		if string(envelope.ID) != strconv.Itoa(id) {
			return nil, false, fmt.Errorf("MCP response ID mismatch")
		}
		if (len(envelope.Error) > 0) == (len(envelope.Result) > 0) {
			return nil, false, fmt.Errorf("response must contain exactly one of result/error")
		}
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			return nil, true, fmt.Errorf("MCP error: %s", envelope.Error)
		}
		if len(envelope.Result) == 0 {
			return nil, true, fmt.Errorf("MCP response missing result")
		}
		return envelope.Result, true, nil
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(limited)
		scanner.Buffer(make([]byte, 4096), limit)
		var data []string
		total := 0
		dispatch := func() (json.RawMessage, bool, error) {
			if len(data) == 0 {
				return nil, false, nil
			}
			b := []byte(strings.Join(data, "\n"))
			data = nil
			return decode(b)
		}
		for scanner.Scan() {
			line := scanner.Text()
			total += len(line) + 1
			if total > limit {
				return nil, fmt.Errorf("MCP response exceeds limit")
			}
			if line == "" {
				if r, done, e := dispatch(); done || e != nil {
					return r, e
				}
			} else if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if scanner.Err() != nil {
			return nil, scanner.Err()
		}
		if r, done, e := dispatch(); done || e != nil {
			return r, e
		}
		return nil, fmt.Errorf("MCP stream ended without matching response")
	}
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, fmt.Errorf("MCP response exceeds limit")
	}
	r, done, err := decode(b)
	if err != nil {
		return nil, err
	}
	if !done {
		return nil, fmt.Errorf("MCP response ID mismatch")
	}
	return r, nil
}

func (c *Client) Initialize(ctx context.Context) error {
	if c.initialized {
		return nil
	}
	if c.Protocol == "" {
		c.Protocol = "2025-03-26"
	}
	// The newer stateless mode is explicit; never infer it from a failed handshake.
	if c.Protocol == "2026-07-28" {
		c.initialized = true
		return nil
	}
	result, err := c.request(ctx, "initialize", map[string]any{"protocolVersion": c.Protocol, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "zot-go", "version": "0.1.0-rc.2"}}, false)
	if err != nil {
		return err
	}
	var response struct {
		Protocol string `json:"protocolVersion"`
	}
	if err = json.Unmarshal(result, &response); err != nil {
		return err
	}
	if response.Protocol != c.Protocol {
		return fmt.Errorf("server negotiated unsupported protocol %q", response.Protocol)
	}
	if _, err = c.request(ctx, "notifications/initialized", map[string]any{}, true); err != nil {
		return err
	}
	c.initialized = true
	return nil
}
func (c *Client) Tools(ctx context.Context) (json.RawMessage, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	var tools []json.RawMessage
	cursor := ""
	seen := map[string]bool{}
	names := map[string]bool{}
	pages := 0
	for {
		pages++
		if pages > 128 {
			return nil, fmt.Errorf("tools pagination exceeds limit")
		}
		p := map[string]any{}
		if cursor != "" {
			p["cursor"] = cursor
		}
		r, e := c.request(ctx, "tools/list", p, false)
		if e != nil {
			return nil, e
		}
		var page struct {
			Tools []json.RawMessage `json:"tools"`
			Next  string            `json:"nextCursor"`
		}
		if e = json.Unmarshal(r, &page); e != nil {
			return nil, e
		}
		if page.Tools == nil {
			return nil, fmt.Errorf("tools/list missing tools array")
		}
		for _, raw := range page.Tools {
			var tool struct {
				Name   string         `json:"name"`
				Schema map[string]any `json:"inputSchema"`
			}
			if err := json.Unmarshal(raw, &tool); err != nil || strings.TrimSpace(tool.Name) == "" || tool.Schema == nil {
				return nil, fmt.Errorf("invalid advertised tool")
			}
			if typ, ok := tool.Schema["type"]; ok && typ != "object" {
				return nil, fmt.Errorf("tool schema type must be object")
			}
			if hasCustomHeader(tool.Schema) {
				return nil, fmt.Errorf("unsupported x-mcp-header annotation")
			}
			if names[tool.Name] {
				return nil, fmt.Errorf("duplicate tool name %q", tool.Name)
			}
			names[tool.Name] = true
		}
		tools = append(tools, page.Tools...)
		if page.Next == "" {
			break
		}
		if seen[page.Next] {
			return nil, fmt.Errorf("MCP repeated tools cursor")
		}
		seen[page.Next] = true
		cursor = page.Next
	}
	return json.Marshal(map[string]any{"tools": tools})
}
func (c *Client) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	result, err := c.Tools(ctx)
	if err != nil {
		return nil, err
	}
	var inventory struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err = json.Unmarshal(result, &inventory); err != nil {
		return nil, err
	}
	found := false
	for _, tool := range inventory.Tools {
		if tool.Name == name {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("tool %q not advertised by server", name)
	}
	result, err = c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, false)
	if err != nil {
		return nil, err
	}
	var rawResult map[string]any
	if err := json.Unmarshal(result, &rawResult); err != nil || rawResult == nil {
		return nil, fmt.Errorf("tool result must be an object")
	}
	for _, k := range []string{"inputRequests", "inputRequired", "input_required", "mrtr"} {
		if _, ok := rawResult[k]; ok {
			return nil, fmt.Errorf("unsupported input_required/MRTR interaction")
		}
	}
	if typ, ok := rawResult["resultType"].(string); ok {
		switch strings.ToLower(typ) {
		case "input_required", "inputrequired", "mrtr":
			return nil, fmt.Errorf("unsupported input_required/MRTR interaction")
		}
	}
	var status struct {
		IsError    bool              `json:"isError"`
		Content    []json.RawMessage `json:"content"`
		Structured map[string]any    `json:"structuredContent"`
	}
	if err = json.Unmarshal(result, &status); err != nil {
		return nil, err
	}
	if status.Content == nil && status.Structured == nil {
		return nil, fmt.Errorf("tool result missing content")
	}
	if status.IsError {
		return result, fmt.Errorf("MCP tool reported an error")
	}
	return result, nil
}
func NewCommand() *cobra.Command {
	var endpoint, protocol, arguments string
	var timeout time.Duration
	var namesOnly bool
	root := &cobra.Command{Use: "zotseek", Short: "Discover and invoke local ZOTseek MCP tools"}
	defaultEndpoint := os.Getenv("ZOTSEEK_MCP_URL")
	if defaultEndpoint == "" {
		defaultEndpoint = "http://localhost:23119/zotseek/mcp"
	}
	defaultProtocol := os.Getenv("ZOTSEEK_MCP_PROTOCOL")
	if defaultProtocol == "" {
		defaultProtocol = "2025-03-26"
	}
	root.PersistentFlags().DurationVar(&timeout, "timeout", 15*time.Second, "Request timeout (e.g. 15s)")
	root.PersistentFlags().StringVar(&endpoint, "endpoint", defaultEndpoint, "MCP endpoint")
	root.PersistentFlags().StringVar(&protocol, "protocol", defaultProtocol, "Explicit MCP protocol version")
	run := func(cmd *cobra.Command, args []string) error {
		if protocol != "2025-03-26" && protocol != "2025-06-18" && protocol != "2025-11-25" && protocol != "2026-07-28" {
			return fmt.Errorf("unsupported protocol version")
		}
		if !cmd.Flags().Changed("timeout") {
			if raw := os.Getenv("ZOTSEEK_MCP_TIMEOUT"); raw != "" {
				seconds, err := strconv.ParseFloat(raw, 64)
				if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 86400 {
					return fmt.Errorf("ZOTSEEK_MCP_TIMEOUT must be positive finite seconds <= 86400")
				}
				timeout = time.Duration(seconds * float64(time.Second))
			}
		}
		if timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		c := &Client{Endpoint: endpoint, Protocol: protocol, HTTP: &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
		var r json.RawMessage
		var e error
		if len(args) == 0 {
			r, e = c.Tools(cmd.Context())
		} else {
			var a map[string]any
			if e = json.Unmarshal([]byte(arguments), &a); e != nil {
				return fmt.Errorf("arguments must be a JSON object: %w", e)
			}
			if a == nil {
				return fmt.Errorf("arguments must be a JSON object")
			}
			r, e = c.Call(cmd.Context(), args[0], a)
		}
		if len(r) > 0 && namesOnly && len(args) == 0 {
			var list struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			}
			if err := json.Unmarshal(r, &list); err != nil {
				return err
			}
			for _, tool := range list.Tools {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), tool.Name); err != nil {
					return err
				}
			}
			return e
		}
		if len(r) > 0 {
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, r, "", "  "); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), pretty.String()); err != nil {
				return err
			}
		}
		return e
	}
	listing := &cobra.Command{Use: "tools", Short: "List live tools and their input schemas", Args: cobra.NoArgs, RunE: run}
	listing.Flags().BoolVar(&namesOnly, "names-only", false, "Print only discovered tool names")
	root.AddCommand(listing)
	call := &cobra.Command{Use: "call TOOL", Short: "Call a discovered tool with explicit JSON arguments", Args: cobra.ExactArgs(1), RunE: run}
	call.Flags().StringVar(&arguments, "arguments", "{}", "Tool arguments as a JSON object")
	root.AddCommand(call)
	return root
}

func headerValue(s string) string {
	safe := s == strings.TrimSpace(s) && !strings.HasPrefix(s, "=?base64?")
	for _, r := range s {
		if r < 32 || r > 126 {
			safe = false
		}
	}
	if safe {
		return s
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}
func hasCustomHeader(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["x-mcp-header"]; ok {
			return true
		}
		for _, child := range x {
			if hasCustomHeader(child) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if hasCustomHeader(child) {
				return true
			}
		}
	}
	return false
}

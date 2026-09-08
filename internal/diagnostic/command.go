// Package diagnostic provides read-only readiness checks without Python.
package diagnostic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/Epistemic-Technology/zotero/internal/semantic"
	"github.com/Epistemic-Technology/zotero/zotero"
	"github.com/spf13/cobra"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func NewCommand(factory func() (*zotero.Client, error)) *cobra.Command {
	var strict, skipLocal, requireSemantic, jsonOut bool
	var timeout time.Duration
	var endpoint string
	cmd := &cobra.Command{Use: "doctor", SilenceUsage: true, SilenceErrors: true, Short: "Check executable, Zotero read access, and ZOTseek MCP readiness", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail when Zotero read access is unavailable")
	cmd.Flags().BoolVar(&skipLocal, "skip-local", false, "Skip Zotero API access check")
	cmd.Flags().BoolVar(&requireSemantic, "require-zotseek", false, "Fail when ZOTseek discovery is unavailable")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON report")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "Timeout for each read check")
	defaultEndpoint := os.Getenv("ZOTSEEK_MCP_URL")
	if defaultEndpoint == "" {
		defaultEndpoint = "http://localhost:23119/zotseek/mcp"
	}
	cmd.Flags().StringVar(&endpoint, "zotseek-endpoint", defaultEndpoint, "ZOTseek MCP endpoint")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		checks := []Check{{"runtime", "PASS", runtime.GOOS + "/" + runtime.GOARCH + "; no Python runtime required"}}
		if skipLocal {
			checks = append(checks, Check{"zotero-api", "SKIP", "not requested"})
		} else {
			client, e := factory()
			if e == nil {
				ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
				_, _, e = client.Request(ctx, "GET", "/items", url.Values{"limit": {"1"}}, nil, nil)
				cancel()
			}
			status, detail := "PASS", "read request succeeded; item data not included in diagnostics"
			if e != nil {
				status = "WARN"
				if strict {
					status = "FAIL"
				}
				detail = "Zotero API unavailable; check configuration and desktop connection"
			}
			checks = append(checks, Check{"zotero-api", status, detail})
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		client := &semantic.Client{Endpoint: endpoint}
		tools, e := client.Tools(ctx)
		cancel()
		status, detail := "PASS", "live tools discovered"
		if e != nil {
			status = "WARN"
			if requireSemantic {
				status = "FAIL"
			}
			detail = "ZOTseek MCP unavailable or discovery failed"
		} else {
			var inventory struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			}
			if e = json.Unmarshal(tools, &inventory); e == nil {
				detail = fmt.Sprintf("%d live tools discovered", len(inventory.Tools))
			}
		}
		checks = append(checks, Check{"zotseek-mcp", status, detail})
		summary := map[string]int{"passed": 0, "warnings": 0, "failed": 0, "skipped": 0}
		for _, c := range checks {
			switch c.Status {
			case "PASS":
				summary["passed"]++
			case "WARN":
				summary["warnings"]++
			case "FAIL":
				summary["failed"]++
			case "SKIP":
				summary["skipped"]++
			}
		}
		if jsonOut {
			if e = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"ready": summary["failed"] == 0, "strict": strict, "checks": checks, "summary": summary}); e != nil {
				return e
			}
		} else {
			for _, c := range checks {
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", c.Status, c.Name, c.Detail)
			}
		}
		if summary["failed"] > 0 {
			return fmt.Errorf("readiness checks failed")
		}
		return nil
	}
	return cmd
}

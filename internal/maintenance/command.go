// Package maintenance replaces the skill's update and package-check helpers.
package maintenance

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Epistemic-Technology/zotero/internal/atomicfile"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

var semver = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`)

func version(s string) ([3]uint64, error) {
	var v [3]uint64
	m := semver.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return v, fmt.Errorf("invalid semantic version")
	}
	for i := range v {
		n, e := strconv.ParseUint(m[i+1], 10, 64)
		if e != nil {
			return v, e
		}
		v[i] = n
	}
	return v, nil
}
func newer(a, b string) bool {
	x, _ := version(a)
	y, _ := version(b)
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}
func readVersion(root string) (string, error) {
	b, e := os.ReadFile(filepath.Join(root, "VERSION"))
	if e != nil {
		return "", e
	}
	s := strings.TrimSpace(string(b))
	_, e = version(s)
	return s, e
}
func atomicJSON(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".zot-state-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(append(b, '\n')); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return atomicfile.Replace(name, path)
}
func defaultState() string {
	if p := os.Getenv("ZOTERO_USE_UPDATE_STATE"); p != "" {
		return p
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, ".local", "state")
	}
	return filepath.Join(base, "zotero-use", "update-check.json")
}
func envDefault(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}
func NewCommand() *cobra.Command {
	root := &cobra.Command{Use: "skill", SilenceUsage: true, SilenceErrors: true, Short: "Validate skill packages and check skill updates without Python"}
	root.AddCommand(packageCommand(), updateCommand())
	return root
}
func updateCommand() *cobra.Command {
	var root, remote, statePath string
	var force, jsonOut, verbose, noWrite, strict bool
	var timeout time.Duration
	cmd := &cobra.Command{Use: "check-updates", Short: "Check the zotero-use skill version; never installs updates", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&root, "root", ".", "Skill root containing VERSION")
	cmd.Flags().StringVar(&remote, "remote-url", envDefault("ZOTERO_USE_VERSION_URL", "https://raw.githubusercontent.com/drguptavivek/zotero-use/main/VERSION"), "HTTPS version URL")
	cmd.Flags().StringVar(&statePath, "state-file", defaultState(), "Update state JSON file")
	cmd.Flags().BoolVar(&force, "force", false, "Check even before next scheduled check")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print all outcomes as JSON")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print skipped/error outcomes")
	cmd.Flags().BoolVar(&noWrite, "no-write", false, "Do not save state")
	cmd.Flags().BoolVar(&strict, "strict", false, "Return an error on network or state failures")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "HTTP timeout, e.g. 10s")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		result := map[string]any{"checked": false, "updateAvailable": false, "releaseUrl": "https://github.com/drguptavivek/zotero-use", "reinstallCommand": "npx skills add drguptavivek/zotero-use --skill zotero-use"}
		var problem, stateProblem error
		emit := func() error {
			if stateProblem != nil && problem != stateProblem {
				problem = errors.Join(stateProblem, problem)
			}
			if problem != nil {
				result["error"] = problem.Error()
			}
			if jsonOut {
				if e := json.NewEncoder(cmd.OutOrStdout()).Encode(result); e != nil {
					return e
				}
			} else if result["updateAvailable"] == true {
				fmt.Fprintf(cmd.OutOrStdout(), "zotero-use update available: %v -> %v\nReview: %v\n", result["installedVersion"], result["remoteVersion"], result["releaseUrl"])
			} else if verbose {
				fmt.Fprintf(cmd.OutOrStdout(), "%v\n", result)
			}
			if strict {
				return problem
			}
			return nil
		}
		installed, e := readVersion(root)
		if e != nil {
			problem = e
			return emit()
		}
		result["installedVersion"] = installed
		enabled := true
		switch strings.ToLower(strings.TrimSpace(os.Getenv("ZOTERO_USE_UPDATE_CHECK"))) {
		case "0", "false", "no", "off":
			enabled = false
		}
		result["enabled"] = enabled
		state := map[string]any{}
		if b, e := os.ReadFile(statePath); e == nil {
			if e = json.Unmarshal(b, &state); e != nil || state == nil {
				problem = fmt.Errorf("invalid update state JSON object")
				state = map[string]any{}
			}
		} else if !os.IsNotExist(e) {
			problem = e
		}
		stateProblem = problem
		if problem != nil && strict {
			return emit()
		}
		now := time.Now().UTC()
		next, _ := time.Parse(time.RFC3339, fmt.Sprint(state["nextCheckAt"]))
		due := enabled && (force || state["installedVersion"] != installed || next.IsZero() || !now.Before(next))
		result["due"] = due
		result["nextCheckAt"] = state["nextCheckAt"]
		if !due {
			return emit()
		}
		u, e := url.Parse(remote)
		if e != nil || u.Scheme != "https" || u.Host == "" {
			problem = fmt.Errorf("remote version URL must use HTTPS")
		} else {
			client := &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "https" {
					return fmt.Errorf("HTTPS downgrade refused")
				}
				if len(via) > 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			}}
			req, e := http.NewRequestWithContext(cmd.Context(), http.MethodGet, remote, nil)
			if e != nil {
				problem = e
			} else {
				req.Header.Set("User-Agent", "zotero-go-cli-skill-updater/"+installed)
				resp, e := client.Do(req)
				if e != nil {
					problem = e
				} else {
					b, e := io.ReadAll(io.LimitReader(resp.Body, 129))
					resp.Body.Close()
					if e != nil {
						problem = e
					} else if resp.StatusCode != 200 {
						problem = fmt.Errorf("version server HTTP %d", resp.StatusCode)
					} else if len(b) > 128 {
						problem = fmt.Errorf("version response exceeds limit")
					} else {
						rv := strings.TrimSpace(string(b))
						if _, e = version(rv); e != nil {
							problem = e
						} else {
							j, e := rand.Int(rand.Reader, big.NewInt(3*24*60*60+1))
							if e != nil {
								problem = e
							} else {
								next = now.Add(14*24*time.Hour + time.Duration(j.Int64())*time.Second)
								result["checked"] = true
								result["remoteVersion"] = rv
								result["updateAvailable"] = newer(rv, installed)
								result["nextCheckAt"] = next.Format(time.RFC3339)
							}
						}
					}
				}
			}
		}
		state = map[string]any{"installedVersion": installed}
		if result["checked"] == true {
			state["lastCheckedAt"] = now.Format(time.RFC3339)
			state["remoteVersion"] = result["remoteVersion"]
			state["nextCheckAt"] = result["nextCheckAt"]
		} else {
			next = now.Add(24 * time.Hour)
			result["nextCheckAt"] = next.Format(time.RFC3339)
			state["lastAttemptAt"] = now.Format(time.RFC3339)
			state["nextCheckAt"] = result["nextCheckAt"]
			if problem != nil {
				state["lastError"] = problem.Error()
			}
		}
		if !noWrite {
			if e = atomicJSON(statePath, state); e != nil {
				if problem == nil {
					problem = e
				} else {
					problem = fmt.Errorf("%v; state write: %w", problem, e)
				}
			}
		}
		return emit()
	}
	return cmd
}

var required = []string{"SKILL.md", "VERSION", "CHANGELOG.md", "LICENSE", ".gitignore", "agents/openai.yaml", "references/pyzotero-cli.md", "references/word-docx-citations.md", "references/zotseek-mcp.md", "references/search-retrieve-brainstorm.md", "references/setup-troubleshooting.md", "references/zotero-mcp.md"}
var linkRE = regexp.MustCompile("`((?:references|scripts)/[A-Za-z0-9_.-]+)`")

func Manifest(root string) (map[string]string, error) {
	paths := map[string]bool{}
	for _, p := range required {
		paths[p] = true
	}
	for _, dir := range []string{"scripts", "references", "agents", "tests"} {
		base := filepath.Join(root, dir)
		if _, e := os.Stat(base); os.IsNotExist(e) {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				if d.Name() == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".pyc") {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink not allowed in package: %s", path)
			}
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			paths[filepath.ToSlash(rel)] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	result := map[string]string{}
	for p := range paths {
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		sum := sha256.Sum256(b)
		result[p] = hex.EncodeToString(sum[:])
	}
	return result, nil
}
func CheckPackage(root string) []string {
	var problems []string
	for _, name := range required {
		info, e := os.Stat(filepath.Join(root, name))
		if e != nil || !info.Mode().IsRegular() {
			problems = append(problems, "missing required file: "+name)
		}
	}
	v, e := readVersion(root)
	if e != nil {
		problems = append(problems, "VERSION must be major.minor.patch")
	}
	changelog, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	releases := regexp.MustCompile(`(?m)^## \[([0-9]+\.[0-9]+\.[0-9]+)\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$`).FindAllStringSubmatch(string(changelog), -1)
	if len(releases) == 0 || releases[0][1] != v {
		problems = append(problems, "latest changelog release does not match VERSION")
	}
	if b, e := os.ReadFile(filepath.Join(root, "README.md")); e == nil && !strings.Contains(string(b), "Current version: **"+v+"**") {
		problems = append(problems, "README current version does not match VERSION")
	}
	skill, _ := os.ReadFile(filepath.Join(root, "SKILL.md"))
	parts := strings.SplitN(strings.ReplaceAll(string(skill), "\r\n", "\n"), "---\n", 3)
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if len(parts) != 3 || parts[0] != "" || yaml.Unmarshal([]byte(parts[1]), &meta) != nil || meta.Name != "zotero-use" || len(meta.Description) == 0 || len(meta.Description) > 1024 {
		problems = append(problems, "invalid skill name/description frontmatter")
	}
	for _, m := range linkRE.FindAllStringSubmatch(string(skill), -1) {
		if info, e := os.Stat(filepath.Join(root, m[1])); e != nil || !info.Mode().IsRegular() {
			problems = append(problems, "broken skill reference: "+m[1])
		}
	}
	b, e := os.ReadFile(filepath.Join(root, "agents", "openai.yaml"))
	var agent struct {
		Interface map[string]string `yaml:"interface"`
	}
	if e != nil || yaml.Unmarshal(b, &agent) != nil {
		problems = append(problems, "invalid agent metadata")
	} else {
		for _, k := range []string{"display_name", "short_description", "default_prompt"} {
			if strings.TrimSpace(agent.Interface[k]) == "" {
				problems = append(problems, "missing interface value: "+k)
			}
		}
		if n := len(agent.Interface["short_description"]); n < 25 || n > 64 {
			problems = append(problems, "short_description must contain 25-64 characters")
		}
		if !strings.Contains(agent.Interface["default_prompt"], "$zotero-use") {
			problems = append(problems, "default_prompt must mention $zotero-use")
		}
	}
	return problems
}
func packageCommand() *cobra.Command {
	var root, mirror string
	var jsonOut bool
	cmd := &cobra.Command{Use: "check-package", Short: "Validate skill metadata, references, versions, and optional mirror hashes", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&root, "root", ".", "Skill root")
	cmd.Flags().StringVar(&mirror, "compare-root", "", "Mirror skill root")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON report")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		problems := CheckPackage(root)
		if mirror != "" {
			for _, p := range CheckPackage(mirror) {
				problems = append(problems, "mirror: "+p)
			}
			a, e := Manifest(root)
			if e != nil {
				problems = append(problems, e.Error())
			}
			b, e := Manifest(mirror)
			if e != nil {
				problems = append(problems, e.Error())
			}
			all := map[string]bool{}
			for k := range a {
				all[k] = true
			}
			for k := range b {
				all[k] = true
			}
			for k := range all {
				if a[k] != b[k] {
					problems = append(problems, "mirror differs: "+k)
				}
			}
		}
		sort.Strings(problems)
		if problems == nil {
			problems = []string{}
		}
		if jsonOut {
			if e := json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"valid": len(problems) == 0, "errors": problems}); e != nil {
				return e
			}
		} else if len(problems) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "Package checks passed")
		} else {
			for _, p := range problems {
				fmt.Fprintln(cmd.OutOrStdout(), "ERROR:", p)
			}
		}
		if len(problems) > 0 {
			return fmt.Errorf("package validation failed")
		}
		return nil
	}
	return cmd
}

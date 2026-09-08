// Package cli contains the dependency-free command implementation. The
// command tree intentionally mirrors pyzotero-cli so existing scripts can
// migrate without changing their verbs or option names.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Epistemic-Technology/zotero/internal/config"
	"github.com/Epistemic-Technology/zotero/internal/diagnostic"
	"github.com/Epistemic-Technology/zotero/internal/docx"
	"github.com/Epistemic-Technology/zotero/internal/maintenance"
	"github.com/Epistemic-Technology/zotero/internal/output"
	"github.com/Epistemic-Technology/zotero/internal/semantic"
	"github.com/Epistemic-Technology/zotero/internal/workflow"
	"github.com/Epistemic-Technology/zotero/zotero"
	"github.com/spf13/cobra"
)

const Version = "0.1.0-rc.1"

type app struct {
	settings                                config.Settings
	overrides                               config.Overrides
	file                                    config.File
	client                                  *zotero.Client
	root                                    *cobra.Command
	resolved                                bool
	out                                     io.Writer
	err                                     io.Writer
	profile, apiKey, libraryID, libraryType string
	local, verbose, debug, noInteraction    bool
}

// NewCommand returns a lazy command tree: asking for help or version never
// reads credentials or contacts Zotero.
func NewCommand() *cobra.Command {
	a := &app{out: os.Stdout, err: os.Stderr}
	root := &cobra.Command{Use: "zotero-go-cli", Short: "A CLI for interacting with Zotero libraries", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(a.out)
	root.SetErr(a.err)
	a.root = root
	root.PersistentFlags().StringVar(&a.profile, "profile", "", "configuration profile")
	root.PersistentFlags().StringVar(&a.apiKey, "api-key", "", "Zotero API key")
	root.PersistentFlags().StringVar(&a.libraryID, "library-id", "", "Zotero library ID")
	root.PersistentFlags().StringVar(&a.libraryType, "library-type", "", "user or group")
	root.PersistentFlags().BoolVar(&a.local, "local", false, "use local Zotero (read-only)")
	root.PersistentFlags().BoolVarP(&a.verbose, "verbose", "v", false, "verbose logging")
	root.PersistentFlags().BoolVar(&a.debug, "debug", false, "debug logging")
	root.PersistentFlags().BoolVar(&a.noInteraction, "no-interaction", false, "disable prompts")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		top := cmd
		for top.Parent() != nil && top.Parent() != root {
			top = top.Parent()
		}
		if cmd.Name() == "configure" || cmd.Parent() != nil && cmd.Parent().Name() == "configure" || top.Name() == "zotseek" || top.Name() == "skill" || top.Name() == "docx" {
			return nil
		}
		return a.resolveCommand(cmd)
	}
	root.Version = Version
	root.AddCommand(a.items(), a.collections(), a.tags(), a.files(), a.searches(), a.fulltext(), a.groups(), a.util(), a.configure(), semantic.NewCommand(), workflow.NewCommand(func() (*zotero.Client, error) { return a.clientFor(context.Background()) }), maintenance.NewCommand(), diagnostic.NewCommand(func() (*zotero.Client, error) { return a.clientFor(context.Background()) }), docx.NewCommand())
	return root
}

var _ = (&app{}).configure // keep command groups discoverable in generated help

// flag values use pointers indirectly by checking Changed; this preserves
// profile values when a global option was omitted.
func (a *app) resolve() error {
	f, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	a.file = f
	o := config.Overrides{}
	if x := a.profile; x != "" {
		o.Profile = &x
	}
	if x := a.apiKey; x != "" {
		o.APIKey = &x
	}
	if x := a.libraryID; x != "" {
		o.LibraryID = &x
	}
	if x := a.libraryType; x != "" {
		o.LibraryType = &x
	}
	if a.local {
		o.Local = &a.local
	}
	if a.verbose {
		o.Verbose = &a.verbose
	}
	if a.debug {
		o.Debug = &a.debug
	}
	if a.noInteraction {
		o.NoInteraction = &a.noInteraction
	}
	a.settings, err = config.Resolve(o, config.Environment(), f)
	a.resolved = err == nil
	if err != nil {
		return err
	}
	return nil
}
func (a *app) resolveCommand(cmd *cobra.Command) error {
	if err := a.resolve(); err != nil {
		return err
	}
	// A flag explicitly supplied on the command line wins even when its value
	// is empty or false. This matters for disabling a profile's local mode.
	setBool := func(name string, dst **bool) {
		if cmd.Flags().Changed(name) {
			v, _ := cmd.Flags().GetBool(name)
			*dst = &v
		}
	}
	o := config.Overrides{}
	if cmd.Flags().Changed("profile") {
		v, _ := cmd.Flags().GetString("profile")
		o.Profile = &v
	}
	if cmd.Flags().Changed("api-key") {
		v, _ := cmd.Flags().GetString("api-key")
		o.APIKey = &v
	}
	if cmd.Flags().Changed("library-id") {
		v, _ := cmd.Flags().GetString("library-id")
		o.LibraryID = &v
	}
	if cmd.Flags().Changed("library-type") {
		v, _ := cmd.Flags().GetString("library-type")
		o.LibraryType = &v
	}
	setBool("local", &o.Local)
	setBool("verbose", &o.Verbose)
	setBool("debug", &o.Debug)
	setBool("no-interaction", &o.NoInteraction)
	var err error
	a.settings, err = config.Resolve(o, config.Environment(), a.file)
	a.resolved = err == nil
	return err
}
func (a *app) clientFor(ctx context.Context) (*zotero.Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	if !a.resolved {
		if err := a.resolve(); err != nil {
			return nil, err
		}
	}
	typ := zotero.LibraryTypeUser
	if a.settings.LibraryType == "group" {
		typ = zotero.LibraryTypeGroup
	}
	opts := []zotero.ClientOption{zotero.WithLocale(a.settings.Locale)}
	if a.settings.APIKey != "" && !a.settings.Local {
		opts = append(opts, zotero.WithAPIKey(a.settings.APIKey))
	}
	if a.settings.BaseURL != "" {
		opts = append(opts, zotero.WithBaseURL(a.settings.BaseURL))
	}
	if a.settings.Local {
		opts = append(opts, zotero.WithBaseURL("http://localhost:23119/api"), zotero.WithRateLimit(0))
	}
	a.client = zotero.NewClient(a.settings.LibraryID, typ, opts...)
	return a.client, nil
}
func (a *app) api(cmd *cobra.Command, method, path string, q url.Values, body []byte) ([]byte, error) {
	return a.request(cmd, method, path, q, body, nil)
}
func (a *app) request(cmd *cobra.Command, method, path string, q url.Values, body []byte, headers http.Header) ([]byte, error) {
	c, e := a.clientFor(cmd.Context())
	if e != nil {
		return nil, e
	}
	if method != http.MethodGet && method != http.MethodHead && a.settings.Local {
		return nil, fmt.Errorf("local Zotero mode is read-only; refusing %s %s", method, path)
	}
	if body != nil && len(body) > 0 {
		if headers == nil {
			headers = http.Header{}
		}
		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", "application/json")
		}
	}
	b, _, e := c.Request(cmd.Context(), method, path, q, body, headers)
	return b, e
}
func (a *app) print(v any, format string) error {
	w := a.out
	if a.root != nil {
		w = a.root.OutOrStdout()
	}
	return output.Write(w, v, format)
}

func (a *app) queryFlags(cmd *cobra.Command) (url.Values, string) {
	q := url.Values{}
	for _, n := range []string{"start", "limit", "sort", "direction", "qmode", "query", "since", "filter-item-type"} {
		if cmd.Flags().Changed(n) || n == "direction" {
			v, _ := cmd.Flags().GetString(n)
			if n == "start" || n == "limit" || n == "since" {
				q.Set(map[string]string{"filter-item-type": "itemType", "since": "since", "start": "start", "limit": "limit"}[n], v)
			} else if n == "query" {
				q.Set("q", v)
			} else if n == "filter-item-type" {
				q.Set("itemType", v)
			} else {
				q.Set(n, v)
			}
		}
	}
	tags, _ := cmd.Flags().GetStringSlice("filter-tag")
	if len(tags) > 0 {
		q.Del("tag")
		for _, tag := range tags {
			q.Add("tag", tag)
		}
	}
	fmtv, _ := cmd.Flags().GetString("output")
	if fmtv == "" {
		fmtv = "json"
	}
	if cmd.Flags().Lookup("style") != nil {
		if style, _ := cmd.Flags().GetString("style"); style != "" {
			q.Set("style", style)
		}
	}
	if fmtv == "bibtex" || fmtv == "bib" || fmtv == "csljson" {
		q.Set("format", fmtv)
	}
	if cmd.Flags().Lookup("linkwrap") != nil {
		if v, _ := cmd.Flags().GetBool("linkwrap"); v {
			q.Set("linkwrap", "1")
		}
	}
	return q, fmtv
}
func addCommon(cmd *cobra.Command) {
	cmd.Flags().String("start", "", "offset")
	cmd.Flags().String("limit", "", "result limit")
	cmd.Flags().String("sort", "", "sort field")
	cmd.Flags().String("direction", "asc", "asc or desc")
	cmd.Flags().String("qmode", "", "titleCreatorYear or everything")
	cmd.Flags().StringP("query", "q", "", "quick search")
	cmd.Flags().String("since", "", "library version")
	cmd.Flags().StringSlice("filter-tag", nil, "filter by tag")
	cmd.Flags().String("filter-item-type", "", "filter by item type")
	cmd.Flags().String("output", "json", "json, yaml, table, keys, bibtex, csljson, bib")
}

func (a *app) list(path string, top bool, cmd *cobra.Command, args []string) error {
	q, format := a.queryFlags(cmd)
	if top {
		path += "/top"
	}
	b, e := a.api(cmd, http.MethodGet, path, q, nil)
	if e != nil {
		return e
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return a.print(b, format)
	}
	return a.print(v, format)
}
func leaf(name, short string, run func(*cobra.Command, []string) error) *cobra.Command {
	return &cobra.Command{Use: name, Short: short, Args: cobra.ArbitraryArgs, RunE: run}
}

func (a *app) items() *cobra.Command {
	g := &cobra.Command{Use: "items", Short: "Manage Zotero items"}
	l := leaf("list", "List items", func(c *cobra.Command, x []string) error {
		top, _ := c.Flags().GetBool("top")
		publications, _ := c.Flags().GetBool("publications")
		trash, _ := c.Flags().GetBool("trash")
		deleted, _ := c.Flags().GetBool("deleted")
		nflags := 0
		if top {
			nflags++
		}
		if publications {
			nflags++
		}
		if trash {
			nflags++
		}
		if deleted {
			nflags++
		}
		if nflags > 1 {
			return fmt.Errorf("only one of --top, --publications, --trash, or --deleted may be specified")
		}
		if deleted {
			if !c.Flags().Changed("since") {
				return fmt.Errorf("--deleted requires --since")
			}
			return a.list("/deleted", false, c, x)
		}
		path := "/items"
		if publications {
			if a.settings.LibraryType != "user" {
				return fmt.Errorf("--publications can only be used with a user library")
			}
			path = "/publications/items"
		}
		if trash {
			path += "/trash"
		}
		return a.list(path, top, c, x)
	})
	l.Flags().Bool("top", false, "top-level items")
	l.Flags().Bool("publications", false, "publications")
	l.Flags().Bool("trash", false, "trash")
	l.Flags().Bool("deleted", false, "deleted")
	addCommon(l)
	g.AddCommand(l)
	get := leaf("get ITEM_KEY_OR_ID...", "Retrieve items", func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("at least one item key is required")
		}
		q, f := a.queryFlags(c)
		q.Set("itemKey", strings.Join(args, ","))
		b, e := a.api(c, http.MethodGet, "/items", q, nil)
		if e != nil {
			return e
		}
		var v any
		if json.Unmarshal(b, &v) == nil {
			return a.print(v, f)
		}
		return a.print(b, f)
	})
	addCommon(get)
	get.Flags().String("style", "", "CSL style for bibliography output")
	get.Flags().Bool("linkwrap", false, "wrap bibliography URLs in links")
	g.AddCommand(get)
	deletedCmd := leaf("deleted", "List deleted items", func(c *cobra.Command, args []string) error {
		if !c.Flags().Changed("since") {
			return fmt.Errorf("--since is required")
		}
		return a.list("/deleted", false, c, args)
	})
	deletedCmd.Flags().String("since", "", "library version")
	deletedCmd.Flags().String("output", "json", "output")
	g.AddCommand(deletedCmd)
	ch := leaf("children PARENT_ITEM_KEY_OR_ID", "Get child items", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one parent key is required")
		}
		return a.list("/items/"+url.PathEscape(args[0])+"/children", false, c, args)
	})
	addCommon(ch)
	g.AddCommand(ch)
	co := leaf("count", "Count items", func(c *cobra.Command, _ []string) error {
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		n, e := x.NumItems(c.Context())
		if e != nil {
			return e
		}
		return a.print(map[string]int{"count": n}, "json")
	})
	g.AddCommand(co)
	ver := leaf("versions", "Get item versions", func(c *cobra.Command, _ []string) error {
		q, f := a.queryFlags(c)
		q.Set("format", "versions")
		b, e := a.api(c, http.MethodGet, "/items", q, nil)
		if e != nil {
			return e
		}
		var v any
		json.Unmarshal(b, &v)
		return a.print(v, f)
	})
	ver.Flags().String("output", "json", "json or yaml")
	ver.Flags().String("since", "", "version")
	g.AddCommand(ver)
	create := leaf("create", "Create items", func(c *cobra.Command, _ []string) error { return a.writeJSON(c, "/items", http.MethodPost) })
	create.Flags().String("from-json", "", "JSON file or object")
	create.Flags().String("template", "", "item type template")
	create.Flags().StringArray("field", nil, "field key/value (key=value)")
	create.Flags().String("parent-id", "", "parent item key")
	addCommon(create)
	g.AddCommand(create)
	update := leaf("update ITEM_KEY_OR_ID", "Update item", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one item key is required")
		}
		return a.writeJSON(c, "/items/"+url.PathEscape(args[0]), http.MethodPatch)
	})
	update.Flags().String("from-json", "", "JSON file or object")
	update.Flags().StringArray("field", nil, "field key/value")
	update.Flags().String("last-modified", "", "version or auto")
	addCommon(update)
	g.AddCommand(update)
	del := leaf("delete ITEM_KEY_OR_ID...", "Delete items", func(c *cobra.Command, args []string) error { return a.delete(c, "items", args) })
	del.Flags().String("last-modified", "", "version")
	del.Flags().Bool("force", false, "skip confirmation")
	addCommon(del)
	g.AddCommand(del)
	tags := leaf("add-tags ITEM_KEY_OR_ID TAG_NAMES...", "Add tags", func(c *cobra.Command, args []string) error {
		if len(args) < 2 {
			return fmt.Errorf("item key and tag required")
		}
		b, e := a.api(c, http.MethodGet, "/items/"+url.PathEscape(args[0]), nil, nil)
		if e != nil {
			return e
		}
		var item map[string]any
		if e = json.Unmarshal(b, &item); e != nil {
			return e
		}
		d, _ := item["data"].(map[string]any)
		existing, _ := d["tags"].([]any)
		for _, tag := range args[1:] {
			existing = append(existing, map[string]any{"tag": tag})
		}
		d["tags"] = existing
		body, _ := json.Marshal(d)
		h := http.Header{}
		if version, ok := item["version"].(float64); ok && version > 0 {
			h.Set("If-Unmodified-Since-Version", fmt.Sprint(int(version)))
		}
		_, e = a.request(c, http.MethodPatch, "/items/"+url.PathEscape(args[0]), nil, body, h)
		if e != nil {
			return e
		}
		f, _ := c.Flags().GetString("output")
		if f == "" {
			f = "json"
		}
		return a.print(map[string]any{"status": "success", "item_key": args[0], "tags_added": args[1:]}, f)
	})
	addCommon(tags)
	g.AddCommand(tags)
	adddoi := leaf("add-doi DOIS...", "Create items from DOI metadata", func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("at least one DOI is required")
		}
		for _, d := range args {
			if check, _ := c.Flags().GetBool("check-duplicate"); check {
				existing, e := a.api(c, http.MethodGet, "/items", url.Values{"q": {d}, "qmode": {"everything"}}, nil)
				if e != nil {
					return e
				}
				var found []map[string]any
				if json.Unmarshal(existing, &found) == nil {
					exact := false
					for _, candidate := range found {
						if dta, ok := candidate["data"].(map[string]any); ok && strings.EqualFold(strings.TrimSpace(fmt.Sprint(dta["DOI"])), strings.TrimSpace(d)) {
							exact = true
							break
						}
					}
					if exact {
						continue
					}
				}
			}
			m, e := doiMetadata(c.Context(), d)
			if e != nil {
				return e
			}
			if coll, _ := c.Flags().GetString("collection"); coll != "" {
				m["collections"] = []string{coll}
			}
			body, _ := json.Marshal([]any{m})
			b, e := a.api(c, http.MethodPost, "/items", nil, body)
			if e != nil {
				return e
			}
			var v any
			json.Unmarshal(b, &v)
			format, _ := c.Flags().GetString("output")
			if format == "" {
				format = "json"
			}
			if e = a.print(v, format); e != nil {
				return e
			}
		}
		return nil
	})
	adddoi.Flags().String("collection", "", "collection key")
	adddoi.Flags().Bool("check-duplicate", false, "check duplicate")
	adddoi.Flags().String("output", "json", "output")
	g.AddCommand(adddoi)
	for _, n := range []string{"bib", "citation"} {
		name := n
		cmd := leaf(name+" ITEM_KEY_OR_ID...", "Get bibliography or citation", func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("at least one item key is required")
			}
			q := url.Values{}
			if name == "citation" {
				q.Set("include", "citation")
			} else {
				q.Set("format", name)
			}
			if s, _ := c.Flags().GetString("style"); s != "" {
				q.Set("style", s)
			}
			if lw, _ := c.Flags().GetBool("linkwrap"); lw {
				q.Set("linkwrap", "1")
			}
			q.Set("itemKey", strings.Join(args, ","))
			b, e := a.api(c, http.MethodGet, "/items", q, nil)
			if e != nil {
				return e
			}
			return a.print(b, name)
		})
		cmd.Flags().String("style", "", "CSL style")
		if name == "bib" {
			cmd.Flags().Bool("linkwrap", false, "wrap URLs in links")
		}
		g.AddCommand(cmd)
	}
	return g
}

func (a *app) writeJSON(c *cobra.Command, path, method string) error {
	raw, _ := c.Flags().GetString("from-json")
	templateType, _ := c.Flags().GetString("template")
	if raw == "" && templateType != "" {
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		m, e := x.NewItemTemplate(c.Context(), templateType)
		if e != nil {
			return e
		}
		b, _ := json.Marshal(m)
		raw = string(b)
	}
	if raw == "" {
		raw = "{}"
	}
	if b, e := os.ReadFile(raw); e == nil {
		raw = string(b)
	}
	fields, _ := c.Flags().GetStringArray("field")
	if len(fields) > 0 {
		m := map[string]any{}
		if e := json.Unmarshal([]byte(raw), &m); e != nil {
			return e
		}
		for _, v := range fields {
			k, val, ok := strings.Cut(v, "=")
			if !ok {
				return fmt.Errorf("--field must be key=value")
			}
			m[k] = val
		}
		b, _ := json.Marshal(m)
		raw = string(b)
	}
	if name, _ := c.Flags().GetString("name"); name != "" {
		m := map[string]any{}
		if json.Unmarshal([]byte(raw), &m) == nil {
			m["name"] = name
			b, _ := json.Marshal(m)
			raw = string(b)
		}
	}
	if parent, _ := c.Flags().GetString("parent-id"); parent != "" {
		m := map[string]any{}
		if json.Unmarshal([]byte(raw), &m) == nil {
			if path == "/items" || strings.HasPrefix(path, "/items/") {
				m["parentItem"] = parent
			} else {
				m["parentCollection"] = parent
			}
			b, _ := json.Marshal(m)
			raw = string(b)
		}
	}
	// Zotero batch create endpoints require an array even for one object.
	if method == http.MethodPost && (path == "/items" || path == "/collections") {
		var value any
		if json.Unmarshal([]byte(raw), &value) == nil {
			if _, ok := value.([]any); !ok {
				if m, ok := value.(map[string]any); ok {
					value = []any{m}
					b, _ := json.Marshal(value)
					raw = string(b)
				}
			}
		}
	}
	headers := http.Header{}
	if lm, _ := c.Flags().GetString("last-modified"); lm != "" {
		if lm == "auto" {
			if path != "/items" && path != "/collections" {
				rawItem, e := a.api(c, http.MethodGet, path, nil, nil)
				if e != nil {
					return e
				}
				var item map[string]any
				if e = json.Unmarshal(rawItem, &item); e != nil {
					return e
				}
				if v, ok := item["version"].(float64); ok {
					lm = fmt.Sprint(int(v))
				} else if d, ok := item["data"].(map[string]any); ok {
					if v, ok := d["version"].(float64); ok {
						lm = fmt.Sprint(int(v))
					}
				}
			}
		}
		if lm != "auto" {
			headers.Set("If-Unmodified-Since-Version", lm)
		}
	}
	b, e := a.request(c, method, path, nil, []byte(raw), headers)
	if e != nil {
		return e
	}
	var v any
	if json.Unmarshal(b, &v) == nil {
		format, _ := c.Flags().GetString("output")
		if format == "" {
			format = "json"
		}
		err := a.print(v, format)
		if m, ok := v.(map[string]any); ok {
			if failed, ok := m["failed"].(map[string]any); ok && len(failed) > 0 {
				return fmt.Errorf("Zotero write partially failed (%d item(s))", len(failed))
			}
		}
		return err
	}
	return a.print(b, "json")
}
func (a *app) delete(c *cobra.Command, kind string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("at least one key is required")
	}
	force, _ := c.Flags().GetBool("force")
	if !force {
		if a.noInteraction {
			return fmt.Errorf("--force is required with --no-interaction")
		}
		if err := a.confirm(c, "delete "+kind+" "+strings.Join(args, ",")); err != nil {
			return err
		}
	}
	path := "/" + kind
	if len(args) == 1 {
		path += "/" + url.PathEscape(args[0])
	} else {
		path += "?itemKey=" + url.QueryEscape(strings.Join(args, ","))
	}
	headers := http.Header{}
	if f, _ := c.Flags().GetString("last-modified"); f != "" {
		if f == "auto" && len(args) == 1 {
			raw, e := a.api(c, http.MethodGet, "/"+kind+"/"+url.PathEscape(args[0]), nil, nil)
			if e != nil {
				return e
			}
			var item map[string]any
			if e = json.Unmarshal(raw, &item); e != nil {
				return e
			}
			if v, ok := item["version"].(float64); ok {
				f = fmt.Sprint(int(v))
			}
		}
		if f != "auto" {
			headers.Set("If-Unmodified-Since-Version", f)
		}
	} else {
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		v, e := x.LastModifiedVersion(c.Context())
		if e != nil {
			return e
		}
		headers.Set("If-Unmodified-Since-Version", fmt.Sprint(v))
	}
	_, e := a.request(c, http.MethodDelete, path, nil, nil, headers)
	if e != nil {
		return e
	}
	f, _ := c.Flags().GetString("output")
	if f == "" {
		f = "json"
	}
	return a.print(map[string]any{"status": "success", "keys": args}, f)
}
func (a *app) confirm(c *cobra.Command, label string) error {
	w := a.out
	if a.root != nil {
		w = a.root.OutOrStdout()
	}
	fmt.Fprintf(w, "Confirm %s? [y/N] ", label)
	line, e := bufio.NewReader(c.InOrStdin()).ReadString('\n')
	if e != nil {
		return e
	}
	if strings.ToLower(strings.TrimSpace(line)) != "y" && strings.ToLower(strings.TrimSpace(line)) != "yes" {
		return fmt.Errorf("operation cancelled")
	}
	return nil
}

// doiMetadata resolves a DOI through Crossref into the subset Zotero accepts.
// The network lookup is deliberately kept outside the Zotero client so a
// failed DOI never creates a partial item.
func doiMetadata(ctx context.Context, doi string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.crossref.org/works/"+url.PathEscape(strings.TrimPrefix(strings.TrimSpace(doi), "https://doi.org/")), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DOI lookup %s: %w", doi, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("DOI lookup %s failed with status %d", doi, resp.StatusCode)
	}
	var envelope struct {
		Message map[string]any `json:"message"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return nil, err
	}
	m := map[string]any{"itemType": "journalArticle", "DOI": doi}
	if v, ok := envelope.Message["title"].([]any); ok && len(v) > 0 {
		m["title"] = v[0]
	}
	if v := envelope.Message["container-title"]; v != nil {
		if x, ok := v.([]any); ok && len(x) > 0 {
			m["publicationTitle"] = x[0]
		}
	}
	if v := envelope.Message["publisher"]; v != nil {
		m["publisher"] = v
	}
	if v := envelope.Message["published-print"]; v != nil {
		m["date"] = crossrefDate(v)
	} else if v := envelope.Message["published-online"]; v != nil {
		m["date"] = crossrefDate(v)
	}
	if arr, ok := envelope.Message["author"].([]any); ok {
		creators := []map[string]any{}
		for _, av := range arr {
			if am, ok := av.(map[string]any); ok {
				c := map[string]any{"creatorType": "author", "firstName": am["given"], "lastName": am["family"]}
				creators = append(creators, c)
			}
		}
		m["creators"] = creators
	}
	return m, nil
}
func crossrefDate(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	a, ok := m["date-parts"].([]any)
	if !ok || len(a) == 0 {
		return ""
	}
	p, ok := a[0].([]any)
	if !ok || len(p) == 0 {
		return ""
	}
	s := fmt.Sprint(p[0])
	if len(p) > 1 {
		s += "-" + fmt.Sprintf("%02v", p[1])
	}
	if len(p) > 2 {
		s += "-" + fmt.Sprintf("%02v", p[2])
	}
	return s
}

func (a *app) collections() *cobra.Command {
	g := &cobra.Command{Use: "collections", Short: "Manage Zotero collections"}
	l := leaf("list", "List collections", func(c *cobra.Command, args []string) error {
		top, _ := c.Flags().GetBool("top")
		return a.list("/collections", top, c, args)
	})
	l.Flags().Bool("top", false, "top-level collections")
	addCommon(l)
	g.AddCommand(l)
	get := leaf("get COLLECTION_KEY_OR_ID", "Get collection", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one collection key is required")
		}
		return a.list("/collections/"+url.PathEscape(args[0]), false, c, args)
	})
	addCommon(get)
	g.AddCommand(get)
	sub := leaf("subcollections PARENT_COLLECTION_KEY_OR_ID", "List subcollections", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one collection key is required")
		}
		return a.list("/collections/"+url.PathEscape(args[0])+"/collections", false, c, args)
	})
	addCommon(sub)
	g.AddCommand(sub)
	all := leaf("all", "List all collections", func(c *cobra.Command, args []string) error {
		q, f := a.queryFlags(c)
		if parent, _ := c.Flags().GetString("parent-collection-id"); parent != "" {
			q.Set("parentCollection", parent)
		}
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		v, e := x.All(c.Context(), "/collections", q)
		if e != nil {
			return e
		}
		return a.print(v, f)
	})
	all.Flags().String("parent-collection-id", "", "parent key")
	addCommon(all)
	g.AddCommand(all)
	items := leaf("items COLLECTION_KEY_OR_ID", "List collection items", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one collection key is required")
		}
		top, _ := c.Flags().GetBool("top")
		return a.list("/collections/"+url.PathEscape(args[0])+"/items", top, c, args)
	})
	items.Flags().Bool("top", false, "top-level items")
	addCommon(items)
	g.AddCommand(items)
	count := leaf("item-count COLLECTION_KEY_OR_ID", "Count collection items", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one collection key is required")
		}
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		b, h, e := x.Request(c.Context(), http.MethodGet, "/collections/"+url.PathEscape(args[0])+"/items", url.Values{"limit": {"1"}}, nil, nil)
		if e != nil {
			return e
		}
		count := 0
		if s := h.Get("Total-Results"); s != "" {
			count, _ = strconv.Atoi(s)
		} else {
			var v []any
			json.Unmarshal(b, &v)
			count = len(v)
		}
		return a.print(map[string]int{"count": count}, "json")
	})
	g.AddCommand(count)
	ver := leaf("versions", "Get collection versions", func(c *cobra.Command, args []string) error {
		q, f := a.queryFlags(c)
		q.Set("format", "versions")
		b, e := a.api(c, http.MethodGet, "/collections", q, nil)
		if e != nil {
			return e
		}
		var v any
		json.Unmarshal(b, &v)
		return a.print(v, f)
	})
	ver.Flags().String("output", "json", "output")
	ver.Flags().String("since", "", "version")
	g.AddCommand(ver)
	create := leaf("create", "Create collections", func(c *cobra.Command, args []string) error {
		names, _ := c.Flags().GetStringSlice("name")
		if len(names) == 0 {
			return fmt.Errorf("--name is required")
		}
		arr := make([]map[string]any, len(names))
		parent, _ := c.Flags().GetString("parent-id")
		for i, n := range names {
			arr[i] = map[string]any{"name": n}
			if parent != "" {
				arr[i]["parentCollection"] = parent
			}
		}
		b, _ := json.Marshal(arr)
		r, e := a.api(c, http.MethodPost, "/collections", nil, b)
		if e != nil {
			return e
		}
		var v any
		json.Unmarshal(r, &v)
		format, _ := c.Flags().GetString("output")
		if format == "" {
			format = "json"
		}
		err := a.print(v, format)
		if m, ok := v.(map[string]any); ok {
			if f, ok := m["failed"].(map[string]any); ok && len(f) > 0 {
				return fmt.Errorf("Zotero write partially failed (%d collection(s))", len(f))
			}
		}
		return err
	})
	create.Flags().StringSlice("name", nil, "collection name")
	create.Flags().String("parent-id", "", "parent collection key")
	addCommon(create)
	g.AddCommand(create)
	update := leaf("update COLLECTION_KEY_OR_ID", "Update collection", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one collection key is required")
		}
		return a.writeJSON(c, "/collections/"+url.PathEscape(args[0]), http.MethodPatch)
	})
	update.Flags().String("name", "", "new name")
	update.Flags().String("parent-id", "", "parent key")
	update.Flags().String("from-json", "", "JSON")
	update.Flags().String("last-modified", "", "version")
	addCommon(update)
	g.AddCommand(update)
	del := leaf("delete COLLECTION_KEY_OR_ID...", "Delete collections", func(c *cobra.Command, args []string) error { return a.delete(c, "collections", args) })
	del.Flags().String("last-modified", "", "version")
	del.Flags().Bool("force", false, "skip confirmation")
	addCommon(del)
	g.AddCommand(del)
	for _, add := range []bool{true, false} {
		name := "add-item"
		if !add {
			name = "remove-item"
		}
		membership := leaf(name+" COLLECTION_KEY_OR_ID ITEM_KEY_OR_ID...", "Modify collection membership", func(c *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("collection and item keys required")
			}
			if !add {
				force, _ := c.Flags().GetBool("force")
				if !force {
					if a.noInteraction {
						return fmt.Errorf("--force is required with --no-interaction")
					}
					if e := a.confirm(c, "remove items from collection "+args[0]); e != nil {
						return e
					}
				}
			}
			for _, k := range args[1:] {
				it, e := a.api(c, http.MethodGet, "/items/"+url.PathEscape(k), nil, nil)
				if e != nil {
					return e
				}
				var m map[string]any
				if e = json.Unmarshal(it, &m); e != nil {
					return e
				}
				d, _ := m["data"].(map[string]any)
				var cols []any
				if existing, ok := d["collections"].([]any); ok {
					cols = append(cols, existing...)
				}
				if add {
					cols = append(cols, args[0])
				} else {
					n := []any{}
					for _, x := range cols {
						if x != args[0] {
							n = append(n, x)
						}
					}
					cols = n
				}
				d["collections"] = cols
				b, _ := json.Marshal(d)
				h := http.Header{}
				if version, ok := m["version"].(float64); ok {
					h.Set("If-Unmodified-Since-Version", fmt.Sprint(int(version)))
				}
				if _, e = a.request(c, http.MethodPatch, "/items/"+url.PathEscape(k), nil, b, h); e != nil {
					return e
				}
			}
			f, _ := c.Flags().GetString("output")
			if f == "" {
				f = "json"
			}
			return a.print(map[string]any{"status": "success", "collection": args[0], "items": args[1:], "operation": name}, f)
		})
		addCommon(membership)
		if !add {
			membership.Flags().Bool("force", false, "skip confirmation")
		}
		g.AddCommand(membership)
	}
	tags := leaf("tags COLLECTION_KEY_OR_ID", "Get collection tags", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one collection key required")
		}
		return a.list("/collections/"+url.PathEscape(args[0])+"/tags", false, c, args)
	})
	addCommon(tags)
	g.AddCommand(tags)
	return g
}

func (a *app) tags() *cobra.Command {
	g := &cobra.Command{Use: "tags", Short: "Manage Zotero tags"}
	l := leaf("list", "List tags", func(c *cobra.Command, args []string) error { return a.list("/tags", false, c, args) })
	addCommon(l)
	g.AddCommand(l)
	i := leaf("list-for-item ITEM_KEY", "List item tags", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one item key required")
		}
		return a.list("/items/"+url.PathEscape(args[0])+"/tags", false, c, args)
	})
	addCommon(i)
	g.AddCommand(i)
	d := leaf("delete TAG_NAMES...", "Delete tags", func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("tag required")
		}
		force, _ := c.Flags().GetBool("force")
		if !force {
			if a.noInteraction {
				return fmt.Errorf("--force is required with --no-interaction")
			}
			if e := a.confirm(c, "delete tags "+strings.Join(args, ",")); e != nil {
				return e
			}
		}
		q := url.Values{"tag": []string{strings.Join(args, " || ")}}
		h := http.Header{}
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		v, e := x.LastModifiedVersion(c.Context())
		if e != nil {
			return e
		}
		h.Set("If-Unmodified-Since-Version", fmt.Sprint(v))
		_, e = a.request(c, http.MethodDelete, "/tags", q, nil, h)
		return e
	})
	d.Flags().Bool("force", false, "skip confirmation")
	g.AddCommand(d)
	return g
}

func (a *app) files() *cobra.Command {
	g := &cobra.Command{Use: "files", Short: "Manage Zotero file attachments"}
	d := leaf("download ITEM_KEY_OF_ATTACHMENT", "Download attachment", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one attachment key required")
		}
		fn, _ := c.Flags().GetString("output")
		if fn == "-" {
			b, e := a.api(c, http.MethodGet, "/items/"+url.PathEscape(args[0])+"/file", nil, nil)
			if e != nil {
				return e
			}
			w := a.out
			if a.root != nil {
				w = a.root.OutOrStdout()
			}
			_, e = w.Write(b)
			return e
		}
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		target := fn
		if target == "" {
			target = "."
		}
		if st, err := os.Stat(target); err == nil && st.IsDir() {
			p, e := x.Dump(c.Context(), args[0], "", target)
			if e != nil {
				return e
			}
			fmt.Fprintf(a.root.OutOrStdout(), "File downloaded to: %s\n", p)
			return nil
		}
		dir := filepath.Dir(target)
		if e := os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		p, e := x.Dump(c.Context(), args[0], filepath.Base(target), dir)
		if e != nil {
			return e
		}
		fmt.Fprintf(a.root.OutOrStdout(), "File downloaded to: %s\n", p)
		return nil
	})
	d.Flags().StringP("output", "o", "-", "destination path or -")
	g.AddCommand(d)
	u := leaf("upload PATHS_TO_LOCAL_FILE...", "Upload attachments", func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("at least one path required")
		}
		parent, _ := c.Flags().GetString("parent-item-id")
		fn, _ := c.Flags().GetString("filename")
		contentType, _ := c.Flags().GetString("content-type")
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		var out []any
		for _, p := range args {
			ct := contentType
			if ct == "" {
				ct = detectContentType(p)
			}
			item, e := x.UploadAttachment(c.Context(), parent, p, fn, ct)
			if e != nil {
				return e
			}
			out = append(out, item)
		}
		return a.print(out, "json")
	})
	u.Flags().String("parent-item-id", "", "parent item key")
	u.Flags().String("filename", "", "remote filename")
	u.Flags().String("content-type", "", "MIME type (defaults from file extension)")
	g.AddCommand(u)
	b := leaf("upload-batch", "Upload attachments from JSON manifest", func(c *cobra.Command, args []string) error {
		m, _ := c.Flags().GetString("json")
		raw, e := os.ReadFile(m)
		if e != nil {
			return e
		}
		var entries []map[string]any
		if e = json.Unmarshal(raw, &entries); e != nil {
			return e
		}
		for i, v := range entries {
			p, _ := v["local_path"].(string)
			if p == "" {
				return fmt.Errorf("manifest entry %d missing local_path", i)
			}
			st, e := os.Stat(p)
			if e != nil {
				return fmt.Errorf("manifest entry %d: %w", i, e)
			}
			if !st.Mode().IsRegular() {
				return fmt.Errorf("manifest entry %d is not a regular file", i)
			}
		}
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		result := []any{}
		for _, v := range entries {
			p, _ := v["local_path"].(string)
			parent, _ := v["parent_item_id"].(string)
			fn, _ := v["zotero_filename"].(string)
			contentType, _ := v["content_type"].(string)
			if contentType == "" {
				contentType, _ = v["mime_type"].(string)
			}
			if contentType == "" {
				contentType = detectContentType(p)
			}
			item, e := x.UploadAttachment(c.Context(), parent, p, fn, contentType)
			if e != nil {
				return e
			}
			result = append(result, item)
		}
		return a.print(result, "json")
	})
	b.Flags().String("json", "", "manifest path")
	_ = b.MarkFlagRequired("json")
	g.AddCommand(b)
	return g
}

func detectContentType(path string) string {
	if typ := mime.TypeByExtension(filepath.Ext(path)); typ != "" {
		return typ
	}
	return "application/octet-stream"
}

func (a *app) searches() *cobra.Command {
	g := &cobra.Command{Use: "search", Short: "Manage saved searches"}
	l := leaf("list", "List saved searches", func(c *cobra.Command, args []string) error { return a.list("/searches", false, c, args) })
	addCommon(l)
	g.AddCommand(l)
	cr := leaf("create", "Create saved search", func(c *cobra.Command, args []string) error {
		name, _ := c.Flags().GetString("name")
		cond, _ := c.Flags().GetString("conditions-json")
		if name == "" || cond == "" {
			return fmt.Errorf("--name and --conditions-json are required")
		}
		var conditions any
		if e := json.Unmarshal([]byte(cond), &conditions); e != nil {
			return e
		}
		b, _ := json.Marshal(map[string]any{"name": name, "conditions": conditions})
		r, e := a.api(c, http.MethodPost, "/searches", nil, b)
		if e != nil {
			return e
		}
		var v any
		json.Unmarshal(r, &v)
		f, _ := c.Flags().GetString("output")
		if f == "" {
			f = "json"
		}
		return a.print(v, f)
	})
	cr.Flags().String("name", "", "search name")
	cr.Flags().String("conditions-json", "", "conditions JSON")
	cr.Flags().String("output", "json", "output")
	g.AddCommand(cr)
	del := leaf("delete SEARCH_KEYS...", "Delete saved searches", func(c *cobra.Command, args []string) error { return a.delete(c, "searches", args) })
	del.Flags().Bool("force", false, "skip confirmation")
	g.AddCommand(del)
	return g
}

func (a *app) fulltext() *cobra.Command {
	g := &cobra.Command{Use: "fulltext", Short: "Work with Zotero full text"}
	get := leaf("get ITEM_KEY", "Get full text", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one item key required")
		}
		b, e := a.api(c, http.MethodGet, "/items/"+url.PathEscape(args[0])+"/fulltext", nil, nil)
		if e != nil {
			return e
		}
		if f, _ := c.Flags().GetString("output"); f == "raw_content" {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				if s, ok := m["content"].(string); ok {
					return a.print([]byte(s), "raw")
				}
			}
		}
		var v any
		if json.Unmarshal(b, &v) == nil {
			f, _ := c.Flags().GetString("output")
			if f == "" {
				f = "json"
			}
			return a.print(v, f)
		}
		return a.print(b, "json")
	})
	get.Flags().String("output", "json", "output")
	g.AddCommand(get)
	l := leaf("list-new", "List new full text", func(c *cobra.Command, args []string) error {
		q, f := a.queryFlags(c)
		if q.Get("since") == "" {
			return fmt.Errorf("--since is required")
		}
		b, e := a.api(c, http.MethodGet, "/fulltext", q, nil)
		if e != nil {
			return e
		}
		var v any
		json.Unmarshal(b, &v)
		return a.print(v, f)
	})
	l.Flags().String("since", "", "library version")
	l.Flags().String("output", "json", "output")
	g.AddCommand(l)
	set := leaf("set ITEM_KEY", "Set full text", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("one item key required")
		}
		raw, _ := c.Flags().GetString("from-json")
		b, e := os.ReadFile(raw)
		if e != nil {
			b = []byte(raw)
		}
		var payload map[string]any
		if e = json.Unmarshal(b, &payload); e != nil {
			return e
		}
		if _, ok := payload["content"]; !ok {
			return fmt.Errorf("fulltext payload requires content")
		}
		_, e = a.api(c, http.MethodPut, "/items/"+url.PathEscape(args[0])+"/fulltext", nil, b)
		return e
	})
	set.Flags().String("from-json", "", "JSON payload")
	_ = set.MarkFlagRequired("from-json")
	g.AddCommand(set)
	return g
}

func (a *app) groups() *cobra.Command {
	g := &cobra.Command{Use: "groups", Short: "Manage Zotero groups"}
	l := leaf("list", "List groups", func(c *cobra.Command, args []string) error {
		q, _ := a.queryFlags(c)
		for _, k := range []string{"tag", "itemType", "q", "qmode", "since"} {
			q.Del(k)
		}
		b, e := a.api(c, http.MethodGet, "/users/"+a.settings.LibraryID+"/groups", q, nil)
		if e != nil {
			return e
		}
		var v any
		json.Unmarshal(b, &v)
		f, _ := c.Flags().GetString("output")
		if f == "" {
			f = "json"
		}
		return a.print(v, f)
	})
	addCommon(l)
	g.AddCommand(l)
	return g
}

func (a *app) util() *cobra.Command {
	g := &cobra.Command{Use: "util", Short: "Utility and informational commands"}
	key := leaf("key-info", "Display API key permissions", func(c *cobra.Command, args []string) error {
		b, e := a.api(c, http.MethodGet, "/keys/"+a.settings.APIKey, nil, nil)
		if e != nil {
			return e
		}
		var v any
		json.Unmarshal(b, &v)
		f, _ := c.Flags().GetString("output")
		if f == "" {
			f = "json"
		}
		return a.print(v, f)
	})
	key.Flags().String("output", "json", "output")
	g.AddCommand(key)
	lm := leaf("last-modified-version", "Get last modified version", func(c *cobra.Command, args []string) error {
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		n, e := x.LastModifiedVersion(c.Context())
		if e != nil {
			return e
		}
		return a.print(map[string]int{"version": n}, "json")
	})
	g.AddCommand(lm)
	types := leaf("item-types", "List item types", func(c *cobra.Command, args []string) error {
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		v, e := x.ItemTypes(c.Context(), "")
		if e != nil {
			return e
		}
		f, _ := c.Flags().GetString("output")
		if f == "" {
			f = "json"
		}
		return a.print(v, f)
	})
	types.Flags().String("output", "json", "output")
	g.AddCommand(types)
	fields := leaf("item-fields", "List item fields", func(c *cobra.Command, args []string) error {
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		v, e := x.ItemFields(c.Context(), "")
		if e != nil {
			return e
		}
		f, _ := c.Flags().GetString("output")
		if f == "" {
			f = "json"
		}
		return a.print(v, f)
	})
	fields.Flags().String("output", "json", "output")
	g.AddCommand(fields)
	tf := leaf("item-type-fields ITEM_TYPE", "List fields for item type", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("item type required")
		}
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		v, e := x.ItemTypeFields(c.Context(), args[0], "")
		if e != nil {
			return e
		}
		f, _ := c.Flags().GetString("output")
		if f == "" {
			f = "json"
		}
		return a.print(v, f)
	})
	tf.Flags().String("output", "json", "output")
	g.AddCommand(tf)
	tt := leaf("item-template ITEM_TYPE", "Generate item template", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("item type required")
		}
		x, e := a.clientFor(c.Context())
		if e != nil {
			return e
		}
		v, e := x.NewItemTemplate(c.Context(), args[0])
		if e != nil {
			return e
		}
		if lm, _ := c.Flags().GetString("linkmode"); lm != "" {
			v["linkMode"] = lm
		}
		f, _ := c.Flags().GetString("output")
		if f == "" {
			f = "json"
		}
		return a.print(v, f)
	})
	tt.Flags().String("linkmode", "", "attachment link mode")
	tt.Flags().String("output", "json", "output")
	g.AddCommand(tt)
	return g
}

func (a *app) configure() *cobra.Command {
	g := &cobra.Command{Use: "configure", Short: "Manage zot-cli configuration profiles"}
	setup := leaf("setup", "Set up a profile", func(c *cobra.Command, args []string) error {
		if a.noInteraction {
			return fmt.Errorf("interactive configuration disabled; use configure set")
		}
		p, _ := c.Flags().GetString("profile")
		if p == "" {
			p = "default"
		}
		f, e := config.Load(config.Path())
		if e != nil {
			return e
		}
		sec := f.Sections["default"]
		if p != "default" {
			sec = f.Sections["profile."+p]
		}
		if sec == nil {
			sec = map[string]string{}
		}
		reader := bufio.NewReader(c.InOrStdin())
		read := func(label, key string) string {
			def := sec[key]
			fmt.Fprintf(a.root.OutOrStdout(), "%s [%s]: ", label, def)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				return def
			}
			return line
		}
		library := read("Zotero Library ID", "library_id")
		if library == "" {
			return fmt.Errorf("library ID is required")
		}
		typ := read("Library type (user/group)", "library_type")
		if typ == "" {
			typ = "user"
		}
		if typ != "user" && typ != "group" {
			return fmt.Errorf("library type must be user or group")
		}
		key := read("Zotero API key", "api_key")
		local := read("Use local Zotero (true/false)", "local_zotero")
		if local == "" {
			local = "false"
		}
		locale := read("Locale", "locale")
		if locale == "" {
			locale = "en-US"
		}
		f.Set(p, "library_id", library)
		f.Set(p, "library_type", typ)
		f.Set(p, "api_key", key)
		f.Set(p, "local_zotero", local)
		f.Set(p, "locale", locale)
		f.SetCurrent(p)
		if e = f.Save(config.Path()); e != nil {
			return e
		}
		fmt.Fprintf(a.root.OutOrStdout(), "Profile %q saved.\n", p)
		return nil
	})
	setup.Flags().String("profile", "default", "profile name")
	g.AddCommand(setup)
	set := leaf("set KEY VALUE", "Set a profile value", func(c *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("key and value required")
		}
		p, _ := c.Flags().GetString("profile")
		f, e := config.Load(config.Path())
		if e != nil {
			return e
		}
		f.Set(p, args[0], args[1])
		return f.Save(config.Path())
	})
	set.Flags().String("profile", "default", "profile name")
	g.AddCommand(set)
	get := leaf("get KEY", "Get a profile value", func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("key required")
		}
		p, _ := c.Flags().GetString("profile")
		if p == "" {
			p = "default"
		}
		f, e := config.Load(config.Path())
		if e != nil {
			return e
		}
		v := f.Sections[p][strings.ToLower(args[0])]
		if p != "default" {
			v = f.Sections["profile."+p][strings.ToLower(args[0])]
		}
		if v == "" {
			return fmt.Errorf("key %q not found", args[0])
		}
		if strings.Contains(strings.ToLower(args[0]), "key") {
			v = "[redacted]"
		}
		fmt.Fprintln(a.out, v)
		return nil
	})
	get.Flags().String("profile", "", "profile name")
	g.AddCommand(get)
	lp := leaf("list-profiles", "List profiles", func(c *cobra.Command, args []string) error {
		f, e := config.Load(config.Path())
		if e != nil {
			return e
		}
		for _, p := range f.Profiles() {
			marker := " "
			if p == f.Active() {
				marker = "*"
			}
			fmt.Fprintf(a.out, "%s %s\n", marker, p)
		}
		return nil
	})
	g.AddCommand(lp)
	cp := leaf("current-profile [NAME]", "Get or set current profile", func(c *cobra.Command, args []string) error {
		f, e := config.Load(config.Path())
		if e != nil {
			return e
		}
		if len(args) == 0 {
			fmt.Fprintln(a.out, f.Active())
			return nil
		}
		sec := args[0]
		if sec != "default" {
			sec = "profile." + sec
		}
		if _, ok := f.Sections[sec]; !ok {
			return fmt.Errorf("profile %q not found", args[0])
		}
		f.SetCurrent(args[0])
		if e = f.Save(config.Path()); e != nil {
			return e
		}
		fmt.Fprintln(a.out, args[0])
		return nil
	})
	g.AddCommand(cp)
	return g
}

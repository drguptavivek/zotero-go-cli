// Package config implements the pyzotero-cli compatible profile file and
// command-line/environment precedence used by zot.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const DefaultProfile = "default"

// Settings is the effective configuration for one invocation. APIKey is
// intentionally omitted from String and JSON representations by callers.
type Settings struct {
	Profile       string
	APIKey        string
	LibraryID     string
	LibraryType   string
	Local         bool
	Verbose       bool
	Debug         bool
	NoInteraction bool
	Locale        string
	BaseURL       string
}

// Overrides are values supplied by command-line flags. Pointers preserve the
// difference between an omitted boolean and an explicit false.
type Overrides struct {
	Profile, APIKey, LibraryID, LibraryType *string
	Local, Verbose, Debug, NoInteraction    *bool
}

// File is a tiny INI representation matching ConfigParser's profile layout.
type File struct{ Sections map[string]map[string]string }

func Path() string {
	if p := os.Getenv("ZOT_CONFIG_FILE"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if h, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(h, ".config")
		}
	}
	return filepath.Join(base, "zotcli", "config.ini")
}

func Load(path string) (File, error) {
	f := File{Sections: map[string]map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	section := "default"
	f.Sections[section] = map[string]string{}
	s := bufio.NewScanner(strings.NewReader(string(b)))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := f.Sections[section]; !ok {
				f.Sections[section] = map[string]string{}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		f.Sections[section][strings.TrimSpace(strings.ToLower(k))] = strings.TrimSpace(v)
	}
	return f, s.Err()
}

func (f File) Active() string {
	if s := f.Sections["zotcli"]["current_profile"]; s != "" {
		return s
	}
	return DefaultProfile
}
func (f File) section(profile string) map[string]string {
	if profile == "" {
		profile = f.Active()
	}
	if profile == DefaultProfile {
		return f.Sections["default"]
	}
	return f.Sections["profile."+profile]
}
func (f File) Profiles() []string {
	seen := map[string]bool{}
	for s := range f.Sections {
		if s == "default" {
			seen[DefaultProfile] = true
		}
		if strings.HasPrefix(s, "profile.") {
			seen[strings.TrimPrefix(s, "profile.")] = true
		}
	}
	if len(seen) == 0 {
		return []string{DefaultProfile}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (f *File) Set(profile, key, value string) {
	if profile == "" {
		profile = DefaultProfile
	}
	sec := profile
	if profile != DefaultProfile {
		sec = "profile." + profile
	}
	if f.Sections == nil {
		f.Sections = map[string]map[string]string{}
	}
	if f.Sections[sec] == nil {
		f.Sections[sec] = map[string]string{}
	}
	f.Sections[sec][strings.ToLower(key)] = value
}
func (f *File) SetCurrent(profile string) {
	if f.Sections == nil {
		f.Sections = map[string]map[string]string{}
	}
	if f.Sections["zotcli"] == nil {
		f.Sections["zotcli"] = map[string]string{}
	}
	f.Sections["zotcli"]["current_profile"] = profile
}

func (f File) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	var b strings.Builder
	secs := make([]string, 0, len(f.Sections))
	for s := range f.Sections {
		secs = append(secs, s)
	}
	sort.Strings(secs)
	for i, s := range secs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("[" + s + "]\n")
		keys := make([]string, 0, len(f.Sections[s]))
		for k := range f.Sections[s] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(k + " = " + f.Sections[s][k] + "\n")
		}
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".zotcli-config-*")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	defer os.Remove(tmp)
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		return err
	}
	if _, err := tmpFile.WriteString(b.String()); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Resolve(o Overrides, env map[string]string, f File) (Settings, error) {
	p := f.Active()
	if o.Profile != nil && *o.Profile != "" {
		p = *o.Profile
	}
	sec := f.section(p)
	s := Settings{Profile: p, LibraryType: "user", Locale: "en-US", BaseURL: "https://api.zotero.org"}
	s.APIKey = sec["api_key"]
	s.LibraryID = sec["library_id"]
	if sec["library_type"] != "" {
		s.LibraryType = sec["library_type"]
	}
	if sec["locale"] != "" {
		s.Locale = sec["locale"]
	}
	if sec["base_url"] != "" {
		s.BaseURL = sec["base_url"]
	}
	if v, err := strconv.ParseBool(sec["local_zotero"]); err == nil {
		s.Local = v
	}
	if v := env["ZOTERO_API_KEY"]; v != "" {
		s.APIKey = v
	}
	if v := env["ZOTERO_LIBRARY_ID"]; v != "" {
		s.LibraryID = v
	}
	if v := env["ZOTERO_LIBRARY_TYPE"]; v == "user" || v == "group" {
		s.LibraryType = v
	}
	if v := env["ZOTERO_LOCALE"]; v != "" {
		s.Locale = v
	}
	if v := env["ZOTERO_BASE_URL"]; v != "" {
		s.BaseURL = v
	}
	if v := env["ZOTERO_USE_LOCAL"]; v != "" {
		if b, e := strconv.ParseBool(v); e == nil {
			s.Local = b
		}
	}
	if o.APIKey != nil {
		s.APIKey = *o.APIKey
	}
	if o.LibraryID != nil {
		s.LibraryID = *o.LibraryID
	}
	if o.LibraryType != nil {
		s.LibraryType = *o.LibraryType
	}
	if o.Local != nil {
		s.Local = *o.Local
	}
	if o.Verbose != nil {
		s.Verbose = *o.Verbose
	}
	if o.Debug != nil {
		s.Debug = *o.Debug
	}
	if o.NoInteraction != nil {
		s.NoInteraction = *o.NoInteraction
	}
	if s.LibraryType != "user" && s.LibraryType != "group" {
		return s, fmt.Errorf("library type must be user or group")
	}
	if !s.Local && s.APIKey == "" {
		return s, fmt.Errorf("API key is required unless --local is set")
	}
	if s.LibraryID == "" {
		return s, fmt.Errorf("library ID is required")
	}
	return s, nil
}

func Environment() map[string]string {
	out := map[string]string{}
	for _, k := range []string{"ZOTERO_API_KEY", "ZOTERO_LIBRARY_ID", "ZOTERO_LIBRARY_TYPE", "ZOTERO_LOCALE", "ZOTERO_BASE_URL", "ZOTERO_USE_LOCAL"} {
		out[k] = os.Getenv(k)
	}
	return out
}

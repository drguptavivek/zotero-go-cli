package config

import "testing"

func TestResolvePrecedenceAndLocalMode(t *testing.T) {
	f := File{Sections: map[string]map[string]string{"default": {"api_key": "profile-secret", "library_id": "1", "library_type": "user", "local_zotero": "false"}}}
	o := Overrides{APIKey: stringPtr("flag-secret"), LibraryType: stringPtr("group")}
	s, err := Resolve(o, map[string]string{"ZOTERO_API_KEY": "env-secret", "ZOTERO_LIBRARY_ID": "2"}, f)
	if err != nil {
		t.Fatal(err)
	}
	if s.APIKey != "flag-secret" || s.LibraryID != "2" || s.LibraryType != "group" {
		t.Fatalf("unexpected settings: %+v", s)
	}
	local := true
	s, err = Resolve(Overrides{Local: &local}, map[string]string{"ZOTERO_API_KEY": "env-secret", "ZOTERO_LIBRARY_ID": "3"}, f)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Local || s.APIKey != "env-secret" {
		t.Fatalf("local/env resolution failed: %+v", s)
	}
}

func stringPtr(s string) *string { return &s }

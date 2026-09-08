package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteFormatsAndKeys(t *testing.T) {
	data := []map[string]any{{"key": "ABC", "title": "A"}, {"key": "DEF", "title": "B"}}
	for _, format := range []string{"json", "yaml", "table", "keys"} {
		var b bytes.Buffer
		if err := Write(&b, data, format); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if b.Len() == 0 {
			t.Fatalf("%s emitted no output", format)
		}
	}
	var b bytes.Buffer
	if err := Write(&b, data, "keys"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "ABC") || !strings.Contains(b.String(), "DEF") {
		t.Fatalf("keys output: %q", b.String())
	}
}

func TestTableNilMapValuesAndRawValidation(t *testing.T) {
	data := []map[string]any{{"key": "A", "value": nil}}
	var b bytes.Buffer
	if err := Write(&b, data, "table"); err != nil {
		t.Fatal(err)
	}
	if err := Write(&b, []byte("x"), "unknown"); err == nil {
		t.Fatal("unknown byte format accepted")
	}
	if err := Write(&b, []byte("x"), "raw"); err != nil {
		t.Fatal(err)
	}
}

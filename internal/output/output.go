package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

var formats = map[string]bool{"json": true, "yaml": true, "table": true, "keys": true, "bibtex": true, "csljson": true, "bib": true, "raw": true, "citation": true}

func Valid(f string) bool { return formats[f] }

// Write emits the formats accepted by pyzotero-cli. Raw citation responses
// are preserved when the API already returned a non-JSON representation.
func Write(w io.Writer, value any, format string) error {
	if format == "" {
		format = "json"
	}
	if !Valid(format) {
		return fmt.Errorf("unsupported output format %q", format)
	}
	if b, ok := value.([]byte); ok && format != "json" {
		_, err := w.Write(b)
		return err
	}
	switch format {
	case "json", "csljson":
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(value); err != nil {
			return err
		}
		_, err := w.Write(b.Bytes())
		return err
	case "yaml":
		b, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	case "keys":
		for _, k := range Keys(value) {
			if _, err := fmt.Fprintln(w, k); err != nil {
				return err
			}
		}
		return nil
	case "bibtex", "bib":
		return writeBibliography(w, value, format == "bibtex")
	default:
		return writeTable(w, value)
	}
}

func Keys(value any) []string {
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Slice {
		return nil
	}
	out := []string{}
	for i := 0; i < v.Len(); i++ {
		x := v.Index(i)
		if x.Kind() == reflect.Interface {
			x = x.Elem()
		}
		if x.Kind() == reflect.Pointer {
			x = x.Elem()
		}
		if x.Kind() == reflect.Map {
			for _, n := range []string{"key", "id"} {
				f := x.MapIndex(reflect.ValueOf(n))
				if f.IsValid() && f.Kind() == reflect.Interface {
					if f.IsNil() {
						continue
					}
					f = f.Elem()
				}
				if f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
					out = append(out, f.String())
					break
				}
				if f.IsValid() && (f.Kind() == reflect.Int || f.Kind() == reflect.Float64) {
					out = append(out, fmt.Sprint(f.Interface()))
					break
				}
			}
			continue
		}
		if x.Kind() != reflect.Struct {
			continue
		}
		for _, n := range []string{"Key", "ID"} {
			f := x.FieldByName(n)
			if f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
				out = append(out, f.String())
				break
			}
			if f.IsValid() && f.Kind() == reflect.Int {
				out = append(out, fmt.Sprint(f.Int()))
				break
			}
		}
	}
	return out
}

func writeBibliography(w io.Writer, v any, bibtex bool) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice {
		return nil
	}
	for i := 0; i < rv.Len(); i++ {
		x := rv.Index(i)
		if x.Kind() == reflect.Pointer {
			x = x.Elem()
		}
		if x.Kind() == reflect.Interface {
			x = x.Elem()
		}
		if x.Kind() == reflect.Map {
			b, _ := json.Marshal(x.Interface())
			fmt.Fprintln(w, string(b))
			continue
		}
		data := x.FieldByName("Data")
		if data.IsValid() {
			title := data.FieldByName("Title")
			if title.IsValid() && title.String() != "" {
				if bibtex {
					fmt.Fprintf(w, "@article{%s,\n  title = {%s}\n}\n\n", keyOf(x), title.String())
				} else {
					fmt.Fprintf(w, "%s.\n", title.String())
				}
				continue
			}
		}
		b, _ := json.Marshal(x.Interface())
		fmt.Fprintln(w, string(b))
	}
	return nil
}
func keyOf(v reflect.Value) string {
	f := v.FieldByName("Key")
	if f.IsValid() {
		return f.String()
	}
	return "item"
}

func writeTable(w io.Writer, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice {
		return Write(w, v, "json")
	}
	if rv.Len() > 0 {
		x := rv.Index(0)
		if x.Kind() == reflect.Interface {
			x = x.Elem()
		}
		if x.Kind() == reflect.Map {
			keys := []string{}
			for _, k := range x.MapKeys() {
				if k.Kind() == reflect.String {
					keys = append(keys, k.String())
				}
			}
			sort.Strings(keys)
			fmt.Fprintln(w, strings.Join(keys, "\t"))
			for i := 0; i < rv.Len(); i++ {
				y := rv.Index(i)
				if y.Kind() == reflect.Interface {
					y = y.Elem()
				}
				vals := []string{}
				for _, k := range keys {
					f := y.MapIndex(reflect.ValueOf(k))
					if f.IsValid() {
						if f.Kind() == reflect.Interface {
							if f.IsNil() {
								vals = append(vals, "")
								continue
							}
							f = f.Elem()
						}
						if !f.IsValid() {
							vals = append(vals, "")
							continue
						}
						vals = append(vals, fmt.Sprint(f.Interface()))
					} else {
						vals = append(vals, "")
					}
				}
				fmt.Fprintln(w, strings.Join(vals, "\t"))
			}
			return nil
		}
	}
	rows := [][]string{}
	headers := []string{}
	for i := 0; i < rv.Len(); i++ {
		x := rv.Index(i)
		if x.Kind() == reflect.Pointer {
			x = x.Elem()
		}
		if x.Kind() != reflect.Struct {
			continue
		}
		if d := x.FieldByName("Data"); d.IsValid() && d.Kind() == reflect.Struct {
			x = d
		}
		if len(headers) == 0 {
			for _, n := range []string{"Key", "Title", "Name", "ItemType", "Date"} {
				f := x.FieldByName(n)
				if f.IsValid() {
					headers = append(headers, n)
				}
			}
			if len(headers) == 0 {
				for j := 0; j < x.NumField(); j++ {
					headers = append(headers, x.Type().Field(j).Name)
				}
			}
		}
		row := []string{}
		for _, h := range headers {
			f := x.FieldByName(h)
			if f.IsValid() {
				row = append(row, fmt.Sprint(f.Interface()))
			} else {
				row = append(row, "")
			}
		}
		rows = append(rows, row)
	}
	if len(headers) == 0 {
		return nil
	}
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return nil
}

func MapKeysSorted(m map[string]any) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}

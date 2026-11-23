package output_test

import (
	"bytes"
	"testing"

	"github.com/theorigamicorporation/rso-cli/internal/output"
)

type item struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func render(t *testing.T, format output.Format, view output.View) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := output.Write(&buffer, format, view); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}

func TestFormats(t *testing.T) {
	view := output.View{
		Data:    []item{{"a", 1}, {"bb", 22}},
		Columns: []string{"NAME", "COUNT"},
		Rows:    [][]string{{"a", "1"}, {"bb", ""}},
	}
	if got, want := render(t, output.Table, view), "NAME  COUNT\na     1\nbb    -\n"; got != want {
		t.Errorf("table:\n%q\nwant\n%q", got, want)
	}
	if got, want := render(t, output.JSON, view), "[\n  {\n    \"name\": \"a\",\n    \"count\": 1\n  },\n  {\n    \"name\": \"bb\",\n    \"count\": 22\n  }\n]\n"; got != want {
		t.Errorf("json:\n%q", got)
	}
	// YAML uses the JSON field names, so both formats agree.
	if got, want := render(t, output.YAML, view), "- count: 1\n  name: a\n- count: 22\n  name: bb\n"; got != want {
		t.Errorf("yaml:\n%q", got)
	}
}

func TestEmptyTableSaysSo(t *testing.T) {
	if got := render(t, output.Table, output.View{Columns: []string{"NAME"}, Empty: "No queues yet."}); got != "No queues yet.\n" {
		t.Errorf("%q", got)
	}
	if got := render(t, output.JSON, output.View{Data: []item{}}); got != "[]\n" {
		t.Errorf("%q", got)
	}
}

func TestFieldsAndHTMLAreNotEscaped(t *testing.T) {
	got := render(t, output.Table, output.View{Fields: [][2]string{{"Name", "a"}, {"Reason", ""}}})
	if got != "Name:    a\nReason:  -\n" {
		t.Errorf("%q", got)
	}
	if got := render(t, output.JSON, output.View{Data: map[string]string{"url": "https://example.com/?a=1&b=2"}}); got != "{\n  \"url\": \"https://example.com/?a=1&b=2\"\n}\n" {
		t.Errorf("%q", got)
	}
}

func TestParseFormat(t *testing.T) {
	for value, want := range map[string]output.Format{"": output.Table, "table": output.Table, "JSON": output.JSON, "yml": output.YAML} {
		if got, err := output.ParseFormat(value); err != nil || got != want {
			t.Errorf("%q: %v, %v", value, got, err)
		}
	}
	if _, err := output.ParseFormat("xml"); err == nil {
		t.Error("xml was accepted")
	}
}

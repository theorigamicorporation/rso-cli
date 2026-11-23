// Package output prints what a command produced: a table for people, JSON or
// YAML for programs.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format is how a result is written.
type Format string

const (
	Table Format = "table"
	JSON  Format = "json"
	YAML  Format = "yaml"
)

// ParseFormat reads the value of --output.
func ParseFormat(value string) (Format, error) {
	switch Format(strings.ToLower(value)) {
	case Table, "":
		return Table, nil
	case JSON:
		return JSON, nil
	case YAML, "yml":
		return YAML, nil
	}
	return "", fmt.Errorf("unknown output format %q: use table, json or yaml", value)
}

// View is a result in both shapes: Data for json and yaml, and columns with
// rows for the table. Rows nil with Fields set prints one object as a list of
// fields.
type View struct {
	Data    any
	Columns []string
	Rows    [][]string
	// Fields is a single object as label and value pairs.
	Fields [][2]string
	// Empty is what the table says when there are no rows.
	Empty string
}

// Write prints the view in the format asked for.
func Write(w io.Writer, format Format, view View) error {
	switch format {
	case JSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		return encoder.Encode(view.Data)
	case YAML:
		// Through JSON first so both formats use the same field names.
		raw, err := json.Marshal(view.Data)
		if err != nil {
			return err
		}
		var generic any
		if err := yaml.Unmarshal(raw, &generic); err != nil {
			return err
		}
		encoder := yaml.NewEncoder(w)
		encoder.SetIndent(2)
		if err := encoder.Encode(generic); err != nil {
			return err
		}
		return encoder.Close()
	}
	return writeTable(w, view)
}

func writeTable(w io.Writer, view View) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if view.Fields != nil {
		for _, field := range view.Fields {
			if _, err := fmt.Fprintf(tw, "%s:\t%s\n", field[0], dash(field[1])); err != nil {
				return err
			}
		}
		return tw.Flush()
	}
	if len(view.Rows) == 0 {
		empty := view.Empty
		if empty == "" {
			empty = "Nothing to show."
		}
		_, err := fmt.Fprintln(w, empty)
		return err
	}
	if _, err := fmt.Fprintln(tw, strings.Join(view.Columns, "\t")); err != nil {
		return err
	}
	for _, row := range view.Rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = dash(cell)
		}
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// dash stands in for an empty cell, so columns stay readable and parseable.
func dash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

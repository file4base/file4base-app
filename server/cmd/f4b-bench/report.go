package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// report prints the results of several runs side by side, one row per
// measured value, one column per run, as a Markdown table.
func report(w io.Writer, files []string) error {
	var names []string
	values := map[string]map[string]string{}
	var keys []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var res map[string]any
		if err := json.Unmarshal(raw, &res); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		name := f
		if meta, ok := res["meta"].(map[string]any); ok {
			name = fmt.Sprint(meta["engine"])
		}
		names = append(names, name)
		flat := map[string]string{}
		flatten("", res, flat)
		for k, v := range flat {
			if values[k] == nil {
				values[k] = map[string]string{}
				keys = append(keys, k)
			}
			values[k][name] = v
		}
	}
	slices.Sort(keys)
	fmt.Fprintf(w, "| measure | %s |\n|---|%s\n", strings.Join(names, " | "), strings.Repeat("---|", len(names)))
	for _, k := range keys {
		row := make([]string, len(names))
		for i, n := range names {
			row[i] = strings.ReplaceAll(values[k][n], "|", "\\|")
		}
		fmt.Fprintf(w, "| %s | %s |\n", k, strings.Join(row, " | "))
	}
	return nil
}

func flatten(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, x, out)
		}
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = fmt.Sprint(x)
		}
		out[prefix] = strings.Join(parts, ", ")
	default:
		out[prefix] = fmt.Sprint(t)
	}
}

package dataio

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

// node is an XML element read whole: its name, its attributes and its
// children, in the order they appeared.
type node struct {
	Name     string
	Attrs    map[string]string
	Text     string
	Children []*node
}

// parseXML reads records out of an XML file.
//
// Two shapes are understood:
//
//   - **FMPXMLRESULT**, which FileMaker writes: the field names are in
//     METADATA and the rows in RESULTSET/ROW/COL/DATA.
//   - **A repeating element**: whichever element appears most often with child
//     elements of its own is taken as the record, and its children are the
//     fields. `<customers><customer><name>…</name></customer>…</customers>` is
//     the usual shape and reads without being told anything.
//
// Attributes of a record element are read as fields too, named `@attr`, so an
// `id="3"` is not lost.
func parseXML(data []byte, opts Options) (*Table, error) {
	root, err := decodeXML(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}

	if table, ok := parseFMPXMLResult(root, opts); ok {
		return table, nil
	}

	element := strings.TrimSpace(opts.RecordElement)
	if element == "" {
		element = mostRepeatedElement(root)
	}
	if element == "" {
		return nil, fmt.Errorf("%w: no element repeats, so there are no records to read",
			ErrInvalidSource)
	}

	records := make([]*node, 0, 64)
	collect(root, element, &records)
	if len(records) == 0 {
		return nil, fmt.Errorf("%w: no <%s> elements", ErrInvalidSource, element)
	}

	// Every field any record has, in the order they were first seen, so the
	// columns read the way the file does.
	columns := make([]string, 0, 16)
	seen := map[string]bool{}
	for _, record := range records {
		for name := range record.Attrs {
			key := "@" + name
			if !seen[key] {
				seen[key] = true
				columns = append(columns, key)
			}
		}
		for _, child := range record.Children {
			if !seen[child.Name] {
				seen[child.Name] = true
				columns = append(columns, child.Name)
			}
		}
	}
	// Attributes come out of a map, so their order is fixed here to keep a
	// file's columns the same from one read to the next.
	sortAttributesFirst(columns)

	if len(columns) > opts.Limits.MaxColumns {
		return nil, fmt.Errorf("%w: %d columns, and the limit is %d",
			ErrSourceTooLarge, len(columns), opts.Limits.MaxColumns)
	}

	rows := make([][]string, 0, len(records))
	truncated := false
	for _, record := range records {
		if len(rows) >= opts.Limits.MaxRows {
			truncated = true
			break
		}
		row := make([]string, len(columns))
		for i, column := range columns {
			if strings.HasPrefix(column, "@") {
				row[i] = record.Attrs[strings.TrimPrefix(column, "@")]
				continue
			}
			for _, child := range record.Children {
				if child.Name == column {
					row[i] = child.Text
					break
				}
			}
		}
		rows = append(rows, row)
	}

	return &Table{Columns: columns, Rows: rows, Truncated: truncated}, nil
}

// sortAttributesFirst puts the `@attr` columns before the child elements, each
// group alphabetically, so a file reads the same way every time.
func sortAttributesFirst(columns []string) {
	sort.SliceStable(columns, func(i, j int) bool {
		ai := strings.HasPrefix(columns[i], "@")
		aj := strings.HasPrefix(columns[j], "@")
		if ai != aj {
			return ai
		}
		if ai {
			return columns[i] < columns[j]
		}
		return false // child elements keep the order they appeared in
	})
}

func decodeXML(data []byte) (*node, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.Strict = false
	// An entity nothing declared is left as it is rather than failing the
	// file, which is what most exports need.
	decoder.Entity = xml.HTMLEntity

	root := &node{Name: "", Attrs: map[string]string{}}
	stack := []*node{root}

	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			child := &node{Name: t.Name.Local, Attrs: map[string]string{}}
			for _, attr := range t.Attr {
				child.Attrs[attr.Name.Local] = attr.Value
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, child)
			stack = append(stack, child)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			current := stack[len(stack)-1]
			current.Text += string(t)
		}
	}

	trimText(root)
	if len(root.Children) == 0 {
		return nil, fmt.Errorf("the file holds no XML elements")
	}
	return root, nil
}

func trimText(n *node) {
	n.Text = strings.TrimSpace(n.Text)
	for _, child := range n.Children {
		trimText(child)
	}
}

// mostRepeatedElement is the element that appears most often while having
// children of its own, which is what a record looks like.
func mostRepeatedElement(root *node) string {
	counts := map[string]int{}
	var walk func(*node)
	walk = func(n *node) {
		for _, child := range n.Children {
			if len(child.Children) > 0 {
				counts[child.Name]++
			}
			walk(child)
		}
	}
	walk(root)

	best, bestCount := "", 0
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names) // a tie resolves the same way every time
	for _, name := range names {
		if counts[name] > bestCount {
			best, bestCount = name, counts[name]
		}
	}
	return best
}

func collect(n *node, name string, into *[]*node) {
	for _, child := range n.Children {
		if child.Name == name {
			*into = append(*into, child)
			continue // a record inside a record is not another record
		}
		collect(child, name, into)
	}
}

// parseFMPXMLResult reads the grammar FileMaker exports, where the field names
// are in METADATA and the values in RESULTSET/ROW/COL/DATA.
func parseFMPXMLResult(root *node, opts Options) (*Table, bool) {
	var result *node
	var find func(*node)
	find = func(n *node) {
		for _, child := range n.Children {
			if child.Name == "FMPXMLRESULT" {
				result = child
				return
			}
			find(child)
		}
	}
	find(root)
	if result == nil {
		return nil, false
	}

	columns := make([]string, 0, 16)
	for _, child := range result.Children {
		if child.Name != "METADATA" {
			continue
		}
		for _, field := range child.Children {
			if field.Name == "FIELD" {
				name := field.Attrs["NAME"]
				if name == "" {
					name = fmt.Sprintf("Column %d", len(columns)+1)
				}
				columns = append(columns, name)
			}
		}
	}

	rows := make([][]string, 0, 64)
	truncated := false
	for _, child := range result.Children {
		if child.Name != "RESULTSET" {
			continue
		}
		for _, row := range child.Children {
			if row.Name != "ROW" {
				continue
			}
			if len(rows) >= opts.Limits.orDefaults().MaxRows {
				truncated = true
				break
			}
			values := make([]string, 0, len(columns))
			for _, col := range row.Children {
				if col.Name != "COL" {
					continue
				}
				// A repeating field has several DATA elements; they are joined
				// one per line, which is how File4Base holds several values in
				// a field (#31).
				parts := make([]string, 0, 1)
				for _, dataNode := range col.Children {
					if dataNode.Name == "DATA" {
						parts = append(parts, dataNode.Text)
					}
				}
				values = append(values, strings.Join(parts, "\n"))
			}
			rows = append(rows, values)
		}
	}

	if len(columns) == 0 && len(rows) == 0 {
		return nil, false
	}
	return &Table{Columns: columns, Rows: rows, Truncated: truncated}, true
}

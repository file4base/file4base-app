package designreport

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"strconv"
	"strings"
)

// Write renders a report in the format asked for.
func Write(report *Report, format Format) ([]byte, error) {
	switch format {
	case FormatXML:
		return WriteXML(report)
	case FormatJSON:
		return WriteJSON(report)
	case FormatHTML:
		return WriteHTML(report)
	default:
		return nil, fmt.Errorf("unknown report format %q", format)
	}
}

// WriteJSON writes the design as JSON: the stored documents — field options,
// step parameters, layout definitions — as the objects they are, with their
// keys in order, so a diff between two versions reads as the change it is.
func WriteJSON(report *Report) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteXML writes the design as XML. The stored documents travel as their
// canonical JSON text: there is no faithful element form of an arbitrary
// object, and inventing one would make the report lie about what is stored.
func WriteXML(report *Report) ([]byte, error) {
	flat := *report
	flattenForXML(&flat)

	body, err := xml.MarshalIndent(&flat, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

// flattenForXML copies every stored document into its text field, since the
// XML encoder cannot marshal an arbitrary object.
func flattenForXML(report *Report) {
	for i := range report.Tables {
		for j := range report.Tables[i].Fields {
			field := &report.Tables[i].Fields[j]
			field.Calculation = field.CalculationJSON.Text()
			field.Options = field.OptionsJSON.Text()
			field.Validation = field.ValidationJSON.Text()
		}
	}
	for i := range report.Layouts {
		report.Layouts[i].Definition = report.Layouts[i].DefinitionJSON.Text()
	}
	for i := range report.Scripts {
		for j := range report.Scripts[i].Steps {
			step := &report.Scripts[i].Steps[j]
			step.Parameters = step.ParametersJSON.Text()
		}
	}
}

// WriteHTML writes the report to read: one page, no scripts, no fonts to
// fetch, so it opens from a file and prints.
func WriteHTML(report *Report) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`<!doctype html>` + "\n")
	b.WriteString(`<html lang="en"><head><meta charset="utf-8">` + "\n")
	b.WriteString(`<title>` + esc(report.Solution) + ` — design report</title>` + "\n")
	b.WriteString(`<style>` + reportCSS + `</style>` + "\n")
	b.WriteString(`</head><body>` + "\n")

	b.WriteString(`<h1>` + esc(report.Solution) + `</h1>` + "\n")
	b.WriteString(`<p class="sub">Design report`)
	if report.Database != "" {
		b.WriteString(` · database <code>` + esc(report.Database) + `</code>`)
	}
	if report.Engine != "" {
		b.WriteString(` · ` + esc(report.Engine))
	}
	if report.GeneratedAt != "" {
		b.WriteString(` · ` + esc(report.GeneratedAt))
	}
	b.WriteString(`</p>` + "\n")

	counts := report.Counts
	b.WriteString(`<ul class="counts">` + "\n")
	for _, pair := range [][2]string{
		{"Tables", strconv.Itoa(counts.Tables)},
		{"Fields", strconv.Itoa(counts.Fields)},
		{"Occurrences", strconv.Itoa(counts.Occurrences)},
		{"Relationships", strconv.Itoa(counts.Relationships)},
		{"Layouts", strconv.Itoa(counts.Layouts)},
		{"Scripts", strconv.Itoa(counts.Scripts)},
		{"Script steps", strconv.Itoa(counts.ScriptSteps)},
		{"Value lists", strconv.Itoa(counts.ValueLists)},
		{"Accounts", strconv.Itoa(counts.Accounts)},
		{"Saved finds", strconv.Itoa(counts.SavedFinds)},
		{"Data sources", strconv.Itoa(counts.DataSources)},
	} {
		b.WriteString(`<li><b>` + pair[1] + `</b> ` + pair[0] + `</li>` + "\n")
	}
	b.WriteString(`</ul>` + "\n")

	// ─── Tables ───────────────────────────────────────────────────────────
	b.WriteString(`<h2>Tables</h2>` + "\n")
	if len(report.Tables) == 0 {
		b.WriteString(`<p class="none">This solution has no tables.</p>` + "\n")
	}
	for _, table := range report.Tables {
		b.WriteString(`<h3>` + esc(table.DisplayName) + ` <code>` + esc(table.Name) + `</code></h3>` + "\n")
		if table.Description != "" {
			b.WriteString(`<p>` + esc(table.Description) + `</p>` + "\n")
		}
		b.WriteString(`<table><thead><tr><th>Field</th><th>Name</th><th>Type</th><th>Options</th></tr></thead><tbody>` + "\n")
		for _, field := range table.Fields {
			notes := []string{}
			if field.PrimaryKey {
				notes = append(notes, "primary key")
			}
			if !field.Nullable {
				notes = append(notes, "required by the column")
			}
			if text := field.CalculationJSON.Text(); text != "" {
				notes = append(notes, "formula "+code(text))
			}
			if text := field.OptionsJSON.Text(); text != "" {
				notes = append(notes, "options "+code(text))
			}
			if text := field.ValidationJSON.Text(); text != "" {
				notes = append(notes, "validation "+code(text))
			}
			b.WriteString(`<tr><td>` + esc(field.DisplayName) + `</td><td><code>` + esc(field.Name) +
				`</code></td><td>` + esc(field.Type) + `</td><td>` + strings.Join(notes, "<br>") + `</td></tr>` + "\n")
		}
		b.WriteString(`</tbody></table>` + "\n")
	}

	// ─── The graph ────────────────────────────────────────────────────────
	b.WriteString(`<h2>Relationships</h2>` + "\n")
	if len(report.Relationships) == 0 {
		b.WriteString(`<p class="none">No relationship is defined.</p>` + "\n")
	} else {
		b.WriteString(`<table><thead><tr><th>Name</th><th>Join</th><th>Options</th></tr></thead><tbody>` + "\n")
		for _, rel := range report.Relationships {
			options := []string{}
			if rel.AllowCreation {
				options = append(options, "records can be created through it")
			}
			if rel.CascadeDelete {
				options = append(options, "deletes cascade")
			}
			if rel.SortRelated != "" {
				options = append(options, "related records sorted by "+code(rel.SortRelated))
			}
			if len(options) == 0 {
				options = append(options, "—")
			}
			join := fmt.Sprintf("%s::%s %s %s::%s", esc(rel.Left), esc(rel.LeftField),
				esc(rel.Operator), esc(rel.Right), esc(rel.RightField))
			b.WriteString(`<tr><td>` + esc(rel.Name) + `</td><td><code>` + join + `</code></td><td>` +
				strings.Join(options, "<br>") + `</td></tr>` + "\n")
		}
		b.WriteString(`</tbody></table>` + "\n")
	}

	if len(report.Occurrences) > 0 {
		b.WriteString(`<p class="sub">Table occurrences: `)
		parts := make([]string, 0, len(report.Occurrences))
		for _, occ := range report.Occurrences {
			parts = append(parts, "<code>"+esc(occ.Name)+"</code> → "+esc(occ.BaseTable))
		}
		b.WriteString(strings.Join(parts, " · ") + `</p>` + "\n")
	}

	// ─── Layouts ──────────────────────────────────────────────────────────
	b.WriteString(`<h2>Layouts</h2>` + "\n")
	if len(report.Layouts) == 0 {
		b.WriteString(`<p class="none">This solution has no layouts.</p>` + "\n")
	}
	for _, layout := range report.Layouts {
		b.WriteString(`<h3>` + esc(layout.Name) + `</h3>` + "\n")
		b.WriteString(`<p class="sub">on <code>` + esc(layout.Occurrence) + `</code>`)
		if len(layout.Parts) > 0 {
			parts := make([]string, 0, len(layout.Parts))
			for _, part := range layout.Parts {
				text := part.Type
				if part.BreakField != "" {
					text += " by " + part.BreakField
				}
				parts = append(parts, esc(text))
			}
			b.WriteString(` · parts: ` + strings.Join(parts, ", "))
		}
		if len(layout.Objects) > 0 {
			objects := make([]string, 0, len(layout.Objects))
			for _, object := range layout.Objects {
				objects = append(objects, fmt.Sprintf("%d %s", object.Count, esc(object.Type)))
			}
			b.WriteString(` · objects: ` + strings.Join(objects, ", "))
		}
		b.WriteString(`</p>` + "\n")

		if len(layout.Fields) > 0 {
			b.WriteString(`<table><thead><tr><th>Field</th><th>Read through</th><th>Control</th></tr></thead><tbody>` + "\n")
			for _, field := range layout.Fields {
				through := "the record in hand"
				if field.Relationship != "" {
					through = "relationship " + esc(field.Relationship)
					if field.Occurrence != "" {
						through += " · " + esc(field.Occurrence)
					}
				}
				if field.InPortal {
					through += " (in a portal)"
				}
				control := field.Control
				if field.ValueList != "" {
					control += " · value list " + field.ValueList
				}
				b.WriteString(`<tr><td><code>` + esc(field.Field) + `</code></td><td>` + through +
					`</td><td>` + esc(control) + `</td></tr>` + "\n")
			}
			b.WriteString(`</tbody></table>` + "\n")
		}
		for _, portal := range layout.Portals {
			b.WriteString(`<p class="sub">Portal through <code>` + esc(portal.Relationship) + `</code>`)
			if portal.Rows > 0 {
				b.WriteString(fmt.Sprintf(` · %d rows`, portal.Rows))
			}
			if portal.SortRows != "" {
				b.WriteString(` · sorted by <code>` + esc(portal.SortRows) + `</code>`)
			}
			if portal.AllowCreate {
				b.WriteString(` · records can be created`)
			}
			if portal.AllowDelete {
				b.WriteString(` · records can be deleted`)
			}
			b.WriteString(`</p>` + "\n")
		}
		for _, script := range layout.Scripts {
			b.WriteString(`<p class="sub">Runs <b>` + esc(script.Script) + `</b> — ` + esc(script.Where) + `</p>` + "\n")
		}
	}

	// ─── Scripts ──────────────────────────────────────────────────────────
	b.WriteString(`<h2>Scripts</h2>` + "\n")
	if len(report.Scripts) == 0 {
		b.WriteString(`<p class="none">This solution has no scripts.</p>` + "\n")
	}
	for _, script := range report.Scripts {
		b.WriteString(`<h3>` + esc(script.Name))
		if !script.Active {
			b.WriteString(` <span class="off">inactive</span>`)
		}
		b.WriteString(`</h3>` + "\n")
		if script.ContextTable != "" {
			b.WriteString(`<p class="sub">context <code>` + esc(script.ContextTable) + `</code></p>` + "\n")
		}
		b.WriteString(`<table><thead><tr><th>#</th><th>Step</th><th>Parameters</th></tr></thead><tbody>` + "\n")
		for _, step := range script.Steps {
			name := esc(step.Type)
			if !step.Enabled {
				name = `<s>` + name + `</s>`
			}
			b.WriteString(`<tr><td>` + strconv.Itoa(step.Sequence) + `</td><td>` + name +
				`</td><td>` + code(step.ParametersJSON.Text()) + `</td></tr>` + "\n")
		}
		b.WriteString(`</tbody></table>` + "\n")
	}

	// ─── Value lists ──────────────────────────────────────────────────────
	if len(report.ValueLists) > 0 {
		b.WriteString(`<h2>Value lists</h2>` + "\n")
		b.WriteString(`<table><thead><tr><th>Name</th><th>Kind</th><th>Values</th></tr></thead><tbody>` + "\n")
		for _, list := range report.ValueLists {
			values := strings.Join(list.Values, ", ")
			if list.Source != "" {
				values = "from " + list.Source
			}
			b.WriteString(`<tr><td>` + esc(list.Name) + `</td><td>` + esc(list.Kind) + `</td><td>` +
				esc(values) + `</td></tr>` + "\n")
		}
		b.WriteString(`</tbody></table>` + "\n")
	}

	// ─── Security ─────────────────────────────────────────────────────────
	b.WriteString(`<h2>Accounts and privileges</h2>` + "\n")
	b.WriteString(`<table><thead><tr><th>Account</th><th>Role</th><th>Status</th><th>Layout permissions</th></tr></thead><tbody>` + "\n")
	for _, account := range report.Accounts {
		status := "active"
		if !account.Active {
			status = "inactive"
		}
		permissions := []string{}
		for _, permission := range account.Permissions {
			permissions = append(permissions, esc(permission.Layout)+": "+esc(permission.Access))
		}
		if len(permissions) == 0 {
			permissions = append(permissions, "every layout (no restriction)")
		}
		b.WriteString(`<tr><td>` + esc(account.Username) + `</td><td>` + esc(account.Role) + `</td><td>` +
			status + `</td><td>` + strings.Join(permissions, "<br>") + `</td></tr>` + "\n")
	}
	b.WriteString(`</tbody></table>` + "\n")

	if len(report.Privileges) > 0 {
		b.WriteString(`<table><thead><tr><th>Role</th><th>Bulk export</th><th>Bulk import</th></tr></thead><tbody>` + "\n")
		for _, privilege := range report.Privileges {
			b.WriteString(`<tr><td>` + esc(privilege.Role) + `</td><td>` + yesNo(privilege.BulkExport) +
				`</td><td>` + yesNo(privilege.BulkImport) + `</td></tr>` + "\n")
		}
		b.WriteString(`</tbody></table>` + "\n")
	}

	// ─── Saved finds and data sources ─────────────────────────────────────
	if len(report.SavedFinds) > 0 {
		b.WriteString(`<h2>Saved finds</h2>` + "\n")
		b.WriteString(`<table><thead><tr><th>Name</th><th>Table</th><th>Requests</th></tr></thead><tbody>` + "\n")
		for _, find := range report.SavedFinds {
			lines := []string{}
			for _, request := range find.Requests {
				criteria := []string{}
				for _, criterion := range request.Criteria {
					criteria = append(criteria, esc(criterion.Field)+" "+esc(criterion.Criteria))
				}
				prefix := "Find: "
				if request.Omit {
					prefix = "Omit: "
				}
				lines = append(lines, prefix+strings.Join(criteria, ", "))
			}
			b.WriteString(`<tr><td>` + esc(find.Name) + `</td><td><code>` + esc(find.Table) + `</code></td><td>` +
				strings.Join(lines, "<br>") + `</td></tr>` + "\n")
		}
		b.WriteString(`</tbody></table>` + "\n")
	}

	if len(report.DataSources) > 0 {
		b.WriteString(`<h2>External data sources</h2>` + "\n")
		b.WriteString(`<table><thead><tr><th>Name</th><th>Engine</th><th>Where</th><th>User</th></tr></thead><tbody>` + "\n")
		for _, source := range report.DataSources {
			where := esc(source.Host)
			if source.Port > 0 {
				where += ":" + strconv.Itoa(source.Port)
			}
			if source.Database != "" {
				where += " · " + esc(source.Database)
			}
			user := esc(source.Username)
			if source.PasswordEnv != "" {
				user += " · password from " + esc(source.PasswordEnv)
			}
			b.WriteString(`<tr><td>` + esc(source.Name) + `</td><td>` + esc(source.Engine) + `</td><td>` +
				where + `</td><td>` + user + `</td></tr>` + "\n")
		}
		b.WriteString(`</tbody></table>` + "\n")
	}

	// ─── What this report does not say ────────────────────────────────────
	if len(report.Notes) > 0 {
		b.WriteString(`<h2>What this report does not describe</h2>` + "\n<ul>\n")
		for _, note := range report.Notes {
			b.WriteString(`<li>` + esc(note) + `</li>` + "\n")
		}
		b.WriteString("</ul>\n")
	}

	b.WriteString(`</body></html>` + "\n")
	return []byte(b.String()), nil
}

func esc(s string) string { return html.EscapeString(s) }

func code(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return `<code>` + esc(s) + `</code>`
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

const reportCSS = `
body { font: 14px/1.5 -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
       margin: 2rem auto; max-width: 60rem; padding: 0 1rem; color: #16202b; }
h1 { font-size: 1.7rem; margin-bottom: .2rem; }
h2 { font-size: 1.25rem; margin-top: 2.2rem; border-bottom: 2px solid #1e88e5; padding-bottom: .2rem; }
h3 { font-size: 1.02rem; margin-top: 1.4rem; }
p.sub { color: #5b6876; font-size: .86rem; margin: .2rem 0 .6rem; }
p.none { color: #5b6876; font-style: italic; }
span.off { color: #b2601a; font-size: .8rem; font-weight: normal; }
ul.counts { list-style: none; padding: 0; display: flex; flex-wrap: wrap; gap: .4rem 1.2rem; }
ul.counts li { font-size: .86rem; color: #5b6876; }
table { border-collapse: collapse; width: 100%; margin: .4rem 0 1rem; font-size: .86rem; }
th, td { border: 1px solid #d8dee5; padding: .32rem .5rem; text-align: left; vertical-align: top; }
th { background: #f2f5f8; font-weight: 600; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .82rem;
       background: #f2f5f8; padding: 0 .2rem; border-radius: 3px; overflow-wrap: anywhere; }
@media print { body { margin: 0; max-width: none; } h2 { page-break-after: avoid; } table { page-break-inside: avoid; } }
`

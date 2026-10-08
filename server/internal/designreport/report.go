// Package designreport describes what a solution is made of: its tables and
// fields, the relationships between them, its layouts, scripts, value lists,
// accounts and privileges (#49).
//
// It serves two purposes that are usually built twice:
//
//   - a **report** to read, as HTML, for documenting a solution or reviewing
//     it before it is handed over;
//   - the **design as text**, as XML or JSON, so that a solution can live in
//     version control. A .f4p file is MessagePack: exact, compact and opaque
//     to a diff. The same design written out as text is not.
//
// Both come from one walk of the catalog, so what the report says and what
// the text form holds cannot drift apart. Neither ever carries a password, a
// password hash or a stored credential: a report is something you attach to
// an email.
package designreport

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Format is how a report is written.
type Format string

const (
	// FormatHTML is the report to read.
	FormatHTML Format = "html"
	// FormatXML is the design as text, for processing and for version control.
	FormatXML Format = "xml"
	// FormatJSON is the same, for a reader that would rather parse JSON.
	FormatJSON Format = "json"
)

// Formats are the forms a report takes, in the order the dialog offers them.
var Formats = []Format{FormatHTML, FormatXML, FormatJSON}

// Known reports whether File4Base writes that format.
func (f Format) Known() bool {
	for _, known := range Formats {
		if known == f {
			return true
		}
	}
	return false
}

// ContentType is the media type of a report in this format.
func (f Format) ContentType() string {
	switch f {
	case FormatXML:
		return "application/xml; charset=utf-8"
	case FormatJSON:
		return "application/json; charset=utf-8"
	default:
		return "text/html; charset=utf-8"
	}
}

// Extension is the file extension of a report in this format.
func (f Format) Extension() string {
	switch f {
	case FormatXML:
		return ".xml"
	case FormatJSON:
		return ".json"
	default:
		return ".html"
	}
}

// ReportFormat and ReportVersion identify the document itself, so a reader
// knows what it is holding.
const (
	ReportFormat  = "file4base_design"
	ReportVersion = "1.0"
)

// Report is a solution's design.
//
// Every collection is in the catalog's own order — tables by display name,
// fields in the order they were added, scripts by name, steps by sequence —
// so two reports of the same design are identical byte for byte, and two of
// different designs differ only where the designs do.
type Report struct {
	XMLName struct{} `json:"-" xml:"design"`

	Format  string `json:"format" xml:"format,attr"`
	Version string `json:"version" xml:"version,attr"`

	Solution string `json:"solution" xml:"solution,attr"`
	Database string `json:"database,omitempty" xml:"database,attr,omitempty"`
	Engine   string `json:"engine,omitempty" xml:"engine,attr,omitempty"`

	// GeneratedAt is left out of the text forms, so committing the same
	// design twice produces no diff. The HTML report shows it.
	GeneratedAt string `json:"generated_at,omitempty" xml:"generated_at,attr,omitempty"`

	Counts Counts `json:"counts" xml:"counts"`

	Tables        []Table         `json:"tables" xml:"tables>table"`
	Occurrences   []Occurrence    `json:"table_occurrences" xml:"table_occurrences>occurrence"`
	Relationships []Relationship  `json:"relationships" xml:"relationships>relationship"`
	Layouts       []Layout        `json:"layouts" xml:"layouts>layout"`
	Scripts       []Script        `json:"scripts" xml:"scripts>script"`
	ValueLists    []ValueList     `json:"value_lists" xml:"value_lists>value_list"`
	Accounts      []Account       `json:"accounts" xml:"accounts>account"`
	Privileges    []RolePrivilege `json:"privileges" xml:"privileges>role"`
	SavedFinds    []SavedFind     `json:"saved_finds" xml:"saved_finds>saved_find"`
	DataSources   []DataSource    `json:"data_sources" xml:"data_sources>data_source"`

	// Notes say what this report does not describe, so a reader is not left
	// to assume that what is absent does not exist.
	Notes []string `json:"notes" xml:"notes>note"`
}

// Counts is the size of the solution at a glance.
type Counts struct {
	Tables        int `json:"tables" xml:"tables,attr"`
	Fields        int `json:"fields" xml:"fields,attr"`
	Occurrences   int `json:"table_occurrences" xml:"table_occurrences,attr"`
	Relationships int `json:"relationships" xml:"relationships,attr"`
	Layouts       int `json:"layouts" xml:"layouts,attr"`
	Scripts       int `json:"scripts" xml:"scripts,attr"`
	ScriptSteps   int `json:"script_steps" xml:"script_steps,attr"`
	ValueLists    int `json:"value_lists" xml:"value_lists,attr"`
	Accounts      int `json:"accounts" xml:"accounts,attr"`
	SavedFinds    int `json:"saved_finds" xml:"saved_finds,attr"`
	DataSources   int `json:"data_sources" xml:"data_sources,attr"`
}

// Table is a base table and its fields.
type Table struct {
	Name        string  `json:"name" xml:"name,attr"`
	DisplayName string  `json:"display_name" xml:"display_name,attr"`
	Description string  `json:"description,omitempty" xml:"description,omitempty"`
	Fields      []Field `json:"fields" xml:"fields>field"`
}

// Field is one column, with everything that decides what it holds.
type Field struct {
	Name        string `json:"name" xml:"name,attr"`
	DisplayName string `json:"display_name" xml:"display_name,attr"`
	Type        string `json:"type" xml:"type,attr"`
	PrimaryKey  bool   `json:"primary_key" xml:"primary_key,attr"`
	Nullable    bool   `json:"nullable" xml:"nullable,attr"`

	// Calculation is the formula of a calculation or summary field, and
	// Options and Validation are the field options and validation rules, as
	// they are stored. In XML they are the JSON text; in JSON they are the
	// objects themselves.
	Calculation     string    `json:"-" xml:"calculation,omitempty"`
	Options         string    `json:"-" xml:"options,omitempty"`
	Validation      string    `json:"-" xml:"validation,omitempty"`
	CalculationJSON JSONValue `json:"calculation,omitempty" xml:"-"`
	OptionsJSON     JSONValue `json:"options,omitempty" xml:"-"`
	ValidationJSON  JSONValue `json:"validation,omitempty" xml:"-"`
}

// Occurrence is a table occurrence: the name a relationship and a layout
// reach a table by.
type Occurrence struct {
	Name      string `json:"name" xml:"name,attr"`
	BaseTable string `json:"base_table" xml:"base_table,attr"`
}

// Relationship is one join of the graph, read left to right as parent to
// child, which is the direction its options act in.
type Relationship struct {
	Name          string `json:"name" xml:"name,attr"`
	Left          string `json:"left" xml:"left,attr"`
	LeftField     string `json:"left_field" xml:"left_field,attr"`
	Operator      string `json:"operator" xml:"operator,attr"`
	Right         string `json:"right" xml:"right,attr"`
	RightField    string `json:"right_field" xml:"right_field,attr"`
	AllowCreation bool   `json:"allow_creation" xml:"allow_creation,attr"`
	CascadeDelete bool   `json:"cascade_delete" xml:"cascade_delete,attr"`
	SortRelated   string `json:"sort_related,omitempty" xml:"sort_related,attr,omitempty"`
}

// Layout is a layout, described rather than drawn: the parts it is made of,
// the fields it binds, the scripts it runs and what else it holds.
type Layout struct {
	Name       string         `json:"name" xml:"name,attr"`
	Occurrence string         `json:"table_occurrence" xml:"table_occurrence,attr"`
	Parts      []LayoutPart   `json:"parts" xml:"parts>part"`
	Fields     []LayoutField  `json:"fields" xml:"fields>field"`
	Objects    []ObjectCount  `json:"objects" xml:"objects>object"`
	Portals    []Portal       `json:"portals,omitempty" xml:"portals>portal,omitempty"`
	Scripts    []LayoutScript `json:"scripts,omitempty" xml:"scripts>script,omitempty"`

	// Definition is the layout exactly as it is stored, for a diff to work
	// on. The HTML report describes the layout instead of printing this.
	Definition     string    `json:"-" xml:"definition,omitempty"`
	DefinitionJSON JSONValue `json:"definition,omitempty" xml:"-"`
}

// LayoutPart is a band of a layout.
type LayoutPart struct {
	Type       string  `json:"type" xml:"type,attr"`
	Height     float64 `json:"height" xml:"height,attr"`
	BreakField string  `json:"break_field,omitempty" xml:"break_field,attr,omitempty"`
}

// LayoutField is a field object: which field it shows, through which
// relationship when it is a related field, and how it is drawn.
type LayoutField struct {
	Field        string `json:"field" xml:"field,attr"`
	Occurrence   string `json:"table_occurrence,omitempty" xml:"table_occurrence,attr,omitempty"`
	Relationship string `json:"relationship,omitempty" xml:"relationship,attr,omitempty"`
	Control      string `json:"control,omitempty" xml:"control,attr,omitempty"`
	ValueList    string `json:"value_list,omitempty" xml:"value_list,attr,omitempty"`
	InPortal     bool   `json:"in_portal,omitempty" xml:"in_portal,attr,omitempty"`
}

// ObjectCount says how many objects of a kind a layout holds.
type ObjectCount struct {
	Type  string `json:"type" xml:"type,attr"`
	Count int    `json:"count" xml:"count,attr"`
}

// Portal is a portal on a layout.
type Portal struct {
	Relationship string `json:"relationship,omitempty" xml:"relationship,attr,omitempty"`
	Occurrence   string `json:"table_occurrence,omitempty" xml:"table_occurrence,attr,omitempty"`
	Rows         int    `json:"rows,omitempty" xml:"rows,attr,omitempty"`
	SortRows     string `json:"sort_rows,omitempty" xml:"sort_rows,attr,omitempty"`
	AllowCreate  bool   `json:"allow_create,omitempty" xml:"allow_create,attr,omitempty"`
	AllowDelete  bool   `json:"allow_delete,omitempty" xml:"allow_delete,attr,omitempty"`
}

// LayoutScript is a script a layout runs: a button, or a trigger.
type LayoutScript struct {
	Script string `json:"script" xml:"script,attr"`
	Where  string `json:"where" xml:"where,attr"`
}

// Script is a script and its steps.
type Script struct {
	Name         string `json:"name" xml:"name,attr"`
	ContextTable string `json:"context_table,omitempty" xml:"context_table,attr,omitempty"`
	Active       bool   `json:"active" xml:"active,attr"`
	Steps        []Step `json:"steps" xml:"steps>step"`
}

// Step is one step of a script.
type Step struct {
	Sequence int    `json:"sequence" xml:"sequence,attr"`
	Type     string `json:"type" xml:"type,attr"`
	Enabled  bool   `json:"enabled" xml:"enabled,attr"`

	Parameters     string    `json:"-" xml:"parameters,omitempty"`
	ParametersJSON JSONValue `json:"parameters,omitempty" xml:"-"`
}

// ValueList is a value list and where its values come from.
type ValueList struct {
	Name   string   `json:"name" xml:"name,attr"`
	Kind   string   `json:"kind" xml:"kind,attr"`
	Values []string `json:"values,omitempty" xml:"values>value,omitempty"`
	Source string   `json:"source,omitempty" xml:"source,attr,omitempty"`
}

// Account is a user account, without anything secret.
type Account struct {
	Username    string             `json:"username" xml:"username,attr"`
	Role        string             `json:"role" xml:"role,attr"`
	Active      bool               `json:"active" xml:"active,attr"`
	Permissions []LayoutPermission `json:"layout_permissions,omitempty" xml:"layout_permissions>permission,omitempty"`
}

// LayoutPermission is the access one account holds on one layout.
type LayoutPermission struct {
	Layout string `json:"layout" xml:"layout,attr"`
	Access string `json:"access" xml:"access,attr"`
}

// RolePrivilege is the extended privileges one role holds.
type RolePrivilege struct {
	Role       string `json:"role" xml:"role,attr"`
	BulkExport bool   `json:"bulk_export" xml:"bulk_export,attr"`
	BulkImport bool   `json:"bulk_import" xml:"bulk_import,attr"`
}

// SavedFind is a named find and what it searches.
type SavedFind struct {
	Name     string             `json:"name" xml:"name,attr"`
	Table    string             `json:"table" xml:"table,attr"`
	Requests []SavedFindRequest `json:"requests" xml:"requests>request"`
}

// SavedFindRequest is one request of a saved find.
type SavedFindRequest struct {
	Omit     bool                 `json:"omit" xml:"omit,attr"`
	Criteria []SavedFindCriterion `json:"criteria" xml:"criteria>criterion"`
}

// SavedFindCriterion is what was typed into one field.
type SavedFindCriterion struct {
	Field    string `json:"field" xml:"field,attr"`
	Criteria string `json:"criteria" xml:"criteria,attr"`
}

// DataSource is a registered external connection. It has no password here
// because it has none in the catalog either.
type DataSource struct {
	Name        string `json:"name" xml:"name,attr"`
	Engine      string `json:"engine" xml:"engine,attr"`
	Host        string `json:"host" xml:"host,attr"`
	Port        int    `json:"port,omitempty" xml:"port,attr,omitempty"`
	Database    string `json:"database,omitempty" xml:"database,attr,omitempty"`
	Username    string `json:"username,omitempty" xml:"username,attr,omitempty"`
	Schema      string `json:"schema,omitempty" xml:"schema,attr,omitempty"`
	TLS         bool   `json:"tls" xml:"tls,attr"`
	PasswordEnv string `json:"password_env,omitempty" xml:"password_env,attr,omitempty"`
}

// JSONValue carries a stored JSON document — a field's options, a script
// step's parameters, a layout definition — through the report. JSON holds it
// as the document it is; XML holds the text, since there is no faithful
// element form of an arbitrary object.
type JSONValue struct {
	Value interface{}
}

// MarshalJSON writes the document itself, or nothing when there is none.
func (v JSONValue) MarshalJSON() ([]byte, error) {
	if v.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v.Value)
}

// Text is the canonical text of the document: JSON with its keys in order,
// which is what makes a diff of two reports readable.
func (v JSONValue) Text() string {
	if v.Value == nil {
		return ""
	}
	raw, err := json.Marshal(v.Value)
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeJSON reads a stored JSON document, or returns an empty value when
// there is nothing stored. A document that does not parse is kept as the
// text it is, so a report never hides what the catalog holds.
func DecodeJSON(raw string) JSONValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return JSONValue{}
	}
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return JSONValue{Value: map[string]interface{}{"unparsed": raw}}
	}
	return JSONValue{Value: value}
}

// FileName is what a report is saved as.
func FileName(solution string, format Format) string {
	name := strings.TrimSpace(solution)
	if name == "" {
		name = "solution"
	}
	cleaned := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			cleaned = append(cleaned, r)
		case r == ' ':
			cleaned = append(cleaned, '_')
		}
	}
	if len(cleaned) == 0 {
		cleaned = []rune("solution")
	}
	return fmt.Sprintf("%s_design%s", string(cleaned), format.Extension())
}

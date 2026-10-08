package schema

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/designreport"
)

// DesignReportOptions describe what to report on.
type DesignReportOptions struct {
	SolutionName string

	// Database is the database the design lives in, as the caller's session
	// names it.
	Database string

	// Timestamped puts the moment the report was made into the document.
	// The HTML report asks for it; the text forms do not, so that writing
	// the same design twice produces no diff (#49).
	Timestamped bool
}

// DesignReport describes the solution's design: its tables and fields, the
// relationships between them, its layouts, scripts, value lists, accounts and
// privileges (#49).
//
// It is built from the same walk of the catalog the solution export uses, so
// the report and the .f4p file cannot describe different solutions, and like
// that walk it carries no password, password hash or stored credential.
func (s *Service) DesignReport(ctx context.Context, opts DesignReportOptions) (*designreport.Report, error) {
	bundle, err := s.buildSolutionBundle(ctx, ExportOptions{SolutionName: opts.SolutionName})
	if err != nil {
		return nil, err
	}

	report := &designreport.Report{
		Format:   designreport.ReportFormat,
		Version:  designreport.ReportVersion,
		Solution: bundle.SolutionName,
		Database: opts.Database,
		Engine:   bundle.DatabaseConnection.Engine,
	}
	if report.Database == "" {
		report.Database = bundle.DatabaseConnection.Database
	}
	if report.Solution == "" {
		report.Solution = report.Database
	}
	if opts.Timestamped {
		report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}

	// Names for the ids the catalog stores, so the report reads as the
	// designer wrote it rather than as the database keeps it.
	tableName := map[string]string{}
	columnName := map[string]string{}
	occurrenceName := map[string]string{}
	layoutName := map[string]string{}
	scriptName := map[string]string{}
	valueListName := map[string]string{}
	relationshipName := map[string]string{}

	for _, table := range bundle.Tables {
		tableName[table.ID] = table.Name
		for _, column := range table.Columns {
			columnName[column.ID] = column.Name
		}
	}
	for _, occurrence := range bundle.TableOccurrences {
		occurrenceName[occurrence.ID] = occurrence.Name
	}
	for _, layout := range bundle.Layouts {
		layoutName[layout.ID] = layout.Name
	}
	for _, script := range bundle.Scripts {
		scriptName[script.ID] = script.Name
	}
	for _, list := range bundle.ValueLists {
		valueListName[list.ID] = list.Name
	}
	for _, relationship := range bundle.Relationships {
		relationshipName[relationship.ID] = relationship.Name
	}

	// ─── Tables ───────────────────────────────────────────────────────────
	for _, table := range bundle.Tables {
		reported := designreport.Table{
			Name: table.Name, DisplayName: table.DisplayName, Description: table.Description,
			Fields: make([]designreport.Field, 0, len(table.Columns)),
		}
		for _, column := range table.Columns {
			reported.Fields = append(reported.Fields, designreport.Field{
				Name: column.Name, DisplayName: column.DisplayName, Type: column.FieldType,
				PrimaryKey: column.IsPrimaryKey, Nullable: column.IsNullable,
				CalculationJSON: decodeStored(column.CalculationFormula),
				OptionsJSON:     decodeStored(column.DefaultValue),
				ValidationJSON:  decodeStored(column.ValidationRules),
			})
			report.Counts.Fields++
		}
		report.Tables = append(report.Tables, reported)
	}
	report.Counts.Tables = len(report.Tables)

	// ─── The graph ────────────────────────────────────────────────────────
	for _, occurrence := range bundle.TableOccurrences {
		report.Occurrences = append(report.Occurrences, designreport.Occurrence{
			Name: occurrence.Name, BaseTable: tableName[occurrence.BaseTableID],
		})
	}
	report.Counts.Occurrences = len(report.Occurrences)

	for _, relationship := range bundle.Relationships {
		report.Relationships = append(report.Relationships, designreport.Relationship{
			Name:          relationship.Name,
			Left:          occurrenceName[relationship.LeftOccurrenceID],
			LeftField:     columnName[relationship.LeftColumnID],
			Operator:      relationship.Operator,
			Right:         occurrenceName[relationship.RightOccurrenceID],
			RightField:    columnName[relationship.RightColumnID],
			AllowCreation: relationship.AllowCreation,
			CascadeDelete: relationship.CascadeDelete,
			SortRelated:   relationship.SortRelated,
		})
	}
	report.Counts.Relationships = len(report.Relationships)

	// ─── Layouts ──────────────────────────────────────────────────────────
	for _, layout := range bundle.Layouts {
		report.Layouts = append(report.Layouts, describeLayout(layout, names{
			occurrences:   occurrenceName,
			relationships: relationshipName,
			valueLists:    valueListName,
			scripts:       scriptName,
		}))
	}
	report.Counts.Layouts = len(report.Layouts)

	// ─── Scripts ──────────────────────────────────────────────────────────
	for _, script := range bundle.Scripts {
		reported := designreport.Script{
			Name: script.Name, ContextTable: script.ContextTable, Active: script.IsActive,
			Steps: make([]designreport.Step, 0, len(script.Steps)),
		}
		for _, step := range script.Steps {
			reported.Steps = append(reported.Steps, designreport.Step{
				Sequence: step.SequenceIdx, Type: step.StepType, Enabled: step.IsEnabled,
				ParametersJSON: designreport.JSONValue{Value: nilWhenEmpty(step.Params)},
			})
			report.Counts.ScriptSteps++
		}
		report.Scripts = append(report.Scripts, reported)
	}
	report.Counts.Scripts = len(report.Scripts)

	// ─── Value lists ──────────────────────────────────────────────────────
	for _, list := range bundle.ValueLists {
		reported := designreport.ValueList{Name: list.Name, Kind: list.Kind}
		if list.Kind == ValueListFromField {
			table, column := "", ""
			if list.SourceTableID != nil {
				table = tableName[*list.SourceTableID]
			}
			if list.SourceColumnID != nil {
				column = columnName[*list.SourceColumnID]
			}
			reported.Source = strings.TrimSuffix(table+"::"+column, "::")
		} else {
			reported.Values = SplitCustomValues(list.CustomValues)
		}
		report.ValueLists = append(report.ValueLists, reported)
	}
	report.Counts.ValueLists = len(report.ValueLists)

	// ─── Accounts and privileges ──────────────────────────────────────────
	for _, account := range bundle.Users {
		reported := designreport.Account{
			Username: account.Username, Role: account.Role, Active: account.IsActive,
		}
		for _, permission := range account.Permissions {
			reported.Permissions = append(reported.Permissions, designreport.LayoutPermission{
				Layout: layoutName[permission.LayoutID], Access: permission.AccessLevel,
			})
		}
		sort.Slice(reported.Permissions, func(i, j int) bool {
			return reported.Permissions[i].Layout < reported.Permissions[j].Layout
		})
		report.Accounts = append(report.Accounts, reported)
	}
	report.Counts.Accounts = len(report.Accounts)

	for _, privilege := range bundle.Privileges {
		report.Privileges = append(report.Privileges, designreport.RolePrivilege{
			Role: privilege.Role, BulkExport: privilege.BulkExport, BulkImport: privilege.BulkImport,
		})
	}

	// ─── Saved finds and data sources ─────────────────────────────────────
	for _, find := range bundle.SavedFinds {
		reported := designreport.SavedFind{Name: find.Name, Table: find.TableName}
		for _, request := range find.Requests {
			criteria := make([]designreport.SavedFindCriterion, 0, len(request.Values))
			for field, value := range request.Values {
				criteria = append(criteria, designreport.SavedFindCriterion{Field: field, Criteria: value})
			}
			// A map has no order of its own; the report must have one.
			sort.Slice(criteria, func(i, j int) bool { return criteria[i].Field < criteria[j].Field })
			reported.Requests = append(reported.Requests, designreport.SavedFindRequest{
				Omit: request.Omit, Criteria: criteria,
			})
		}
		report.SavedFinds = append(report.SavedFinds, reported)
	}
	report.Counts.SavedFinds = len(report.SavedFinds)

	for _, source := range bundle.DataSources {
		report.DataSources = append(report.DataSources, designreport.DataSource{
			Name: source.Name, Engine: source.Engine, Host: source.Host, Port: source.Port,
			Database: source.Database, Username: source.Username, Schema: source.Schema,
			TLS: source.TLS, PasswordEnv: source.PasswordEnv,
		})
	}
	report.Counts.DataSources = len(report.DataSources)

	report.Notes = designReportNotes()
	return report, nil
}

// designReportNotes say what the report leaves out, so that what is absent
// from it is not read as absent from the solution.
func designReportNotes() []string {
	return []string{
		"The records are not design: this report describes the structure, and counts no data.",
		"No password, password hash or stored credential is written, in any format. " +
			"An account appears with its name, role and layout permissions only.",
		"A layout is described — its parts, the fields it binds, the scripts it runs — not drawn. " +
			"The XML and JSON forms carry each layout's definition exactly as it is stored.",
		"What a script step does is what the runner makes of it; the report lists the steps and their parameters.",
		"Client settings kept in the solution file (File Options, Page Setup) are not part of the design report.",
	}
}

// names are the catalog's ids resolved to the names a reader knows.
type names struct {
	occurrences   map[string]string
	relationships map[string]string
	valueLists    map[string]string
	scripts       map[string]string
}

func (n names) occurrence(id string) string { return n.occurrences[id] }

// occurrenceNamed takes what a layout stored — an occurrence's id, or its
// name — and answers the name. An id says nothing to a reader.
func (n names) occurrenceNamed(value string) string {
	if value == "" {
		return ""
	}
	if name, ok := n.occurrences[value]; ok {
		return name
	}
	return value
}
func (n names) relationship(id string) string { return n.relationships[id] }
func (n names) valueList(id string) string    { return n.valueLists[id] }

// describeLayout reads a layout definition and says what it is made of. A
// definition that does not hold what is expected is skipped rather than
// guessed at: the definition itself travels in the text forms.
func describeLayout(layout BundleLayout, n names) designreport.Layout {
	reported := designreport.Layout{
		Name:           layout.Name,
		Occurrence:     n.occurrence(layout.TableOccurrenceID),
		DefinitionJSON: designreport.JSONValue{Value: nilWhenEmpty(layout.Definition)},
	}
	if reported.Occurrence == "" {
		reported.Occurrence = asString(layout.Definition["table_occurrence"])
	}

	for _, raw := range asSlice(layout.Definition["parts"]) {
		part := asMap(raw)
		if part == nil {
			continue
		}
		reported.Parts = append(reported.Parts, designreport.LayoutPart{
			Type:       asString(part["type"]),
			Height:     asFloat(part["height"]),
			BreakField: asString(part["break_field"]),
		})
	}

	counts := map[string]int{}
	var kinds []string
	for _, raw := range asSlice(layout.Definition["objects"]) {
		object := asMap(raw)
		if object == nil {
			continue
		}
		kind := asString(object["type"])
		if _, seen := counts[kind]; !seen {
			kinds = append(kinds, kind)
		}
		counts[kind]++

		if binding := asMap(object["field_binding"]); binding != nil {
			reported.Fields = append(reported.Fields, designreport.LayoutField{
				Field:        asString(binding["field_name"]),
				Occurrence:   n.occurrenceNamed(asString(binding["table_occurrence"])),
				Relationship: n.relationship(asString(binding["relationship_id"])),
				Control:      asString(binding["control_style"]),
				ValueList:    n.valueList(asString(binding["value_list_id"])),
			})
		}
		if portal := asMap(object["portal_config"]); portal != nil {
			reported.Portals = append(reported.Portals, designreport.Portal{
				Relationship: n.relationship(asString(portal["relationship_id"])),
				Occurrence:   n.occurrenceNamed(asString(portal["occurrence"])),
				Rows:         int(asFloat(portal["row_count"])),
				SortRows:     asString(portal["sort"]),
				AllowCreate:  asBool(portal["allow_creation"]),
				AllowDelete:  asBool(portal["allow_deletion"]),
			})
		}
		if action := asMap(object["action"]); action != nil {
			if script := scriptOf(action, n); script != "" {
				where := "button"
				if name := asString(object["name"]); name != "" {
					where = "button " + name
				} else if text := asString(object["text"]); text != "" {
					where = "button " + text
				}
				reported.Scripts = append(reported.Scripts, designreport.LayoutScript{Script: script, Where: where})
			}
		}
	}
	for _, kind := range kinds {
		reported.Objects = append(reported.Objects, designreport.ObjectCount{Type: kind, Count: counts[kind]})
	}

	for _, trigger := range []struct{ key, where string }{
		{"on_layout_enter", "when the layout is entered"},
		{"on_layout_exit", "when the layout is left"},
	} {
		if script := scriptOf(asMap(layout.Definition[trigger.key]), n); script != "" {
			reported.Scripts = append(reported.Scripts, designreport.LayoutScript{Script: script, Where: trigger.where})
		}
	}
	return reported
}

// scriptOf reads the script an action or a trigger runs, by name when the
// definition kept one and by id otherwise.
func scriptOf(definition map[string]interface{}, n names) string {
	if definition == nil {
		return ""
	}
	if id := asString(definition["script_id"]); id != "" {
		if name := n.scripts[id]; name != "" {
			return name
		}
	}
	return asString(definition["script_name"])
}

// decodeStored reads one of the catalog's optional JSON columns.
func decodeStored(raw *string) designreport.JSONValue {
	if raw == nil {
		return designreport.JSONValue{}
	}
	return designreport.DecodeJSON(*raw)
}

// nilWhenEmpty keeps an empty map out of the report, so "no parameters" and
// "an empty object" do not look the same in a diff.
func nilWhenEmpty(value map[string]interface{}) interface{} {
	if len(value) == 0 {
		return nil
	}
	return value
}

func asMap(value interface{}) map[string]interface{} {
	m, _ := value.(map[string]interface{})
	return m
}

func asSlice(value interface{}) []interface{} {
	s, _ := value.([]interface{})
	return s
}

func asString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func asFloat(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func asBool(value interface{}) bool {
	b, _ := value.(bool)
	return b
}

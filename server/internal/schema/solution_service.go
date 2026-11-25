package schema

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/msgpackguard"
	"github.com/file4base/file4base-app/server/internal/repetition"
	"github.com/file4base/file4base-app/server/internal/sourcetext"
	"github.com/file4base/file4base-app/server/internal/validation"
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
)

// Solution and data bundles. docs/specs/solution_bundle_format.md is the
// contract shared with the Flutter client; keep both in sync.
const (
	SolutionFormat  = "file4base_solution"
	SolutionVersion = "2.0"
	DataFormat      = "file4base_data"
	DataVersion     = "2.0"
)

// ErrInvalidBundle is returned (wrapped) for a bundle that cannot be restored.
var ErrInvalidBundle = errors.New("invalid bundle")

// BundleConnection identifies the database a solution belongs to. It never
// carries a password.
type BundleConnection struct {
	Engine   string `msgpack:"engine" json:"engine"`
	Host     string `msgpack:"host" json:"host"`
	Port     int    `msgpack:"port" json:"port"`
	Database string `msgpack:"database" json:"database"`
	User     string `msgpack:"user" json:"user"`
	SSLMode  string `msgpack:"ssl_mode" json:"ssl_mode"`
}

type BundleColumn struct {
	ID                 string  `msgpack:"id"`
	Name               string  `msgpack:"name"`
	DisplayName        string  `msgpack:"display_name"`
	FieldType          string  `msgpack:"field_type"`
	IsNullable         bool    `msgpack:"is_nullable"`
	IsPrimaryKey       bool    `msgpack:"is_primary_key"`
	DefaultValue       *string `msgpack:"default_value,omitempty"`
	CalculationFormula *string `msgpack:"calculation_formula,omitempty"`
	ValidationRules    *string `msgpack:"validation_rules,omitempty"`
	// MaxRepetitions is absent for a field that does not repeat, so bundles
	// written before repeating fields read as they always did.
	MaxRepetitions int `msgpack:"max_repetitions,omitempty"`
	// SourceFieldID is the field's FileMaker id, kept by the migration and
	// absent otherwise (#107).
	SourceFieldID string `msgpack:"source_field_id,omitempty"`
}

type BundleTable struct {
	ID          string         `msgpack:"id"`
	Name        string         `msgpack:"name"`
	DisplayName string         `msgpack:"display_name"`
	Description string         `msgpack:"description"`
	Columns     []BundleColumn `msgpack:"columns"`
}

// BundleOccurrence is a table occurrence of a solution file: of one of its
// tables (BaseTableID), or of a table of another solution (External, with no
// BaseTableID).
type BundleOccurrence struct {
	ID          string                    `msgpack:"id"`
	BaseTableID string                    `msgpack:"base_table_id,omitempty"`
	Name        string                    `msgpack:"name"`
	XPos        float64                   `msgpack:"x_pos"`
	YPos        float64                   `msgpack:"y_pos"`
	External    *BundleExternalOccurrence `msgpack:"external,omitempty"`
}

// BundleExternalOccurrence names the file reference an external occurrence
// goes through, by name, and the table's name in the other solution, empty
// while it is not known.
type BundleExternalOccurrence struct {
	FileReference string `msgpack:"file_reference"`
	Table         string `msgpack:"table,omitempty"`
}

// bundleOccurrence converts a catalog occurrence to a solution file's.
func bundleOccurrence(o TableOccurrence) BundleOccurrence {
	b := BundleOccurrence{ID: o.ID, BaseTableID: o.BaseTableID, Name: o.Name, XPos: o.XPos, YPos: o.YPos}
	if o.External != nil {
		b.External = &BundleExternalOccurrence{FileReference: o.External.FileReference, Table: o.External.Table}
	}
	return b
}

// BundleRelationship is a relationship of a solution file. Its first match
// criterion is LeftColumnID, Operator and RightColumnID. A relationship with
// more than one also lists all of them, the first included, in Predicates; a
// file written before relationships could have several has none there.
//
// A side on an occurrence of another solution's table has no column id: its
// field is named, by LeftColumnName or RightColumnName for the first
// criterion and in Predicates for every one.
//
// AllowCreation, CascadeDelete and SortRelated are the right side's options;
// the left side's are LeftAllowCreation, LeftCascadeDelete and
// LeftSortRelated, which a file written before they were kept does not have:
// its relationships have none on the left, as they had none there before.
type BundleRelationship struct {
	ID                string            `msgpack:"id"`
	Name              string            `msgpack:"name"`
	LeftOccurrenceID  string            `msgpack:"left_occurrence_id"`
	LeftColumnID      string            `msgpack:"left_column_id"`
	RightOccurrenceID string            `msgpack:"right_occurrence_id"`
	RightColumnID     string            `msgpack:"right_column_id"`
	Operator          string            `msgpack:"operator"`
	Predicates        []BundlePredicate `msgpack:"predicates,omitempty"`
	AllowCreation     bool              `msgpack:"allow_creation"`
	CascadeDelete     bool              `msgpack:"cascade_delete"`
	SortRelated       string            `msgpack:"sort_related"`
	LeftColumnName    string            `msgpack:"left_column_name,omitempty"`
	RightColumnName   string            `msgpack:"right_column_name,omitempty"`
	LeftAllowCreation bool              `msgpack:"left_allow_creation,omitempty"`
	LeftCascadeDelete bool              `msgpack:"left_cascade_delete,omitempty"`
	LeftSortRelated   string            `msgpack:"left_sort_related,omitempty"`
}

// BundlePredicate is one match criterion of a relationship of a solution
// file. The field names are informative: a criterion is resolved by its ids,
// except on a side of an external occurrence, which has only its name.
type BundlePredicate struct {
	LeftColumnID    string `msgpack:"left_column_id"`
	LeftColumnName  string `msgpack:"left_column_name,omitempty"`
	Operator        string `msgpack:"operator"`
	RightColumnID   string `msgpack:"right_column_id"`
	RightColumnName string `msgpack:"right_column_name,omitempty"`
}

// PredicateList is the relationship's criteria, in order: Predicates, or the
// one pair of a relationship that has no list.
func (r BundleRelationship) PredicateList() []BundlePredicate {
	if len(r.Predicates) > 0 {
		return r.Predicates
	}
	return []BundlePredicate{{
		LeftColumnID: r.LeftColumnID, LeftColumnName: r.LeftColumnName, Operator: r.Operator,
		RightColumnID: r.RightColumnID, RightColumnName: r.RightColumnName,
	}}
}

// WithPredicates sets the relationship's criteria: the first is also its
// pair, and the list is kept only when there is more than one, so a
// single-criterion relationship is written as it always was. The pair keeps a
// field's name only on a side that has no id, an external one.
func (r *BundleRelationship) WithPredicates(predicates []BundlePredicate) {
	if len(predicates) == 0 {
		return
	}
	first := predicates[0]
	r.LeftColumnID, r.Operator, r.RightColumnID = first.LeftColumnID, first.Operator, first.RightColumnID
	r.LeftColumnName, r.RightColumnName = "", ""
	if first.LeftColumnID == "" {
		r.LeftColumnName = first.LeftColumnName
	}
	if first.RightColumnID == "" {
		r.RightColumnName = first.RightColumnName
	}
	r.Predicates = nil
	if len(predicates) > 1 {
		r.Predicates = predicates
	}
}

// bundlePredicates converts catalog criteria to a solution file's.
func bundlePredicates(predicates []RelationshipPredicate) []BundlePredicate {
	out := make([]BundlePredicate, len(predicates))
	for i, p := range predicates {
		out[i] = BundlePredicate{
			LeftColumnID: p.LeftColumnID, LeftColumnName: p.LeftColumnName, Operator: p.Operator,
			RightColumnID: p.RightColumnID, RightColumnName: p.RightColumnName,
		}
	}
	return out
}

type BundleLayout struct {
	ID                string                 `msgpack:"id"`
	Name              string                 `msgpack:"name"`
	TableOccurrenceID string                 `msgpack:"table_occurrence_id"`
	Definition        map[string]interface{} `msgpack:"definition"`
}

type BundleStep struct {
	ID           string                 `msgpack:"id"`
	SequenceIdx  int                    `msgpack:"sequence_idx"`
	StepType     string                 `msgpack:"step_type"`
	Params       map[string]interface{} `msgpack:"params"`
	IsEnabled    bool                   `msgpack:"is_enabled"`
	ParentStepID *string                `msgpack:"parent_step_id,omitempty"`
}

type BundleScript struct {
	ID           string       `msgpack:"id"`
	Name         string       `msgpack:"name"`
	ContextTable string       `msgpack:"context_table"`
	FolderID     *string      `msgpack:"folder_id,omitempty"`
	IsActive     bool         `msgpack:"is_active"`
	Steps        []BundleStep `msgpack:"steps"`
}

type BundlePermission struct {
	LayoutID    string `msgpack:"layout_id"`
	AccessLevel string `msgpack:"access_level"`
}

type BundleTablePermission struct {
	TableName       string `msgpack:"table_name" json:"table_name"`
	AccessLevel     string `msgpack:"access_level" json:"access_level"`
	CanCreate       bool   `msgpack:"can_create" json:"can_create"`
	CanEdit         bool   `msgpack:"can_edit" json:"can_edit"`
	CanDelete       bool   `msgpack:"can_delete" json:"can_delete"`
	CanModifySchema bool   `msgpack:"can_modify_schema" json:"can_modify_schema"`
}

type BundleAccount struct {
	ID               string                  `msgpack:"id"`
	Username         string                  `msgpack:"username"`
	Role             string                  `msgpack:"role"`
	IsActive         bool                    `msgpack:"is_active"`
	Permissions      []BundlePermission      `msgpack:"permissions"`
	TablePermissions []BundleTablePermission `msgpack:"table_permissions,omitempty"`
}

// SolutionBundle is the content of a .f4p solution file.
// BundleValueList carries a value list (#31). A list taken from a field keeps
// the ids of its table, fields and occurrences so they can be remapped on
// import, like every other reference in the bundle.
type BundleValueList struct {
	ID                  string  `msgpack:"id"`
	Name                string  `msgpack:"name"`
	Kind                string  `msgpack:"kind"`
	CustomValues        string  `msgpack:"custom_values,omitempty"`
	SourceTableID       *string `msgpack:"source_table_id,omitempty"`
	SourceColumnID      *string `msgpack:"source_column_id,omitempty"`
	SecondColumnID      *string `msgpack:"second_column_id,omitempty"`
	ShowSecondOnly      bool    `msgpack:"show_second_only,omitempty"`
	SortBy              string  `msgpack:"sort_by,omitempty"`
	SourceOccurrenceID  *string `msgpack:"source_occurrence_id,omitempty"`
	RelatedOccurrenceID *string `msgpack:"related_occurrence_id,omitempty"`
	// A list taken from a field of another solution's table has no source
	// table or field: SourceOccurrenceID is the external occurrence it is
	// read through and the field is named, as it has no id here (#107).
	SourceColumnName string `msgpack:"source_column_name,omitempty"`
	// SecondColumnName is such a list's second field, by name.
	SecondColumnName string `msgpack:"second_column_name,omitempty"`
}

// remappedID is an optional reference of a bundle rewritten to the
// destination's id; nil stays nil.
func remappedID(ids map[string]string, id *string) *string {
	if id == nil {
		return nil
	}
	mapped := ids[*id]
	return &mapped
}

// BundleSavedFind carries a named find (#34). It names the table by name, as
// tables are matched by name on import, so there is no id to remap. The
// account that saved it is not carried: a find is shared with everyone who
// can search the table, and an imported find has no owner account.
type BundleSavedFind struct {
	Name      string             `msgpack:"name"`
	TableName string             `msgpack:"table_name"`
	Requests  []SavedFindRequest `msgpack:"requests"`
}

// BundleDataSource carries a registered external SQL connection (#47). It
// holds no password, because the catalog holds none either: a bundle can be
// opened and read by anyone it is sent to.
type BundleDataSource struct {
	Name        string `msgpack:"name"`
	Engine      string `msgpack:"engine"`
	Host        string `msgpack:"host"`
	Port        int    `msgpack:"port"`
	Database    string `msgpack:"database,omitempty"`
	Username    string `msgpack:"username,omitempty"`
	Schema      string `msgpack:"schema,omitempty"`
	TLS         bool   `msgpack:"tls"`
	PasswordEnv string `msgpack:"password_env,omitempty"`
}

// BundleTransfer carries a transfer definition (#96). It names its tables
// and its solutions by name, as a transfer always has, and it holds no
// credentials, because the catalog holds none either: which accounts may run
// a transfer is decided when it runs, from the session tokens presented for
// each end.
//
// A definition whose far end is empty means "the solution holding this
// definition", so a bundle restored into another database keeps working
// without being edited.
type BundleTransfer struct {
	Name        string             `msgpack:"name"`
	Source      TransferEnd        `msgpack:"source"`
	Destination TransferEnd        `msgpack:"destination"`
	FoundSet    []data.FindRequest `msgpack:"found_set,omitempty"`
	Sort        []data.SortField   `msgpack:"sort,omitempty"`
	Mappings    []TransferMapping  `msgpack:"mappings"`
	MatchFields []string           `msgpack:"match_fields,omitempty"`
	Mode        string             `msgpack:"mode"`
	OnUnmatched string             `msgpack:"on_unmatched,omitempty"`
	OnRejected  string             `msgpack:"on_rejected"`
	DateOrder   string             `msgpack:"date_order"`
}

// BundleFileReference carries a named link to another solution (#107). The
// solution is named by its database name, as a reference always is. A
// reference with no database is one that has not been mapped yet — the
// assisted migration emits those for every external file a Perform Script
// step calls, and for every External Data Source of the file, with its
// FileMaker paths.
type BundleFileReference struct {
	Name     string   `msgpack:"name"`
	Database string   `msgpack:"database,omitempty"`
	Paths    []string `msgpack:"paths,omitempty"`
}

// BundleCustomFunction carries a custom function: its name, its parameters
// and its formula, which reads the solution's fields by the names the bundle
// gives them, and who may see it in the design.
type BundleCustomFunction struct {
	Name       string   `msgpack:"name"`
	Parameters []string `msgpack:"parameters,omitempty"`
	Formula    string   `msgpack:"formula"`
	Access     string   `msgpack:"access,omitempty"`
}

// BundleMenuSet carries a menu set (#52). Its items name scripts and layouts
// by id, and layouts may name a menu set by id, so those ids are remapped on
// import like every other reference.
type BundleMenuSet struct {
	ID        string       `msgpack:"id,omitempty"`
	Name      string       `msgpack:"name"`
	IsDefault bool         `msgpack:"is_default"`
	Menus     []CustomMenu `msgpack:"menus"`
}

type SolutionBundle struct {
	Format             string                 `msgpack:"format"`
	Version            string                 `msgpack:"version"`
	SolutionName       string                 `msgpack:"solution_name"`
	ExportedAt         string                 `msgpack:"exported_at"`
	DatabaseConnection BundleConnection       `msgpack:"database_connection"`
	Tables             []BundleTable          `msgpack:"tables"`
	TableOccurrences   []BundleOccurrence     `msgpack:"table_occurrences"`
	Relationships      []BundleRelationship   `msgpack:"relationships"`
	Layouts            []BundleLayout         `msgpack:"layouts"`
	Scripts            []BundleScript         `msgpack:"scripts"`
	ValueLists         []BundleValueList      `msgpack:"value_lists,omitempty"`
	Users              []BundleAccount        `msgpack:"users"`
	Privileges         []RolePrivileges       `msgpack:"privileges,omitempty"`
	SavedFinds         []BundleSavedFind      `msgpack:"saved_finds,omitempty"`
	DataSources        []BundleDataSource     `msgpack:"data_sources,omitempty"`
	MenuSets           []BundleMenuSet        `msgpack:"menu_sets,omitempty"`
	Transfers          []BundleTransfer       `msgpack:"transfers,omitempty"`
	FileReferences     []BundleFileReference  `msgpack:"file_references,omitempty"`
	CustomFunctions    []BundleCustomFunction `msgpack:"custom_functions,omitempty"`
	FileOptions        map[string]interface{} `msgpack:"file_options,omitempty"`
	PageSetup          map[string]interface{} `msgpack:"page_setup,omitempty"`
}

// DatabaseDataBundle is the content of a .f4data file (all user records).
type DatabaseDataBundle struct {
	Format       string                              `msgpack:"format"`
	Version      string                              `msgpack:"version"`
	DatabaseName string                              `msgpack:"database_name"`
	ExportedAt   time.Time                           `msgpack:"exported_at"`
	TablesData   map[string][]map[string]interface{} `msgpack:"tables_data"`
}

// ExportOptions describe a solution export. FileOptions and PageSetup are
// client settings stored in the file as they are (minus any password).
type ExportOptions struct {
	SolutionName string
	Connection   BundleConnection
	FileOptions  map[string]interface{}
	PageSetup    map[string]interface{}
}

// ─── Export ──────────────────────────────────────────────────────────────────

// ExportSolution packages the design of the active database: tables and
// field options, occurrences, relationships, layouts, scripts and accounts
// (without passwords).
func (s *Service) ExportSolution(ctx context.Context, opts ExportOptions) ([]byte, error) {
	b, err := s.buildSolutionBundle(ctx, opts)
	if err != nil {
		return nil, err
	}
	return msgpack.Marshal(b)
}

func (s *Service) buildSolutionBundle(ctx context.Context, opts ExportOptions) (*SolutionBundle, error) {
	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing tables: %w", err)
	}
	occurrences, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing occurrences: %w", err)
	}
	relationships, err := s.ListRelationships(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing relationships: %w", err)
	}
	layouts, err := s.ListLayouts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing layouts: %w", err)
	}
	scripts, err := s.ListScripts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing scripts: %w", err)
	}
	valueLists, err := s.ListValueLists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing value lists: %w", err)
	}
	users, err := s.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing accounts: %w", err)
	}
	privileges, err := s.ListRolePrivileges(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing role privileges: %w", err)
	}
	savedFinds, err := s.ListSavedFinds(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed listing saved finds: %w", err)
	}
	dataSources, err := s.ListDataSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing data sources: %w", err)
	}
	menuSets, err := s.ListMenuSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing menu sets: %w", err)
	}
	transfers, err := s.ListTransfers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing transfer definitions: %w", err)
	}
	fileReferences, err := s.ListFileReferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing file references: %w", err)
	}
	customFunctions, err := s.ListCustomFunctions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing custom functions: %w", err)
	}

	conn := opts.Connection
	if conn.Engine == "" {
		conn.Engine = string(s.driver.Dialect().Engine())
	}
	b := &SolutionBundle{
		Format:             SolutionFormat,
		Version:            SolutionVersion,
		SolutionName:       opts.SolutionName,
		ExportedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		DatabaseConnection: conn,
		Tables:             make([]BundleTable, 0, len(tables)),
		TableOccurrences:   make([]BundleOccurrence, 0, len(occurrences)),
		Relationships:      make([]BundleRelationship, 0, len(relationships)),
		Layouts:            make([]BundleLayout, 0, len(layouts)),
		Scripts:            make([]BundleScript, 0, len(scripts)),
		ValueLists:         make([]BundleValueList, 0, len(valueLists)),
		Users:              make([]BundleAccount, 0, len(users)),
		Privileges:         privileges,
		SavedFinds:         make([]BundleSavedFind, 0, len(savedFinds)),
		DataSources:        make([]BundleDataSource, 0, len(dataSources)),
		MenuSets:           make([]BundleMenuSet, 0, len(menuSets)),
		Transfers:          make([]BundleTransfer, 0, len(transfers)),
		FileReferences:     make([]BundleFileReference, 0, len(fileReferences)),
		FileOptions:        withoutSecrets(opts.FileOptions),
		PageSetup:          opts.PageSetup,
	}
	for _, t := range tables {
		bt := BundleTable{ID: t.ID, Name: t.Name, DisplayName: t.DisplayName, Description: t.Description}
		for _, c := range t.Columns {
			col := BundleColumn{
				ID: c.ID, Name: c.Name, DisplayName: c.DisplayName, FieldType: string(c.FieldType),
				IsNullable: c.IsNullable, IsPrimaryKey: c.IsPrimaryKey,
				DefaultValue: c.DefaultValue, CalculationFormula: c.CalculationFormula, ValidationRules: c.ValidationRules,
			}
			if c.MaxRepetitions > 1 {
				col.MaxRepetitions = c.MaxRepetitions
			}
			col.SourceFieldID = c.SourceFieldID
			bt.Columns = append(bt.Columns, col)
		}
		b.Tables = append(b.Tables, bt)
	}
	for _, o := range occurrences {
		b.TableOccurrences = append(b.TableOccurrences, bundleOccurrence(o))
	}
	for _, r := range relationships {
		br := BundleRelationship{
			ID: r.ID, Name: r.Name,
			LeftOccurrenceID: r.LeftOccurrenceID, LeftColumnID: r.LeftColumnID,
			RightOccurrenceID: r.RightOccurrenceID, RightColumnID: r.RightColumnID,
			Operator: r.Operator, AllowCreation: r.AllowCreation, CascadeDelete: r.CascadeDelete, SortRelated: r.SortRelated,
			LeftAllowCreation: r.LeftAllowCreation, LeftCascadeDelete: r.LeftCascadeDelete, LeftSortRelated: r.LeftSortRelated,
		}
		br.WithPredicates(bundlePredicates(r.Predicates))
		b.Relationships = append(b.Relationships, br)
	}
	for _, l := range layouts {
		def, err := jsonObject(l.Definition)
		if err != nil {
			return nil, fmt.Errorf("layout %q has an invalid definition: %w", l.Name, err)
		}
		b.Layouts = append(b.Layouts, BundleLayout{ID: l.ID, Name: l.Name, TableOccurrenceID: l.TableOccurrenceID, Definition: def})
	}
	for _, sc := range scripts {
		bs := BundleScript{ID: sc.ID, Name: sc.Name, ContextTable: sc.ContextTable, FolderID: sc.FolderID, IsActive: sc.IsActive, Steps: make([]BundleStep, 0, len(sc.Steps))}
		for _, st := range sc.Steps {
			params, err := jsonObject(st.Params)
			if err != nil {
				return nil, fmt.Errorf("script %q step %d has invalid parameters: %w", sc.Name, st.SequenceIdx, err)
			}
			bs.Steps = append(bs.Steps, BundleStep{ID: st.ID, SequenceIdx: st.SequenceIdx, StepType: st.StepType, Params: params, IsEnabled: st.IsEnabled, ParentStepID: st.ParentStepID})
		}
		b.Scripts = append(b.Scripts, bs)
	}
	for _, vl := range valueLists {
		b.ValueLists = append(b.ValueLists, BundleValueList{
			ID: vl.ID, Name: vl.Name, Kind: vl.Kind, CustomValues: vl.CustomValues,
			SourceTableID: vl.SourceTableID, SourceColumnID: vl.SourceColumnID,
			SecondColumnID: vl.SecondColumnID, ShowSecondOnly: vl.ShowSecondOnly, SortBy: vl.SortBy,
			SourceOccurrenceID: vl.SourceOccurrenceID, RelatedOccurrenceID: vl.RelatedOccurrenceID,
			SourceColumnName: vl.SourceColumnName, SecondColumnName: vl.SecondColumnName,
		})
	}
	for _, f := range savedFinds {
		b.SavedFinds = append(b.SavedFinds, BundleSavedFind{
			Name: f.Name, TableName: f.TableName, Requests: f.Requests,
		})
	}
	for _, m := range menuSets {
		b.MenuSets = append(b.MenuSets, BundleMenuSet{
			ID: m.ID, Name: m.Name, IsDefault: m.IsDefault, Menus: m.Menus,
		})
	}
	for _, t := range transfers {
		b.Transfers = append(b.Transfers, BundleTransfer{
			Name: t.Name, Source: t.Source, Destination: t.Destination,
			FoundSet: t.FoundSet, Sort: t.Sort, Mappings: t.Mappings,
			MatchFields: t.MatchFields, Mode: t.Mode, OnUnmatched: t.OnUnmatched,
			OnRejected: t.OnRejected, DateOrder: t.DateOrder,
		})
	}
	for _, f := range fileReferences {
		b.FileReferences = append(b.FileReferences, BundleFileReference{Name: f.Name, Database: f.Database, Paths: f.Paths})
	}
	for _, f := range customFunctions {
		b.CustomFunctions = append(b.CustomFunctions, BundleCustomFunction{Name: f.Name, Parameters: f.Parameters, Formula: f.Formula, Access: f.Access})
	}
	for _, d := range dataSources {
		b.DataSources = append(b.DataSources, BundleDataSource{
			Name: d.Name, Engine: d.Engine, Host: d.Host, Port: d.Port, Database: d.Database,
			Username: d.Username, Schema: d.Schema, TLS: d.TLS, PasswordEnv: d.PasswordEnv,
		})
	}
	for _, u := range users {
		perms, err := s.GetUserPermissions(ctx, u.ID)
		if err != nil {
			return nil, fmt.Errorf("failed reading permissions of %s: %w", u.Username, err)
		}
		tablePerms, err := s.GetUserTablePermissions(ctx, u.ID)
		if err != nil {
			tablePerms = nil
		}
		acc := BundleAccount{
			ID:               u.ID,
			Username:         u.Username,
			Role:             u.Role,
			IsActive:         u.IsActive,
			Permissions:      make([]BundlePermission, 0, len(perms)),
			TablePermissions: make([]BundleTablePermission, 0, len(tablePerms)),
		}
		for _, p := range perms {
			acc.Permissions = append(acc.Permissions, BundlePermission{LayoutID: p.LayoutID, AccessLevel: p.AccessLevel})
		}
		for _, tp := range tablePerms {
			acc.TablePermissions = append(acc.TablePermissions, BundleTablePermission{
				TableName:       tp.TableName,
				AccessLevel:     tp.AccessLevel,
				CanCreate:       tp.CanCreate,
				CanEdit:         tp.CanEdit,
				CanDelete:       tp.CanDelete,
				CanModifySchema: tp.CanModifySchema,
			})
		}
		b.Users = append(b.Users, acc)
	}
	return b, nil
}

// withoutSecrets drops remembered passwords from client settings.
func withoutSecrets(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if strings.Contains(strings.ToLower(k), "password") {
			continue
		}
		out[k] = v
	}
	return out
}

// jsonObject decodes a stored JSON object for MessagePack, keeping integers
// as integers (the client reads e.g. tab orders as ints).
func jsonObject(raw json.RawMessage) (map[string]interface{}, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return map[string]interface{}{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v map[string]interface{}
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if v == nil {
		v = map[string]interface{}{}
	}
	return fixNumbers(v).(map[string]interface{}), nil
}

func fixNumbers(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, x := range t {
			t[k] = fixNumbers(x)
		}
		return t
	case []interface{}:
		for i, x := range t {
			t[i] = fixNumbers(x)
		}
		return t
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64()
		return f
	}
	return v
}

// ─── Import ──────────────────────────────────────────────────────────────────

// ImportReport describes what a solution import changed.
type ImportReport struct {
	SolutionName            string                 `json:"solution_name"`
	TablesCreated           int                    `json:"tables_created"`
	ColumnsCreated          int                    `json:"columns_created"`
	OccurrencesCreated      int                    `json:"occurrences_created"`
	RelationshipsCreated    int                    `json:"relationships_created"`
	RelationshipsUpdated    int                    `json:"relationships_updated"`
	LayoutsCreated          int                    `json:"layouts_created"`
	LayoutsUpdated          int                    `json:"layouts_updated"`
	ScriptsCreated          int                    `json:"scripts_created"`
	ScriptsUpdated          int                    `json:"scripts_updated"`
	ValueListsCreated       int                    `json:"value_lists_created"`
	ValueListsUpdated       int                    `json:"value_lists_updated"`
	AccountsCreated         int                    `json:"accounts_created"`
	PrivilegesUpdated       int                    `json:"privileges_updated"`
	SavedFindsCreated       int                    `json:"saved_finds_created"`
	DataSourcesCreated      int                    `json:"data_sources_created"`
	MenuSetsCreated         int                    `json:"menu_sets_created"`
	TransfersCreated        int                    `json:"transfers_created"`
	FileReferencesCreated   int                    `json:"file_references_created"`
	CustomFunctionsCreated  int                    `json:"custom_functions_created"`
	AccountsPendingPassword []string               `json:"accounts_pending_password"`
	Warnings                []string               `json:"warnings,omitempty"` // restored as saved but not working, such as a value list of an unindexable field
	FileOptions             map[string]interface{} `json:"file_options,omitempty"`
	PageSetup               map[string]interface{} `json:"page_setup,omitempty"`
	// Totals in the bundle, kept for older clients.
	TablesCount  int `json:"tables_count"`
	LayoutsCount int `json:"layouts_count"`
}

// DecodeSolutionBundle reads a solution file: the current format, client
// files of version 1.0 (same keys) and legacy server exports of version 1.0.
func DecodeSolutionBundle(data []byte) (*SolutionBundle, error) {
	// Check declared sizes before the decoder allocates from them (#13).
	if err := msgpackguard.Check(data, msgpackguard.Limits{}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	var b SolutionBundle
	errCurrent := msgpack.Unmarshal(data, &b)
	if errCurrent == nil && !looksLikeLegacyServerBundle(&b) {
		return checkBundleHeader(&b)
	}
	var legacy legacyServerBundle
	if err := msgpack.Unmarshal(data, &legacy); err == nil && legacy.Format == SolutionFormat && len(legacy.Tables)+len(legacy.Layouts) > 0 {
		converted, err := legacy.toCurrent()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
		}
		return checkBundleHeader(converted)
	}
	if errCurrent != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBundle, errCurrent)
	}
	return checkBundleHeader(&b)
}

func checkBundleHeader(b *SolutionBundle) (*SolutionBundle, error) {
	if b.Format != SolutionFormat {
		return nil, fmt.Errorf("%w: not a File4Base solution file (format %q)", ErrInvalidBundle, b.Format)
	}
	if b.Version != "1.0" && b.Version != SolutionVersion {
		return nil, fmt.Errorf("%w: unsupported solution file version %q", ErrInvalidBundle, b.Version)
	}
	return b, nil
}

// looksLikeLegacyServerBundle detects 1.0 server exports, whose nested keys
// were Go field names (Name, Columns...) and so decode as empty objects.
func looksLikeLegacyServerBundle(b *SolutionBundle) bool {
	for _, t := range b.Tables {
		if t.Name == "" {
			return true
		}
	}
	for _, l := range b.Layouts {
		if l.Name == "" {
			return true
		}
	}
	return false
}

// ImportSolution merges a solution file into the active database. The whole
// bundle is validated before anything is written; if a write fails, what the
// import created is removed and what it updated is restored.
func (s *Service) ImportSolution(ctx context.Context, data []byte) (*ImportReport, error) {
	b, err := DecodeSolutionBundle(data)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureSystemTables(ctx); err != nil {
		return nil, fmt.Errorf("failed ensuring system tables: %w", err)
	}
	plan, err := s.planImport(ctx, b)
	if err != nil {
		return nil, err
	}

	report := &ImportReport{SolutionName: b.SolutionName, TablesCount: len(b.Tables), LayoutsCount: len(b.Layouts), AccountsPendingPassword: []string{}}
	var undo []func()
	rollback := func(cause error) error {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return fmt.Errorf("solution import failed and was rolled back: %w", cause)
	}
	// Undo steps run after the request may have been cancelled.
	bg := context.WithoutCancel(ctx)

	// 0. Custom functions, before any formula that calls them is saved.
	addedFunctions, err := s.importCustomFunctions(ctx, b.CustomFunctions)
	for _, id := range addedFunctions {
		undo = append(undo, func() { _ = s.deleteCustomFunction(bg, id) })
	}
	if err != nil {
		return nil, rollback(fmt.Errorf("restoring the custom functions: %w", err))
	}
	report.CustomFunctionsCreated = len(addedFunctions)

	// 1. Tables and columns
	tableIDs := map[string]string{}  // bundle table id -> destination id
	columnIDs := map[string]string{} // bundle column id -> destination id
	createdTables := map[string]bool{}
	var addedCalcs []ColumnMetadata // their formulas are checked after step 3
	for _, t := range b.Tables {
		name := dbal.NormalizeIdentifier(t.Name)
		existing := plan.tables[name]
		var dstID string
		existingCols := map[string]string{}
		if existing == nil {
			created, err := s.CreateTable(ctx, t.DisplayName, name)
			if err != nil {
				return nil, rollback(fmt.Errorf("creating table %s: %w", name, err))
			}
			undo = append(undo, func() { _ = s.DeleteTable(bg, created.ID) })
			report.TablesCreated++
			dstID = created.ID
			createdTables[created.ID] = true
			for _, c := range created.Columns {
				existingCols[c.Name] = c.ID
			}
		} else {
			dstID = existing.ID
			for _, c := range existing.Columns {
				existingCols[c.Name] = c.ID
			}
		}
		tableIDs[t.ID] = dstID
		for _, calcPass := range []bool{false, true} {
			for _, c := range t.Columns {
				isCalc := strings.EqualFold(c.FieldType, string(dbal.FieldTypeCalculation))
				if isCalc != calcPass {
					continue
				}
				cname := dbal.NormalizeIdentifier(c.Name)
				if id, ok := existingCols[cname]; ok {
					columnIDs[c.ID] = id
					continue
				}
				if c.IsPrimaryKey {
					return nil, rollback(fmt.Errorf("table %s: primary key %s does not exist in the destination", name, cname))
				}
				col, err := s.addColumn(ctx, dstID, ColumnMetadata{
					Name: cname, DisplayName: c.DisplayName, FieldType: dbal.AgnosticFieldType(c.FieldType), IsNullable: c.IsNullable,
					DefaultValue: c.DefaultValue, CalculationFormula: c.CalculationFormula, ValidationRules: c.ValidationRules,
					MaxRepetitions: c.MaxRepetitions, SourceFieldID: c.SourceFieldID,
				}, true)
				if err != nil {
					return nil, rollback(fmt.Errorf("adding field %s to table %s: %w", cname, name, err))
				}
				if existing != nil {
					tblID, colID := dstID, col.ID
					undo = append(undo, func() { _ = s.DeleteColumn(bg, tblID, colID) })
				}
				report.ColumnsCreated++
				if isCalc {
					addedCalcs = append(addedCalcs, *col)
				}
				columnIDs[c.ID] = col.ID
				existingCols[cname] = col.ID
			}
		}
	}

	// 1b. File references (#107), matched by name, before the occurrences
	// that go through them. A reference that already exists keeps the
	// solution it is mapped to: restoring a bundle must not silently repoint
	// a script or an occurrence at a different solution.
	for _, f := range b.FileReferences {
		if _, err := s.GetFileReferenceByName(ctx, f.Name); err == nil {
			continue
		} else if !errors.Is(err, ErrFileReferenceNotFound) {
			return nil, rollback(fmt.Errorf("reading the file reference %q: %w", f.Name, err))
		}
		created, err := s.CreateFileReference(ctx, FileReferenceInput{Name: f.Name, Database: f.Database, Paths: f.Paths})
		if err != nil {
			return nil, rollback(fmt.Errorf("restoring the file reference %q: %w", f.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteFileReference(bg, created.ID) })
		report.FileReferencesCreated++
	}

	// 2. Table occurrences (tables created above brought their default one)
	currentOccs, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, rollback(err)
	}
	occByName := map[string]TableOccurrence{}
	for _, o := range currentOccs {
		occByName[o.Name] = o
	}
	occIDs := map[string]string{}
	for _, o := range b.TableOccurrences {
		dstTable := tableIDs[o.BaseTableID]
		if existing, ok := occByName[o.Name]; ok {
			if o.External == nil && existing.BaseTableID != dstTable ||
				o.External != nil && (existing.External == nil || !strings.EqualFold(existing.External.FileReference, strings.TrimSpace(o.External.FileReference))) {
				return nil, rollback(fmt.Errorf("table occurrence %q already exists for another table", o.Name))
			}
			if existing.XPos != o.XPos || existing.YPos != o.YPos {
				newX, newY := o.XPos, o.YPos
				prevX, prevY := existing.XPos, existing.YPos
				exID := existing.ID
				if _, err := s.UpdateTableOccurrence(ctx, exID, UpdateOccurrenceInput{XPos: &newX, YPos: &newY}); err != nil {
					return nil, rollback(fmt.Errorf("updating table occurrence %q position: %w", o.Name, err))
				}
				undo = append(undo, func() {
					px, py := prevX, prevY
					_, _ = s.UpdateTableOccurrence(bg, exID, UpdateOccurrenceInput{XPos: &px, YPos: &py})
				})
			}
			occIDs[o.ID] = existing.ID
			continue
		}
		in := CreateOccurrenceInput{BaseTableID: dstTable, Name: o.Name, XPos: o.XPos, YPos: o.YPos}
		if o.External != nil {
			in.BaseTableID = ""
			in.External = &ExternalOccurrenceInput{FileReference: o.External.FileReference, Table: o.External.Table}
		}
		created, err := s.CreateTableOccurrence(ctx, in)
		if err != nil {
			return nil, rollback(fmt.Errorf("creating table occurrence %q: %w", o.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteTableOccurrence(bg, created.ID) })
		report.OccurrencesCreated++
		occIDs[o.ID] = created.ID
	}

	// A table this import created came with a default occurrence named after
	// the table itself. Where the file declares that table's occurrences, the
	// default is not one of them and nothing in the file points at it, so it
	// is an occurrence the import invented: a FileMaker file with the
	// occurrences Surveys and SurveysByTitle over one base table arrived with
	// four boxes in the graph instead of two, and comparing the migrated graph
	// against the source's then failed on objects the source never had (#112).
	//
	// Only a table created here can have one, and only occurrences no
	// declaration claimed are removed, so a solution file that carries no
	// occurrences at all keeps the defaults it relies on. No undo is recorded:
	// a rollback deletes the table itself, and its occurrences with it.
	declaredFor := map[string]bool{}
	for _, o := range b.TableOccurrences {
		if dst, ok := tableIDs[o.BaseTableID]; ok {
			declaredFor[dst] = true
		}
	}
	if len(declaredFor) > 0 {
		claimed := map[string]bool{}
		for _, id := range occIDs {
			claimed[id] = true
		}
		after, err := s.ListTableOccurrences(ctx)
		if err != nil {
			return nil, rollback(err)
		}
		for _, occ := range after {
			if !createdTables[occ.BaseTableID] || !declaredFor[occ.BaseTableID] || claimed[occ.ID] {
				continue
			}
			if err := s.DeleteTableOccurrence(ctx, occ.ID); err != nil {
				return nil, rollback(fmt.Errorf("removing the default table occurrence %q, which this file does not declare: %w", occ.Name, err))
			}
		}
	}

	// 3. Relationships (an identical one is not duplicated)
	currentRels, err := s.ListRelationships(ctx)
	if err != nil {
		return nil, rollback(err)
	}
	// A relationship is the same one when it joins the same two occurrences on
	// the same criteria, in the same order.
	relKey := func(lo, ro string, predicates []RelationshipPredicate) string {
		parts := []string{lo, ro}
		for _, p := range predicates {
			op, err := NormalizeRelationshipOperator(p.Operator)
			if err != nil {
				op = p.Operator
			}
			parts = append(parts, predicateSide(p.LeftColumnID, p.LeftColumnName), op, predicateSide(p.RightColumnID, p.RightColumnName))
		}
		return strings.Join(parts, "|")
	}
	// The id each relationship of the file ends up with, so the portals and
	// related fields of a layout can be pointed at it (#36).
	relIDs := map[string]string{}
	haveRel := map[string]string{}
	existingRel := map[string]RelationshipMetadata{}
	for _, r := range currentRels {
		haveRel[relKey(r.LeftOccurrenceID, r.RightOccurrenceID, r.Predicates)] = r.ID
		existingRel[r.ID] = r
	}
	for _, r := range b.Relationships {
		filePredicates := r.PredicateList()
		predicates := make([]RelationshipPredicate, len(filePredicates))
		for i, p := range filePredicates {
			predicates[i] = RelationshipPredicate{
				LeftColumnID: columnIDs[p.LeftColumnID], LeftColumnName: p.LeftColumnName, Operator: p.Operator,
				RightColumnID: columnIDs[p.RightColumnID], RightColumnName: p.RightColumnName,
			}
		}
		in := CreateRelationshipInput{
			Name:             r.Name,
			LeftOccurrenceID: occIDs[r.LeftOccurrenceID], RightOccurrenceID: occIDs[r.RightOccurrenceID],
			Predicates:    predicates,
			AllowCreation: r.AllowCreation, CascadeDelete: r.CascadeDelete,
			LeftAllowCreation: r.LeftAllowCreation, LeftCascadeDelete: r.LeftCascadeDelete,
		}
		if r.SortRelated != "" {
			sort := r.SortRelated
			in.SortRelated = &sort
		}
		if r.LeftSortRelated != "" {
			sort := r.LeftSortRelated
			in.LeftSortRelated = &sort
		}
		key := relKey(in.LeftOccurrenceID, in.RightOccurrenceID, in.Predicates)
		if existing := haveRel[key]; existing != "" {
			// The same join is already there: a layout that points at the
			// file's relationship is pointed at the one in the database, and
			// the relationship takes the file's options for each side, so a
			// design imported again corrects what an earlier import set.
			relIDs[r.ID] = existing
			cur, ok := existingRel[existing]
			if !ok {
				continue // created from an earlier entry of this file
			}
			delete(existingRel, existing)
			updated, err := s.importRelationshipOptions(ctx, cur, in)
			if err != nil {
				return nil, rollback(fmt.Errorf("updating the options of relationship %q: %w", r.Name, err))
			}
			if updated {
				undo = append(undo, func() { _, _ = s.UpdateRelationship(bg, cur.ID, relationshipOptionsOf(cur)) })
				report.RelationshipsUpdated++
			}
			continue
		}
		created, err := s.CreateRelationship(ctx, in)
		if err != nil {
			return nil, rollback(fmt.Errorf("creating relationship %q: %w", r.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteRelationship(bg, created.ID) })
		haveRel[key] = created.ID
		relIDs[r.ID] = created.ID
		report.RelationshipsCreated++
	}
	for _, col := range addedCalcs {
		if err := s.checkCalculation(ctx, col.TableID, col.ID, col.Name, col.FieldType, col.CalculationFormula); err != nil {
			return nil, rollback(fmt.Errorf("field %s: %w", col.Name, err))
		}
	}

	// 4. Scripts (before layouts, whose buttons reference them)
	scriptIDs := map[string]string{}
	for _, sc := range b.Scripts {
		steps := remapSteps(sc.Steps)
		if prev := plan.scriptFor(sc); prev != nil {
			if _, err := s.UpdateScript(ctx, prev.ID, sc.Name, sc.ContextTable, sc.FolderID, sc.IsActive, steps); err != nil {
				return nil, rollback(fmt.Errorf("updating script %q: %w", sc.Name, err))
			}
			p := *prev
			undo = append(undo, func() { _, _ = s.UpdateScript(bg, p.ID, p.Name, p.ContextTable, p.FolderID, p.IsActive, p.Steps) })
			report.ScriptsUpdated++
			scriptIDs[sc.ID] = prev.ID
			continue
		}
		created, err := s.CreateScript(ctx, sc.Name, sc.ContextTable, sc.FolderID, sc.IsActive, steps)
		if err != nil {
			return nil, rollback(fmt.Errorf("creating script %q: %w", sc.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteScript(bg, created.ID) })
		report.ScriptsCreated++
		scriptIDs[sc.ID] = created.ID
	}

	// 5. Layouts. Their script triggers get ids of their own here, once, so
	// a solution imported into another that already has them — or imported
	// twice — never has two triggers by one id (#26).
	for _, l := range b.Layouts {
		renewScriptTriggerIDs(l.Definition)
	}
	layoutIDs := map[string]string{}
	layoutDefs := map[string]string{}
	for _, l := range b.Layouts {
		def, err := json.Marshal(rewriteRefs(l.Definition, map[string]map[string]string{
			"script_id":       scriptIDs,
			"relationship_id": relIDs,
		}))
		if err != nil {
			return nil, rollback(fmt.Errorf("layout %q: %w", l.Name, err))
		}
		layoutDefs[l.ID] = string(def)
		if prev := plan.layoutFor(l); prev != nil {
			if _, err := s.UpdateLayout(ctx, prev.ID, l.Name, def); err != nil {
				return nil, rollback(fmt.Errorf("updating layout %q: %w", l.Name, err))
			}
			p := *prev
			undo = append(undo, func() { _, _ = s.UpdateLayout(bg, p.ID, p.Name, p.Definition) })
			report.LayoutsUpdated++
			layoutIDs[l.ID] = prev.ID
			continue
		}
		created, err := s.CreateLayout(ctx, l.Name, occIDs[l.TableOccurrenceID], def)
		if err != nil {
			return nil, rollback(fmt.Errorf("creating layout %q: %w", l.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteLayout(bg, created.ID) })
		report.LayoutsCreated++
		layoutIDs[l.ID] = created.ID
	}
	for _, l := range b.Layouts {
		dstLayoutID := layoutIDs[l.ID]
		if dstLayoutID == "" {
			continue
		}
		def, err := json.Marshal(rewriteRefs(l.Definition, map[string]map[string]string{
			"script_id":        scriptIDs,
			"relationship_id":  relIDs,
			"layout_id":        layoutIDs,
			"target_layout_id": layoutIDs,
		}))
		if err != nil {
			return nil, rollback(fmt.Errorf("layout %q cross-layout reference: %w", l.Name, err))
		}
		if string(def) != layoutDefs[l.ID] {
			if _, err := s.UpdateLayout(ctx, dstLayoutID, l.Name, def); err != nil {
				return nil, rollback(fmt.Errorf("updating layout %q cross-layout reference: %w", l.Name, err))
			}
		}
	}

	// 5b. Value lists (#31). A list is matched by name, so importing the same
	// file twice does not make a second copy; a list taken from a field has
	// its table, fields and occurrences rewritten to the destination's ids.
	for _, vl := range b.ValueLists {
		in := ValueListInput{Name: vl.Name, Kind: vl.Kind, CustomValues: vl.CustomValues}
		if vl.Kind == ValueListFromField && vl.SourceTableID == nil && vl.SourceOccurrenceID != nil {
			// Taken from a field of another solution's table (#107).
			occ := occIDs[*vl.SourceOccurrenceID]
			in.SourceOccurrenceID, in.SourceColumnName = &occ, vl.SourceColumnName
			in.SecondColumnName, in.ShowSecondOnly, in.SortBy = vl.SecondColumnName, vl.ShowSecondOnly, vl.SortBy
			in.RelatedOccurrenceID = remappedID(occIDs, vl.RelatedOccurrenceID)
		} else if vl.Kind == ValueListFromField {
			srcTable, srcColumn := "", ""
			if vl.SourceTableID != nil {
				srcTable = tableIDs[*vl.SourceTableID]
			}
			if vl.SourceColumnID != nil {
				srcColumn = columnIDs[*vl.SourceColumnID]
			}
			if srcTable == "" || srcColumn == "" {
				return nil, rollback(fmt.Errorf("value list %q reads a field that is not in this file", vl.Name))
			}
			in.SourceTableID, in.SourceColumnID = &srcTable, &srcColumn
			in.SecondColumnID = remappedID(columnIDs, vl.SecondColumnID)
			in.ShowSecondOnly, in.SortBy = vl.ShowSecondOnly, vl.SortBy
			in.SourceOccurrenceID = remappedID(occIDs, vl.SourceOccurrenceID)
			in.RelatedOccurrenceID = remappedID(occIDs, vl.RelatedOccurrenceID)
		}

		// A list whose field cannot be indexed is refused when it is
		// defined, but one in a solution file was valid when the file was
		// saved: it is restored as it was, never failing the import, and
		// reported.
		warn := func(unindexable string) {
			if unindexable != "" {
				report.Warnings = append(report.Warnings, fmt.Sprintf(
					"value list %q was restored as saved but offers no values: %s", vl.Name, unindexable))
			}
		}
		if prev := plan.valueLists[strings.ToLower(strings.TrimSpace(vl.Name))]; prev != nil {
			_, unindexable, err := s.updateValueList(ctx, prev.ID, in, true)
			if err != nil {
				return nil, rollback(fmt.Errorf("updating value list %q: %w", vl.Name, err))
			}
			warn(unindexable)
			p := *prev
			undo = append(undo, func() {
				_, _, _ = s.updateValueList(bg, p.ID, ValueListInput{
					Name: p.Name, Kind: p.Kind, CustomValues: p.CustomValues,
					SourceTableID: p.SourceTableID, SourceColumnID: p.SourceColumnID,
					SecondColumnID: p.SecondColumnID, ShowSecondOnly: p.ShowSecondOnly, SortBy: p.SortBy,
					SourceOccurrenceID: p.SourceOccurrenceID, RelatedOccurrenceID: p.RelatedOccurrenceID,
					SourceColumnName: p.SourceColumnName, SecondColumnName: p.SecondColumnName,
				}, true)
			})
			report.ValueListsUpdated++
			continue
		}
		created, unindexable, err := s.createValueList(ctx, in, true)
		if err != nil {
			return nil, rollback(fmt.Errorf("creating value list %q: %w", vl.Name, err))
		}
		warn(unindexable)
		undo = append(undo, func() { _ = s.DeleteValueList(bg, created.ID) })
		report.ValueListsCreated++
	}

	// 6. Accounts: missing ones are created disabled, without a usable
	// password, with their role and layout/table permissions. Existing accounts
	// keep their active status and password while restoring any layout/table
	// permissions carried by the bundle.
	restoreAccountPermissions := func(userID, username string, a BundleAccount) error {
		perms := make([]UserLayoutPermission, 0, len(a.Permissions))
		for _, p := range a.Permissions {
			perms = append(perms, UserLayoutPermission{LayoutID: layoutIDs[p.LayoutID], AccessLevel: p.AccessLevel})
		}
		if len(perms) > 0 {
			if err := s.SetUserPermissions(ctx, userID, perms); err != nil {
				return fmt.Errorf("restoring permissions of %q: %w", username, err)
			}
		}
		tablePerms := make([]UserTablePermission, 0, len(a.TablePermissions))
		for _, tp := range a.TablePermissions {
			tablePerms = append(tablePerms, UserTablePermission{
				TableName:       dbal.NormalizeIdentifier(tp.TableName),
				AccessLevel:     tp.AccessLevel,
				CanCreate:       tp.CanCreate,
				CanEdit:         tp.CanEdit,
				CanDelete:       tp.CanDelete,
				CanModifySchema: tp.CanModifySchema,
			})
		}
		if len(tablePerms) > 0 {
			if err := s.SetUserTablePermissions(ctx, userID, tablePerms); err != nil {
				return fmt.Errorf("restoring table permissions of %q: %w", username, err)
			}
		}
		return nil
	}
	for _, a := range b.Users {
		if existingID := plan.userIDs[strings.ToLower(strings.TrimSpace(a.Username))]; existingID != "" {
			if err := restoreAccountPermissions(existingID, a.Username, a); err != nil {
				return nil, rollback(err)
			}
			continue
		}
		created, err := s.CreateUser(ctx, strings.TrimSpace(a.Username), randomSecret(), a.Role, false)
		if err != nil {
			return nil, rollback(fmt.Errorf("creating account %q: %w", a.Username, err))
		}
		undo = append(undo, func() { _ = s.DeleteUser(bg, created.ID) })
		if err := restoreAccountPermissions(created.ID, a.Username, a); err != nil {
			return nil, rollback(err)
		}
		report.AccountsCreated++
		report.AccountsPendingPassword = append(report.AccountsPendingPassword, created.Username)
	}
	if fo := withoutSecrets(b.FileOptions); fo != nil {
		if rewritten, ok := rewriteRefs(fo, map[string]map[string]string{
			"startup_layout_id":       layoutIDs,
			"kiosk_primary_layout_id": layoutIDs,
			"layout_id":               layoutIDs,
			"target_layout_id":        layoutIDs,
			"script_id":               scriptIDs,
		}).(map[string]interface{}); ok {
			report.FileOptions = rewritten
		} else {
			report.FileOptions = fo
		}
	}
	report.PageSetup = b.PageSetup

	// 7. Saved finds (#34). They are matched by table and name, so importing
	// the same file twice does not make a second copy, and a find whose
	// fields the destination does not have is reported rather than stored.
	for _, f := range b.SavedFinds {
		existing, err := s.ListSavedFinds(ctx, f.TableName)
		if err != nil {
			return nil, rollback(fmt.Errorf("reading the saved finds of %q: %w", f.TableName, err))
		}
		known := false
		for _, e := range existing {
			if strings.EqualFold(strings.TrimSpace(e.Name), strings.TrimSpace(f.Name)) {
				known = true
				break
			}
		}
		if known {
			continue
		}
		// An imported find has no owner account, so anyone who can search
		// the table may rename or delete it.
		created, err := s.CreateSavedFind(ctx, SavedFindInput{
			Name: f.Name, TableName: f.TableName, Requests: f.Requests,
		}, "")
		if err != nil {
			return nil, rollback(fmt.Errorf("restoring the saved find %q: %w", f.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteSavedFind(bg, created.ID) })
		report.SavedFindsCreated++
	}

	// 8. Data sources (#47), matched by name. They carry no password, so a
	// restored source needs one given at import time or in the server's
	// environment before it can be read.
	for _, d := range b.DataSources {
		existing, err := s.ListDataSources(ctx)
		if err != nil {
			return nil, rollback(fmt.Errorf("reading the data sources: %w", err))
		}
		known := false
		for _, e := range existing {
			if strings.EqualFold(strings.TrimSpace(e.Name), strings.TrimSpace(d.Name)) {
				known = true
				break
			}
		}
		if known {
			continue
		}
		created, err := s.CreateDataSource(ctx, DataSourceInput{
			Name: d.Name, Engine: d.Engine, Host: d.Host, Port: d.Port, Database: d.Database,
			Username: d.Username, Schema: d.Schema, TLS: d.TLS, PasswordEnv: d.PasswordEnv,
		})
		if err != nil {
			return nil, rollback(fmt.Errorf("restoring the data source %q: %w", d.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteDataSource(bg, created.ID) })
		report.DataSourcesCreated++
	}

	// 8b. Transfer definitions (#96), matched by name. A transfer carries no
	// credentials, so a restored transfer cannot move anything until an
	// account signs in to each of its ends; and its mapped fields are
	// resolved when it runs, against both databases, so a definition whose
	// far solution is not reachable yet restores cleanly and refuses at run
	// time rather than at restore time.
	for _, t := range b.Transfers {
		existing, err := s.ListTransfers(ctx)
		if err != nil {
			return nil, rollback(fmt.Errorf("reading the transfer definitions: %w", err))
		}
		known := false
		for _, e := range existing {
			if strings.EqualFold(strings.TrimSpace(e.Name), strings.TrimSpace(t.Name)) {
				known = true
				break
			}
		}
		if known {
			continue
		}
		created, err := s.CreateTransfer(ctx, TransferInput{
			Name: t.Name, Source: t.Source, Destination: t.Destination,
			FoundSet: t.FoundSet, Sort: t.Sort, Mappings: t.Mappings,
			MatchFields: t.MatchFields, Mode: t.Mode, OnUnmatched: t.OnUnmatched,
			OnRejected: t.OnRejected, DateOrder: t.DateOrder,
		}, "")
		if err != nil {
			return nil, rollback(fmt.Errorf("restoring the transfer %q: %w", t.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteTransfer(bg, created.ID) })
		report.TransfersCreated++
	}

	// 9. Menu sets (#52), matched by name. The script or layout a menu item
	// references is named by id, so those are rewritten to the destination's
	// ids; layouts that name a menu set by id are rewritten to match.
	menuSetIDs := map[string]string{}
	for _, m := range b.MenuSets {
		existing, err := s.ListMenuSets(ctx)
		if err != nil {
			return nil, rollback(fmt.Errorf("reading the menu sets: %w", err))
		}
		var knownID string
		for _, e := range existing {
			if strings.EqualFold(strings.TrimSpace(e.Name), strings.TrimSpace(m.Name)) {
				knownID = e.ID
				break
			}
		}
		if knownID != "" {
			if m.ID != "" {
				menuSetIDs[m.ID] = knownID
			}
			continue
		}
		menus := make([]CustomMenu, 0, len(m.Menus))
		for _, menu := range m.Menus {
			items := make([]MenuItem, 0, len(menu.Items))
			for _, item := range menu.Items {
				if len(item.Action) > 0 {
					rewritten, _ := rewriteRefs(item.Action, map[string]map[string]string{
						"script_id": scriptIDs,
						"layout_id": layoutIDs,
					}).(map[string]interface{})
					if rewritten != nil {
						item.Action = rewritten
					}
				}
				items = append(items, item)
			}
			menus = append(menus, CustomMenu{Menu: menu.Menu, Mode: menu.Mode, Items: items})
		}
		created, err := s.CreateMenuSet(ctx, MenuSetInput{
			Name: m.Name, IsDefault: m.IsDefault, Menus: menus,
		})
		if err != nil {
			return nil, rollback(fmt.Errorf("restoring the menu set %q: %w", m.Name, err))
		}
		if m.ID != "" {
			menuSetIDs[m.ID] = created.ID
		}
		undo = append(undo, func() { _ = s.DeleteMenuSet(bg, created.ID) })
		report.MenuSetsCreated++
	}
	if len(menuSetIDs) > 0 {
		for _, l := range b.Layouts {
			rawID, _ := l.Definition["menu_set_id"].(string)
			if rawID == "" || menuSetIDs[rawID] == "" {
				continue
			}
			dstLayoutID := layoutIDs[l.ID]
			if dstLayoutID == "" {
				continue
			}
			def, err := json.Marshal(rewriteRefs(l.Definition, map[string]map[string]string{
				"script_id":       scriptIDs,
				"relationship_id": relIDs,
				"menu_set_id":     menuSetIDs,
			}))
			if err != nil {
				return nil, rollback(fmt.Errorf("layout %q menu set reference: %w", l.Name, err))
			}
			if _, err := s.UpdateLayout(ctx, dstLayoutID, l.Name, def); err != nil {
				return nil, rollback(fmt.Errorf("updating layout %q menu set reference: %w", l.Name, err))
			}
		}
	}

	// 10. Role privileges (#39). They are keyed by role, so there is nothing
	// to remap; a file written before they existed leaves them untouched.
	if len(b.Privileges) > 0 {
		previous, err := s.ListRolePrivileges(ctx)
		if err != nil {
			return nil, rollback(fmt.Errorf("reading role privileges: %w", err))
		}
		if err := s.SetRolePrivileges(ctx, b.Privileges); err != nil {
			return nil, rollback(fmt.Errorf("restoring role privileges: %w", err))
		}
		undo = append(undo, func() { _ = s.SetRolePrivileges(bg, previous) })
		report.PrivilegesUpdated = len(b.Privileges)
	}

	return report, nil
}

// importPlan is the destination state an import is matched against.
type importPlan struct {
	tables        map[string]*TableMetadata // by name
	layoutsByID   map[string]*LayoutMetadata
	layoutsByName map[string]*LayoutMetadata
	scriptsByID   map[string]*ScriptMetadata
	scriptsByName map[string]*ScriptMetadata
	valueLists    map[string]*ValueList // by lower-case name
	users         map[string]bool       // lower-case usernames
	userIDs       map[string]string     // lower-case username -> user id
}

func (p *importPlan) layoutFor(l BundleLayout) *LayoutMetadata {
	if m := p.layoutsByID[l.ID]; m != nil {
		return m
	}
	return p.layoutsByName[l.Name]
}

func (p *importPlan) scriptFor(sc BundleScript) *ScriptMetadata {
	if m := p.scriptsByID[sc.ID]; m != nil {
		return m
	}
	return p.scriptsByName[sc.Name]
}

// planImport validates the whole bundle against itself and the destination
// before anything is written.
func (s *Service) planImport(ctx context.Context, b *SolutionBundle) (*importPlan, error) {
	invalid := func(format string, args ...interface{}) error {
		return fmt.Errorf("%w: %s", ErrInvalidBundle, fmt.Sprintf(format, args...))
	}

	tableByID := map[string]*BundleTable{}
	tableNames := map[string]bool{}
	columnTable := map[string]string{} // column id -> table id
	for i := range b.Tables {
		t := &b.Tables[i]
		name := dbal.NormalizeIdentifier(t.Name)
		if err := dbal.CheckUserTableName(name); err != nil {
			return nil, invalid("%v", err)
		}
		if t.ID == "" || tableByID[t.ID] != nil {
			return nil, invalid("table %s has a missing or duplicate id", name)
		}
		if tableNames[name] {
			return nil, invalid("table %s appears twice", name)
		}
		tableByID[t.ID], tableNames[name] = t, true
		colNames := map[string]bool{}
		for _, c := range t.Columns {
			cname := dbal.NormalizeIdentifier(c.Name)
			if err := dbal.CheckIdentifier("column", cname); err != nil {
				return nil, invalid("table %s: %v", name, err)
			}
			if colNames[cname] {
				return nil, invalid("table %s: field %s appears twice", name, cname)
			}
			colNames[cname] = true
			if c.ID == "" || columnTable[c.ID] != "" {
				return nil, invalid("table %s: field %s has a missing or duplicate id", name, cname)
			}
			columnTable[c.ID] = t.ID
			if err := ValidateFieldOptions(c.DefaultValue); err != nil {
				return nil, invalid("table %s: field %s: %v", name, cname, err)
			}
			if !ValidSourceFieldID(c.SourceFieldID) {
				return nil, invalid("table %s: field %s has a source field id that is not a FileMaker field id", name, cname)
			}
		}
	}

	fileReferenceNames := map[string]bool{}
	for _, f := range b.FileReferences {
		fileReferenceNames[strings.ToLower(strings.TrimSpace(f.Name))] = true
	}
	occTable := map[string]string{} // occurrence id -> table id, "" for an external one
	occExternal := map[string]bool{}
	occNames := map[string]bool{}
	for _, o := range b.TableOccurrences {
		if _, dup := occTable[o.ID]; o.ID == "" || dup || strings.TrimSpace(o.Name) == "" || occNames[o.Name] {
			return nil, invalid("table occurrence %q has a missing or duplicate id or name", o.Name)
		}
		occNames[o.Name] = true
		if o.External != nil {
			// An occurrence of another solution's table goes through one of
			// the file's references, and has no table of the file.
			if o.BaseTableID != "" {
				return nil, invalid("table occurrence %q is both of a table of the file and of another solution", o.Name)
			}
			if !fileReferenceNames[strings.ToLower(strings.TrimSpace(o.External.FileReference))] {
				return nil, invalid("table occurrence %q goes through a file reference that is not in the file", o.Name)
			}
			if _, err := normalizeExternalTable(o.External.Table); err != nil {
				return nil, invalid("table occurrence %q: %v", o.Name, err)
			}
			occTable[o.ID], occExternal[o.ID] = "", true
			continue
		}
		if tableByID[o.BaseTableID] == nil {
			return nil, invalid("table occurrence %q refers to a table that is not in the file", o.Name)
		}
		occTable[o.ID] = o.BaseTableID
	}
	for _, r := range b.Relationships {
		if len(r.Predicates) > 0 {
			first := r.Predicates[0]
			if first.LeftColumnID != r.LeftColumnID || first.RightColumnID != r.RightColumnID {
				return nil, invalid("relationship %q lists criteria whose first is not its own pair of fields", r.Name)
			}
		}
		for _, p := range r.PredicateList() {
			if _, err := NormalizeRelationshipOperator(p.Operator); err != nil {
				return nil, invalid("relationship %q: %v", r.Name, err)
			}
			for _, side := range []struct{ occ, col, name string }{
				{r.LeftOccurrenceID, p.LeftColumnID, p.LeftColumnName},
				{r.RightOccurrenceID, p.RightColumnID, p.RightColumnName},
			} {
				tbl, ok := occTable[side.occ]
				if !ok {
					return nil, invalid("relationship %q refers to a table occurrence that is not in the file", r.Name)
				}
				if occExternal[side.occ] {
					// The field of another solution's table is named.
					if _, err := normalizeExternalField(side.name); side.col != "" || err != nil {
						return nil, invalid("relationship %q must name the field of its external table occurrence", r.Name)
					}
					continue
				}
				if columnTable[side.col] != tbl {
					return nil, invalid("relationship %q refers to a field that is not in its table occurrence's table", r.Name)
				}
			}
		}
	}
	layoutIDs := map[string]bool{}
	for _, l := range b.Layouts {
		if l.ID == "" || layoutIDs[l.ID] || strings.TrimSpace(l.Name) == "" {
			return nil, invalid("layout %q has a missing or duplicate id or name", l.Name)
		}
		if _, ok := occTable[l.TableOccurrenceID]; !ok {
			return nil, invalid("layout %q refers to a table occurrence that is not in the file", l.Name)
		}
		layoutIDs[l.ID] = true
	}
	for _, sc := range b.Scripts {
		if strings.TrimSpace(sc.Name) == "" {
			return nil, invalid("a script has no name")
		}
		stepIDs := map[string]bool{}
		for _, st := range sc.Steps {
			if st.ID != "" {
				stepIDs[st.ID] = true
			}
		}
		for _, st := range sc.Steps {
			if st.ParentStepID != nil && *st.ParentStepID != "" && !stepIDs[*st.ParentStepID] {
				return nil, invalid("script %q: a step refers to a parent step that is not in the script", sc.Name)
			}
		}
	}
	accountNames := map[string]bool{}
	for _, a := range b.Users {
		uname := strings.ToLower(strings.TrimSpace(a.Username))
		if uname == "" || accountNames[uname] {
			return nil, invalid("an account has a missing or duplicate username")
		}
		accountNames[uname] = true
		if !isValidRole(a.Role) {
			return nil, invalid("account %q has unknown role %q", a.Username, a.Role)
		}
		for _, p := range a.Permissions {
			if !layoutIDs[p.LayoutID] {
				return nil, invalid("account %q has a permission for a layout that is not in the file", a.Username)
			}
			if !isValidAccessLevel(p.AccessLevel) {
				return nil, invalid("account %q has unknown access level %q", a.Username, p.AccessLevel)
			}
		}
		for _, tp := range a.TablePermissions {
			tName := dbal.NormalizeIdentifier(tp.TableName)
			if tName == "" || !tableNames[tName] {
				return nil, invalid("account %q has a permission for a table that is not in the file (%q)", a.Username, tp.TableName)
			}
			if !isValidAccessLevel(tp.AccessLevel) {
				return nil, invalid("account %q has unknown table access level %q", a.Username, tp.AccessLevel)
			}
		}
	}

	// Destination state
	plan := &importPlan{
		tables: map[string]*TableMetadata{}, layoutsByID: map[string]*LayoutMetadata{}, layoutsByName: map[string]*LayoutMetadata{},
		scriptsByID: map[string]*ScriptMetadata{}, scriptsByName: map[string]*ScriptMetadata{},
		valueLists: map[string]*ValueList{}, users: map[string]bool{}, userIDs: map[string]string{},
	}
	existingValueLists, err := s.ListValueLists(ctx)
	if err != nil {
		return nil, err
	}
	for i := range existingValueLists {
		plan.valueLists[strings.ToLower(strings.TrimSpace(existingValueLists[i].Name))] = &existingValueLists[i]
	}

	// Value lists in the file: named, unique, and a field list must read a
	// field the file carries.
	seenValueLists := map[string]bool{}
	for _, vl := range b.ValueLists {
		key := strings.ToLower(strings.TrimSpace(vl.Name))
		if key == "" {
			return nil, invalid("a value list in the file has no name")
		}
		if seenValueLists[key] {
			return nil, invalid("value list %q appears twice", vl.Name)
		}
		seenValueLists[key] = true
		if vl.Kind == ValueListFromField && vl.SourceTableID == nil && vl.SourceOccurrenceID != nil {
			// Taken from a field of another solution's table: named, through
			// one of the file's external occurrences, its second field named
			// too, and its related values starting from one of the file's
			// own occurrences.
			_, nameErr := normalizeExternalField(vl.SourceColumnName)
			if vl.SourceColumnID != nil || !occExternal[*vl.SourceOccurrenceID] || nameErr != nil {
				return nil, invalid("value list %q reads a field of another solution's table that is not named through one of the file's external occurrences", vl.Name)
			}
			if vl.SecondColumnID != nil {
				return nil, invalid("value list %q reads a field of another solution's table and names its second field by id; it is named, as the first one is", vl.Name)
			}
			if _, secondErr := normalizeExternalField(vl.SecondColumnName); vl.SecondColumnName != "" && secondErr != nil {
				return nil, invalid("value list %q reads a second field of another solution's table that is not a field name", vl.Name)
			}
			if vl.RelatedOccurrenceID != nil && occTable[*vl.RelatedOccurrenceID] == "" {
				return nil, invalid("value list %q includes only related values starting from an occurrence that is not one of the file's occurrences of its own tables", vl.Name)
			}
		} else if vl.Kind == ValueListFromField {
			if vl.SourceColumnID == nil || columnTable[*vl.SourceColumnID] == "" {
				return nil, invalid("value list %q reads a field that is not in the file", vl.Name)
			}
			if vl.SourceTableID == nil || columnTable[*vl.SourceColumnID] != *vl.SourceTableID {
				return nil, invalid("value list %q reads a field that is not in its own table", vl.Name)
			}
			if vl.SecondColumnID != nil && columnTable[*vl.SecondColumnID] != *vl.SourceTableID {
				return nil, invalid("value list %q has a second field that is not in its own table", vl.Name)
			}
			if vl.RelatedOccurrenceID != nil && occTable[*vl.RelatedOccurrenceID] == "" {
				return nil, invalid("value list %q starts its related values from a table occurrence that is not in the file", vl.Name)
			}
			if vl.SourceOccurrenceID != nil && occTable[*vl.SourceOccurrenceID] != *vl.SourceTableID {
				return nil, invalid("value list %q reads its related values through a table occurrence that is not of its table", vl.Name)
			}
		}
	}

	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	for i := range tables {
		plan.tables[tables[i].Name] = &tables[i]
	}
	// An existing field with the same name must have the same type.
	for _, t := range b.Tables {
		existing := plan.tables[dbal.NormalizeIdentifier(t.Name)]
		if existing == nil {
			continue
		}
		have := map[string]dbal.AgnosticFieldType{}
		for _, c := range existing.Columns {
			have[c.Name] = c.FieldType
		}
		for _, c := range t.Columns {
			if ft, ok := have[dbal.NormalizeIdentifier(c.Name)]; ok && ft != dbal.AgnosticFieldType(c.FieldType) {
				return nil, invalid("field %s.%s is %s in the destination but %s in the file", t.Name, c.Name, ft, c.FieldType)
			}
		}
	}
	occs, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, err
	}
	tableNameByID := map[string]string{}
	for _, t := range tables {
		tableNameByID[t.ID] = t.Name
	}
	for _, o := range b.TableOccurrences {
		for _, d := range occs {
			if d.Name == o.Name && !sameOccurrenceTarget(d, o, tableNameByID, tableByID) {
				return nil, invalid("table occurrence %q already exists for another table", o.Name)
			}
		}
	}
	layouts, err := s.ListLayouts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range layouts {
		plan.layoutsByID[layouts[i].ID] = &layouts[i]
		plan.layoutsByName[layouts[i].Name] = &layouts[i]
	}
	scripts, err := s.ListScripts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range scripts {
		plan.scriptsByID[scripts[i].ID] = &scripts[i]
		plan.scriptsByName[scripts[i].Name] = &scripts[i]
	}
	users, err := s.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		uname := strings.ToLower(u.Username)
		plan.users[uname] = true
		plan.userIDs[uname] = u.ID
	}
	return plan, nil
}

// predicateSide keys one side of a criterion: its column id, or the name of
// a field of another solution's table, which has none.
func predicateSide(id, name string) string {
	if id != "" {
		return id
	}
	return "#" + externalFieldKey(name)
}

// sameOccurrenceTarget reports whether an occurrence of the destination is
// one the file's occurrence of the same name can be merged with: of the same
// table, or both external through the same file reference.
func sameOccurrenceTarget(d TableOccurrence, o BundleOccurrence, tableNameByID map[string]string, tableByID map[string]*BundleTable) bool {
	if d.External != nil || o.External != nil {
		if d.External == nil || o.External == nil ||
			!strings.EqualFold(strings.TrimSpace(d.External.FileReference), strings.TrimSpace(o.External.FileReference)) {
			return false
		}
		// Two tables of the other solution are two targets: the occurrence
		// is not silently pointed at the other one. An unbound side takes
		// the table of the other.
		have, want := dbal.NormalizeIdentifier(d.External.Table), dbal.NormalizeIdentifier(o.External.Table)
		return have == "" || want == "" || have == want
	}
	return tableNameByID[d.BaseTableID] == dbal.NormalizeIdentifier(tableByID[o.BaseTableID].Name)
}

// remapSteps gives the steps fresh IDs (step IDs are global in the
// destination) and rewrites parent references accordingly.
func remapSteps(steps []BundleStep) []ScriptStepMetadata {
	ids := make(map[string]string, len(steps))
	for _, st := range steps {
		if st.ID != "" {
			ids[st.ID] = uuid.NewString()
		}
	}
	out := make([]ScriptStepMetadata, 0, len(steps))
	seenSequence := make(map[int]bool, len(steps))
	for i, st := range steps {
		params, _ := json.Marshal(st.Params)
		if st.Params == nil {
			params = []byte("{}")
		}
		id := ids[st.ID]
		if id == "" {
			id = uuid.NewString()
		}
		m := ScriptStepMetadata{ID: id, SequenceIdx: st.SequenceIdx, StepType: st.StepType, Params: params, IsEnabled: st.IsEnabled}
		if m.SequenceIdx <= 0 || seenSequence[m.SequenceIdx] {
			// Sequence indices are 1-based, unique and in the order the steps
			// arrived: a repeated position leaves no total order for the
			// workspace to sort by or an export to round-trip (#98).
			m.SequenceIdx = i + 1
		}
		seenSequence[m.SequenceIdx] = true
		if st.ParentStepID != nil && *st.ParentStepID != "" {
			p := ids[*st.ParentStepID]
			m.ParentStepID = &p
		}
		out = append(out, m)
	}
	return out
}

// rewriteRefs walks a layout or script definition and rewrites every id that
// names something the import has just created or matched.
//
// byKey maps a JSON key — `script_id`, `relationship_id` — to the ids that
// thing had in the file and the ids it has in this database. An id that is not
// in the map is left as it is, so a definition that names something the file
// does not carry is not silently changed.
func rewriteRefs(v interface{}, byKey map[string]map[string]string) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, x := range t {
			if ids, interesting := byKey[k]; interesting {
				if id, ok := x.(string); ok {
					if dst, ok := ids[id]; ok {
						t[k] = dst
						continue
					}
				}
			}
			t[k] = rewriteRefs(x, byKey)
		}
		return t
	case []interface{}:
		for i, x := range t {
			t[i] = rewriteRefs(x, byKey)
		}
		return t
	}
	return v
}

func randomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// ─── Legacy 1.0 server exports ───────────────────────────────────────────────

// legacyServerBundle reads solution files exported by servers before 2.0:
// nested objects used Go field names and timestamps were MessagePack
// timestamps.
type legacyServerBundle struct {
	Format             string                 `msgpack:"format"`
	Version            string                 `msgpack:"version"`
	SolutionName       string                 `msgpack:"solution_name"`
	DatabaseConnection BundleConnection       `msgpack:"database_connection"`
	Tables             []TableMetadata        `msgpack:"tables"`
	TableOccurrences   []TableOccurrence      `msgpack:"table_occurrences"`
	Relationships      []RelationshipMetadata `msgpack:"relationships"`
	Layouts            []LayoutMetadata       `msgpack:"layouts"`
}

func (l *legacyServerBundle) toCurrent() (*SolutionBundle, error) {
	b := &SolutionBundle{
		Format: l.Format, Version: l.Version, SolutionName: l.SolutionName, DatabaseConnection: l.DatabaseConnection,
	}
	b.DatabaseConnection.User = "" // was obfuscated
	for _, t := range l.Tables {
		bt := BundleTable{ID: t.ID, Name: t.Name, DisplayName: t.DisplayName, Description: t.Description}
		for _, c := range t.Columns {
			bt.Columns = append(bt.Columns, BundleColumn{
				ID: c.ID, Name: c.Name, DisplayName: c.DisplayName, FieldType: string(c.FieldType), IsNullable: c.IsNullable, IsPrimaryKey: c.IsPrimaryKey,
				DefaultValue: c.DefaultValue, CalculationFormula: c.CalculationFormula, ValidationRules: c.ValidationRules,
			})
		}
		b.Tables = append(b.Tables, bt)
	}
	for _, o := range l.TableOccurrences {
		b.TableOccurrences = append(b.TableOccurrences, BundleOccurrence{ID: o.ID, BaseTableID: o.BaseTableID, Name: o.Name, XPos: o.XPos, YPos: o.YPos})
	}
	for _, r := range l.Relationships {
		b.Relationships = append(b.Relationships, BundleRelationship{
			ID: r.ID, Name: r.Name, LeftOccurrenceID: r.LeftOccurrenceID, LeftColumnID: r.LeftColumnID,
			RightOccurrenceID: r.RightOccurrenceID, RightColumnID: r.RightColumnID, Operator: r.Operator,
			AllowCreation: r.AllowCreation, CascadeDelete: r.CascadeDelete, SortRelated: r.SortRelated,
		})
	}
	for _, ly := range l.Layouts {
		def, err := jsonObject(ly.Definition)
		if err != nil {
			return nil, fmt.Errorf("layout %q has an invalid definition: %w", ly.Name, err)
		}
		b.Layouts = append(b.Layouts, BundleLayout{ID: ly.ID, Name: ly.Name, TableOccurrenceID: ly.TableOccurrenceID, Definition: def})
	}
	return b, nil
}

// ─── Data bundles ────────────────────────────────────────────────────────────

// ExportDatabaseData exports every record of every user table. CONTAINER
// values stay binary; other values keep their database type.
func (s *Service) ExportDatabaseData(ctx context.Context, dbName string) ([]byte, error) {
	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing tables: %w", err)
	}
	db := s.driver.DB()
	dialect := s.driver.Dialect()
	tablesData := make(map[string][]map[string]interface{}, len(tables))

	for _, tbl := range tables {
		if dbal.IsReservedTableName(tbl.Name) {
			continue
		}
		types := make(map[string]dbal.AgnosticFieldType, len(tbl.Columns))
		for _, c := range tbl.Columns {
			types[c.Name] = c.FieldType
		}
		companions, err := s.companionColumns(ctx, tbl.Name)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(tbl.Columns))
		repeats := map[string]ColumnMetadata{}
		for _, c := range tbl.Columns {
			names = append(names, dialect.QuoteIdentifier(c.Name))
			// Repetitions from 2 travel as "field[n]" keys, as the data API
			// returns them, so a restore loses none of them, including those
			// a lowered maximum hides. The companion column is read under its
			// own name: "$" is in no field name, and an alias "field[]" would
			// be cut at the identifier limit for a long field name.
			if companion := repetition.Column(c.Name); companions[companion] {
				repeats[companion] = c
				names = append(names, dialect.QuoteIdentifier(companion))
			}
		}
		// Registered fields only, named explicitly (see data.selectList), in
		// the order the records were created, which a restore inserts them in
		// and so keeps: by id in a table not yet numbered in creation order,
		// as its records are read.
		order := " ORDER BY " + dialect.QuoteIdentifier("id")
		if has, err := data.HasCreationOrder(ctx, s.driver, tbl.Name); err != nil {
			return nil, err
		} else if has {
			order = " ORDER BY " + dialect.QuoteIdentifier(dbal.CreationOrderColumn)
		}
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM %s%s`, strings.Join(names, ", "), dialect.QuoteIdentifier(tbl.Name), order))
		if err != nil {
			return nil, fmt.Errorf("failed reading table %s: %w", tbl.Name, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		tableRows := make([]map[string]interface{}, 0)
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed reading a record of %s: %w", tbl.Name, err)
			}
			row := make(map[string]interface{}, len(cols))
			for i, name := range cols {
				if c, ok := repeats[name]; ok {
					if err := exportRepetitions(row, c, StorageType(c.FieldType, c.CalculationFormula), vals[i]); err != nil {
						rows.Close()
						return nil, fmt.Errorf("failed reading a record of %s: %w", tbl.Name, err)
					}
					continue
				}
				if b, ok := vals[i].([]byte); ok && types[name] == dbal.FieldTypeNumber {
					row[name] = dbal.TrimNumericText(string(b))
				} else if b, ok := vals[i].([]byte); ok && types[name] != dbal.FieldTypeContainer {
					row[name] = string(b) // text the driver returned as bytes
				} else if types[name] == dbal.FieldTypeBoolean {
					row[name] = dbal.NormalizeBool(vals[i])
				} else {
					row[name] = vals[i]
				}
			}
			tableRows = append(tableRows, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("failed reading table %s: %w", tbl.Name, err)
		}
		// Values kept with their original text travel with their records,
		// so a restore shows what the source showed (#124).
		texts, err := sourcetext.LoadTable(ctx, db, dialect, tbl.Name)
		if err != nil {
			return nil, err
		}
		sourcetext.Attach(tableRows, texts)
		cMeta, err := containermeta.LoadTable(ctx, db, dialect, tbl.Name)
		if err != nil {
			return nil, err
		}
		containermeta.Attach(tableRows, cMeta)
		tablesData[tbl.Name] = tableRows
	}

	return msgpack.Marshal(DatabaseDataBundle{
		Format: DataFormat, Version: DataVersion, DatabaseName: dbName, ExportedAt: time.Now().UTC(), TablesData: tablesData,
	})
}

// DataImportReport describes what a data import restored.
type DataImportReport struct {
	DatabaseName    string   `json:"database"`
	TablesRestored  int      `json:"tables_restored"`
	RecordsInserted int      `json:"records_inserted"`
	RecordsSkipped  int      `json:"records_skipped"`
	SkippedTables   []string `json:"skipped_tables"`
}

// ImportDatabaseData restores records in one transaction. Tables and fields
// are validated first; the first failing record rolls everything back. A
// record whose id already exists is skipped, not overwritten.
func (s *Service) ImportDatabaseData(ctx context.Context, data []byte) (*DataImportReport, error) {
	// Check declared sizes before the decoder allocates from them (#13).
	if err := msgpackguard.Check(data, msgpackguard.Limits{}); err != nil {
		return nil, fmt.Errorf("invalid MessagePack database data file: %w", err)
	}
	var bundle DatabaseDataBundle
	if err := msgpack.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("invalid MessagePack database data file: %w", err)
	}
	if bundle.Format != DataFormat {
		return nil, fmt.Errorf("%w: not a File4Base data file (format %q)", ErrInvalidBundle, bundle.Format)
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// Only catalog tables receive rows (internal tables never do, even if a
	// catalog row names one), and only their registered fields are written.
	registered, err := s.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing tables: %w", err)
	}
	userTables := make(map[string]map[string]dbal.AgnosticFieldType, len(registered))
	storedTypes := make(map[string]map[string]dbal.AgnosticFieldType, len(registered))
	// The fields of each table that may carry repetitions, by name: all but
	// the primary key and summaries, as a field whose maximum was lowered
	// keeps the repetitions past it.
	repeating := make(map[string]map[string]ColumnMetadata, len(registered))
	for _, t := range registered {
		if dbal.IsReservedTableName(t.Name) {
			continue
		}
		cols := make(map[string]dbal.AgnosticFieldType, len(t.Columns))
		stored := make(map[string]dbal.AgnosticFieldType, len(t.Columns))
		for _, c := range t.Columns {
			cols[c.Name] = c.FieldType
			// The type it is stored as, a calculation's result's.
			stored[c.Name] = StorageType(c.FieldType, c.CalculationFormula)
			if !c.IsPrimaryKey && c.FieldType != dbal.FieldTypeSummary {
				if repeating[t.Name] == nil {
					repeating[t.Name] = map[string]ColumnMetadata{}
				}
				repeating[t.Name][c.Name] = c
			}
		}
		userTables[t.Name] = cols
		storedTypes[t.Name] = stored
	}
	// Fields that do not repeat now but carry repetitions get their companion
	// column before the transaction, as a failed statement would abort it.
	hidden := map[string]map[string]bool{}
	report := &DataImportReport{DatabaseName: bundle.DatabaseName, SkippedTables: []string{}}
	tableNames := make([]string, 0, len(bundle.TablesData))
	// The original texts a record carries (#124), by table and record index.
	originals := map[string]map[int]map[string]string{}
	// Container file metadata carried with records (#125), by table and index.
	containerMetas := map[string]map[int]map[string]containermeta.Entry{}
	for tableName, rows := range bundle.TablesData {
		cols, ok := userTables[tableName]
		if !ok {
			report.SkippedTables = append(report.SkippedTables, tableName)
			continue
		}
		tableNames = append(tableNames, tableName)
		for i, row := range rows {
			texts, err := sourcetext.FromRecord(row, cols)
			if err != nil {
				return nil, fmt.Errorf("record %d of table %s: %w", i+1, tableName, err)
			}
			if len(texts) > 0 {
				if originals[tableName] == nil {
					originals[tableName] = map[int]map[string]string{}
				}
				originals[tableName][i] = texts
			}
			cm, err := containermeta.FromRecord(row, cols)
			if err != nil {
				return nil, fmt.Errorf("record %d of table %s: %w", i+1, tableName, err)
			}
			if len(cm) > 0 {
				if containerMetas[tableName] == nil {
					containerMetas[tableName] = map[int]map[string]containermeta.Entry{}
				}
				containerMetas[tableName][i] = cm
			}
			for k := range row {
				if _, exists := cols[k]; exists {
					continue
				}
				if base, n, ok := repetition.Split(k); ok && n >= 2 && n <= repetition.Max {
					if c, known := repeating[tableName][base]; known {
						if c.MaxRepetitions <= 1 {
							if hidden[tableName] == nil {
								hidden[tableName] = map[string]bool{}
							}
							hidden[tableName][base] = true
						}
						continue
					}
				}
				return nil, fmt.Errorf("record %d of table %s has field '%s', which is not a field of that table", i+1, tableName, k)
			}
		}
	}
	if len(originals) > 0 {
		// Created before the transaction: a database older than the table
		// gets it here, and a failed statement would abort the transaction.
		if err := sourcetext.Ensure(ctx, db); err != nil {
			return nil, err
		}
	}
	if len(containerMetas) > 0 {
		if err := containermeta.Ensure(ctx, db); err != nil {
			return nil, err
		}
	}
	for tableName, fields := range hidden {
		for field := range fields {
			if _, err := db.ExecContext(ctx, s.addRepetitionsColumnSQL(tableName, field)); err != nil {
				return nil, fmt.Errorf("failed adding the repetitions of %s.%s: %w", tableName, field, err)
			}
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, tableName := range tableNames {
		// The type each column is stored as: a calculation's result's.
		cols := storedTypes[tableName]
		// Validation rules whose timing is "always" apply to imports (#15)
		fields, err := validation.LoadFields(ctx, tx, dialect, tableName)
		if err != nil {
			return nil, err
		}
		checker := &validation.Checker{Dialect: dialect, DB: tx, Table: tableName, Fields: fields, Restore: true}
		for i, row := range bundle.TablesData[tableName] {
			if id, ok := row["id"]; !ok || id == nil || id == "" {
				row["id"] = uuid.NewString()
			}
			if err := checker.Check(ctx, fmt.Sprint(row["id"]), row, nil, false); err != nil {
				return nil, fmt.Errorf("record %d of table %s could not be restored, nothing was imported: %w", i+1, tableName, err)
			}
			companions, err := packBundleRepetitions(row, repeating[tableName])
			if err != nil {
				return nil, fmt.Errorf("record %d of table %s could not be restored, nothing was imported: %w", i+1, tableName, err)
			}
			colNames := make([]string, 0, len(row)+len(companions))
			placeholders := make([]string, 0, len(row))
			vals := make([]interface{}, 0, len(row))
			idx := 1
			for k, v := range row {
				colType := cols[k]
				if strVal, ok := v.(string); ok && strVal == "" {
					switch colType {
					case dbal.FieldTypeNumber, dbal.FieldTypeDate, dbal.FieldTypeTimestamp:
						v = nil
					}
				}
				// A date or timestamp as the file writes it, RFC 3339, is
				// bound as the value its engine stores.
				v = dbal.TemporalValue(dialect.Engine(), colType, v)
				colNames = append(colNames, dialect.QuoteIdentifier(k))
				placeholders = append(placeholders, dialect.Placeholder(idx))
				vals = append(vals, v)
				idx++
			}
			for field, v := range companions {
				colNames = append(colNames, dialect.QuoteIdentifier(repetition.Column(field)))
				placeholders = append(placeholders, dialect.Placeholder(idx))
				vals = append(vals, v)
				idx++
			}
			insert := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
				dialect.QuoteIdentifier(tableName), strings.Join(colNames, ", "), strings.Join(placeholders, ", "))
			if dialect.Engine() == dbal.EnginePostgres {
				// A record whose id is already there is skipped, not replaced.
				insert += ` ON CONFLICT (id) DO NOTHING`
			}
			res, err := tx.ExecContext(ctx, insert, vals...)
			if err != nil {
				// MariaDB has no ON CONFLICT: an existing id comes back as a
				// duplicate key, which is this record being skipped. Any other
				// error still fails the whole import.
				if dialect.Engine() != dbal.EnginePostgres && dbal.IsDuplicateKey(err) {
					report.RecordsSkipped++
					continue
				}
				if f, ok := validation.IsUniqueIndexViolation(err, tableName, fields); ok {
					err = validation.UniqueViolation(tableName, f)
				}
				return nil, fmt.Errorf("record %d of table %s could not be restored, nothing was imported: %w", i+1, tableName, err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				report.RecordsInserted++
				if texts := originals[tableName][i]; len(texts) > 0 {
					if err := sourcetext.Put(ctx, tx, dialect, tableName, fmt.Sprint(row["id"]), texts); err != nil {
						return nil, fmt.Errorf("record %d of table %s could not be restored, nothing was imported: %w", i+1, tableName, err)
					}
				}
				if cm := containerMetas[tableName][i]; len(cm) > 0 {
					if err := containermeta.Put(ctx, tx, dialect, tableName, fmt.Sprint(row["id"]), cm); err != nil {
						return nil, fmt.Errorf("record %d of table %s could not be restored, nothing was imported: %w", i+1, tableName, err)
					}
				}
			} else {
				report.RecordsSkipped++
			}
		}
		report.TablesRestored++
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed committing the restored records: %w", err)
	}
	return report, nil
}

// relationshipOptionsOf is the per-side options of r, as an update.
func relationshipOptionsOf(r RelationshipMetadata) UpdateRelationshipInput {
	return UpdateRelationshipInput{
		AllowCreation: &r.AllowCreation, CascadeDelete: &r.CascadeDelete, SortRelated: &r.SortRelated,
		LeftAllowCreation: &r.LeftAllowCreation, LeftCascadeDelete: &r.LeftCascadeDelete, LeftSortRelated: &r.LeftSortRelated,
	}
}

// importRelationshipOptions gives the existing relationship cur the options
// each side has in a solution file, and reports whether any changed.
func (s *Service) importRelationshipOptions(ctx context.Context, cur RelationshipMetadata, in CreateRelationshipInput) (bool, error) {
	want := RelationshipMetadata{
		AllowCreation: in.AllowCreation, CascadeDelete: in.CascadeDelete,
		LeftAllowCreation: in.LeftAllowCreation, LeftCascadeDelete: in.LeftCascadeDelete,
	}
	if in.SortRelated != nil {
		want.SortRelated = *in.SortRelated
	}
	if in.LeftSortRelated != nil {
		want.LeftSortRelated = *in.LeftSortRelated
	}
	if want.AllowCreation == cur.AllowCreation && want.CascadeDelete == cur.CascadeDelete &&
		want.SortRelated == cur.SortRelated && want.LeftAllowCreation == cur.LeftAllowCreation &&
		want.LeftCascadeDelete == cur.LeftCascadeDelete && want.LeftSortRelated == cur.LeftSortRelated {
		return false, nil
	}
	if _, err := s.UpdateRelationship(ctx, cur.ID, relationshipOptionsOf(want)); err != nil {
		return false, err
	}
	return true, nil
}

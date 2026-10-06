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

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/msgpackguard"
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
}

type BundleTable struct {
	ID          string         `msgpack:"id"`
	Name        string         `msgpack:"name"`
	DisplayName string         `msgpack:"display_name"`
	Description string         `msgpack:"description"`
	Columns     []BundleColumn `msgpack:"columns"`
}

type BundleOccurrence struct {
	ID          string  `msgpack:"id"`
	BaseTableID string  `msgpack:"base_table_id"`
	Name        string  `msgpack:"name"`
	XPos        float64 `msgpack:"x_pos"`
	YPos        float64 `msgpack:"y_pos"`
}

type BundleRelationship struct {
	ID                string `msgpack:"id"`
	Name              string `msgpack:"name"`
	LeftOccurrenceID  string `msgpack:"left_occurrence_id"`
	LeftColumnID      string `msgpack:"left_column_id"`
	RightOccurrenceID string `msgpack:"right_occurrence_id"`
	RightColumnID     string `msgpack:"right_column_id"`
	Operator          string `msgpack:"operator"`
	AllowCreation     bool   `msgpack:"allow_creation"`
	CascadeDelete     bool   `msgpack:"cascade_delete"`
	SortRelated       string `msgpack:"sort_related"`
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

type BundleAccount struct {
	ID          string             `msgpack:"id"`
	Username    string             `msgpack:"username"`
	Role        string             `msgpack:"role"`
	IsActive    bool               `msgpack:"is_active"`
	Permissions []BundlePermission `msgpack:"permissions"`
}

// SolutionBundle is the content of a .f4p solution file.
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
	Users              []BundleAccount        `msgpack:"users"`
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
	users, err := s.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing accounts: %w", err)
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
		Users:              make([]BundleAccount, 0, len(users)),
		FileOptions:        withoutSecrets(opts.FileOptions),
		PageSetup:          opts.PageSetup,
	}
	for _, t := range tables {
		bt := BundleTable{ID: t.ID, Name: t.Name, DisplayName: t.DisplayName, Description: t.Description}
		for _, c := range t.Columns {
			bt.Columns = append(bt.Columns, BundleColumn{
				ID: c.ID, Name: c.Name, DisplayName: c.DisplayName, FieldType: string(c.FieldType),
				IsNullable: c.IsNullable, IsPrimaryKey: c.IsPrimaryKey,
				DefaultValue: c.DefaultValue, CalculationFormula: c.CalculationFormula, ValidationRules: c.ValidationRules,
			})
		}
		b.Tables = append(b.Tables, bt)
	}
	for _, o := range occurrences {
		b.TableOccurrences = append(b.TableOccurrences, BundleOccurrence{ID: o.ID, BaseTableID: o.BaseTableID, Name: o.Name, XPos: o.XPos, YPos: o.YPos})
	}
	for _, r := range relationships {
		b.Relationships = append(b.Relationships, BundleRelationship{
			ID: r.ID, Name: r.Name,
			LeftOccurrenceID: r.LeftOccurrenceID, LeftColumnID: r.LeftColumnID,
			RightOccurrenceID: r.RightOccurrenceID, RightColumnID: r.RightColumnID,
			Operator: r.Operator, AllowCreation: r.AllowCreation, CascadeDelete: r.CascadeDelete, SortRelated: r.SortRelated,
		})
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
	for _, u := range users {
		perms, err := s.GetUserPermissions(ctx, u.ID)
		if err != nil {
			return nil, fmt.Errorf("failed reading permissions of %s: %w", u.Username, err)
		}
		acc := BundleAccount{ID: u.ID, Username: u.Username, Role: u.Role, IsActive: u.IsActive, Permissions: make([]BundlePermission, 0, len(perms))}
		for _, p := range perms {
			acc.Permissions = append(acc.Permissions, BundlePermission{LayoutID: p.LayoutID, AccessLevel: p.AccessLevel})
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
	SolutionName            string   `json:"solution_name"`
	TablesCreated           int      `json:"tables_created"`
	ColumnsCreated          int      `json:"columns_created"`
	OccurrencesCreated      int      `json:"occurrences_created"`
	RelationshipsCreated    int      `json:"relationships_created"`
	LayoutsCreated          int      `json:"layouts_created"`
	LayoutsUpdated          int      `json:"layouts_updated"`
	ScriptsCreated          int      `json:"scripts_created"`
	ScriptsUpdated          int      `json:"scripts_updated"`
	AccountsCreated         int      `json:"accounts_created"`
	AccountsPendingPassword []string `json:"accounts_pending_password"`
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

	// 1. Tables and columns
	tableIDs := map[string]string{}  // bundle table id -> destination id
	columnIDs := map[string]string{} // bundle column id -> destination id
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
		for _, c := range t.Columns {
			cname := dbal.NormalizeIdentifier(c.Name)
			if id, ok := existingCols[cname]; ok {
				columnIDs[c.ID] = id
				continue
			}
			if c.IsPrimaryKey {
				return nil, rollback(fmt.Errorf("table %s: primary key %s does not exist in the destination", name, cname))
			}
			col, err := s.AddColumn(ctx, dstID, ColumnMetadata{
				Name: cname, DisplayName: c.DisplayName, FieldType: dbal.AgnosticFieldType(c.FieldType), IsNullable: c.IsNullable,
				DefaultValue: c.DefaultValue, CalculationFormula: c.CalculationFormula, ValidationRules: c.ValidationRules,
			})
			if err != nil {
				return nil, rollback(fmt.Errorf("adding field %s to table %s: %w", cname, name, err))
			}
			if existing != nil {
				tblID, colID := dstID, col.ID
				undo = append(undo, func() { _ = s.DeleteColumn(bg, tblID, colID) })
			}
			report.ColumnsCreated++
			columnIDs[c.ID] = col.ID
			existingCols[cname] = col.ID
		}
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
			if existing.BaseTableID != dstTable {
				return nil, rollback(fmt.Errorf("table occurrence %q already exists for another table", o.Name))
			}
			occIDs[o.ID] = existing.ID
			continue
		}
		created, err := s.CreateTableOccurrence(ctx, CreateOccurrenceInput{BaseTableID: dstTable, Name: o.Name, XPos: o.XPos, YPos: o.YPos})
		if err != nil {
			return nil, rollback(fmt.Errorf("creating table occurrence %q: %w", o.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteTableOccurrence(bg, created.ID) })
		report.OccurrencesCreated++
		occIDs[o.ID] = created.ID
	}

	// 3. Relationships (an identical one is not duplicated)
	currentRels, err := s.ListRelationships(ctx)
	if err != nil {
		return nil, rollback(err)
	}
	relKey := func(lo, lc, ro, rc, op string) string { return strings.Join([]string{lo, lc, ro, rc, op}, "|") }
	haveRel := map[string]bool{}
	for _, r := range currentRels {
		haveRel[relKey(r.LeftOccurrenceID, r.LeftColumnID, r.RightOccurrenceID, r.RightColumnID, r.Operator)] = true
	}
	for _, r := range b.Relationships {
		in := CreateRelationshipInput{
			Name:             r.Name,
			LeftOccurrenceID: occIDs[r.LeftOccurrenceID], LeftColumnID: columnIDs[r.LeftColumnID],
			RightOccurrenceID: occIDs[r.RightOccurrenceID], RightColumnID: columnIDs[r.RightColumnID],
			Operator: r.Operator, AllowCreation: r.AllowCreation, CascadeDelete: r.CascadeDelete,
		}
		if r.SortRelated != "" {
			sort := r.SortRelated
			in.SortRelated = &sort
		}
		key := relKey(in.LeftOccurrenceID, in.LeftColumnID, in.RightOccurrenceID, in.RightColumnID, in.Operator)
		if haveRel[key] {
			continue
		}
		created, err := s.CreateRelationship(ctx, in)
		if err != nil {
			return nil, rollback(fmt.Errorf("creating relationship %q: %w", r.Name, err))
		}
		undo = append(undo, func() { _ = s.DeleteRelationship(bg, created.ID) })
		haveRel[key] = true
		report.RelationshipsCreated++
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

	// 5. Layouts
	layoutIDs := map[string]string{}
	for _, l := range b.Layouts {
		def, err := json.Marshal(rewriteScriptIDs(l.Definition, scriptIDs))
		if err != nil {
			return nil, rollback(fmt.Errorf("layout %q: %w", l.Name, err))
		}
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

	// 6. Accounts: missing ones are created disabled, without a usable
	// password, with their role and layout permissions. Existing accounts are
	// never changed.
	for _, a := range b.Users {
		if plan.users[strings.ToLower(strings.TrimSpace(a.Username))] {
			continue
		}
		created, err := s.CreateUser(ctx, strings.TrimSpace(a.Username), randomSecret(), a.Role, false)
		if err != nil {
			return nil, rollback(fmt.Errorf("creating account %q: %w", a.Username, err))
		}
		undo = append(undo, func() { _ = s.DeleteUser(bg, created.ID) })
		perms := make([]UserLayoutPermission, 0, len(a.Permissions))
		for _, p := range a.Permissions {
			perms = append(perms, UserLayoutPermission{LayoutID: layoutIDs[p.LayoutID], AccessLevel: p.AccessLevel})
		}
		if len(perms) > 0 {
			if err := s.SetUserPermissions(ctx, created.ID, perms); err != nil {
				return nil, rollback(fmt.Errorf("restoring permissions of %q: %w", a.Username, err))
			}
		}
		report.AccountsCreated++
		report.AccountsPendingPassword = append(report.AccountsPendingPassword, created.Username)
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
	users         map[string]bool // lower-case usernames
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
		}
	}

	occTable := map[string]string{} // occurrence id -> table id
	occNames := map[string]bool{}
	for _, o := range b.TableOccurrences {
		if o.ID == "" || occTable[o.ID] != "" || strings.TrimSpace(o.Name) == "" || occNames[o.Name] {
			return nil, invalid("table occurrence %q has a missing or duplicate id or name", o.Name)
		}
		if tableByID[o.BaseTableID] == nil {
			return nil, invalid("table occurrence %q refers to a table that is not in the file", o.Name)
		}
		occTable[o.ID], occNames[o.Name] = o.BaseTableID, true
	}
	for _, r := range b.Relationships {
		for _, side := range []struct{ occ, col string }{{r.LeftOccurrenceID, r.LeftColumnID}, {r.RightOccurrenceID, r.RightColumnID}} {
			tbl, ok := occTable[side.occ]
			if !ok {
				return nil, invalid("relationship %q refers to a table occurrence that is not in the file", r.Name)
			}
			if columnTable[side.col] != tbl {
				return nil, invalid("relationship %q refers to a field that is not in its table occurrence's table", r.Name)
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
	}

	// Destination state
	plan := &importPlan{
		tables: map[string]*TableMetadata{}, layoutsByID: map[string]*LayoutMetadata{}, layoutsByName: map[string]*LayoutMetadata{},
		scriptsByID: map[string]*ScriptMetadata{}, scriptsByName: map[string]*ScriptMetadata{}, users: map[string]bool{},
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
			if d.Name == o.Name && tableNameByID[d.BaseTableID] != dbal.NormalizeIdentifier(tableByID[o.BaseTableID].Name) {
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
		plan.users[strings.ToLower(u.Username)] = true
	}
	return plan, nil
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
		if m.SequenceIdx <= 0 {
			m.SequenceIdx = i + 1
		}
		if st.ParentStepID != nil && *st.ParentStepID != "" {
			p := ids[*st.ParentStepID]
			m.ParentStepID = &p
		}
		out = append(out, m)
	}
	return out
}

// rewriteScriptIDs replaces the script_id of button actions and script
// triggers with the destination's script IDs.
func rewriteScriptIDs(v interface{}, ids map[string]string) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, x := range t {
			if k == "script_id" {
				if id, ok := x.(string); ok {
					if dst, ok := ids[id]; ok {
						t[k] = dst
						continue
					}
				}
			}
			t[k] = rewriteScriptIDs(x, ids)
		}
		return t
	case []interface{}:
		for i, x := range t {
			t[i] = rewriteScriptIDs(x, ids)
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
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM %s`, dialect.QuoteIdentifier(tbl.Name)))
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
				if b, ok := vals[i].([]byte); ok && types[name] != dbal.FieldTypeContainer {
					row[name] = string(b) // text the driver returned as bytes
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
	userTables := make(map[string]map[string]bool, len(registered))
	for _, t := range registered {
		if dbal.IsReservedTableName(t.Name) {
			continue
		}
		cols := make(map[string]bool, len(t.Columns))
		for _, c := range t.Columns {
			cols[c.Name] = true
		}
		userTables[t.Name] = cols
	}
	report := &DataImportReport{DatabaseName: bundle.DatabaseName, SkippedTables: []string{}}
	tableNames := make([]string, 0, len(bundle.TablesData))
	for tableName, rows := range bundle.TablesData {
		cols, ok := userTables[tableName]
		if !ok {
			report.SkippedTables = append(report.SkippedTables, tableName)
			continue
		}
		tableNames = append(tableNames, tableName)
		for i, row := range rows {
			for k := range row {
				if !cols[k] {
					return nil, fmt.Errorf("record %d of table %s has field '%s', which is not a field of that table", i+1, tableName, k)
				}
			}
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, tableName := range tableNames {
		for i, row := range bundle.TablesData[tableName] {
			if id, ok := row["id"]; !ok || id == nil || id == "" {
				row["id"] = uuid.NewString()
			}
			colNames := make([]string, 0, len(row))
			placeholders := make([]string, 0, len(row))
			vals := make([]interface{}, 0, len(row))
			idx := 1
			for k, v := range row {
				colNames = append(colNames, dialect.QuoteIdentifier(k))
				placeholders = append(placeholders, dialect.Placeholder(idx))
				vals = append(vals, v)
				idx++
			}
			insert := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
				dialect.QuoteIdentifier(tableName), strings.Join(colNames, ", "), strings.Join(placeholders, ", "))
			if dialect.Engine() == dbal.EnginePostgres {
				insert += ` ON CONFLICT (id) DO NOTHING`
			} else {
				insert += fmt.Sprintf(` ON DUPLICATE KEY UPDATE %s = %s`, dialect.QuoteIdentifier("id"), dialect.QuoteIdentifier("id"))
			}
			res, err := tx.ExecContext(ctx, insert, vals...)
			if err != nil {
				return nil, fmt.Errorf("record %d of table %s could not be restored, nothing was imported: %w", i+1, tableName, err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				report.RecordsInserted++
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

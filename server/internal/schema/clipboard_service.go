package schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

const (
	ClipboardFormat  = "file4base_clipboard"
	ClipboardVersion = "1.0"

	ClipboardKindTable   = "table"
	ClipboardKindColumns = "columns"
	ClipboardKindLayout  = "layout"
	ClipboardKindScript  = "script"

	CollisionRename  = "rename"
	CollisionReplace = "replace"
)

// ErrInvalidClipboard is returned when a clipboard envelope cannot be parsed or
// is missing required fields (#54).
var ErrInvalidClipboard = errors.New("invalid structure clipboard envelope")

// ClipboardColumn represents a field copied to the structure clipboard (#54).
type ClipboardColumn struct {
	Name               string  `json:"name"`
	DisplayName        string  `json:"display_name"`
	FieldType          string  `json:"field_type"`
	IsNullable         bool    `json:"is_nullable"`
	IsPrimaryKey       bool    `json:"is_primary_key"`
	DefaultValue       *string `json:"default_value,omitempty"`
	CalculationFormula *string `json:"calculation_formula,omitempty"`
	ValidationRules    *string `json:"validation_rules,omitempty"`
}

// ClipboardTable represents a table and its fields on the clipboard (#54).
type ClipboardTable struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Description string            `json:"description,omitempty"`
	Columns     []ClipboardColumn `json:"columns"`
}

// ClipboardLayout represents a layout on the structure clipboard (#54).
type ClipboardLayout struct {
	Name                string                 `json:"name"`
	TableOccurrenceName string                 `json:"table_occurrence_name"`
	BaseTableName       string                 `json:"base_table_name,omitempty"`
	Definition          map[string]interface{} `json:"definition"`
}

// ClipboardStep represents a script step on the structure clipboard (#54).
type ClipboardStep struct {
	SequenceIdx int                    `json:"sequence_idx"`
	StepType    string                 `json:"step_type"`
	Params      map[string]interface{} `json:"params"`
	IsEnabled   bool                   `json:"is_enabled"`
}

// ClipboardScript represents a script on the structure clipboard (#54).
type ClipboardScript struct {
	Name         string          `json:"name"`
	ContextTable string          `json:"context_table,omitempty"`
	IsActive     bool            `json:"is_active"`
	Steps        []ClipboardStep `json:"steps"`
}

// ClipboardEnvelope is the reviewable JSON text placed on the clipboard when
// copying a table, fields, layout, or script between solutions (#54).
type ClipboardEnvelope struct {
	Format              string            `json:"format"`
	Version             string            `json:"version"`
	Kind                string            `json:"kind"`
	SourceTable         string            `json:"source_table,omitempty"`
	Table               *ClipboardTable   `json:"table,omitempty"`
	Columns             []ClipboardColumn `json:"columns,omitempty"`
	Layout              *ClipboardLayout  `json:"layout,omitempty"`
	Script              *ClipboardScript  `json:"script,omitempty"`
	ReferencedScripts   map[string]string `json:"referenced_scripts,omitempty"`
	ReferencedValueList map[string]string `json:"referenced_value_lists,omitempty"`
}

// CopyStructureInput names the design object to copy to the clipboard (#54).
type CopyStructureInput struct {
	Kind      string   `json:"kind"`
	TableID   string   `json:"table_id,omitempty"`
	ColumnIDs []string `json:"column_ids,omitempty"`
	LayoutID  string   `json:"layout_id,omitempty"`
	ScriptID  string   `json:"script_id,omitempty"`
}

// ClipboardDependency describes a missing reference in the destination
// solution discovered before or during a paste (#54).
type ClipboardDependency struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// ClipboardPreview reports collisions and missing dependencies in the
// destination solution before pasting (#54).
type ClipboardPreview struct {
	Kind                string                `json:"kind"`
	ObjectName          string                `json:"object_name"`
	HasCollision        bool                  `json:"has_collision"`
	Collisions          []string              `json:"collisions,omitempty"`
	MissingDependencies []ClipboardDependency `json:"missing_dependencies,omitempty"`
}

// PreviewClipboardInput is the request for PreviewClipboard (#54).
type PreviewClipboardInput struct {
	Envelope      ClipboardEnvelope `json:"envelope"`
	TargetTableID string            `json:"target_table_id,omitempty"`
}

// PasteClipboardInput is the request for PasteClipboard (#54).
type PasteClipboardInput struct {
	Envelope      ClipboardEnvelope `json:"envelope"`
	TargetTableID string            `json:"target_table_id,omitempty"`
	OnCollision   string            `json:"on_collision,omitempty"`
}

// ClipboardPasteReport describes the result of pasting a design object (#54).
type ClipboardPasteReport struct {
	Kind                string                `json:"kind"`
	CreatedID           string                `json:"created_id,omitempty"`
	CreatedName         string                `json:"created_name"`
	CreatedCount        int                   `json:"created_count"`
	CollisionResolved   string                `json:"collision_resolved,omitempty"`
	MissingDependencies []ClipboardDependency `json:"missing_dependencies,omitempty"`
}

// CopyStructure serializes a table, a set of fields, a layout, or a script into
// a reviewable JSON ClipboardEnvelope (#54).
func (s *Service) CopyStructure(ctx context.Context, in CopyStructureInput) (*ClipboardEnvelope, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	env := &ClipboardEnvelope{
		Format:  ClipboardFormat,
		Version: ClipboardVersion,
		Kind:    kind,
	}

	switch kind {
	case ClipboardKindTable:
		t, err := s.findTableByIDOrName(ctx, in.TableID)
		if err != nil {
			return nil, err
		}
		cols := make([]ClipboardColumn, 0, len(t.Columns))
		for _, c := range t.Columns {
			cols = append(cols, ClipboardColumn{
				Name:               c.Name,
				DisplayName:        c.DisplayName,
				FieldType:          string(c.FieldType),
				IsNullable:         c.IsNullable,
				IsPrimaryKey:       c.IsPrimaryKey,
				DefaultValue:       c.DefaultValue,
				CalculationFormula: c.CalculationFormula,
				ValidationRules:    c.ValidationRules,
			})
		}
		env.Table = &ClipboardTable{
			Name:        t.Name,
			DisplayName: t.DisplayName,
			Description: t.Description,
			Columns:     cols,
		}
		return env, nil

	case ClipboardKindColumns:
		t, err := s.findTableByIDOrName(ctx, in.TableID)
		if err != nil {
			return nil, err
		}
		wanted := map[string]bool{}
		for _, id := range in.ColumnIDs {
			wanted[strings.ToLower(strings.TrimSpace(id))] = true
		}
		var cols []ClipboardColumn
		for _, c := range t.Columns {
			if c.IsPrimaryKey {
				continue
			}
			if len(wanted) > 0 && !wanted[strings.ToLower(c.ID)] && !wanted[strings.ToLower(c.Name)] {
				continue
			}
			cols = append(cols, ClipboardColumn{
				Name:               c.Name,
				DisplayName:        c.DisplayName,
				FieldType:          string(c.FieldType),
				IsNullable:         c.IsNullable,
				IsPrimaryKey:       false,
				DefaultValue:       c.DefaultValue,
				CalculationFormula: c.CalculationFormula,
				ValidationRules:    c.ValidationRules,
			})
		}
		if len(cols) == 0 {
			return nil, fmt.Errorf("%w: no non-primary-key fields matched column_ids", ErrInvalidClipboard)
		}
		env.SourceTable = t.DisplayName
		env.Columns = cols
		return env, nil

	case ClipboardKindLayout:
		l, err := s.GetLayout(ctx, strings.TrimSpace(in.LayoutID))
		if err != nil {
			return nil, err
		}
		def, err := jsonObject(l.Definition)
		if err != nil {
			return nil, fmt.Errorf("invalid layout definition: %w", err)
		}
		toName, baseTable := s.resolveOccurrenceNames(ctx, l.TableOccurrenceID, def)
		scripts, _ := s.ListScripts(ctx)
		refScripts := map[string]string{}
		for _, sc := range scripts {
			refScripts[sc.ID] = sc.Name
		}
		vls, _ := s.ListValueLists(ctx)
		refVLs := map[string]string{}
		for _, vl := range vls {
			refVLs[vl.ID] = vl.Name
		}
		env.Layout = &ClipboardLayout{
			Name:                l.Name,
			TableOccurrenceName: toName,
			BaseTableName:       baseTable,
			Definition:          def,
		}
		if len(refScripts) > 0 {
			env.ReferencedScripts = refScripts
		}
		if len(refVLs) > 0 {
			env.ReferencedValueList = refVLs
		}
		return env, nil

	case ClipboardKindScript:
		sc, err := s.GetScript(ctx, strings.TrimSpace(in.ScriptID))
		if err != nil {
			return nil, err
		}
		steps := make([]ClipboardStep, 0, len(sc.Steps))
		for _, st := range sc.Steps {
			params, _ := jsonObject(st.Params)
			steps = append(steps, ClipboardStep{
				SequenceIdx: st.SequenceIdx,
				StepType:    st.StepType,
				Params:      params,
				IsEnabled:   st.IsEnabled,
			})
		}
		env.Script = &ClipboardScript{
			Name:         sc.Name,
			ContextTable: sc.ContextTable,
			IsActive:     sc.IsActive,
			Steps:        steps,
		}
		return env, nil

	default:
		return nil, fmt.Errorf("%w: kind must be table, columns, layout, or script", ErrInvalidClipboard)
	}
}

// PreviewClipboard checks a ClipboardEnvelope against the destination database
// and reports any name collisions and missing dependencies (#54).
func (s *Service) PreviewClipboard(ctx context.Context, in PreviewClipboardInput) (*ClipboardPreview, error) {
	env := in.Envelope
	if env.Format != "" && env.Format != ClipboardFormat {
		return nil, fmt.Errorf("%w: unsupported format %q", ErrInvalidClipboard, env.Format)
	}
	kind := strings.ToLower(strings.TrimSpace(env.Kind))
	preview := &ClipboardPreview{Kind: kind}

	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	tableByName := map[string]TableMetadata{}
	for _, t := range tables {
		tableByName[strings.ToLower(t.Name)] = t
		tableByName[strings.ToLower(t.DisplayName)] = t
	}

	switch kind {
	case ClipboardKindTable:
		if env.Table == nil || strings.TrimSpace(env.Table.DisplayName) == "" && strings.TrimSpace(env.Table.Name) == "" {
			return nil, fmt.Errorf("%w: table payload is required", ErrInvalidClipboard)
		}
		displayName := strings.TrimSpace(env.Table.DisplayName)
		if displayName == "" {
			displayName = strings.TrimSpace(env.Table.Name)
		}
		preview.ObjectName = displayName
		normName := dbal.NormalizeIdentifier(env.Table.Name)
		if normName == "" {
			normName = dbal.NormalizeIdentifier(displayName)
		}
		if _, exists := tableByName[strings.ToLower(normName)]; exists {
			preview.HasCollision = true
			preview.Collisions = []string{displayName}
		} else if _, exists := tableByName[strings.ToLower(displayName)]; exists {
			preview.HasCollision = true
			preview.Collisions = []string{displayName}
		}
		return preview, nil

	case ClipboardKindColumns:
		if len(env.Columns) == 0 {
			return nil, fmt.Errorf("%w: columns payload is required", ErrInvalidClipboard)
		}
		t, err := s.findTableByIDOrName(ctx, in.TargetTableID)
		if err != nil {
			return nil, err
		}
		preview.ObjectName = fmt.Sprintf("%d field(s) -> %s", len(env.Columns), t.DisplayName)
		existingCols := map[string]bool{}
		for _, c := range t.Columns {
			existingCols[strings.ToLower(c.Name)] = true
			existingCols[strings.ToLower(c.DisplayName)] = true
		}
		for _, c := range env.Columns {
			cname := dbal.NormalizeIdentifier(c.Name)
			if cname == "" {
				cname = dbal.NormalizeIdentifier(c.DisplayName)
			}
			if existingCols[strings.ToLower(cname)] || existingCols[strings.ToLower(strings.TrimSpace(c.DisplayName))] {
				preview.HasCollision = true
				preview.Collisions = append(preview.Collisions, c.DisplayName)
			}
		}
		return preview, nil

	case ClipboardKindLayout:
		if env.Layout == nil || strings.TrimSpace(env.Layout.Name) == "" {
			return nil, fmt.Errorf("%w: layout payload is required", ErrInvalidClipboard)
		}
		preview.ObjectName = strings.TrimSpace(env.Layout.Name)
		layouts, err := s.ListLayouts(ctx)
		if err != nil {
			return nil, err
		}
		for _, l := range layouts {
			if strings.EqualFold(strings.TrimSpace(l.Name), preview.ObjectName) {
				preview.HasCollision = true
				preview.Collisions = []string{preview.ObjectName}
				break
			}
		}
		preview.MissingDependencies = s.inspectLayoutDependencies(ctx, env, tableByName)
		return preview, nil

	case ClipboardKindScript:
		if env.Script == nil || strings.TrimSpace(env.Script.Name) == "" {
			return nil, fmt.Errorf("%w: script payload is required", ErrInvalidClipboard)
		}
		preview.ObjectName = strings.TrimSpace(env.Script.Name)
		scripts, err := s.ListScripts(ctx)
		if err != nil {
			return nil, err
		}
		for _, sc := range scripts {
			if strings.EqualFold(strings.TrimSpace(sc.Name), preview.ObjectName) {
				preview.HasCollision = true
				preview.Collisions = []string{preview.ObjectName}
				break
			}
		}
		preview.MissingDependencies = s.inspectScriptDependencies(ctx, env.Script, tableByName, scripts)
		return preview, nil

	default:
		return nil, fmt.Errorf("%w: kind must be table, columns, layout, or script", ErrInvalidClipboard)
	}
}

// PasteClipboard creates or replaces the copied table, columns, layout, or
// script in the destination solution (#54).
func (s *Service) PasteClipboard(ctx context.Context, in PasteClipboardInput) (*ClipboardPasteReport, error) {
	preview, err := s.PreviewClipboard(ctx, PreviewClipboardInput{
		Envelope:      in.Envelope,
		TargetTableID: in.TargetTableID,
	})
	if err != nil {
		return nil, err
	}

	policy := strings.ToLower(strings.TrimSpace(in.OnCollision))
	if policy == "" {
		policy = CollisionRename
	}
	if policy != CollisionRename && policy != CollisionReplace {
		return nil, fmt.Errorf("%w: on_collision must be rename or replace", ErrInvalidClipboard)
	}

	env := in.Envelope
	kind := preview.Kind
	report := &ClipboardPasteReport{
		Kind:                kind,
		MissingDependencies: preview.MissingDependencies,
	}
	if preview.HasCollision {
		report.CollisionResolved = policy
	}

	switch kind {
	case ClipboardKindTable:
		return s.pasteTable(ctx, env.Table, preview.HasCollision, policy, report)
	case ClipboardKindColumns:
		return s.pasteColumns(ctx, in.TargetTableID, env.Columns, policy, report)
	case ClipboardKindLayout:
		return s.pasteLayout(ctx, env, preview.HasCollision, policy, report)
	case ClipboardKindScript:
		return s.pasteScript(ctx, env.Script, preview.HasCollision, policy, report)
	default:
		return nil, fmt.Errorf("%w: unsupported kind %q", ErrInvalidClipboard, kind)
	}
}

func (s *Service) pasteTable(
	ctx context.Context,
	tbl *ClipboardTable,
	hasCollision bool,
	policy string,
	report *ClipboardPasteReport,
) (*ClipboardPasteReport, error) {
	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	displayName := strings.TrimSpace(tbl.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(tbl.Name)
	}
	physName := dbal.NormalizeIdentifier(tbl.Name)
	if physName == "" {
		physName = dbal.NormalizeIdentifier(displayName)
	}

	var existing *TableMetadata
	for i := range tables {
		if strings.EqualFold(tables[i].Name, physName) || strings.EqualFold(tables[i].DisplayName, displayName) {
			existing = &tables[i]
			break
		}
	}

	if existing != nil && policy == CollisionReplace {
		// Add any missing columns to the existing table.
		haveCols := map[string]bool{}
		for _, c := range existing.Columns {
			haveCols[strings.ToLower(c.Name)] = true
		}
		added := 0
		for _, c := range tbl.Columns {
			if c.IsPrimaryKey {
				continue
			}
			cname := dbal.NormalizeIdentifier(c.Name)
			if cname == "" {
				cname = dbal.NormalizeIdentifier(c.DisplayName)
			}
			if haveCols[strings.ToLower(cname)] {
				continue
			}
			if _, err := s.AddColumn(ctx, existing.ID, ColumnMetadata{
				Name:               cname,
				DisplayName:        c.DisplayName,
				FieldType:          dbal.AgnosticFieldType(c.FieldType),
				IsNullable:         c.IsNullable,
				DefaultValue:       c.DefaultValue,
				CalculationFormula: c.CalculationFormula,
				ValidationRules:    c.ValidationRules,
			}); err != nil {
				return nil, err
			}
			added++
		}
		report.CreatedID = existing.ID
		report.CreatedName = existing.DisplayName
		report.CreatedCount = added
		return report, nil
	}

	if hasCollision || existing != nil {
		displayName, physName = uniqueTableNames(displayName, physName, tables)
	}

	created, err := s.CreateTable(ctx, displayName, physName)
	if err != nil {
		return nil, err
	}
	for _, c := range tbl.Columns {
		if c.IsPrimaryKey {
			continue
		}
		cname := dbal.NormalizeIdentifier(c.Name)
		if cname == "" {
			cname = dbal.NormalizeIdentifier(c.DisplayName)
		}
		if _, err := s.AddColumn(ctx, created.ID, ColumnMetadata{
			Name:               cname,
			DisplayName:        c.DisplayName,
			FieldType:          dbal.AgnosticFieldType(c.FieldType),
			IsNullable:         c.IsNullable,
			DefaultValue:       c.DefaultValue,
			CalculationFormula: c.CalculationFormula,
			ValidationRules:    c.ValidationRules,
		}); err != nil {
			_ = s.DeleteTable(ctx, created.ID)
			return nil, err
		}
	}
	report.CreatedID = created.ID
	report.CreatedName = created.DisplayName
	report.CreatedCount = 1
	return report, nil
}

func (s *Service) pasteColumns(
	ctx context.Context,
	targetTableID string,
	cols []ClipboardColumn,
	policy string,
	report *ClipboardPasteReport,
) (*ClipboardPasteReport, error) {
	t, err := s.findTableByIDOrName(ctx, targetTableID)
	if err != nil {
		return nil, err
	}
	existingByName := map[string]ColumnMetadata{}
	for _, c := range t.Columns {
		existingByName[strings.ToLower(c.Name)] = c
		existingByName[strings.ToLower(c.DisplayName)] = c
	}

	var lastID, lastName string
	count := 0
	for _, c := range cols {
		if c.IsPrimaryKey {
			continue
		}
		disp := strings.TrimSpace(c.DisplayName)
		if disp == "" {
			disp = strings.TrimSpace(c.Name)
		}
		cname := dbal.NormalizeIdentifier(c.Name)
		if cname == "" {
			cname = dbal.NormalizeIdentifier(disp)
		}
		if prev, exists := existingByName[strings.ToLower(cname)]; exists {
			if policy == CollisionReplace {
				updated, err := s.UpdateColumn(ctx, t.ID, prev.ID, UpdateColumnOptions{
					DisplayName:        disp,
					DefaultValue:       c.DefaultValue,
					UpdateDefaultValue: true,
					CalculationFormula: c.CalculationFormula,
					UpdateCalculation:  true,
					ValidationRules:    c.ValidationRules,
					UpdateValidation:   true,
				})
				if err != nil {
					return nil, err
				}
				lastID = updated.ID
				lastName = updated.DisplayName
				count++
				continue
			}
			disp, cname = uniqueColumnNames(disp, cname, existingByName)
		}
		created, err := s.AddColumn(ctx, t.ID, ColumnMetadata{
			Name:               cname,
			DisplayName:        disp,
			FieldType:          dbal.AgnosticFieldType(c.FieldType),
			IsNullable:         c.IsNullable,
			DefaultValue:       c.DefaultValue,
			CalculationFormula: c.CalculationFormula,
			ValidationRules:    c.ValidationRules,
		})
		if err != nil {
			return nil, err
		}
		existingByName[strings.ToLower(created.Name)] = *created
		existingByName[strings.ToLower(created.DisplayName)] = *created
		lastID = created.ID
		lastName = created.DisplayName
		count++
	}
	report.CreatedID = lastID
	report.CreatedName = lastName
	report.CreatedCount = count
	return report, nil
}

func (s *Service) pasteLayout(
	ctx context.Context,
	env ClipboardEnvelope,
	hasCollision bool,
	policy string,
	report *ClipboardPasteReport,
) (*ClipboardPasteReport, error) {
	l := env.Layout
	layouts, err := s.ListLayouts(ctx)
	if err != nil {
		return nil, err
	}
	var existing *LayoutMetadata
	for i := range layouts {
		if strings.EqualFold(strings.TrimSpace(layouts[i].Name), strings.TrimSpace(l.Name)) {
			existing = &layouts[i]
			break
		}
	}

	// Remap script_id and value_list_id references in the layout definition by
	// matching names in the destination database.
	def := cloneMap(l.Definition)
	s.remapLayoutDefinitionByNames(ctx, def, env.ReferencedScripts, env.ReferencedValueList)
	rawDef, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}

	toID, err := s.resolveDestinationOccurrenceID(ctx, l.TableOccurrenceName, l.BaseTableName, def)
	if err != nil {
		return nil, err
	}

	if existing != nil && policy == CollisionReplace {
		updated, err := s.UpdateLayout(ctx, existing.ID, existing.Name, rawDef)
		if err != nil {
			return nil, err
		}
		report.CreatedID = updated.ID
		report.CreatedName = updated.Name
		report.CreatedCount = 1
		return report, nil
	}

	name := strings.TrimSpace(l.Name)
	if hasCollision || existing != nil {
		name = uniqueObjectName(name, func(candidate string) bool {
			for _, item := range layouts {
				if strings.EqualFold(strings.TrimSpace(item.Name), candidate) {
					return true
				}
			}
			return false
		})
	}

	created, err := s.CreateLayout(ctx, name, toID, rawDef)
	if err != nil {
		return nil, err
	}
	report.CreatedID = created.ID
	report.CreatedName = created.Name
	report.CreatedCount = 1
	return report, nil
}

func (s *Service) pasteScript(
	ctx context.Context,
	sc *ClipboardScript,
	hasCollision bool,
	policy string,
	report *ClipboardPasteReport,
) (*ClipboardPasteReport, error) {
	scripts, err := s.ListScripts(ctx)
	if err != nil {
		return nil, err
	}
	var existing *ScriptMetadata
	for i := range scripts {
		if strings.EqualFold(strings.TrimSpace(scripts[i].Name), strings.TrimSpace(sc.Name)) {
			existing = &scripts[i]
			break
		}
	}

	steps := make([]ScriptStepMetadata, 0, len(sc.Steps))
	for i, st := range sc.Steps {
		seq := st.SequenceIdx
		if seq <= 0 {
			seq = i + 1
		}
		rawParams, err := json.Marshal(st.Params)
		if err != nil {
			rawParams = []byte("{}")
		}
		steps = append(steps, ScriptStepMetadata{
			SequenceIdx: seq,
			StepType:    st.StepType,
			Params:      rawParams,
			IsEnabled:   st.IsEnabled,
		})
	}

	if existing != nil && policy == CollisionReplace {
		updated, err := s.UpdateScript(ctx, existing.ID, existing.Name, sc.ContextTable, existing.FolderID, sc.IsActive, steps)
		if err != nil {
			return nil, err
		}
		report.CreatedID = updated.ID
		report.CreatedName = updated.Name
		report.CreatedCount = 1
		return report, nil
	}

	name := strings.TrimSpace(sc.Name)
	if hasCollision || existing != nil {
		name = uniqueObjectName(name, func(candidate string) bool {
			for _, item := range scripts {
				if strings.EqualFold(strings.TrimSpace(item.Name), candidate) {
					return true
				}
			}
			return false
		})
	}

	created, err := s.CreateScript(ctx, name, sc.ContextTable, nil, sc.IsActive, steps)
	if err != nil {
		return nil, err
	}
	report.CreatedID = created.ID
	report.CreatedName = created.Name
	report.CreatedCount = 1
	return report, nil
}

func (s *Service) findTableByIDOrName(ctx context.Context, idOrName string) (*TableMetadata, error) {
	key := strings.TrimSpace(idOrName)
	if key == "" {
		return nil, fmt.Errorf("%w: table_id is required", ErrInvalidClipboard)
	}
	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	for i := range tables {
		if tables[i].ID == key || strings.EqualFold(tables[i].Name, key) || strings.EqualFold(tables[i].DisplayName, key) {
			return &tables[i], nil
		}
	}
	return nil, fmt.Errorf("table not found: %s", key)
}

func (s *Service) resolveOccurrenceNames(ctx context.Context, occID string, def map[string]interface{}) (string, string) {
	occs, _ := s.ListTableOccurrences(ctx)
	tables, _ := s.ListTables(ctx)
	tableByID := map[string]TableMetadata{}
	for _, t := range tables {
		tableByID[t.ID] = t
	}
	for _, o := range occs {
		if o.ID == occID {
			t := tableByID[o.BaseTableID]
			return o.Name, t.Name
		}
	}
	if t, ok := tableByID[occID]; ok {
		return t.Name, t.Name
	}
	if raw, ok := def["table_occurrence"].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw), strings.TrimSpace(raw)
	}
	return "", ""
}

func (s *Service) resolveDestinationOccurrenceID(
	ctx context.Context,
	occName string,
	baseTable string,
	def map[string]interface{},
) (string, error) {
	if occName == "" {
		if raw, ok := def["table_occurrence"].(string); ok {
			occName = strings.TrimSpace(raw)
		}
	}
	occs, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return "", err
	}
	for _, o := range occs {
		if strings.EqualFold(o.Name, occName) || (baseTable != "" && strings.EqualFold(o.Name, baseTable)) {
			return o.ID, nil
		}
	}
	tables, err := s.ListTables(ctx)
	if err != nil {
		return "", err
	}
	for _, t := range tables {
		if strings.EqualFold(t.Name, occName) || strings.EqualFold(t.DisplayName, occName) ||
			(baseTable != "" && (strings.EqualFold(t.Name, baseTable) || strings.EqualFold(t.DisplayName, baseTable))) {
			for _, o := range occs {
				if o.BaseTableID == t.ID {
					return o.ID, nil
				}
			}
			return t.ID, nil
		}
	}
	if len(occs) > 0 {
		return occs[0].ID, nil
	}
	if len(tables) > 0 {
		return tables[0].ID, nil
	}
	return "", fmt.Errorf("destination solution has no tables to attach layout to")
}

func (s *Service) inspectLayoutDependencies(
	ctx context.Context,
	env ClipboardEnvelope,
	tableByName map[string]TableMetadata,
) []ClipboardDependency {
	var deps []ClipboardDependency
	seen := map[string]bool{}
	addDep := func(kind, name, detail string) {
		key := kind + ":" + strings.ToLower(name)
		if name == "" || seen[key] {
			return
		}
		seen[key] = true
		deps = append(deps, ClipboardDependency{Kind: kind, Name: name, Detail: detail})
	}

	toName := strings.TrimSpace(env.Layout.TableOccurrenceName)
	if toName == "" {
		if raw, ok := env.Layout.Definition["table_occurrence"].(string); ok {
			toName = strings.TrimSpace(raw)
		}
	}
	var targetTable *TableMetadata
	if toName != "" {
		if t, ok := tableByName[strings.ToLower(toName)]; ok {
			targetTable = &t
		} else if env.Layout.BaseTableName != "" {
			if t, ok := tableByName[strings.ToLower(env.Layout.BaseTableName)]; ok {
				targetTable = &t
			}
		}
		if targetTable == nil {
			occs, _ := s.ListTableOccurrences(ctx)
			foundOcc := false
			for _, o := range occs {
				if strings.EqualFold(o.Name, toName) {
					foundOcc = true
					break
				}
			}
			if !foundOcc {
				addDep("table_occurrence", toName, fmt.Sprintf("Table occurrence %q does not exist in destination", toName))
			}
		}
	}

	fieldSet := map[string]bool{}
	if targetTable != nil {
		for _, c := range targetTable.Columns {
			fieldSet[strings.ToLower(c.Name)] = true
			fieldSet[strings.ToLower(c.DisplayName)] = true
		}
	}

	scripts, _ := s.ListScripts(ctx)
	scriptNames := map[string]bool{}
	for _, sc := range scripts {
		scriptNames[strings.ToLower(strings.TrimSpace(sc.Name))] = true
	}

	vls, _ := s.ListValueLists(ctx)
	vlNames := map[string]bool{}
	for _, vl := range vls {
		vlNames[strings.ToLower(strings.TrimSpace(vl.Name))] = true
	}

	if objs, ok := env.Layout.Definition["objects"].([]interface{}); ok {
		for _, raw := range objs {
			obj, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if fname, _ := obj["field_name"].(string); strings.TrimSpace(fname) != "" && targetTable != nil {
				clean := strings.TrimSpace(fname)
				if !strings.Contains(clean, "::") && !fieldSet[strings.ToLower(clean)] {
					addDep("field", clean, fmt.Sprintf("Field %q does not exist on table %q", clean, targetTable.DisplayName))
				}
			}
			if action, ok := obj["action"].(map[string]interface{}); ok {
				sname, _ := action["script_name"].(string)
				if sname == "" {
					if sid, _ := action["script_id"].(string); sid != "" && env.ReferencedScripts != nil {
						sname = env.ReferencedScripts[sid]
					}
				}
				if strings.TrimSpace(sname) != "" && !scriptNames[strings.ToLower(strings.TrimSpace(sname))] {
					addDep("script", sname, fmt.Sprintf("Button references script %q which does not exist in destination", sname))
				}
			}
			if vlID, _ := obj["value_list_id"].(string); vlID != "" && env.ReferencedValueList != nil {
				if vlName := env.ReferencedValueList[vlID]; vlName != "" && !vlNames[strings.ToLower(strings.TrimSpace(vlName))] {
					addDep("value_list", vlName, fmt.Sprintf("Field control references value list %q which does not exist in destination", vlName))
				}
			}
		}
	}

	return deps
}

func (s *Service) inspectScriptDependencies(
	ctx context.Context,
	sc *ClipboardScript,
	tableByName map[string]TableMetadata,
	scripts []ScriptMetadata,
) []ClipboardDependency {
	var deps []ClipboardDependency
	seen := map[string]bool{}
	addDep := func(kind, name, detail string) {
		key := kind + ":" + strings.ToLower(name)
		if name == "" || seen[key] {
			return
		}
		seen[key] = true
		deps = append(deps, ClipboardDependency{Kind: kind, Name: name, Detail: detail})
	}

	if ct := strings.TrimSpace(sc.ContextTable); ct != "" {
		if _, ok := tableByName[strings.ToLower(ct)]; !ok {
			addDep("table_occurrence", ct, fmt.Sprintf("Context table %q does not exist in destination", ct))
		}
	}

	layouts, _ := s.ListLayouts(ctx)
	layoutNames := map[string]bool{}
	for _, l := range layouts {
		layoutNames[strings.ToLower(strings.TrimSpace(l.Name))] = true
	}
	scriptNames := map[string]bool{}
	for _, item := range scripts {
		scriptNames[strings.ToLower(strings.TrimSpace(item.Name))] = true
	}

	for _, st := range sc.Steps {
		switch st.StepType {
		case "go_to_layout":
			if lname, _ := st.Params["layout_name"].(string); strings.TrimSpace(lname) != "" {
				if !layoutNames[strings.ToLower(strings.TrimSpace(lname))] {
					addDep("layout", lname, fmt.Sprintf("Step go_to_layout references layout %q which does not exist in destination", lname))
				}
			}
		case "perform_script":
			if sname, _ := st.Params["script_name"].(string); strings.TrimSpace(sname) != "" {
				if !scriptNames[strings.ToLower(strings.TrimSpace(sname))] {
					addDep("script", sname, fmt.Sprintf("Step perform_script references script %q which does not exist in destination", sname))
				}
			}
		}
	}
	return deps
}

func (s *Service) remapLayoutDefinitionByNames(
	ctx context.Context,
	def map[string]interface{},
	refScripts map[string]string,
	refVLs map[string]string,
) {
	scripts, _ := s.ListScripts(ctx)
	scriptIDByName := map[string]string{}
	for _, sc := range scripts {
		scriptIDByName[strings.ToLower(strings.TrimSpace(sc.Name))] = sc.ID
	}
	vls, _ := s.ListValueLists(ctx)
	vlIDByName := map[string]string{}
	for _, vl := range vls {
		vlIDByName[strings.ToLower(strings.TrimSpace(vl.Name))] = vl.ID
	}

	objs, ok := def["objects"].([]interface{})
	if !ok {
		return
	}
	for _, raw := range objs {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if action, ok := obj["action"].(map[string]interface{}); ok {
			sname, _ := action["script_name"].(string)
			if sname == "" && refScripts != nil {
				if sid, _ := action["script_id"].(string); sid != "" {
					sname = refScripts[sid]
				}
			}
			if dstID := scriptIDByName[strings.ToLower(strings.TrimSpace(sname))]; dstID != "" {
				action["script_id"] = dstID
			}
		}
		if vlID, _ := obj["value_list_id"].(string); vlID != "" && refVLs != nil {
			if vlName := refVLs[vlID]; vlName != "" {
				if dstID := vlIDByName[strings.ToLower(strings.TrimSpace(vlName))]; dstID != "" {
					obj["value_list_id"] = dstID
				}
			}
		}
	}
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	raw, err := json.Marshal(in)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func uniqueTableNames(displayName, physName string, existing []TableMetadata) (string, string) {
	taken := map[string]bool{}
	for _, t := range existing {
		taken[strings.ToLower(t.Name)] = true
		taken[strings.ToLower(t.DisplayName)] = true
	}
	for i := 1; ; i++ {
		suffixDisp := " Copy"
		suffixPhys := "_copy"
		if i > 1 {
			suffixDisp = fmt.Sprintf(" Copy %d", i)
			suffixPhys = fmt.Sprintf("_copy_%d", i)
		}
		candDisp := displayName + suffixDisp
		candPhys := copyName(physName, suffixPhys)
		if !taken[strings.ToLower(candDisp)] && !taken[strings.ToLower(candPhys)] {
			return candDisp, candPhys
		}
	}
}

func uniqueColumnNames(displayName, physName string, existing map[string]ColumnMetadata) (string, string) {
	for i := 1; ; i++ {
		suffixDisp := " Copy"
		suffixPhys := "_copy"
		if i > 1 {
			suffixDisp = fmt.Sprintf(" Copy %d", i)
			suffixPhys = fmt.Sprintf("_copy_%d", i)
		}
		candDisp := displayName + suffixDisp
		candPhys := copyName(physName, suffixPhys)
		_, ok1 := existing[strings.ToLower(candDisp)]
		_, ok2 := existing[strings.ToLower(candPhys)]
		if !ok1 && !ok2 {
			return candDisp, candPhys
		}
	}
}

func uniqueObjectName(base string, isTaken func(string) bool) string {
	for i := 1; ; i++ {
		cand := base + " Copy"
		if i > 1 {
			cand = fmt.Sprintf("%s Copy %d", base, i)
		}
		if !isTaken(cand) {
			return cand
		}
	}
}

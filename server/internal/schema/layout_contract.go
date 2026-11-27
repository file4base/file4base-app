package schema

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/file4base/file4base-app/server/internal/autoenter"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/validation"
)

// The layout contract is a layout as the FileMaker Data API sees it: the
// fields on it in FileMaker's order, each with its control, value list and
// repetitions; and its portals, each with its occurrence, sort and fields.
// The part of it the layout's definition alone gives is compiled once per
// definition and cached; the fields' own attributes are read with each
// request, so a change in Manage Database shows at once.

// Source types: FileMaker's type of a migrated field where File4Base's own
// type does not say it, as "type/result" (sys_columns.source_type).
const (
	// SourceTypeTime is a Time field, kept as a timestamp.
	SourceTypeTime = "normal/time"
	// SourceTypeCalculationTime is a calculation with a time result,
	// computed as a timestamp.
	SourceTypeCalculationTime = "calculation/time"
)

// summarySourceTypes are the summary fields, by result, which File4Base
// keeps as plain columns.
var summarySourceTypes = map[string]string{
	"TEXT":      "summary/text",
	"NUMBER":    "summary/number",
	"DATE":      "summary/date",
	"TIME":      "summary/time",
	"TIMESTAMP": "summary/timestamp",
}

// SummarySourceType is the source type of a summary field with result
// dataType, as FileMaker writes it; a number when it does not say.
func SummarySourceType(dataType string) string {
	if t, ok := summarySourceTypes[strings.ToUpper(strings.TrimSpace(dataType))]; ok {
		return t
	}
	return "summary/number"
}

// ValidSourceType reports whether t is empty or a source type File4Base
// keeps.
func ValidSourceType(t string) bool {
	if t == "" || t == SourceTypeTime || t == SourceTypeCalculationTime {
		return true
	}
	for _, v := range summarySourceTypes {
		if t == v {
			return true
		}
	}
	return false
}

// ContractField is a field object of a layout.
type ContractField struct {
	// Occurrence is the occurrence the field is read from, empty for the
	// layout's own; Field is its column.
	Occurrence string
	Field      string
	// ControlStyle is File4Base's: edit_box, drop_down_list, ...
	ControlStyle string
	Concealed    bool
	ValueListID  string
	// RepetitionStart is the first repetition shown, at least 1, and
	// RepetitionsShown how many are, 0 when the object does not say (all
	// of them from the first). ResolveLayout bounds both by the field's.
	RepetitionStart  int
	RepetitionsShown int
}

// ContractSort is a sort field of a portal.
type ContractSort struct {
	Occurrence, Field string
	// Order is ascending, descending or custom (a value list's order).
	Order       string
	ValueListID string
}

// ContractPortal is a portal of a layout, with the fields of its rows.
type ContractPortal struct {
	ID string
	// Name is the portal's object name, empty when it has none.
	Name       string
	Occurrence string
	Sort       []ContractSort
	Filter     string
	Fields     []ContractField
}

// LayoutContract is the compiled part of a layout's contract.
type LayoutContract struct {
	// Fields are the field objects outside portals, in object order.
	Fields  []ContractField
	Portals []ContractPortal
}

// contractCache holds compiled contracts by the hash of their definition.
var contractCache = struct {
	sync.Mutex
	m map[[32]byte]*LayoutContract
}{m: map[[32]byte]*LayoutContract{}}

// contractCacheSize bounds the cache: it is emptied when full.
const contractCacheSize = 512

// CompileLayoutContract compiles a layout definition. The result is shared
// and must not be changed.
func CompileLayoutContract(definition []byte) (*LayoutContract, error) {
	key := sha256.Sum256(definition)
	contractCache.Lock()
	c, ok := contractCache.m[key]
	contractCache.Unlock()
	if ok {
		return c, nil
	}
	c, err := compileLayoutContract(definition)
	if err != nil {
		return nil, err
	}
	contractCache.Lock()
	if len(contractCache.m) >= contractCacheSize {
		contractCache.m = map[[32]byte]*LayoutContract{}
	}
	contractCache.m[key] = c
	contractCache.Unlock()
	return c, nil
}

type contractObject struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Name   string  `json:"name"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Portal *string `json:"portal"`
	Panel  *struct {
		Control string `json:"control"`
		Index   int    `json:"index"`
	} `json:"panel"`
	FieldBinding *contractField  `json:"field_binding"`
	PortalConfig *contractPortal `json:"portal_config"`
}

type contractField struct {
	FieldName       string `json:"field_name"`
	Occurrence      string `json:"table_occurrence"`
	ControlStyle    string `json:"control_style"`
	ValueListID     string `json:"value_list_id"`
	Concealed       bool   `json:"concealed"`
	FirstRepetition int    `json:"first_repetition"`
	Shown           int    `json:"repetitions_shown"`
	Missing         bool   `json:"missing"`
}

type contractPortal struct {
	Occurrence string `json:"occurrence"`
	Sort       string `json:"sort"`
	Filter     string `json:"filter"`
	SortFields []struct {
		FieldName   string `json:"field_name"`
		Occurrence  string `json:"table_occurrence"`
		Order       string `json:"order"`
		ValueListID string `json:"value_list_id"`
	} `json:"sort_fields"`
}

// panel is the tab or slide control panel the object is drawn on, if any.
func (o contractObject) panel() string {
	if o.Panel == nil || o.Panel.Control == "" {
		return ""
	}
	return fmt.Sprintf("%s#%d", o.Panel.Control, o.Panel.Index)
}

func compileLayoutContract(definition []byte) (*LayoutContract, error) {
	var def struct {
		Objects []contractObject `json:"objects"`
	}
	if len(strings.TrimSpace(string(definition))) > 0 {
		if err := json.Unmarshal(definition, &def); err != nil {
			return nil, fmt.Errorf("reading the layout's definition: %w", err)
		}
	}
	c := &LayoutContract{}
	portalAt := map[string]int{}
	for _, o := range def.Objects {
		if o.Type != "portal" || o.PortalConfig == nil {
			continue
		}
		p := ContractPortal{ID: o.ID, Name: strings.TrimSpace(o.Name), Occurrence: o.PortalConfig.Occurrence, Filter: o.PortalConfig.Filter}
		if len(o.PortalConfig.SortFields) > 0 {
			for _, s := range o.PortalConfig.SortFields {
				p.Sort = append(p.Sort, ContractSort{Occurrence: s.Occurrence, Field: s.FieldName, Order: s.Order, ValueListID: s.ValueListID})
			}
		} else {
			for _, k := range strings.Split(o.PortalConfig.Sort, ",") {
				if k = strings.TrimSpace(k); k == "" {
					continue
				}
				order := "ascending"
				if strings.HasPrefix(k, "-") {
					k, order = k[1:], "descending"
				}
				p.Sort = append(p.Sort, ContractSort{Occurrence: p.Occurrence, Field: k, Order: order})
			}
		}
		if o.ID != "" {
			portalAt[o.ID] = len(c.Portals)
		}
		c.Portals = append(c.Portals, p)
	}
	portals := def.Objects[:0:0]
	for _, o := range def.Objects {
		if o.Type == "portal" && o.PortalConfig != nil {
			portals = append(portals, o)
		}
	}
	for _, o := range def.Objects {
		if o.Type != "field" || o.FieldBinding == nil || o.FieldBinding.Missing || o.FieldBinding.FieldName == "" {
			continue
		}
		b := o.FieldBinding
		f := ContractField{
			Occurrence: b.Occurrence, Field: b.FieldName, ControlStyle: b.ControlStyle, Concealed: b.Concealed,
			ValueListID: b.ValueListID, RepetitionStart: max(b.FirstRepetition, 1), RepetitionsShown: max(b.Shown, 0),
		}
		// A field that says its portal ("" for none) is where it says, as a
		// migrated one is; any other is where the client draws it: in the
		// first portal of its panel whose bounds wholly hold it.
		in := -1
		if o.Portal != nil {
			if i, ok := portalAt[*o.Portal]; ok {
				in = i
			}
		} else {
			for i, p := range portals {
				if o.panel() == p.panel() && o.X >= p.X && o.Y >= p.Y && o.X+o.Width <= p.X+p.Width && o.Y+o.Height <= p.Y+p.Height {
					in = i
					break
				}
			}
		}
		if in >= 0 {
			c.Portals[in].Fields = append(c.Portals[in].Fields, f)
		} else {
			c.Fields = append(c.Fields, f)
		}
	}
	return c, nil
}

// LayoutField is a field object of a layout with the field it shows.
type LayoutField struct {
	ContractField
	// Occurrence is the field's occurrence, resolved: the layout's when
	// the object names none.
	OccurrenceName string
	// Related is a field read from another occurrence than the layout's,
	// or in a portal: the Data API names it Occurrence::Field.
	Related bool
	Column  ColumnMetadata
	// Ordinal is the field's position among its table's, from 1.
	Ordinal int
	// ValueList is the name of the object's value list, if any.
	ValueList string
}

// DataAPIName is the field's name as the Data API gives it.
func (f LayoutField) DataAPIName() string {
	name := f.Column.DisplayName
	if name == "" {
		name = f.Column.Name
	}
	if f.Related {
		return f.OccurrenceName + "::" + name
	}
	return name
}

// LayoutPortal is a portal of a layout with its fields.
type LayoutPortal struct {
	ContractPortal
	Fields []LayoutField
}

// DataAPIName is the portal's name as the Data API keys it: its object
// name, or its occurrence's.
func (p LayoutPortal) DataAPIName() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Occurrence
}

// ResolvedLayout is a layout's contract with its fields read.
type ResolvedLayout struct {
	Layout     LayoutMetadata
	Occurrence string
	// Table is the layout's table, empty when its occurrence has none here.
	Table   string
	Fields  []LayoutField
	Portals []LayoutPortal
}

// ResolveLayout reads the contract of layout id. A field object whose
// field cannot be found (a missing field, or one of another solution's
// occurrence) is left out.
func (s *Service) ResolveLayout(ctx context.Context, id string) (*ResolvedLayout, error) {
	layout, err := s.GetLayout(ctx, id)
	if err != nil {
		return nil, err
	}
	contract, err := CompileLayoutContract(layout.Definition)
	if err != nil {
		return nil, err
	}
	occurrences, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, err
	}
	occByName := map[string]TableOccurrence{}
	occByID := map[string]TableOccurrence{}
	out := &ResolvedLayout{Layout: *layout}
	for _, o := range occurrences {
		occByName[strings.ToLower(o.Name)] = o
		occByID[o.ID] = o
		if o.ID == layout.TableOccurrenceID {
			out.Occurrence = o.Name
		}
	}
	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	tableByID := map[string]*TableMetadata{}
	for i := range tables {
		tableByID[tables[i].ID] = &tables[i]
	}
	for _, o := range occurrences {
		if t := tableByID[o.BaseTableID]; t != nil && o.ID == layout.TableOccurrenceID && o.External == nil {
			out.Table = t.Name
		}
	}
	lists, err := s.ListValueLists(ctx)
	if err != nil {
		return nil, err
	}
	listName := map[string]string{}
	for _, l := range lists {
		listName[l.ID] = l.Name
	}
	// An object names its occurrence by name, or by id as the designer may.
	occurrence := func(ref string) (TableOccurrence, bool) {
		if o, ok := occByID[ref]; ok {
			return o, true
		}
		o, ok := occByName[strings.ToLower(ref)]
		return o, ok
	}
	// resolve finds a field object's field; one that names no occurrence is
	// of its portal's, or of the layout's.
	resolve := func(cf ContractField, inPortal bool, home string) (LayoutField, bool) {
		f := LayoutField{ContractField: cf, OccurrenceName: cf.Occurrence}
		if f.OccurrenceName == "" {
			f.OccurrenceName = home
		}
		occ, ok := occurrence(f.OccurrenceName)
		if !ok || occ.External != nil {
			return f, false
		}
		f.OccurrenceName = occ.Name
		t := tableByID[occ.BaseTableID]
		if t == nil {
			return f, false
		}
		found := false
		for i, c := range t.Columns {
			if c.Name == cf.Field {
				f.Column, f.Ordinal, found = c, i+1, true
				break
			}
		}
		if !found {
			for i, c := range t.Columns {
				if strings.EqualFold(c.Name, cf.Field) {
					f.Column, f.Ordinal, found = c, i+1, true
					break
				}
			}
		}
		if !found {
			return f, false
		}
		f.Related = inPortal || occ.ID != layout.TableOccurrenceID
		f.ValueList = listName[cf.ValueListID]
		// The repetitions shown, as the layout shows them: within the
		// field's, all from the first by default.
		most := max(f.Column.MaxRepetitions, 1)
		f.RepetitionStart = min(max(cf.RepetitionStart, 1), most)
		left := most - f.RepetitionStart + 1
		f.RepetitionsShown = left
		if cf.RepetitionsShown >= 1 {
			f.RepetitionsShown = min(cf.RepetitionsShown, left)
		}
		return f, true
	}
	for _, cf := range contract.Fields {
		if f, ok := resolve(cf, false, out.Occurrence); ok {
			out.Fields = append(out.Fields, f)
		}
	}
	for _, cp := range contract.Portals {
		p := LayoutPortal{ContractPortal: cp}
		if occ, ok := occurrence(cp.Occurrence); ok {
			p.Occurrence = occ.Name
		}
		home := p.Occurrence
		if home == "" {
			home = out.Occurrence
		}
		for _, cf := range cp.Fields {
			if f, ok := resolve(cf, true, home); ok {
				p.Fields = append(p.Fields, f)
			}
		}
		out.Portals = append(out.Portals, p)
	}
	return out, nil
}

// FieldKind is a field's FileMaker type and result, as the Data API names
// them: normal, calculation or summary; text, number, date, time,
// timeStamp or container.
func (c ColumnMetadata) FieldKind() (kind, result string) {
	kind, result = "normal", fieldResult(string(c.FieldType))
	switch c.FieldType {
	case dbal.FieldTypeCalculation:
		kind, result = "calculation", "text"
		if c.CalculationFormula != nil {
			var spec struct {
				ResultType string `json:"result_type"`
			}
			if json.Unmarshal([]byte(*c.CalculationFormula), &spec) == nil {
				result = fieldResult(spec.ResultType)
			}
		}
	case dbal.FieldTypeSummary:
		kind, result = "summary", "number"
	}
	// The source type says what File4Base's type cannot, where the two
	// agree.
	switch src := c.SourceType; {
	case src == SourceTypeTime && kind == "normal" && result == "timeStamp":
		result = "time"
	case src == SourceTypeCalculationTime && kind == "calculation" && result == "timeStamp":
		result = "time"
	case strings.HasPrefix(src, "summary/") && kind == "normal" && ValidSourceType(src):
		r := fieldResult(strings.TrimPrefix(src, "summary/"))
		if r == result || (r == "time" && result == "timeStamp") {
			kind, result = "summary", r
		}
	}
	return kind, result
}

// fieldResult is the Data API's name of a File4Base field or result type.
func fieldResult(t string) string {
	switch strings.ToUpper(t) {
	case "NUMBER", "BOOLEAN":
		return "number"
	case "DATE":
		return "date"
	case "TIME":
		return "time"
	case "TIMESTAMP":
		return "timeStamp"
	case "CONTAINER":
		return "container"
	}
	return "text"
}

// FieldOptions are the options of a field the Data API describes.
type FieldOptions struct {
	Global, AutoEnter                           bool
	NotEmpty, Numeric, FourDigitYear, TimeOfDay bool
	MaxCharacters                               int
	MaxRepeat                                   int
}

// Options reads the options of a field the Data API describes: its storage,
// auto-enter and validation options.
func (c ColumnMetadata) Options() FieldOptions {
	o := FieldOptions{MaxRepeat: max(c.MaxRepetitions, 1)}
	if ae, err := autoenter.Parse(c.DefaultValue); err == nil {
		o.Global = ae.StorageGlobal
		o.AutoEnter = ae.Any() || ae.ValueLastVisited
	}
	if c.FieldType == dbal.FieldTypeCalculation || c.FieldType == dbal.FieldTypeSummary {
		o.AutoEnter = false
	}
	if r, err := validation.Parse(c.ValidationRules); err == nil && r != nil {
		o.NotEmpty = r.NotEmpty
		if r.StrictTypeEnabled {
			o.Numeric = r.StrictType == "Numeric Only"
			o.FourDigitYear = r.StrictType == "4-Digit Year"
			o.TimeOfDay = r.StrictType == "Time of Day"
		}
		if r.MaxLengthEnabled && r.MaxLength != nil {
			o.MaxCharacters = *r.MaxLength
		}
	}
	return o
}

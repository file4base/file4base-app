package migration

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/google/uuid"
)

// Converting the parts and objects of a layout of a <FMSaveAsXML> export.
//
// FileMaker writes each part of a layout with its height (<Definition
// size>), its own style and the objects drawn in it, and each object with
// its <Bounds> in layout coordinates: the top of the layout is 0 and the
// parts follow each other down. File4Base places objects the same way (a
// part is the band its objects' tops fall in), so a part keeps its height
// and an object its box, and the layout reads as it did.
//
// Objects kept: text (with its merge fields), field objects with their
// control style, value list, repetitions, placeholder and display format,
// rectangles, rounded rectangles, ovals, lines and pictures of the
// layout's library, each with its style (layout_css.go), its name, tooltip
// and hide condition. Objects grouped together are kept one by one. A tab
// control or slide control shows the objects of its front panel.
// Buttons, portals, popovers, web viewers and charts are not converted here
// yet; the report says, once per layout, which ones were left out.

// LayoutObjectCount counts the layout objects of one FileMaker kind: how many
// the source has, and how many were migrated.
type LayoutObjectCount struct {
	Source   int `json:"source"`
	Migrated int `json:"migrated"`
}

// maxLayoutImageBase64 bounds one picture kept in a layout definition,
// as layout_schema.json does for media.
const maxLayoutImageBase64 = 2 << 20

// layoutObjectContext is what every layout of a file is converted against.
type layoutObjectContext struct {
	binding       formulaBinding
	valueLists    map[string]string // lower-cased name -> value list id
	relationships []schema.BundleRelationship
	library       map[string]*xmlNode // library reference id -> <BinaryData>
	themes        map[string]*fmTheme // theme name -> its CSS
	defaultTheme  *fmTheme            // for a layout that names no theme
	dateOrder     string              // dmy, mdy or iso: how the file writes dates; empty when unknown

	counts map[string]*LayoutObjectCount
	media  map[string]map[string]interface{}
}

func newLayoutObjectContext(root *xmlNode, bundle *schema.SolutionBundle, binding formulaBinding, dateOrder string) *layoutObjectContext {
	ctx := &layoutObjectContext{
		binding:       binding,
		valueLists:    map[string]string{},
		relationships: bundle.Relationships,
		library:       map[string]*xmlNode{},
		themes:        map[string]*fmTheme{},
		dateOrder:     dateOrder,
		counts:        map[string]*LayoutObjectCount{},
		media:         map[string]map[string]interface{}{},
	}
	for _, vl := range bundle.ValueLists {
		key := strings.ToLower(vl.Name)
		if _, taken := ctx.valueLists[key]; !taken {
			ctx.valueLists[key] = vl.ID
		}
	}
	for _, bd := range catalogDefinitions(root, []string{"LibraryCatalog"}, "BinaryData") {
		if ref := findFirstChild(bd, "LibraryReference"); ref != nil {
			if id := firstAttr(ref, "id"); id != "" {
				ctx.library[id] = bd
			}
		}
	}
	// A layout of the classic look names no theme: it takes the file's
	// default theme, or the classic one.
	var only *fmTheme
	for _, th := range catalogDefinitions(root, []string{"ThemeCatalog"}, "Theme") {
		css := findFirstChild(th, "CSS")
		if css == nil {
			continue
		}
		theme := newFMTheme(css.Text)
		name := firstAttr(th, "name")
		ctx.themes[name] = theme
		only = theme
		if parseBoolAttr(th, "defaultTheme") {
			ctx.defaultTheme = theme
		}
	}
	if ctx.defaultTheme == nil {
		ctx.defaultTheme = ctx.themes["com.filemaker.theme.classic"]
	}
	if ctx.defaultTheme == nil && len(ctx.themes) == 1 {
		ctx.defaultTheme = only
	}
	return ctx
}

// isNativeLayout reports whether a layout is written the <FMSaveAsXML> way,
// with each part's definition and objects.
func isNativeLayout(ln *xmlNode) bool {
	parts := findFirstChild(ln, "PartsList")
	if parts == nil {
		return false
	}
	for _, p := range parts.Children {
		if p.Name == "Part" && findFirstChild(p, "Definition") != nil {
			return true
		}
	}
	return false
}

// fmPartTypes maps a FileMaker part to the File4Base part and to the theme
// class that styles it.
var fmPartTypes = map[string][2]string{
	"top navigation":         {"top_navigation", "top_nav_part"},
	"title header":           {"title_header", "title_header"},
	"header":                 {"header", "header"},
	"leading grand summary":  {"leading_grand_summary", "leading_grand_summary"},
	"leading sub-summary":    {"subsummary", "leading_sub_summary"},
	"sub-summary":            {"subsummary", "leading_sub_summary"},
	"body":                   {"body", "body"},
	"trailing sub-summary":   {"subsummary", "trailing_sub_summary"},
	"trailing grand summary": {"trailing_grand_summary", "trailing_grand_summary"},
	"footer":                 {"footer", "footer"},
	"title footer":           {"footer", "title_footer"},
	"bottom navigation":      {"bottom_navigation", "bottom_nav_part"},
}

// fmFieldControls maps a field object to its control style and theme class.
var fmFieldControls = map[string][2]string{
	"Edit Box":           {"edit_box", "edit_box"},
	"Drop-down List":     {"drop_down_list", "drop_down"},
	"Pop-up Menu":        {"pop_up_menu", "pop_up"},
	"Checkbox Set":       {"checkbox_set", "checkbox_set"},
	"Radio Button Set":   {"radio_button_set", "radio_set"},
	"Drop-down Calendar": {"drop_down_calendar", "calendar"},
	"Container":          {"edit_box", "container"},
}

// fmShapes maps a drawn object to its File4Base type and theme class.
var fmShapes = map[string][2]string{
	"Rectangle":         {"rect", "rectangle"},
	"Rounded Rectangle": {"rounded_rect", "rounded"},
	"Oval":              {"oval", "oval"},
	"Line":              {"line", "line"},
}

// pendingObjectKinds are converted by a later change; until then a layout
// says it has them.
var pendingObjectKinds = map[string]string{
	"Button":          "button",
	"Grouped Button":  "grouped button",
	"Button Bar":      "button bar",
	"Portal":          "portal",
	"Popover Button":  "popover button",
	"PopoverPanel":    "popover",
	"Web Viewer":      "web viewer",
	"Chart":           "chart",
	"Tab Control":     "tab control",
	"Slide Control":   "slide control",
	"Panel":           "panel",
	"Group":           "group",
	"External Object": "external object",
}

// nativeLayout is one layout being converted.
type nativeLayout struct {
	ctx            *layoutObjectContext
	theme          *fmTheme
	name, layoutID string
	occName, occID string
	table          string // the layout's table, as imported
	occExternal    bool
	objects        []map[string]interface{}
	ids            map[string]bool
	objectIDs      map[*xmlNode]string

	pending      map[string]int // FileMaker kinds left out, by readable name
	frontPanels  int            // tab and slide controls shown by their front panel only
	hideUnbound  int
	unreachable  int
	missingField int
	noImage      int
	vectorImage  int
	mergeKept    int
}

// convertNativeLayout converts the parts and objects of a <FMSaveAsXML>
// layout into def, and answers what the report says about them.
//
// objectIDs receives the id each migrated object was given, by its
// <LayoutObject> and by its <Field>, so a script trigger set on the object
// is attached to it.
func convertNativeLayout(ln *xmlNode, ctx *layoutObjectContext, def map[string]interface{}, name, occName, occID string, objectIDs map[*xmlNode]string) []ReportItem {
	l := &nativeLayout{ctx: ctx, name: name, occName: occName, occID: occID, ids: map[string]bool{}, pending: map[string]int{}, objectIDs: objectIDs}
	if uid := findFirstChild(ln, "UUID"); uid != nil {
		l.layoutID = strings.TrimSpace(uid.Text)
	}
	l.theme = ctx.defaultTheme
	if ref := findFirstChild(ln, "LayoutThemeReference"); ref != nil && firstAttr(ref, "name") != "" {
		if theme := ctx.themes[firstAttr(ref, "name")]; theme != nil {
			l.theme = theme
		}
	}
	if o, ok := ctx.binding.occByName[strings.ToLower(occName)]; ok {
		l.table, l.occExternal = o.table, o.external
	}
	if w, err := strconv.ParseFloat(firstAttr(ln, "width"), 64); err == nil && w > 0 {
		def["width"] = w
	}
	if view := nativeLayoutView(ln); view != "" {
		def["default_view"] = view
	}
	// How a date typed into a field with no date format of its own is read:
	// in the order the file writes dates, as FileMaker reads it.
	if ctx.dateOrder != "" {
		def["date_order"] = ctx.dateOrder
	}
	for _, obj := range findNodes(ln, "LayoutObject") {
		ctx.count(firstAttr(obj, "type")).Source++
	}

	var parts []map[string]interface{}
	for _, pn := range findFirstChild(ln, "PartsList").Children {
		if pn.Name != "Part" {
			continue
		}
		defn := findFirstChild(pn, "Definition")
		if defn == nil {
			continue
		}
		kind := fmPartTypes[strings.ToLower(firstAttr(pn, "type"))]
		if kind[0] == "" {
			kind = [2]string{"body", "body"}
		}
		height, _ := strconv.ParseFloat(firstAttr(defn, "size"), 64)
		part := map[string]interface{}{"id": uuid.NewString(), "type": kind[0], "height": math.Max(0, height)}
		css := resolveStyle(l.theme, []string{kind[1]}, findFirstChild(defn, "LocalCSS"))
		if c, ok := cssColor(css["self"]["background-color"]); ok && c != "#00000000" {
			part["fill_color"] = c
		}
		if ref := findFirstChild(defn, "FieldReference"); ref != nil && kind[0] == "subsummary" {
			if column, _, ok := l.column(ref); ok {
				part["break_field"] = column
			}
		}
		parts = append(parts, part)
		if list := findFirstChild(pn, "ObjectList"); list != nil {
			l.convertList(list)
		}
	}
	if len(parts) > 0 {
		def["parts"] = parts
	}
	if l.objects == nil {
		l.objects = []map[string]interface{}{}
	}
	def["objects"] = l.objects
	return l.report()
}

// nativeLayoutView reads the view a layout opens in from its options.
// FileMaker writes no name for it, so this is read from the option bits an
// authentic export was seen to use: table view, then list view.
func nativeLayoutView(ln *xmlNode) string {
	opts := findFirstChild(ln, "Options")
	if opts == nil {
		return ""
	}
	bits, err := strconv.ParseUint(strings.TrimSpace(opts.Text), 10, 64)
	if err != nil {
		return ""
	}
	switch {
	case bits&0x4000 != 0:
		return "table"
	case bits&0x200 != 0:
		return "list"
	}
	return "form"
}

func (c *layoutObjectContext) count(kind string) *LayoutObjectCount {
	if kind == "" {
		kind = "(untyped)"
	}
	if c.counts[kind] == nil {
		c.counts[kind] = &LayoutObjectCount{}
	}
	return c.counts[kind]
}

// LayoutObjectCounts answers what was counted, for the report.
func (c *layoutObjectContext) LayoutObjectCounts() map[string]LayoutObjectCount {
	if len(c.counts) == 0 {
		return nil
	}
	out := make(map[string]LayoutObjectCount, len(c.counts))
	for k, v := range c.counts {
		out[k] = *v
	}
	return out
}

func (l *nativeLayout) convertList(list *xmlNode) {
	for _, obj := range list.Children {
		if obj.Name == "LayoutObject" {
			l.convertObject(obj)
		}
	}
}

// objectID is a migrated object's id: derived from the layout's and the
// object's FileMaker UUIDs, so migrating the same file again gives the same
// ids, unique in the layout.
func (l *nativeLayout) objectID(obj *xmlNode) string {
	id := ""
	if uid := findFirstChild(obj, "UUID"); uid != nil && l.layoutID != "" && strings.TrimSpace(uid.Text) != "" {
		id = uuid.NewSHA1(uuid.NameSpaceOID, []byte(l.layoutID+"/"+strings.TrimSpace(uid.Text))).String()
	}
	if id == "" || l.ids[id] {
		id = uuid.NewString()
	}
	l.ids[id] = true
	l.objectIDs[obj] = id
	if f := findFirstChild(obj, "Field"); f != nil {
		l.objectIDs[f] = id
	}
	return id
}

func (l *nativeLayout) convertObject(obj *xmlNode) {
	kind := firstAttr(obj, "type")
	switch kind {
	case "Group":
		// A group is a selection convenience; its members are kept as they
		// sit.
		if g := findFirstChild(obj, "GroupedButton"); g != nil {
			if list := findFirstChild(g, "ObjectList"); list != nil {
				l.convertList(list)
			}
		}
		l.ctx.count(kind).Migrated++
		return
	case "Tab Control", "Slide Control":
		l.convertFrontPanel(obj, kind)
		return
	}

	base := l.baseObject(obj)
	if base == nil {
		return
	}
	var ok bool
	switch {
	case kind == "Text":
		ok = l.text(obj, base)
	case fmFieldControls[kind][0] != "":
		ok = l.field(obj, kind, base)
	case fmShapes[kind][0] != "":
		ok = l.shape(obj, kind, base)
	case kind == "Graphic":
		ok = l.graphic(obj, base)
	default:
		readable := pendingObjectKinds[kind]
		if readable == "" {
			readable = strings.ToLower(kind)
		}
		l.pending[readable]++
		return
	}
	if ok {
		base["id"] = l.objectID(obj)
		l.objects = append(l.objects, base)
		l.ctx.count(kind).Migrated++
	}
}

// convertFrontPanel keeps the objects of the panel a tab or slide control
// shows first. File4Base has no tab panels yet, so the other panels are
// left out rather than drawn on top of each other.
func (l *nativeLayout) convertFrontPanel(obj *xmlNode, kind string) {
	holder := findFirstChild(obj, "TabControl", "SlideControl")
	if holder == nil {
		return
	}
	list := findFirstChild(holder, "ObjectList")
	if list == nil {
		return
	}
	var panels []*xmlNode
	for _, p := range list.Children {
		if p.Name == "LayoutObject" && firstAttr(p, "type") == "Panel" {
			panels = append(panels, p)
		}
	}
	if len(panels) == 0 {
		return
	}
	front, _ := strconv.Atoi(firstAttr(holder, "default"))
	if front < 0 || front >= len(panels) {
		front = 0
	}
	if inner := findFirstChild(panels[front], "TabPanel", "SlidePanel"); inner != nil {
		if objects := findFirstChild(inner, "ObjectList"); objects != nil {
			l.convertList(objects)
		}
	}
	l.frontPanels++
	l.pending[pendingObjectKinds[kind]]++
}

// baseObject reads what every object has: its box, name, tooltip and hide
// condition. Nil for an object without a box.
func (l *nativeLayout) baseObject(obj *xmlNode) map[string]interface{} {
	b := findFirstChild(obj, "Bounds")
	if b == nil {
		return nil
	}
	num := func(k string) float64 { f, _ := strconv.ParseFloat(firstAttr(b, k), 64); return f }
	top, left, bottom, right := num("top"), num("left"), num("bottom"), num("right")
	out := map[string]interface{}{
		"x": left, "y": top,
		"width": math.Max(0, right-left), "height": math.Max(0, bottom-top),
	}
	if name := firstAttr(obj, "name"); name != "" {
		out["name"] = name
	}
	if tip := findFirstChild(obj, "Tooltip"); tip != nil {
		if text, ok := calcLiteral(calculationText(tip)); ok && text != "" {
			out["tooltip"] = text
		}
	}
	if cond := findFirstChild(obj, "Conditions"); cond != nil {
		if hide := findFirstChild(cond, "Hide"); hide != nil {
			formula := calculationText(hide)
			if formula != "" {
				if bound, ok := l.bindFormula(formula); ok {
					out["hide_when"] = bound
					if parseBoolAttr(hide, "findMode") {
						out["hide_in_find"] = true
					}
				} else {
					l.hideUnbound++
				}
			}
		}
	}
	return out
}

// bindFormula binds a calculation of the layout to the imported names.
func (l *nativeLayout) bindFormula(formula string) (string, bool) {
	if l.table == "" || l.occExternal {
		return "", false
	}
	// A constant needs no field and is kept as written.
	fb := l.ctx.binding.bind(l.table, formula, l.occName, fmt.Sprintf("an object of the layout %q", l.name))
	if fb.why != "" {
		return "", false
	}
	return fb.rewritten, true
}

func (l *nativeLayout) style(obj *xmlNode, classes []string, withText bool) map[string]interface{} {
	return objectStyle(resolveStyle(l.theme, classes, findFirstChild(obj, "LocalCSS")), withText)
}

var fmMergeField = regexp.MustCompile(`<<([^<>]+)>>`)

func (l *nativeLayout) text(obj *xmlNode, out map[string]interface{}) bool {
	out["type"] = "label"
	text := ""
	var styled *xmlNode
	if t := findFirstChild(obj, "Text"); t != nil {
		if styled = findFirstChild(t, "StyledText"); styled != nil {
			if d := findFirstChild(styled, "Data"); d != nil {
				text = d.Text
			}
		}
	}
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	out["text"] = l.mergeText(text)
	style := l.style(obj, []string{"text_box"}, true)
	if styled != nil {
		applyCharacterStyle(styled, len([]rune(text)), style)
	}
	out["style"] = style
	return true
}

// mergeText writes FileMaker's merge fields and symbols the way File4Base
// resolves them: <<Field>> of the layout's table as {{column}}, and the
// symbols it knows. A related field or a variable is left as FileMaker
// wrote it, so it is visible rather than silently blank.
func (l *nativeLayout) mergeText(text string) string {
	text = fmMergeField.ReplaceAllStringFunc(text, func(m string) string {
		inner := strings.TrimSpace(m[2 : len(m)-2])
		if strings.HasPrefix(inner, "$") {
			l.mergeKept++
			return m
		}
		occ, field, qualified := strings.Cut(inner, "::")
		if !qualified {
			occ, field = l.occName, inner
		}
		o, ok := l.ctx.binding.occByName[strings.ToLower(occ)]
		if !ok || o.id != l.occID || o.external {
			l.mergeKept++
			return m
		}
		names := l.ctx.binding.fields[o.table]
		if names == nil {
			l.mergeKept++
			return m
		}
		column, ok := names.resolve(field)
		if !ok {
			l.mergeKept++
			return m
		}
		return "{{" + column + "}}"
	})
	return strings.NewReplacer("{{UserName}}", "{{CurrentUser}}", "{{AccountName}}", "{{CurrentUser}}").Replace(text)
}

// applyCharacterStyle applies the character style of a text that styles
// all of it: its font, size and face. A text styled in pieces keeps the
// style of its object; File4Base draws a text in one style.
func applyCharacterStyle(styled *xmlNode, length int, style map[string]interface{}) {
	list := findFirstChild(styled, "CharacterStyleList")
	if list == nil {
		return
	}
	var spans []*xmlNode
	for _, c := range list.Children {
		if c.Name == "CharacterStyle" {
			spans = append(spans, c)
		}
	}
	if len(spans) != 1 {
		return
	}
	start, _ := strconv.Atoi(firstAttr(spans[0], "Start"))
	end, _ := strconv.ParseInt(firstAttr(spans[0], "End"), 10, 64)
	if start != 0 || end < int64(length) {
		return
	}
	s := findFirstChild(spans[0], "Style")
	if s == nil {
		return
	}
	if f := findFirstChild(s, "FontReference"); f != nil && firstAttr(f, "name") != "" {
		style["font_family"] = firstAttr(f, "name")
	}
	if fs := findFirstChild(s, "FontStyle"); fs != nil {
		if size, err := strconv.ParseFloat(firstAttr(fs, "size"), 64); err == nil && size > 0 {
			style["font_size"] = size
		}
		if face, err := strconv.Atoi(firstAttr(fs, "face")); err == nil {
			if face&0x100 != 0 {
				style["font_weight"] = "bold"
			}
			if face&0x200 != 0 {
				style["font_style"] = "italic"
			}
			if face&0x400 != 0 {
				style["underline"] = true
			}
		}
	}
}

// column resolves a <FieldReference> to the occurrence it is read through
// and the column the import gave it. external is true for a field of
// another file's table, named as that file's migration names it.
func (l *nativeLayout) column(ref *xmlNode) (column string, occ boundOccurrence, ok bool) {
	to := findFirstChild(ref, "TableOccurrenceReference")
	occName := l.occName
	if to != nil && firstAttr(to, "name") != "" {
		occName = firstAttr(to, "name")
	}
	name := referencedFieldName(ref)
	o, found := l.ctx.binding.occByName[strings.ToLower(occName)]
	if !found || name == "" {
		return "", boundOccurrence{name: occName}, false
	}
	if o.external {
		return externalFieldName(name), o, true
	}
	names := l.ctx.binding.fields[o.table]
	if names == nil {
		return "", o, false
	}
	column, ok = names.resolve(name)
	return column, o, ok
}

// relationshipBetween is the relationship joining two occurrences directly.
func (l *nativeLayout) relationshipBetween(a, b string) string {
	for _, r := range l.ctx.relationships {
		if (r.LeftOccurrenceID == a && r.RightOccurrenceID == b) || (r.LeftOccurrenceID == b && r.RightOccurrenceID == a) {
			return r.ID
		}
	}
	return ""
}

func (l *nativeLayout) field(obj *xmlNode, kind string, out map[string]interface{}) bool {
	fn := findFirstChild(obj, "Field")
	if fn == nil {
		return false
	}
	ref := findFirstChild(fn, "FieldReference")
	if ref == nil {
		return false
	}
	control := fmFieldControls[kind]
	out["type"] = "field"
	column, occ, ok := l.column(ref)
	binding := map[string]interface{}{"control_style": control[0]}
	if !ok {
		// A field deleted in FileMaker, or one this import does not have:
		// shown as missing, never bound to a column of the same name.
		l.missingField++
		column = sanitizeIdentifier(referencedFieldName(ref))
		binding["missing"] = true
	}
	binding["field_name"] = column
	if occ.name != "" {
		binding["table_occurrence"] = occ.name
	}
	if ok && occ.id != l.occID {
		if rel := l.relationshipBetween(l.occID, occ.id); rel != "" {
			binding["relationship_id"] = rel
		} else {
			// FileMaker follows several relationships to get there;
			// File4Base reads related fields through one.
			l.unreachable++
			binding["unresolved_relationship"] = true
		}
	}
	if display := findFirstChild(fn, "Display"); display != nil {
		if n, err := strconv.Atoi(firstAttr(display, "show")); err == nil && n != 0 {
			if n < 0 {
				n = -n
				binding["repetition_orientation"] = "horizontal"
			}
			binding["repetitions_shown"] = n
		}
		if vl := findFirstChild(display, "ValueListReference"); vl != nil && control[0] != "edit_box" && control[0] != "drop_down_calendar" {
			if id := l.ctx.valueLists[strings.ToLower(firstAttr(vl, "name"))]; id != "" {
				binding["value_list_id"] = id
			}
		}
		if ph := findFirstChild(display, "Placeholder"); ph != nil {
			if text, ok := calcLiteral(calculationText(ph)); ok && text != "" {
				binding["placeholder"] = text
			}
		}
	}
	// A box showing Amount[3] alone, or from repetition 3, starts there.
	if n, err := strconv.Atoi(firstAttr(ref, "repetition")); err == nil && n > 1 {
		binding["first_repetition"] = n
	}
	// Field entry, per mode: bit 0 is FileMaker's "Browse mode" left
	// unchecked, bit 2 its "Find mode" (as an authentic export was seen to
	// write them). Bit 1 is taken as no entry in Browse mode too, so a field
	// FileMaker may only select is never typed into here.
	if usage := findFirstChild(fn, "Usage"); usage != nil {
		if t, err := strconv.Atoi(firstAttr(usage, "type")); err == nil {
			if t&0x3 != 0 {
				binding["allow_browse_entry"] = false
			}
			if t&0x4 != 0 {
				binding["allow_find_entry"] = false
			}
		}
	}
	out["field_binding"] = binding
	style := l.style(obj, []string{control[1]}, true)
	l.displayFormat(obj, style)
	out["style"] = style
	return true
}

// displayFormat reads a field's number, date and time formats. FileMaker
// names none of them in the export, so what is read is the option bits and
// values an authentic export was seen to use; a format not recognised is
// shown as entered. For a number: 0x4 fixes the decimal digits, 0x8 groups
// thousands, 0x200 shows the symbol. A date written with "/" is shown in the
// file's day order; a time with ":" in 24 hours.
func (l *nativeLayout) displayFormat(obj *xmlNode, style map[string]interface{}) {
	ext := findFirstChild(obj, "ExtendedAttributes")
	if ext == nil {
		return
	}
	f := findFirstChild(ext, "Formatting")
	if f == nil {
		return
	}
	childText := func(n *xmlNode, name string) string {
		if c := findFirstChild(n, name); c != nil {
			return strings.TrimSpace(c.Text)
		}
		return ""
	}
	if n := findFirstChild(f, "Numeric"); n != nil {
		opts, _ := strconv.Atoi(childText(n, "Options"))
		digits, digitsErr := strconv.Atoi(childText(n, "DecimalDigits"))
		symbol := childText(n, "Symbol")
		fixed := opts&0x4 != 0 && digitsErr == nil
		switch {
		case symbol == "%":
			style["number_format"] = "percent"
		case opts&0x200 != 0 && symbol != "" && symbol != "#":
			// The symbol is shown: a currency, or a unit written the same way.
			style["number_format"] = "currency"
			style["currency_symbol"] = symbol
		case fixed:
			style["number_format"] = "decimal"
		}
		if _, set := style["number_format"]; set {
			if digitsErr == nil {
				style["decimal_places"] = digits
			}
			style["use_thousands_separator"] = opts&0x8 != 0
		}
	}
	if d := findFirstChild(f, "Date"); d != nil && childText(d, "Separator") == "/" {
		switch l.ctx.dateOrder {
		case "dmy":
			style["date_format"] = "dd/mm/yyyy"
		case "mdy":
			style["date_format"] = "mm/dd/yyyy"
		}
	}
	if t := findFirstChild(f, "Time"); t != nil && childText(t, "Separator") == ":" {
		if s := findFirstChild(t, "Style"); s != nil && childText(s, "MinuteSecond") != "" {
			style["time_format"] = "24h"
		}
	}
}

func (l *nativeLayout) shape(obj *xmlNode, kind string, out map[string]interface{}) bool {
	shape := fmShapes[kind]
	out["type"] = shape[0]
	style := l.style(obj, []string{shape[1]}, false)
	out["style"] = style
	if shape[0] != "line" {
		return true
	}
	// A line is drawn along the longer side of its box, in its middle: the
	// box is given the line's thickness, centred on where FileMaker draws it.
	w, _ := style["border_width"].(float64)
	if w <= 0 {
		w = 1
	}
	if out["width"].(float64) >= out["height"].(float64) {
		out["y"] = out["y"].(float64) + out["height"].(float64)/2 - w/2
		out["height"] = w
	} else {
		out["x"] = out["x"].(float64) + out["width"].(float64)/2 - w/2
		out["width"] = w
	}
	return true
}

// fmImageStreams are the picture formats of a library entry, best first,
// with the MIME type a client decodes them with.
var fmImageStreams = []struct{ stream, mime string }{
	{"PNGf", "image/png"},
	{"JPEG", "image/jpeg"},
	{"GIFf", "image/gif"},
	{"BMPf", "image/bmp"},
	{"SVG ", "image/svg+xml"},
}

func (l *nativeLayout) graphic(obj *xmlNode, out map[string]interface{}) bool {
	out["type"] = "media"
	out["style"] = l.style(obj, nil, false)
	g := findFirstChild(obj, "Graphic")
	ref := (*xmlNode)(nil)
	if g != nil {
		if bd := findFirstChild(g, "BinaryData"); bd != nil {
			ref = findFirstChild(bd, "LibraryReference")
		}
	}
	fit := "contain"
	if g != nil {
		if fm := findFirstChild(g, "Formatting"); fm != nil {
			if o := findFirstChild(fm, "Options"); o != nil {
				if opts, err := strconv.Atoi(strings.TrimSpace(o.Text)); err == nil && opts&0x4 == 0 && opts&0x3 == 0x3 {
					fit = "fill"
				}
			}
		}
	}
	media := map[string]interface{}{"kind": "image", "name": "Picture", "fit": fit}
	if ref != nil {
		if img := l.ctx.image(firstAttr(ref, "id")); img != nil {
			for k, v := range img {
				media[k] = v
			}
		}
	}
	if media["data"] == nil {
		l.noImage++
	} else if media["mime_type"] == "image/svg+xml" {
		l.vectorImage++
	}
	out["media"] = media
	return true
}

// image is the picture of a library entry, read once per file.
func (c *layoutObjectContext) image(id string) map[string]interface{} {
	if m, ok := c.media[id]; ok {
		return m
	}
	var out map[string]interface{}
	if bd := c.library[id]; bd != nil {
		streams := map[string]*xmlNode{}
		if list := findFirstChild(bd, "StreamList"); list != nil {
			for _, s := range list.Children {
				if s.Name == "Stream" {
					streams[firstAttr(s, "name")] = s
				}
			}
		}
		for _, candidate := range fmImageStreams {
			s := streams[candidate.stream]
			if s == nil {
				// The stream's name may have lost its trailing space.
				s = streams[strings.TrimSpace(candidate.stream)]
			}
			if s == nil || !strings.EqualFold(firstAttr(s, "type"), "Base64") {
				continue
			}
			data := strings.Join(strings.Fields(s.Text), "")
			if data == "" || len(data) > maxLayoutImageBase64 {
				continue
			}
			out = map[string]interface{}{"mime_type": candidate.mime, "data": data}
			break
		}
	}
	c.media[id] = out
	return out
}

// calcLiteral answers the text of a calculation that is a single quoted
// text constant, which is how a placeholder or tooltip is usually written.
func calcLiteral(formula string) (string, bool) {
	f := strings.TrimSpace(formula)
	if len(f) < 2 || f[0] != '"' || f[len(f)-1] != '"' {
		return "", false
	}
	var b strings.Builder
	inner := f[1 : len(f)-1]
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case c == '\\' && i+1 < len(inner):
			i++
			b.WriteByte(inner[i])
		case c == '"':
			return "", false // two texts joined by an operator
		default:
			b.WriteByte(c)
		}
	}
	return strings.ReplaceAll(b.String(), "¶", "\n"), true
}

// report says, once for the layout, which objects were left out and what
// was approximated.
func (l *nativeLayout) report() []ReportItem {
	var items []ReportItem
	if len(l.pending) > 0 {
		kinds := make([]string, 0, len(l.pending))
		for k := range l.pending {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		parts := make([]string, 0, len(kinds))
		for _, k := range kinds {
			parts = append(parts, plural(l.pending[k], k))
		}
		detail := "Layout objects not migrated yet: " + strings.Join(parts, ", ")
		if l.frontPanels > 0 {
			detail += "; a tab or slide control shows the objects of its front panel only, without the control itself"
		}
		items = append(items, ReportItem{Category: CategoryUnsupported, Domain: "layout", SourceName: l.name, TargetName: l.name, Detail: detail})
	}
	var review []string
	if l.hideUnbound > 0 {
		review = append(review, plural(l.hideUnbound, "object")+" with a \"Hide object when\" calculation that could not be bound to the imported fields, so always shown")
	}
	if l.unreachable > 0 {
		review = append(review, plural(l.unreachable, "field")+" read through more than one relationship, shown as unavailable")
	}
	if l.missingField > 0 {
		review = append(review, plural(l.missingField, "field object")+" showing a field that is not in the imported tables")
	}
	if l.noImage > 0 {
		review = append(review, plural(l.noImage, "picture")+" whose image is not in the export, shown as a placeholder")
	}
	if l.vectorImage > 0 {
		review = append(review, plural(l.vectorImage, "vector (SVG) picture")+" that may need replacing by a PNG")
	}
	if l.mergeKept > 0 {
		review = append(review, plural(l.mergeKept, "merge field")+" of a related field or a variable, shown as FileMaker wrote it")
	}
	if len(review) > 0 {
		items = append(items, ReportItem{Category: CategoryManualActionRequired, Domain: "layout", SourceName: l.name, TargetName: l.name,
			Detail: "Review the migrated layout: " + strings.Join(review, "; ")})
	}
	return items
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

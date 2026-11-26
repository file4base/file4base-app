package migration

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
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
	library       map[string]*xmlNode    // library reference id -> <BinaryData>
	themes        map[string]*fmTheme    // theme name -> its CSS
	defaultTheme  *fmTheme               // for a layout that names no theme
	dateOrder     string                 // dmy, mdy or iso: how the file writes dates; empty when unknown
	edges         map[string][][2]string // occurrence id -> {occurrence joined, relationship}
	occNames      map[string]string      // occurrence id -> name

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
		occNames:      map[string]string{},
	}
	for _, occ := range bundle.TableOccurrences {
		ctx.occNames[occ.ID] = occ.Name
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

	pending        map[string]int // FileMaker kinds left out, by readable name
	hideUnbound    int
	unreachable    int
	alongPath      int
	missingField   int
	noImage        int
	buttonsUnrun   int             // buttons whose single step File4Base does not run
	buttonsUnbound int             // buttons whose single step names a field not among the imported ones
	buttonIcons    int             // icons dropped from buttons that have a label
	portalsFar     int             // portals of an occurrence several relationships away, listed along the path
	portalFilter   int             // portal filters that could not be bound, the portal kept empty
	portalSort     int             // portal sort fields of another occurrence, left out of the sort
	formatsLeft    int             // conditional formats not kept
	formatsDiffer  int             // "Value is" conditions kept as a calculation that compares with other values
	formatsOff     int             // conditional formats switched off in FileMaker, left out
	popovers       map[string]bool // ids of the popover buttons, whose panels open over the layout
	inPortal       bool            // converting a portal's row objects
	vectorImage    int
	mergeKept      int
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
	// A popover opens over everything else: its objects are drawn last.
	if len(l.popovers) > 0 {
		var under, over []map[string]interface{}
		for _, o := range l.objects {
			if p, _ := o["panel"].(map[string]interface{}); p != nil && l.popovers[fmt.Sprint(p["control"])] {
				over = append(over, o)
			} else {
				under = append(under, o)
			}
		}
		l.objects = append(under, over...)
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
		l.panelControl(obj, kind)
		return
	case "Grouped Button":
		l.groupedButton(obj)
		return
	case "Button Bar":
		l.buttonBar(obj)
		return
	case "Portal":
		l.portal(obj)
		return
	case "Popover Button":
		l.popover(obj)
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
	case kind == "Button":
		ok = l.button(obj, findFirstChild(obj, "Button"), base)
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
		l.conditionalFormats(obj, base)
		l.textHideSelf(obj, base)
		l.objects = append(l.objects, base)
		l.ctx.count(kind).Migrated++
	}
}

// fmConditionOperands is, by the type of a <Condition>, how many values of
// its <Range> the condition compares with: 0 is FileMaker's "Formula is",
// 1 to 20 its "Value is" conditions, in the order of its menu: between, not
// between, equal to, not equal to, greater than, less than, greater than or
// equal to, less than or equal to, containing, not containing, beginning
// with, ending with, empty, before today, today, after today, more than x
// days passed, more than x days ahead, less than x days passed and less than
// x days ahead.
var fmConditionOperands = map[string]int{
	"0": 0, "1": 2, "2": 2, "3": 1, "4": 1, "5": 1, "6": 1, "7": 1, "8": 1, "9": 1, "10": 1,
	"11": 1, "12": 1, "13": 0, "14": 0, "15": 0, "16": 0, "17": 1, "18": 1, "19": 1, "20": 1,
}

// conditionalFormats keeps an object's conditional formatting, bound from the
// layout's occurrence, with the text colour, fill, bold, italic and
// underline it gives. FileMaker writes every condition as the calculation it
// evaluates, a "Value is" condition too (greater than 0 as Self>0, a date
// as Date(3;15;1990)): each is kept as that calculation, Self included,
// which Browse mode answers as the object's value (a field's, so it
// compares as the field's type does, or the text a text or a button
// shows). Formats that cannot be bound, and formats of other objects, are
// reported.
func (l *nativeLayout) conditionalFormats(obj *xmlNode, out map[string]interface{}) {
	f := childPath(obj, "Conditions", "Formatting")
	if f == nil {
		return
	}
	style, _ := out["style"].(map[string]interface{})
	var rules []interface{}
	for _, c := range f.Children {
		if c.Name != "Condition" {
			continue
		}
		// Bit 0 of <Options> is the condition's check box in FileMaker's
		// dialog: a condition switched off is never applied.
		if opt := findFirstChild(c, "Options"); opt != nil {
			if o, err := strconv.Atoi(strings.TrimSpace(opt.Text)); err == nil && o&1 == 0 {
				l.formatsOff++
				continue
			}
		}
		operands, known := fmConditionOperands[firstAttr(c, "type")]
		formula := calculationText(c)
		if !formattedKinds[out["type"]] || style == nil || !known || formula == "" {
			l.formatsLeft++
			continue
		}
		bound, ok := l.bindCondition(formula)
		if !ok || !l.selfReadable(obj, out, formula) {
			l.formatsLeft++
			continue
		}
		if !rangeInFormula(findFirstChild(c, "Range"), operands, formula) {
			l.formatsDiffer++
		}
		css := resolveStyle(nil, nil, findFirstChild(c, "LocalCSS"))
		rule := map[string]interface{}{"operator": "formula", "value": bound}
		if color, ok := cssColor(css.text("color")); ok {
			rule["text_color"] = color
		}
		if fill, ok := cssColor(css["self"]["background-color"]); ok {
			rule["fill_color"] = fill
		}
		weight := strings.ToLower(css.text("font-weight"))
		if n, err := strconv.Atoi(weight); (err == nil && n >= 600) || weight == "bold" || weight == "bolder" {
			rule["bold"] = true
		}
		if strings.EqualFold(css.text("font-style"), "italic") {
			rule["italic"] = true
		}
		if strings.Contains(strings.ToLower(css.text("text-decoration")), "underline") {
			rule["underline"] = true
		}
		rules = append(rules, rule)
	}
	if len(rules) > 0 {
		style["conditional_rules"] = rules
	}
}

// formattedKinds are the objects whose conditional formatting is kept:
// fields, texts and buttons (a popover's and a button bar's segments too),
// the objects FileMaker's dialog formats. A grouped button's formats, which
// FileMaker applies to the objects it groups, are not kept.
var formattedKinds = map[interface{}]bool{"field": true, "label": true, "button": true, "popover_button": true}

// selfReadable reports whether the Self of a condition of the object obj,
// converted as out, can be answered. In a text's and a button's conditions
// Self is the text they show, which Browse mode sends: one that merges
// fields shows another text on each record, and one FileMaker computes
// (a button's calculated label) is not known, so a condition reading those
// is not kept. In a field's, Self is the value the object shows (in each
// repetition and each portal row it shows), and the condition is kept when
// that field is one the import has: it is bound once more with Self written
// as the object's field, Occurrence::Field as FileMaker names them, to tell.
func (l *nativeLayout) selfReadable(obj *xmlNode, out map[string]interface{}, formula string) bool {
	opts, _ := l.ctx.binding.options(l.table)
	if out["type"] != "field" {
		if textSelfKnown(obj, out) {
			return true
		}
		replaced, err := calc.ReplaceSelf(formula, opts, "")
		return err == nil && replaced == formula
	}
	ref := childPath(obj, "Field", "FieldReference")
	name, occName := "", l.occName
	if ref != nil {
		name = referencedFieldName(ref)
		if to := findFirstChild(ref, "TableOccurrenceReference"); to != nil && firstAttr(to, "name") != "" {
			occName = firstAttr(to, "name")
		}
	}
	if name == "" || strings.ContainsAny(name+occName, "{}") {
		// Self names nothing a formula can write: the formula stands only
		// if it does not read it.
		replaced, err := calc.ReplaceSelf(formula, opts, "")
		return err == nil && replaced == formula
	}
	replaced, err := calc.ReplaceSelf(formula, opts, calc.QuoteQualifiedName(occName, name))
	if err != nil || replaced == formula {
		return true
	}
	_, ok := l.bindCondition(replaced)
	return ok
}

// textSelfKnown reports whether Browse mode knows the Self of the text or
// button obj, converted as out: the text it shows, unless it merges a
// field or FileMaker computes it (see selfReadable).
func textSelfKnown(obj *xmlNode, out map[string]interface{}) bool {
	text, _ := out["text"].(string)
	return !strings.Contains(text, "{{") && !fmMergeField.MatchString(text) && !calculatedLabel(obj)
}

// textHideSelf drops the hide condition of a text or a button that reads a
// Self Browse mode does not know (see textSelfKnown), which it would
// answer with another text: it is reported as a hide condition that could
// not be bound, as FileMaker's would show the object otherwise.
func (l *nativeLayout) textHideSelf(obj *xmlNode, out map[string]interface{}) {
	hide, _ := out["hide_when"].(string)
	if hide == "" || !formattedKinds[out["type"]] || out["type"] == "field" || textSelfKnown(obj, out) {
		return
	}
	// The bound formula reads the imported names.
	if replaced, err := calc.ReplaceSelf(hide, calc.Options{Library: l.ctx.binding.stored}, ""); err == nil && replaced == hide {
		return
	}
	delete(out, "hide_when")
	delete(out, "hide_in_find")
	l.hideUnbound++
}

// calculatedLabel reports whether the button obj (a popover button, a
// segment of a button bar) shows a label FileMaker computes, which the
// migration keeps only when it is a constant.
func calculatedLabel(obj *xmlNode) bool {
	for _, holder := range []string{"Button", "PopoverButton"} {
		lb := childPath(obj, holder, "Label")
		if lb == nil || findFirstChild(lb, "Text") != nil || findFirstChild(lb, "Calculation") == nil {
			continue
		}
		if _, ok := calcLiteral(calculationText(lb)); !ok {
			return true
		}
	}
	return false
}

// bindCondition binds a conditional format's or a hide condition's
// calculation from the layout's occurrence, as FileMaker reads it. Browse
// mode evaluates a portal row's from the portal's occurrence, against the
// row's record, as it does the portal filter: the layout's own fields are
// then written qualified, for them to read the layout's record still.
func (l *nativeLayout) bindCondition(formula string) (string, bool) {
	bound, ok := l.bindFormula(formula)
	if !ok || !l.inPortal {
		return bound, ok
	}
	// The bound formula reads the imported names.
	opts := calc.Options{Library: l.ctx.binding.stored}
	qualified, unresolved, err := calc.Rewrite(bound, opts, func(occurrence, field string) (string, bool) {
		if occurrence == "" {
			occurrence = l.occName
		}
		return calc.QuoteQualifiedName(occurrence, field), true
	})
	if err != nil || len(unresolved) > 0 {
		return "", false
	}
	return qualified, true
}

// rangeInFormula reports whether the calculation FileMaker keeps for a
// "Value is" condition compares with the values its <Range> shows (the
// first operands of Start and End): a number is one of the formula's
// numbers, sign included; anything else is one of its quoted texts, or its
// digits are among the formula's, as 15/03/1990 is written Date(3;15;1990).
// An export was seen to keep, for a condition showing greater than one
// number, a calculation comparing with another.
func rangeInFormula(r *xmlNode, operands int, formula string) bool {
	if operands == 0 || r == nil {
		return true
	}
	texts, numbers, digits := formulaLiterals(formula)
	for _, tag := range []string{"Start", "End"}[:operands] {
		n := findFirstChild(r, tag)
		if n == nil || strings.TrimSpace(n.Text) == "" {
			continue
		}
		// FileMaker writes the quotes of a value typed in quotes, which
		// it compares as that text, as &quot;.
		v := html.UnescapeString(strings.TrimSpace(n.Text))
		if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
			v = v[1 : len(v)-1]
		}
		if slices.ContainsFunc(texts, func(t string) bool { return strings.EqualFold(t, v) }) {
			continue
		}
		if fmNumber.MatchString(v) {
			if !slices.Contains(numbers, fmNumberValue(v)) {
				return false
			}
			continue
		}
		runs := fmDigitRuns.FindAllString(v, -1)
		if len(runs) == 0 {
			return false
		}
		for _, d := range runs {
			if !digits[strings.TrimLeft(d, "0")] {
				return false
			}
		}
	}
	return true
}

var (
	fmDigitRuns     = regexp.MustCompile(`[0-9]+`)
	fmNumber        = regexp.MustCompile(`^-?[0-9]+(?:[.,][0-9]+)?$`)
	fmNumberLiteral = regexp.MustCompile(`-?[0-9]+(?:[.,][0-9]+)?`)
)

// fmNumberValue is the value of a number written with "." or ",".
func fmNumberValue(s string) float64 {
	f, _ := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	return f
}

// formulaLiterals are the quoted texts of a formula, unescaped, the numbers
// written outside them, and their runs of digits without leading zeros.
func formulaLiterals(formula string) (texts []string, numbers []float64, digits map[string]bool) {
	digits = map[string]bool{}
	var outside, text strings.Builder
	quoted := false
	runes := []rune(formula)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case !quoted && r == '"':
			quoted = true
			text.Reset()
		case quoted && r == '\\' && i+1 < len(runes):
			i++
			text.WriteRune(runes[i])
		case quoted && r == '"':
			quoted = false
			texts = append(texts, text.String())
			outside.WriteRune(' ')
		case quoted:
			text.WriteRune(r)
		default:
			outside.WriteRune(r)
		}
	}
	for _, n := range fmNumberLiteral.FindAllString(outside.String(), -1) {
		numbers = append(numbers, fmNumberValue(n))
	}
	for _, d := range fmDigitRuns.FindAllString(outside.String(), -1) {
		digits[strings.TrimLeft(d, "0")] = true
	}
	return texts, numbers, digits
}

// panelControl converts a tab or slide control: the control itself, drawn
// beneath its panels, with its tab labels and the panel in front when the
// layout opens, then the objects of every panel, each saying which panel of
// which control it sits on, so only the panel in front is shown.
func (l *nativeLayout) panelControl(obj *xmlNode, kind string) {
	holder := findFirstChild(obj, "TabControl", "SlideControl")
	base := l.baseObject(obj)
	if holder == nil || base == nil {
		return
	}
	var panels []*xmlNode
	if list := findFirstChild(holder, "ObjectList"); list != nil {
		for _, p := range list.Children {
			if p.Name == "LayoutObject" && firstAttr(p, "type") == "Panel" {
				panels = append(panels, p)
			}
		}
	}
	if len(panels) == 0 {
		return
	}
	front, _ := strconv.Atoi(firstAttr(holder, "default"))
	if front < 0 || front >= len(panels) {
		front = 0
	}
	id := l.objectID(obj)
	base["id"] = id
	class := "tab_control"
	if kind == "Slide Control" {
		base["type"] = "slide_control"
		class = "slide_control"
	} else {
		base["type"] = "tab_control"
		if h, err := strconv.ParseFloat(firstAttr(holder, "height"), 64); err == nil && h > 0 {
			base["tab_height"] = h
		}
	}
	base["style"] = l.style(obj, []string{class}, true)
	base["default_panel"] = front
	tabs := make([]interface{}, len(panels))
	l.objects = append(l.objects, base)
	l.ctx.count(kind).Migrated++
	for i, p := range panels {
		inner := findFirstChild(p, "TabPanel", "SlidePanel")
		label := ""
		if inner != nil {
			if c := findFirstChild(inner, "Calculation"); c != nil {
				label, _ = calcLiteral(calculationText(c))
			}
		}
		if label == "" && kind == "Tab Control" {
			label = fmt.Sprintf("Tab %d", i+1)
		}
		tabs[i] = label
		start := len(l.objects)
		if inner != nil {
			if objects := findFirstChild(inner, "ObjectList"); objects != nil {
				l.convertList(objects)
			}
		}
		for _, o := range l.objects[start:] {
			if _, nested := o["panel"]; !nested {
				o["panel"] = map[string]interface{}{"control": id, "index": i}
			}
		}
		l.ctx.count("Panel").Migrated++
	}
	base["tabs"] = tabs
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
				if bound, ok := l.bindCondition(formula); ok {
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

// occurrencePath is the chain of occurrences FileMaker follows from the
// occurrence from to the occurrence to, to included, and the relationship
// of each step. FileMaker's graph has no cycle, so there is one at most:
// one step to an occurrence joined to the layout's, more in an anchor–buoy
// graph. Nil when nothing joins them.
func (c *layoutObjectContext) occurrencePath(from, to string) (occurrences, relationships []string) {
	if c.edges == nil {
		c.edges = map[string][][2]string{}
		for _, r := range c.relationships {
			c.edges[r.LeftOccurrenceID] = append(c.edges[r.LeftOccurrenceID], [2]string{r.RightOccurrenceID, r.ID})
			c.edges[r.RightOccurrenceID] = append(c.edges[r.RightOccurrenceID], [2]string{r.LeftOccurrenceID, r.ID})
		}
	}
	prev := map[string][2]string{from: {}}
	queue := []string{from}
	for len(queue) > 0 && from != to {
		at := queue[0]
		queue = queue[1:]
		for _, e := range c.edges[at] {
			if _, seen := prev[e[0]]; seen {
				continue
			}
			prev[e[0]] = [2]string{at, e[1]}
			if e[0] == to {
				for id := to; id != from; id = prev[id][0] {
					occurrences = append([]string{id}, occurrences...)
					relationships = append([]string{prev[id][1]}, relationships...)
				}
				return occurrences, relationships
			}
			queue = append(queue, e[0])
		}
	}
	return nil, nil
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
		occurrences, relationships := l.ctx.occurrencePath(l.occID, occ.id)
		switch {
		case len(relationships) == 1:
			binding["relationship_id"] = relationships[0]
		case len(occurrences) > 1:
			// Several relationships away: the first record at the end of
			// the chain is shown, as FileMaker shows it.
			path := make([]interface{}, len(occurrences))
			for i, id := range occurrences {
				path[i] = l.ctx.occNames[id]
			}
			binding["occurrence_path"] = path
			l.alongPath++
		default:
			// Nothing joins them: FileMaker shows <Unrelated Table>.
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
		items = append(items, ReportItem{Category: CategoryUnsupported, Domain: "layout", SourceName: l.name, TargetName: l.name, Detail: detail})
	}
	var review []string
	if l.hideUnbound > 0 {
		review = append(review, plural(l.hideUnbound, "object")+" with a \"Hide object when\" calculation that could not be bound to the imported fields, so always shown")
	}
	if l.unreachable > 0 {
		review = append(review, plural(l.unreachable, "field")+" of an occurrence no relationship joins to the layout's, shown as unavailable")
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
	if l.buttonsUnrun > 0 {
		review = append(review, plural(l.buttonsUnrun, "button")+" running a script step File4Base does not run, kept switched off")
	}
	if l.buttonsUnbound > 0 {
		review = append(review, plural(l.buttonsUnbound, "button")+" whose script step names a field that is not among the imported fields, kept switched off")
	}
	if l.buttonIcons > 0 {
		review = append(review, plural(l.buttonIcons, "button")+" with both an icon and a label, shown with its label only")
	}
	if l.portalsFar > 0 {
		review = append(review, plural(l.portalsFar, "portal")+" showing the records of an occurrence several relationships away, listed along the path; no record can be created in them")
	}
	if l.portalFilter > 0 {
		review = append(review, plural(l.portalFilter, "portal")+" whose filter calculation could not be bound, showing no records until it is set again")
	}
	if l.portalSort > 0 {
		review = append(review, plural(l.portalSort, "portal sort field")+" of another occurrence, left out of the portal's sort")
	}
	if l.formatsLeft > 0 {
		review = append(review, plural(l.formatsLeft, "conditional format")+" not kept (a condition that could not be bound, or on an object other than a field, a text or a button)")
	}
	if l.formatsOff > 0 {
		review = append(review, plural(l.formatsOff, "conditional format")+" switched off in FileMaker, left out")
	}
	if l.formatsDiffer > 0 {
		review = append(review, plural(l.formatsDiffer, "conditional format")+" whose \"Value is\" condition shows other values than the calculation FileMaker keeps for it, kept as that calculation")
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

// button converts a FileMaker button (holder is its <Button> element):
// its label, its look, and what a click runs. A button showing only an
// icon is drawn as the icon, with a button without a fill over it.
//
// A click runs a script (bound to the imported scripts once they are
// known, see bindLayoutButtons) or a single step, converted as a step of a
// script is. A button whose step File4Base does not run is kept, switched
// off, and reported.
func (l *nativeLayout) button(obj, holder *xmlNode, out map[string]interface{}) bool {
	out["type"] = "button"
	style := l.style(obj, []string{"button"}, true)
	label := ""
	if holder != nil {
		if lb := findFirstChild(holder, "Label"); lb != nil && (findFirstChild(lb, "Text") != nil || findFirstChild(lb, "Calculation") != nil) {
			if t := findFirstChild(lb, "Text"); t != nil {
				if st := findFirstChild(t, "StyledText"); st != nil {
					if d := findFirstChild(st, "Data"); d != nil {
						label = strings.TrimSpace(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(d.Text))
					}
				}
			} else if text, ok := calcLiteral(calculationText(lb)); ok {
				label = text
			}
		}
	}
	out["text"] = l.mergeText(label)
	if icon := l.buttonIcon(obj, holder, out); icon != nil {
		if label == "" {
			// The icon is the button: drawn, with the click over it.
			l.objects = append(l.objects, icon)
			style["fill_color"] = "#00000000"
			style["border_width"] = 0.0
		} else {
			l.buttonIcons++
		}
	}
	out["style"] = style
	if holder != nil {
		if action := l.buttonAction(findFirstChild(holder, "action")); action != nil {
			out["action"] = action
		}
	}
	return true
}

// buttonIcon is the picture a button shows from the file's library, as a
// media object in the button's box, or nil.
func (l *nativeLayout) buttonIcon(obj, holder *xmlNode, box map[string]interface{}) map[string]interface{} {
	if holder == nil {
		return nil
	}
	ref := childPath(holder, "IconData", "BinaryData", "LibraryReference")
	if ref == nil {
		return nil
	}
	img := l.ctx.image(firstAttr(ref, "id"))
	if img == nil {
		return nil
	}
	media := map[string]interface{}{"kind": "image", "name": "Icon", "fit": "contain"}
	for k, v := range img {
		media[k] = v
	}
	out := map[string]interface{}{
		"type": "media", "x": box["x"], "y": box["y"], "width": box["width"], "height": box["height"],
		"media": media, "style": map[string]interface{}{},
	}
	// Stable like the button's own id, and distinct from it.
	if uid := findFirstChild(obj, "UUID"); uid != nil && l.layoutID != "" && strings.TrimSpace(uid.Text) != "" {
		out["id"] = uuid.NewSHA1(uuid.NameSpaceOID, []byte(l.layoutID+"/"+strings.TrimSpace(uid.Text)+"/icon")).String()
	} else {
		out["id"] = uuid.NewString()
	}
	return out
}

// childPath follows nested elements by name, nil when one is missing.
func childPath(n *xmlNode, names ...string) *xmlNode {
	for _, name := range names {
		if n == nil {
			return nil
		}
		n = findFirstChild(n, name)
	}
	return n
}

// buttonAction converts what a click on a button runs: a script, by its
// FileMaker id and name until the scripts are imported, or one step.
func (l *nativeLayout) buttonAction(action *xmlNode) map[string]interface{} {
	if action == nil {
		return nil
	}
	if ref := findFirstChild(action, "ScriptReference"); ref != nil {
		out := map[string]interface{}{"type": "perform_script", "script_name": firstAttr(ref, "name")}
		// Marked as migrated, so bindLayoutButtons binds it, even without an id.
		out[sourceScriptIDKey] = firstAttr(ref, "id")
		if external := findFirstChild(ref, "DataSourceReference", "FileReference"); external != nil {
			// A script of another file: its id is that file's, and must
			// never find one of ours (see readTrigger).
			reference := firstAttr(external, "name", "Name")
			if reference == "" {
				reference = "another file"
			}
			out["file_reference"] = reference
		}
		if calc := findFirstChild(action, "Calculation"); calc != nil {
			param := strings.TrimSpace(calculationText(calc))
			if param == "" {
				return out
			}
			if bound, ok := l.bindFormula(param); ok {
				out["parameter"] = bound
			} else {
				out["parameter"] = param
				out["parameter_problem"] = "the parameter could not be bound to the imported fields"
				out["enabled"] = false
			}
		}
		return out
	}
	step := findFirstChild(action, "Step")
	if step == nil {
		return nil
	}
	resolved := canonicalScriptStep(firstAttr(step, "name"), firstAttr(step, "id"))
	params, _, _ := decodeStepParams(step, resolved.Canonical)
	// As convertScriptNode does: a protected PDF must not be written
	// unprotected.
	if resolved.Canonical == "Save Records as PDF" && pdfSecurityRequested(step) {
		params["pdf_security"] = true
	}
	if id := firstAttr(step, "id"); id != "" {
		params["filemaker_step_id"] = id
	}
	out := map[string]interface{}{"type": "single_step", "step_type": resolved.Canonical, "params": params}
	fieldsBound := l.bindStepFields(params)
	if !fieldsBound {
		out["field_problem"] = "a field the step names is not among the imported fields"
	}
	if !resolved.Supported || !fieldsBound || strings.EqualFold(firstAttr(step, "enable"), "false") {
		out["enabled"] = false
		if !resolved.Supported {
			l.buttonsUnrun++
		} else if !fieldsBound {
			l.buttonsUnbound++
		}
	}
	return out
}

// bindStepFields points every field a button's step names (its target, its
// sort or find fields...) at the column the import gave that field. The
// step decoder only sanitises the source name, and two source fields
// sanitised alike (Unit Price, Unit_Price) are imported as two columns
// (unit_price, unit_price_2): only the table's field map tells which one is
// meant. It answers false when a field of an imported table cannot be
// placed. A field of an occurrence that is not known here is left as the
// decoder wrote it; one of another file's table gets that file's name for it.
func (l *nativeLayout) bindStepFields(v interface{}) bool {
	ok := true
	switch t := v.(type) {
	case map[string]interface{}:
		if label, _ := t["field_label"].(string); label != "" {
			if field, _ := t["field"].(string); field != "" && !strings.HasPrefix(field, "$") && !l.bindStepField(t, label) {
				ok = false
			}
		}
		for _, child := range t {
			if !l.bindStepFields(child) {
				ok = false
			}
		}
	case []interface{}:
		for _, child := range t {
			if !l.bindStepFields(child) {
				ok = false
			}
		}
	case []map[string]interface{}:
		for _, child := range t {
			if !l.bindStepFields(child) {
				ok = false
			}
		}
	}
	return ok
}

// bindStepField binds one entry naming the source field label, read
// through the occurrence the entry names (field_table or table, as the
// source or sanitised) or else the layout's.
func (l *nativeLayout) bindStepField(entry map[string]interface{}, label string) bool {
	occName, _ := entry["field_table"].(string)
	if occName == "" {
		occName, _ = entry["table"].(string)
	}
	if occName == "" {
		occName = l.occName
	}
	o, found := l.ctx.binding.occByName[strings.ToLower(occName)]
	if !found {
		// The sanitised name the DDR decoder writes: taken only when one
		// occurrence has it.
		matches := 0
		for _, candidate := range l.ctx.binding.occByName {
			if sanitizeIdentifier(candidate.name) == occName {
				o, matches = candidate, matches+1
			}
		}
		if matches != 1 {
			return true
		}
	}
	if o.external {
		// Named as the other file's migration names its columns, as a
		// field object of that occurrence is (see column).
		entry["field"] = externalFieldName(label)
		return true
	}
	names := l.ctx.binding.fields[o.table]
	if names == nil {
		return true
	}
	column, ok := names.resolve(label)
	if !ok {
		return false
	}
	entry["field"] = column
	return true
}

// portal converts a portal showing the records of an occurrence one
// relationship from the layout's: the rows it shows, from which, their
// sort and filter; then the objects of its first row, which every row
// repeats with its own record. A portal reaching its occurrence through
// several relationships lists the records along that path
// (occurrence_path), as a field there reads the first of them.
func (l *nativeLayout) portal(obj *xmlNode) {
	p := findFirstChild(obj, "Portal")
	base := l.baseObject(obj)
	if p == nil || base == nil {
		return
	}
	to := findFirstChild(p, "TableOccurrenceReference")
	if to == nil {
		l.pending["portal"]++
		return
	}
	occ, found := l.ctx.binding.occByName[strings.ToLower(firstAttr(to, "name"))]
	var occurrences, relationships []string
	if found && !l.occExternal {
		occurrences, relationships = l.ctx.occurrencePath(l.occID, occ.id)
	}
	if len(relationships) == 0 {
		l.pending["portal"]++
		return
	}
	config := map[string]interface{}{
		"occurrence":      occ.name,
		"show_scroll_bar": true,
	}
	if len(relationships) == 1 {
		config["relationship_id"] = relationships[0]
		// As in FileMaker, the empty last row is offered only where the
		// relationship allows creating records on the portal's side.
		config["allow_creation"] = true
	} else {
		// Several relationships away: every record reached at the end of
		// the chain is listed. No record is created through it.
		path := make([]interface{}, len(occurrences))
		for i, id := range occurrences {
			path[i] = l.ctx.occNames[id]
		}
		config["occurrence_path"] = path
		config["allow_creation"] = false
		l.portalsFar++
	}
	if o := findFirstChild(p, "Options"); o != nil {
		if n, err := strconv.Atoi(firstAttr(o, "index")); err == nil && n > 1 {
			config["initial_row"] = n
		}
		if n, err := strconv.Atoi(firstAttr(o, "show")); err == nil && n > 0 {
			config["row_count"] = n
		}
	}
	if spec := findFirstChild(p, "SortSpecification"); spec != nil {
		var keys []string
		for _, sort := range findNodes(spec, "Sort") {
			ref := childPath(sort, "PrimaryField", "FieldReference")
			if ref == nil {
				continue
			}
			column, at, ok := l.column(ref)
			if !ok || at.id != occ.id {
				l.portalSort++
				continue
			}
			if firstAttr(sort, "type") == "Descending" {
				column = "-" + column
			}
			keys = append(keys, column)
		}
		if len(keys) > 0 {
			config["sort"] = strings.Join(keys, ",")
		}
	}
	if c := findFirstChild(p, "Calculation"); c != nil {
		if formula := calculationText(c); formula != "" {
			// FileMaker evaluates the filter from the portal's occurrence,
			// against each related record.
			fb := l.ctx.binding.bind(occ.table, formula, occ.name, fmt.Sprintf("the portal filter of the layout %q", l.name))
			if fb.why == "" && !occ.external {
				config["filter"] = fb.rewritten
			} else {
				// Rows the filter would leave out are never shown.
				config["filter"] = "0"
				config["filter_problem"] = true
				l.portalFilter++
			}
		}
	}
	base["type"] = "portal"
	base["portal_config"] = config
	base["style"] = l.style(obj, []string{"portal"}, false)
	base["id"] = l.objectID(obj)
	l.objects = append(l.objects, base)
	l.ctx.count("Portal").Migrated++
	if list := findFirstChild(p, "ObjectList"); list != nil {
		outer := l.inPortal
		l.inPortal = true
		l.convertList(list)
		l.inPortal = outer
	}
}

// popover converts a popover button: the button, then its panel's
// background and objects, each saying it is on the button's only panel,
// closed when the layout opens (default_panel -1), so a click on the button
// shows them and another hides them.
func (l *nativeLayout) popover(obj *xmlNode) {
	holder := findFirstChild(obj, "PopoverButton")
	base := l.baseObject(obj)
	if holder == nil || base == nil {
		return
	}
	l.button(obj, holder, base)
	delete(base, "action")
	base["type"] = "popover_button"
	base["default_panel"] = -1
	id := l.objectID(obj)
	base["id"] = id
	l.conditionalFormats(obj, base)
	l.textHideSelf(obj, base)
	l.objects = append(l.objects, base)
	l.ctx.count("Popover Button").Migrated++
	if l.popovers == nil {
		l.popovers = map[string]bool{}
	}
	l.popovers[id] = true
	var panel *xmlNode
	for _, c := range holder.Children {
		if c.Name == "LayoutObject" && firstAttr(c, "type") == "PopoverPanel" {
			panel = c
		}
	}
	if panel == nil {
		return
	}
	start := len(l.objects)
	if back := l.baseObject(panel); back != nil {
		back["type"] = "rect"
		style := l.style(panel, []string{"popover"}, false)
		if _, filled := style["fill_color"]; !filled {
			style["fill_color"] = "#FFFFFF"
		}
		if w, _ := style["border_width"].(float64); w <= 0 {
			style["border_width"] = 1.0
			style["border_color"] = "#BDBDBD"
		}
		back["style"] = style
		back["id"] = l.objectID(panel)
		l.objects = append(l.objects, back)
		l.ctx.count("PopoverPanel").Migrated++
	}
	if list := findFirstChild(panel, "ObjectList"); list != nil {
		l.convertList(list)
	}
	for _, o := range l.objects[start:] {
		if _, nested := o["panel"]; !nested {
			o["panel"] = map[string]interface{}{"control": id, "index": 0}
		}
	}
}

// groupedButton keeps the objects a grouped button draws, and puts over
// them a button without a fill or a label that runs its action.
func (l *nativeLayout) groupedButton(obj *xmlNode) {
	g := findFirstChild(obj, "GroupedButton")
	if g == nil {
		return
	}
	if list := findFirstChild(g, "ObjectList"); list != nil {
		l.convertList(list)
	}
	base := l.baseObject(obj)
	if base == nil {
		return
	}
	base["type"] = "button"
	base["text"] = ""
	base["style"] = map[string]interface{}{"fill_color": "#00000000", "border_width": 0.0}
	if action := l.buttonAction(findFirstChild(g, "action")); action != nil {
		base["action"] = action
	}
	base["id"] = l.objectID(obj)
	// Its formats are reported, not kept (see formattedKinds).
	l.conditionalFormats(obj, map[string]interface{}{"type": "grouped_button"})
	l.objects = append(l.objects, base)
	l.ctx.count("Grouped Button").Migrated++
}

// buttonBar keeps each segment of a button bar as a button at its place.
func (l *nativeLayout) buttonBar(obj *xmlNode) {
	list := childPath(obj, "ButtonBar", "ObjectList")
	if list == nil {
		return
	}
	for _, seg := range list.Children {
		if seg.Name != "LayoutObject" {
			continue
		}
		if firstAttr(seg, "type") == "Popover Button" {
			l.popover(seg) // a segment opening a popover
			continue
		}
		base := l.baseObject(seg)
		if base == nil {
			continue
		}
		if l.button(seg, findFirstChild(seg, "Button"), base) {
			base["id"] = l.objectID(seg)
			l.conditionalFormats(seg, base)
			l.textHideSelf(seg, base)
			l.objects = append(l.objects, base)
			l.ctx.count("Button").Migrated++
		}
	}
	l.ctx.count("Button Bar").Migrated++
}

// bindLayoutButtons binds the script each migrated button runs to the
// imported scripts, as a trigger's is: by FileMaker id, then by a name only
// one script has. A button whose script is not found, or whose name several
// scripts share, is kept switched off, and the solution's report says how
// many there are.
func bindLayoutButtons(bundle *schema.SolutionBundle, scripts scriptIndex, solution string) []ReportItem {
	missing, ambiguous, external := 0, 0, 0
	for li := range bundle.Layouts {
		forEachLayoutObject(bundle.Layouts[li].Definition, func(obj map[string]interface{}) {
			action, _ := obj["action"].(map[string]interface{})
			if action == nil || action["type"] != "perform_script" {
				return
			}
			_, fromSource := action[sourceScriptIDKey]
			if !fromSource {
				return // not a migrated button
			}
			id, matches := scripts.resolve(action)
			delete(action, sourceScriptIDKey)
			switch {
			case id != "":
				action["script_id"] = id
				return
			case action["file_reference"] != nil:
				external++
			case matches > 1:
				ambiguous++
			default:
				missing++
			}
			action["enabled"] = false
		})
	}
	var parts []string
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("%d run a script that is not among the imported scripts", missing))
	}
	if ambiguous > 0 {
		parts = append(parts, fmt.Sprintf("%d run a script whose name several imported scripts share", ambiguous))
	}
	if external > 0 {
		parts = append(parts, fmt.Sprintf("%d run a script of another FileMaker file", external))
	}
	if len(parts) == 0 {
		return nil
	}
	return []ReportItem{{Category: CategoryManualActionRequired, Domain: "layout", SourceName: solution,
		Detail: "Migrated buttons kept switched off: " + strings.Join(parts, "; ") + ". Choose their script in Layout mode"}}
}

// forEachLayoutObject calls fn with every object of a layout definition,
// as converted or as read back from JSON.
func forEachLayoutObject(def map[string]interface{}, fn func(map[string]interface{})) {
	switch objects := def["objects"].(type) {
	case []map[string]interface{}:
		for _, o := range objects {
			fn(o)
		}
	case []interface{}:
		for _, raw := range objects {
			if o, ok := raw.(map[string]interface{}); ok {
				fn(o)
			}
		}
	}
}

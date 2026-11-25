package migration

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Reading the look of a FileMaker layout object.
//
// A <FMSaveAsXML> export describes how an object looks with CSS: the theme
// of its layout (<ThemeCatalog>/<Theme>/<CSS>) styles every object of a kind
// ("edit_box:normal .self"), a named style of the theme refines it
// ("text_box.header_text:normal .self"), and the object's own <LocalCSS>
// overrides both ("self:normal .self"). The cascade is resolved here, for the
// normal state only, and what File4Base's object style can hold is kept:
// fill, line, corner radius, font, text colour, alignment and indents.
// Everything else (gradients, shadows, hover and pressed states, per-side
// borders) is approximated or left out without a report item each: the
// report says once per layout what is approximated.

// cssRule is one block of FileMaker CSS: "class[.style]:state .part { … }".
type cssRule struct {
	class, style, state, part string
	decls                     map[string]string
}

var (
	cssComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssSelector = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_\-]*)(?:\.([A-Za-z0-9_\-]+))?:([A-Za-z_]+)\s+\.([A-Za-z_]+)$`)
)

// parseFMCSS reads the blocks of a theme or of an object's local style.
// What it does not understand is skipped: a style is decoration, never a
// reason to refuse a layout.
func parseFMCSS(text string) []cssRule {
	text = cssComment.ReplaceAllString(text, "")
	var rules []cssRule
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			break
		}
		closing := strings.IndexByte(text[open:], '}')
		if closing < 0 {
			break
		}
		selector := strings.TrimSpace(text[:open])
		body := text[open+1 : open+closing]
		text = text[open+closing+1:]
		m := cssSelector.FindStringSubmatch(selector)
		if m == nil {
			continue
		}
		decls := map[string]string{}
		for _, d := range strings.Split(body, ";") {
			name, value, ok := strings.Cut(d, ":")
			if !ok {
				continue
			}
			decls[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
		}
		rules = append(rules, cssRule{class: m[1], style: m[2], state: m[3], part: m[4], decls: decls})
	}
	return rules
}

// fmTheme is a layout theme's CSS, by class.
type fmTheme struct {
	byClass map[string][]cssRule
}

func newFMTheme(css string) *fmTheme {
	t := &fmTheme{byClass: map[string][]cssRule{}}
	for _, r := range parseFMCSS(css) {
		if r.state != "normal" {
			continue
		}
		t.byClass[r.class] = append(t.byClass[r.class], r)
	}
	return t
}

// cssStyle is the resolved declarations of an object, by part of the object
// (self, text, inner_border…).
type cssStyle map[string]map[string]string

func (s cssStyle) apply(r cssRule) {
	if s[r.part] == nil {
		s[r.part] = map[string]string{}
	}
	for k, v := range r.decls {
		s[r.part][k] = v
	}
}

// get answers a declaration of the object's text: the text part wins over
// the box, as it does in FileMaker.
func (s cssStyle) text(name string) string {
	if v := s["text"][name]; v != "" {
		return v
	}
	return s["self"][name]
}

// resolveStyle cascades the theme's style of class, its named style and the
// object's own local CSS.
func resolveStyle(theme *fmTheme, classes []string, localCSS *xmlNode) cssStyle {
	out := cssStyle{}
	styleName := ""
	var local []cssRule
	if localCSS != nil {
		styleName = firstAttr(localCSS, "name")
		local = parseFMCSS(localCSS.Text)
	}
	if theme != nil {
		for _, class := range classes {
			for _, r := range theme.byClass[class] {
				if r.style == "" {
					out.apply(r)
				}
			}
			if styleName != "" {
				for _, r := range theme.byClass[class] {
					if r.style == styleName {
						out.apply(r)
					}
				}
			}
		}
	}
	for _, r := range local {
		if r.state == "normal" && r.class == "self" {
			out.apply(r)
		}
	}
	return out
}

var cssRGBA = regexp.MustCompile(`^rgba?\(\s*([0-9.]+)(%?)\s*,\s*([0-9.]+)(%?)\s*,\s*([0-9.]+)(%?)\s*(?:,\s*([0-9.]+)\s*)?\)$`)

// cssColor converts a FileMaker CSS colour, rgba(…%,…%,…%,a), to #RRGGBB, or
// #AARRGGBB when it is not opaque. ok is false for anything else.
func cssColor(value string) (string, bool) {
	v := strings.TrimSpace(strings.ToLower(value))
	if v == "transparent" {
		return "#00000000", true
	}
	if strings.HasPrefix(v, "#") && (len(v) == 7 || len(v) == 9) {
		return strings.ToUpper(v), true
	}
	m := cssRGBA.FindStringSubmatch(v)
	if m == nil {
		return "", false
	}
	channel := func(num, pct string) int {
		f, _ := strconv.ParseFloat(num, 64)
		if pct == "%" {
			f = f * 255 / 100
		}
		return int(math.Round(math.Max(0, math.Min(255, f))))
	}
	r, g, b := channel(m[1], m[2]), channel(m[3], m[4]), channel(m[5], m[6])
	alpha := 1.0
	if m[7] != "" {
		alpha, _ = strconv.ParseFloat(m[7], 64)
	}
	if alpha >= 0.999 {
		return fmt.Sprintf("#%02X%02X%02X", r, g, b), true
	}
	a := int(math.Round(math.Max(0, math.Min(1, alpha)) * 255))
	return fmt.Sprintf("#%02X%02X%02X%02X", a, r, g, b), true
}

// cssLength reads "10pt" (or 10px, or a bare number) as layout points.
func cssLength(value string) (float64, bool) {
	v := strings.TrimSpace(strings.ToLower(value))
	for _, unit := range []string{"pt", "px"} {
		v = strings.TrimSuffix(v, unit)
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

var cssFMFont = regexp.MustCompile(`^-fm-font-family\(([^)]*)\)$`)

// cssFontFamily is the first family a font-family declaration names.
func cssFontFamily(value string) string {
	v := strings.TrimSpace(value)
	if m := cssFMFont.FindStringSubmatch(v); m != nil {
		v = m[1]
	}
	first, _, _ := strings.Cut(v, ",")
	return strings.Trim(strings.TrimSpace(first), `"'`)
}

// objectStyle converts the resolved CSS of an object to a File4Base object
// style. withText says whether the object shows text, so a shape does not
// carry a font it never draws.
func objectStyle(css cssStyle, withText bool) map[string]interface{} {
	style := map[string]interface{}{}
	self := css["self"]
	if c, ok := cssColor(self["background-color"]); ok {
		style["fill_color"] = c
	}

	// One line for the whole box: File4Base draws the same line on every
	// side, so the widest visible side gives it.
	width, color := 0.0, ""
	for _, side := range []string{"top", "right", "bottom", "left"} {
		if strings.EqualFold(self["border-"+side+"-style"], "none") {
			continue
		}
		w, ok := cssLength(self["border-"+side+"-width"])
		if !ok || w <= 0 {
			continue
		}
		c, ok := cssColor(self["border-"+side+"-color"])
		if !ok || c == "#00000000" {
			continue
		}
		if w > width {
			width, color = w, c
		}
	}
	style["border_width"] = width
	if color != "" {
		style["border_color"] = color
	}
	radius := 0.0
	for _, corner := range []string{"top-left", "top-right", "bottom-right", "bottom-left"} {
		if r, ok := cssLength(firstWord(self["border-"+corner+"-radius"])); ok && r > radius {
			radius = r
		}
	}
	style["corner_radius"] = radius

	if !withText {
		return style
	}
	if f := cssFontFamily(css.text("font-family")); f != "" {
		style["font_family"] = f
	}
	if size, ok := cssLength(css.text("font-size")); ok && size > 0 {
		style["font_size"] = size
	}
	weight := strings.ToLower(css.text("font-weight"))
	if n, err := strconv.Atoi(weight); (err == nil && n >= 600) || weight == "bold" || weight == "bolder" {
		style["font_weight"] = "bold"
	} else {
		style["font_weight"] = "normal"
	}
	if strings.EqualFold(css.text("font-style"), "italic") {
		style["font_style"] = "italic"
	}
	decoration := strings.ToLower(css.text("text-decoration"))
	if strings.Contains(decoration, "underline") {
		style["underline"] = true
	}
	if strings.Contains(decoration, "line-through") {
		style["strikethrough"] = true
	}
	switch strings.ToLower(css.text("text-transform")) {
	case "uppercase":
		style["text_case"] = "upper"
	case "lowercase":
		style["text_case"] = "lower"
	case "capitalize":
		style["text_case"] = "title"
	}
	if c, ok := cssColor(css.text("color")); ok {
		style["text_color"] = c
	}
	switch align := strings.ToLower(css.text("text-align")); align {
	case "left", "center", "right", "justify":
		style["text_align"] = align
	default:
		style["text_align"] = "left"
	}
	if m, ok := cssLength(css.text("-fm-paragraph-margin-left")); ok && m > 0 {
		style["indent_left"] = m
	}
	if m, ok := cssLength(css.text("-fm-paragraph-margin-right")); ok && m > 0 {
		style["indent_right"] = m
	}
	return style
}

func firstWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

package fmdapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// The metadata endpoints: the layout and script menus, and a layout's
// fields and portals as the layout contract gives them (schema).

// metadataSession runs a metadata call through the checks every call with a
// session makes, in their order, and opens its database's catalog.
func (h *Handler) metadataSession(req *request) (auth.Session, dbal.DatabaseDriver, *Error) {
	sess, err := h.bearer(req, true)
	if err != nil {
		return auth.Session{}, nil, err
	}
	driver, derr := h.deps.Databases.DriverFor(req.Context(), sess.Database)
	if derr != nil {
		return auth.Session{}, nil, engineError("-1")
	}
	return sess, driver, nil
}

// layouts lists the layout menu: GET {version}/databases/{database}/layouts.
// A layout the account has no access to is left out; a folder is listed
// whatever its contents, as FileMaker Server lists it.
func (h *Handler) layouts(w http.ResponseWriter, req *request) {
	sess, driver, err := h.metadataSession(req)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, svc := req.Context(), schema.NewService(driver)
	menu, merr := svc.Menu(ctx, schema.CatalogLayouts)
	if merr != nil {
		writeError(w, engineError("-1"))
		return
	}
	hidden, herr := hiddenLayouts(ctx, svc, sess)
	if herr != nil {
		writeError(w, engineError("-1"))
		return
	}
	occurrences, oerr := svc.ListTableOccurrences(ctx)
	if oerr != nil {
		writeError(w, engineError("-1"))
		return
	}
	occName := map[string]string{}
	for _, o := range occurrences {
		occName[o.ID] = o.Name
	}
	var list func([]*schema.CatalogNode) []Object
	list = func(nodes []*schema.CatalogNode) []Object {
		out := []Object{}
		for _, n := range nodes {
			switch n.Kind {
			case schema.MenuFolder:
				out = append(out, Object{{"name", n.Name}, {"isFolder", true}, {"folderLayoutNames", list(n.Items)}})
			case schema.MenuSeparator:
				out = append(out, Object{{"name", "-"}, {"table", occName[n.TableOccurrenceID]}})
			case schema.MenuLayout:
				if !hidden[n.ID] {
					out = append(out, Object{{"name", n.Name}, {"table", occName[n.TableOccurrenceID]}})
				}
			}
		}
		return out
	}
	writeOK(w, Object{{"layouts", list(menu)}}, true)
}

// hiddenLayouts are the layouts an account has no access to. An owner or an
// administrator reaches them all.
func hiddenLayouts(ctx context.Context, svc *schema.Service, sess auth.Session) (map[string]bool, error) {
	hidden := map[string]bool{}
	if sess.IsAdmin() {
		return hidden, nil
	}
	perms, err := svc.GetUserPermissions(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	for _, p := range perms {
		if p.AccessLevel != schema.AccessReadWrite && p.AccessLevel != schema.AccessReadOnly {
			hidden[p.LayoutID] = true
		}
	}
	return hidden, nil
}

// scripts lists the script menu: GET {version}/databases/{database}/scripts.
func (h *Handler) scripts(w http.ResponseWriter, req *request) {
	_, driver, err := h.metadataSession(req)
	if err != nil {
		writeError(w, err)
		return
	}
	menu, merr := schema.NewService(driver).Menu(req.Context(), schema.CatalogScripts)
	if merr != nil {
		writeError(w, engineError("-1"))
		return
	}
	var list func([]*schema.CatalogNode) []Object
	list = func(nodes []*schema.CatalogNode) []Object {
		out := []Object{}
		for _, n := range nodes {
			switch n.Kind {
			case schema.MenuFolder:
				out = append(out, Object{{"name", n.Name}, {"folderScriptNames", list(n.Items)}, {"isFolder", true}})
			case schema.MenuSeparator:
				out = append(out, Object{{"name", "-"}, {"isFolder", false}})
			case schema.MenuScript:
				out = append(out, Object{{"name", n.Name}, {"isFolder", false}})
			}
		}
		return out
	}
	writeOK(w, Object{{"scripts", list(menu)}}, true)
}

// layoutContract finds the layout a call names, if the account may see it,
// and reads its contract.
func (h *Handler) layoutContract(req *request) (*schema.ResolvedLayout, auth.Session, dbal.DatabaseDriver, *Error) {
	sess, driver, err := h.metadataSession(req)
	if err != nil {
		return nil, sess, nil, err
	}
	ctx, svc := req.Context(), schema.NewService(driver)
	item, lerr := svc.LayoutByName(ctx, req.params["layout"])
	if errors.Is(lerr, schema.ErrLayoutNotFound) {
		return nil, sess, nil, engineError("105")
	}
	if lerr != nil {
		return nil, sess, nil, engineError("-1")
	}
	if !sess.IsAdmin() {
		level, aerr := svc.LayoutAccess(ctx, sess.UserID, item.ID)
		if aerr != nil {
			return nil, sess, nil, engineError("-1")
		}
		if level == schema.AccessNone {
			return nil, sess, nil, engineError("105")
		}
	}
	layout, rerr := svc.ResolveLayout(ctx, item.ID)
	if rerr != nil {
		return nil, sess, nil, engineError("-1")
	}
	return layout, sess, driver, nil
}

// layout describes a layout: GET {version}/databases/{database}/layouts/
// {layout}, its fields (fieldMetaData) and its portals' (portalMetaData),
// keyed by portal name. A recordId must name one of the layout's records.
func (h *Handler) layout(w http.ResponseWriter, req *request) {
	layout, sess, driver, err := h.layoutContract(req)
	if err != nil {
		writeError(w, err)
		return
	}
	if v, ok := parseQuery(req.URL.RawQuery).Get("recordId"); ok {
		if err := checkLayoutRecord(req.Context(), driver, sess, layout, v); err != nil {
			writeError(w, err)
			return
		}
	}
	fields := make([]Object, 0, len(layout.Fields))
	for _, f := range layout.Fields {
		fields = append(fields, fieldMetaData(f))
	}
	portals := map[string][]Object{}
	for _, p := range layout.Portals {
		key := p.DataAPIName()
		if _, seen := portals[key]; seen {
			continue
		}
		rows := make([]Object, 0, len(p.Fields))
		for _, f := range p.Fields {
			rows = append(rows, fieldMetaData(f))
		}
		portals[key] = rows
	}
	keys := make([]string, 0, len(portals))
	for k := range portals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	portalMeta := Object{}
	for _, k := range keys {
		portalMeta = append(portalMeta, Field{k, portals[k]})
	}
	writeOK(w, Object{{"fieldMetaData", fields}, {"portalMetaData", portalMeta}}, true)
}

// checkLayoutRecord checks that the recordId of a layout description names
// a record of the layout's table. A table whose records the account may not
// read has none it can name, so that the answer says nothing of them.
func checkLayoutRecord(ctx context.Context, driver dbal.DatabaseDriver, sess auth.Session, layout *schema.ResolvedLayout, v Value) *Error {
	id, ok := readInteger(v)
	if !ok || !(id > 0) || id >= 1<<63 || layout.Table == "" {
		return engineError("101")
	}
	if !sess.IsAdmin() {
		level, err := schema.NewService(driver).TableAccess(ctx, sess.UserID, layout.Table)
		if err != nil {
			return engineError("-1")
		}
		if level == schema.AccessNone {
			return engineError("101")
		}
	}
	if _, err := data.NewService(driver).FindByRecordID(ctx, layout.Table, int64(id)); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			return engineError("101")
		}
		return engineError("-1")
	}
	return nil
}

// fieldMetaData describes a field object of a layout.
func fieldMetaData(f schema.LayoutField) Object {
	kind, result := f.Column.FieldKind()
	o := f.Column.Options()
	out := Object{
		{"name", f.DataAPIName()},
		{"type", kind},
		{"displayType", displayType(f)},
		{"result", result},
		{"global", o.Global},
		{"autoEnter", o.AutoEnter},
		{"fourDigitYear", o.FourDigitYear},
		{"maxRepeat", o.MaxRepeat},
		{"maxCharacters", o.MaxCharacters},
		{"notEmpty", o.NotEmpty},
		{"numeric", o.Numeric},
		{"timeOfDay", o.TimeOfDay},
	}
	if f.ValueList != "" {
		out = append(out, Field{"valueList", f.ValueList})
	}
	return append(out,
		Field{"repetitionStart", f.RepetitionStart},
		Field{"repetitionEnd", f.RepetitionStart + f.RepetitionsShown - 1})
}

// displayTypes are the Data API's names of File4Base's controls.
var displayTypes = map[string]string{
	"edit_box":           "editText",
	"drop_down_list":     "popupList",
	"pop_up_menu":        "popupMenu",
	"checkbox_set":       "checkBox",
	"radio_button_set":   "radioButtons",
	"drop_down_calendar": "calendar",
}

func displayType(f schema.LayoutField) string {
	if f.Concealed && (f.ControlStyle == "" || f.ControlStyle == "edit_box") {
		// Unverified: no recording has a concealed edit box.
		return "secureText"
	}
	if t, ok := displayTypes[f.ControlStyle]; ok {
		return t
	}
	return "editText"
}

// metadata describes a layout's fields as the Data API's first version
// did: GET {version}/databases/{database}/layouts/{layout}/metadata. A
// repeating field is listed once per repetition shown; portals' fields are
// not listed.
func (h *Handler) metadata(w http.ResponseWriter, req *request) {
	layout, _, _, err := h.layoutContract(req)
	if err != nil {
		writeError(w, err)
		return
	}
	out := []Object{}
	for _, f := range layout.Fields {
		kind, result := f.Column.FieldKind()
		o := f.Column.Options()
		global := "false"
		if o.Global {
			global = "true"
		}
		id := f.Column.SourceFieldID
		if id == "" {
			id = strconv.Itoa(f.Ordinal)
		}
		entry := func(name, id string, repetitions int) Object {
			e := Object{{"name", name}, {"type", kind}, {"result", result}, {"global", global}}
			if f.ValueList != "" {
				e = append(e, Field{"valueList", f.ValueList})
			}
			return append(e, Field{"repetitions", repetitions}, Field{"id", id})
		}
		if o.MaxRepeat <= 1 {
			out = append(out, entry(f.DataAPIName(), id, 1))
			continue
		}
		for r := f.RepetitionStart; r < f.RepetitionStart+f.RepetitionsShown; r++ {
			n := strconv.Itoa(r)
			out = append(out, entry(f.DataAPIName()+"("+n+")", id+"_"+n, f.RepetitionsShown))
		}
	}
	writeOK(w, Object{{"metaData", out}}, true)
}

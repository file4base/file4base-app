package fmdapi

import (
	"net/http"
	"strings"
)

// The formats productInfo describes: those of dateformats=0, the default.
const (
	dateFormat      = "MM/dd/yyyy"
	timeFormat      = "HH:mm:ss"
	timeStampFormat = "MM/dd/yyyy HH:mm:ss"
)

// productInfo answers GET {version}/productInfo, which needs no session.
// Unlike every other reply, it does not forbid caching.
func (h *Handler) productInfo(w http.ResponseWriter) {
	info := Object{{"name", h.cfg.ProductName}}
	if h.cfg.ProductBuildDate != "" {
		info = append(info, Field{"buildDate", h.cfg.ProductBuildDate})
	}
	if h.cfg.ProductVersion != "" {
		info = append(info, Field{"version", h.cfg.ProductVersion})
	}
	info = append(info,
		Field{"dateFormat", dateFormat},
		Field{"timeFormat", timeFormat},
		Field{"timeStampFormat", timeStampFormat})
	writeOK(w, Object{{"productInfo", info}}, false)
}

// databases answers GET {version}/databases: the served databases, by the
// names clients sign in with. Basic credentials may come along and change
// nothing; any other credentials are refused.
func (h *Handler) databases(w http.ResponseWriter, req *request) {
	if header, _ := authorization(req.Request); strings.TrimSpace(header) != "" {
		if s, _ := scheme(header); !strings.EqualFold(s, "basic") {
			writeError(w, errAuthorizationInvalid)
			return
		}
	}
	list := []Object{}
	for _, a := range h.cfg.Databases {
		_, exists, err := h.deps.Databases.LookupDatabase(req.Context(), a.Database)
		if err != nil {
			writeError(w, engineError("-1"))
			return
		}
		if exists {
			list = append(list, Object{{"name", a.Name}})
		}
	}
	writeOK(w, Object{{"databases", list}}, true)
}

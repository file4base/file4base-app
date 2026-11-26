package fmdapi

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// endpoint is a Data API operation.
type endpoint uint8

const (
	epProductInfo endpoint = iota + 1
	epDatabases
	epValidateSession
	epLogin
	epLogout
	epLayouts
	epScripts
	epLayout
	epMetadata
	epRecords
	epCreate
	epRecord
	epEdit
	epDelete
	epDuplicate
	epUpload
	epUploadRepetition
	epFind
	epScript
	epGlobals
)

type endpointSpec struct {
	// consumes is the body media type the request validator requires on a
	// POST or PUT.
	consumes string
	// body is the request validator's check of the JSON body, if any.
	body func(*Value) *Error
	// params is the front end's check of the parameters, if any.
	params func(*request) *Error
}

const (
	consumesJSON      = "application/json"
	consumesMultipart = "multipart/form-data"
)

// endpointSpecs lists the endpoints with a body or parameter check. The
// checks of the endpoints that write, run scripts or read metadata come
// with those endpoints.
var endpointSpecs = map[endpoint]endpointSpec{
	epLogin:            {consumes: consumesJSON},
	epRecords:          {params: checkRangeQuery},
	epCreate:           {consumes: consumesJSON},
	epRecord:           {params: checkRecordQuery},
	epEdit:             {consumes: consumesJSON},
	epDuplicate:        {consumes: consumesJSON},
	epUpload:           {consumes: consumesMultipart},
	epUploadRepetition: {consumes: consumesMultipart},
	epFind:             {consumes: consumesJSON, body: checkFindBody},
	epGlobals:          {consumes: consumesJSON},
}

// route is a path pattern below /fmi/data and the endpoint of each method.
// A segment starting with ':' is a parameter; the others match in any case,
// as Express routes do.
type route struct {
	segments []string
	methods  map[string]endpoint
}

func pattern(p string) []string { return strings.Split(p, "/") }

var routes = []route{
	{pattern(":version/productInfo"), map[string]endpoint{http.MethodGet: epProductInfo}},
	{pattern(":version/databases"), map[string]endpoint{http.MethodGet: epDatabases}},
	{pattern(":version/validateSession"), map[string]endpoint{http.MethodGet: epValidateSession}},
	{pattern(":version/databases/:database/sessions"), map[string]endpoint{http.MethodPost: epLogin}},
	{pattern(":version/databases/:database/sessions/:token"), map[string]endpoint{http.MethodDelete: epLogout}},
	{pattern(":version/databases/:database/layouts"), map[string]endpoint{http.MethodGet: epLayouts}},
	{pattern(":version/databases/:database/scripts"), map[string]endpoint{http.MethodGet: epScripts}},
	{pattern(":version/databases/:database/globals"), map[string]endpoint{http.MethodPatch: epGlobals}},
	{pattern(":version/databases/:database/layouts/:layout"), map[string]endpoint{http.MethodGet: epLayout}},
	{pattern(":version/databases/:database/layouts/:layout/metadata"), map[string]endpoint{http.MethodGet: epMetadata}},
	{pattern(":version/databases/:database/layouts/:layout/records"), map[string]endpoint{http.MethodGet: epRecords, http.MethodPost: epCreate}},
	{pattern(":version/databases/:database/layouts/:layout/_find"), map[string]endpoint{http.MethodPost: epFind}},
	{pattern(":version/databases/:database/layouts/:layout/script/:script"), map[string]endpoint{http.MethodGet: epScript}},
	{pattern(":version/databases/:database/layouts/:layout/records/:recordId"), map[string]endpoint{
		http.MethodGet: epRecord, http.MethodPatch: epEdit, http.MethodDelete: epDelete, http.MethodPost: epDuplicate,
	}},
	{pattern(":version/databases/:database/layouts/:layout/records/:recordId/containers/:field"), map[string]endpoint{http.MethodPost: epUpload}},
	{pattern(":version/databases/:database/layouts/:layout/records/:recordId/containers/:field/:repetition"), map[string]endpoint{http.MethodPost: epUploadRepetition}},
}

// apiVersions are the versions FileMaker Server serves. They answer alike.
var apiVersions = map[string]bool{"v1": true, "v2": true, "vLatest": true}

// IsPath reports whether path is the Data API's, below /fmi/data.
func IsPath(path string) bool {
	return path == "/fmi/data" || strings.HasPrefix(path, "/fmi/data/")
}

// rawParam is a path parameter as sent, still escaped.
type rawParam struct{ name, value string }

// matchRoute finds the route of a path below /fmi/data, its slashes merged.
// Like Express, it matches the path as sent, escapes included, allows one
// trailing slash and returns the parameters still escaped, in path order.
func matchRoute(path string) (*route, []rawParam, bool) {
	rest, ok := strings.CutPrefix(path, "/fmi/data")
	if !ok || (rest != "" && rest[0] != '/') {
		return nil, nil, false
	}
	rest = strings.TrimSuffix(strings.TrimPrefix(rest, "/"), "/")
	if rest == "" {
		return nil, nil, false
	}
	segs := strings.Split(rest, "/")
	for i := range routes {
		rt := &routes[i]
		if len(rt.segments) != len(segs) {
			continue
		}
		var params []rawParam
		matched := true
		for j, want := range rt.segments {
			if name, isParam := strings.CutPrefix(want, ":"); isParam {
				params = append(params, rawParam{name, segs[j]})
			} else if !equalFoldASCII(want, segs[j]) {
				matched = false
				break
			}
		}
		if matched {
			return rt, params, true
		}
	}
	return nil, nil, false
}

// equalFoldASCII is JavaScript's case-insensitive comparison of an ASCII
// literal: only ASCII letters fold.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// decodeParams decodes the path parameters as decodeURIComponent does, in
// path order. The first parameter it cannot decode fails the request as
// Express fails it.
func decodeParams(raw []rawParam) (map[string]string, *Error) {
	out := make(map[string]string, len(raw))
	for _, p := range raw {
		d, err := url.PathUnescape(p.value)
		if err != nil || !utf8.ValidString(d) {
			return nil, bodyError("Failed to decode param '" + p.value + "'")
		}
		out[p.name] = d
	}
	return out, nil
}

// mergeSlashes collapses runs of slashes, as FileMaker Server's web server
// does before the request reaches the Data API.
func mergeSlashes(p string) string {
	if !strings.Contains(p, "//") {
		return p
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '/' && i > 0 && p[i-1] == '/' {
			continue
		}
		b.WriteByte(p[i])
	}
	return b.String()
}

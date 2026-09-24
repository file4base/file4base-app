package api

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

//go:embed swagger/ui/*
var swaggerDistFS embed.FS

// SwaggerHandler serves embedded Swagger UI and OpenAPI 3.0 specification
type SwaggerHandler struct {
	fileServer   http.Handler
	openapiBytes []byte
}

// NewSwaggerHandler initializes the embedded filesystem handler
func NewSwaggerHandler() (*SwaggerHandler, error) {
	subFS, err := fs.Sub(swaggerDistFS, "swagger/ui")
	if err != nil {
		return nil, err
	}

	specFile, err := subFS.Open("openapi.json")
	var specBytes []byte
	if err == nil {
		defer specFile.Close()
		specBytes, _ = io.ReadAll(specFile)
	}

	return &SwaggerHandler{
		fileServer:   http.FileServer(http.FS(subFS)),
		openapiBytes: specBytes,
	}, nil
}

// RegisterRoutes mounts Swagger UI and OpenAPI spec routes
func (h *SwaggerHandler) RegisterRoutes(r chi.Router) {
	// Redirect convenience routes to /swagger/
	r.Get("/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/", http.StatusMovedPermanently)
	})
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/", http.StatusMovedPermanently)
	})

	// Direct raw OpenAPI spec routes
	r.Get("/swagger/openapi.json", h.ServeOpenAPI)
	r.Get("/openapi.json", h.ServeOpenAPI)
	r.Get("/api/v1/openapi.json", h.ServeOpenAPI)

	// Swagger UI static assets
	r.Get("/swagger/*", func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		fsHandler := http.StripPrefix(pathPrefix, h.fileServer)
		fsHandler.ServeHTTP(w, r)
	})
}

// ServeOpenAPI responds with the OpenAPI 3.0 JSON specification
func (h *SwaggerHandler) ServeOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.openapiBytes)
}

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
}

func setupTestRouter(t *testing.T) (*chi.Mux, *schema.Service) {
	dsn := "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable"
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: dbal.EnginePostgres,
		DSN:        dsn,
	})
	if err != nil {
		t.Skip("PostgreSQL not available:", err)
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := driver.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed:", err)
		return nil, nil
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	r := chi.NewRouter()
	handler := api.NewSchemaHandler(svc)
	handler.RegisterRoutes(r)

	return r, svc
}

func TestSchemaHandler_OccurrencesAndRelationships(t *testing.T) {
	router, svc := setupTestRouter(t)
	if router == nil {
		return
	}

	ctx := context.Background()
	ts := time.Now().UnixNano() % 1000000
	t1Name := fmt.Sprintf("api_occ_t1_%d", ts)
	t2Name := fmt.Sprintf("api_occ_t2_%d", ts)

	tbl1, err := svc.CreateTable(ctx, "Occ Customers", t1Name)
	require.NoError(t, err)
	tbl2, err := svc.CreateTable(ctx, "Occ Invoices", t2Name)
	require.NoError(t, err)

	col1, err := svc.AddColumn(ctx, tbl1.ID, schema.ColumnMetadata{
		Name:        "c_id",
		DisplayName: "Cust ID",
		FieldType:   dbal.FieldTypeText,
	})
	require.NoError(t, err)

	col2, err := svc.AddColumn(ctx, tbl2.ID, schema.ColumnMetadata{
		Name:        "fk_c_id",
		DisplayName: "FK Cust ID",
		FieldType:   dbal.FieldTypeText,
	})
	require.NoError(t, err)

	// 1. GET /api/v1/schemas/occurrences
	req := httptest.NewRequest(http.MethodGet, "/api/v1/schemas/occurrences", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var occs []schema.TableOccurrence
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&occs))
	require.NotEmpty(t, occs)

	var to1ID, to2ID string
	for _, o := range occs {
		if o.BaseTableID == tbl1.ID {
			to1ID = o.ID
		}
		if o.BaseTableID == tbl2.ID {
			to2ID = o.ID
		}
	}
	require.NotEmpty(t, to1ID)
	require.NotEmpty(t, to2ID)

	// 2. POST /api/v1/schemas/occurrences
	createOccBody, _ := json.Marshal(schema.CreateOccurrenceInput{
		BaseTableID: tbl1.ID,
		Name:        fmt.Sprintf("Occ_Cust_2_%d", ts),
		XPos:        300,
		YPos:        200,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/schemas/occurrences", bytes.NewReader(createOccBody))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var createdTO schema.TableOccurrence
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&createdTO))
	assert.Equal(t, 300.0, createdTO.XPos)

	// 3. PUT /api/v1/schemas/occurrences/{id}
	newX := 450.0
	updBody, _ := json.Marshal(schema.UpdateOccurrenceInput{
		XPos: &newX,
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/schemas/occurrences/"+createdTO.ID, bytes.NewReader(updBody))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. POST /api/v1/schemas/relationships
	relBody, _ := json.Marshal(schema.CreateRelationshipInput{
		Name:              fmt.Sprintf("rel_api_%d", ts),
		LeftOccurrenceID:  to1ID,
		LeftColumnID:      col1.ID,
		RightOccurrenceID: to2ID,
		RightColumnID:     col2.ID,
		Operator:          "=",
		AllowCreation:     true,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/schemas/relationships", bytes.NewReader(relBody))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var createdRel schema.RelationshipMetadata
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&createdRel))
	assert.NotEmpty(t, createdRel.ID)
	assert.Equal(t, "=", createdRel.Operator)

	// 5. GET /api/v1/schemas/relationships
	req = httptest.NewRequest(http.MethodGet, "/api/v1/schemas/relationships", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 6. DELETE /api/v1/schemas/relationships/{id}
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/schemas/relationships/"+createdRel.ID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// 7. DELETE /api/v1/schemas/occurrences/{id}
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/schemas/occurrences/"+createdTO.ID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

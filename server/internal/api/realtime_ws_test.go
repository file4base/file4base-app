package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/realtime"
)

func TestAPI_RealtimeNotifiesOnDataChange(t *testing.T) {
	hub := realtime.NewHub()
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true, Hub: hub})
	db := uniqueDB("f4b_rt")
	h.createDatabase(db, "alice", "alice-secret-pw")
	token := h.login(db, "alice", "alice-secret-pw")

	// A table to change.
	var tbl struct {
		ID string `json:"id"`
	}
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/schemas/tables", token,
		map[string]string{"display_name": "Tasks", "custom_name": "tasks"}, &tbl).Code)
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", token,
		map[string]interface{}{"display_name": "title", "name": "title", "field_type": "TEXT"}, nil).Code)

	srv := httptest.NewServer(h.router)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/realtime"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer c.CloseNow()

	require.NoError(t, wsjson.Write(ctx, c, map[string]string{"token": token}))
	var welcome realtime.Event
	require.NoError(t, wsjson.Read(ctx, c, &welcome))
	require.Equal(t, "welcome", welcome.Type)

	// A find (read-only POST) must not notify; a row insert must.
	require.Equal(t, http.StatusOK, h.do(http.MethodPost, "/api/v1/data/tasks/find", token,
		map[string]interface{}{"requests": []map[string]interface{}{{"criteria": []any{}}}}, nil).Code)
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/tasks", token,
		map[string]interface{}{"title": "write tests"}, nil).Code)

	var ev realtime.Event
	readCtx, rc := context.WithTimeout(ctx, 5*time.Second)
	defer rc()
	require.NoError(t, wsjson.Read(readCtx, c, &ev))
	require.Equal(t, "data", ev.Type, "the first event must be the insert, not the find")
	require.Equal(t, "tasks", ev.Resource)
	require.Equal(t, db, ev.Database)
	require.NotEmpty(t, ev.Origin)
}

func TestAPI_RealtimeRejectsABadToken(t *testing.T) {
	hub := realtime.NewHub()
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true, Hub: hub})
	srv := httptest.NewServer(h.router)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/realtime"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer c.CloseNow()
	require.NoError(t, wsjson.Write(ctx, c, map[string]string{"token": "nope"}))
	// The server closes the socket; the next read fails.
	_, _, err = c.Read(ctx)
	require.Error(t, err)
}

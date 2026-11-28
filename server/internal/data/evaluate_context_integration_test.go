package data_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// texts is the text of each result, or its error.
func texts(t *testing.T, got []data.ExpressionResult) []string {
	t.Helper()
	out := make([]string, len(got))
	for i, e := range got {
		if e.Err != nil {
			out[i] = "error: " + e.Err.Error()
			continue
		}
		out[i] = e.Text
	}
	return out
}

// A portal's row is evaluated in the layout's record: a field of the
// layout's occurrence reads that record, not the first one related to the
// row. With a relationship that is many-to-many on both sides (teams and
// players of a city), reading it back from the row finds another team.
//
//	teams (layout) ──city── players (portal) ──name── badges
func TestEvaluateExpressions_PortalRowInTheLayoutsRecord(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "xevalctx", "xevalctx_unused")
	_, teamsOcc, teamFields := a.table(t, ctx, "teams", [2]string{"name", "TEXT"}, [2]string{"city", "TEXT"})
	_, playersOcc, playerFields := a.table(t, ctx, "players",
		[2]string{"name", "TEXT"}, [2]string{"city", "TEXT"}, [2]string{"rating", "NUMBER"})
	_, badgesOcc, badgeFields := a.table(t, ctx, "badges", [2]string{"player", "TEXT"}, [2]string{"label", "TEXT"})
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "teams_players", LeftOccurrenceID: teamsOcc, RightOccurrenceID: playersOcc,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: teamFields["city"], Operator: "=", RightColumnID: playerFields["city"]}}})
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "players_badges", LeftOccurrenceID: playersOcc, RightOccurrenceID: badgesOcc,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: playerFields["name"], Operator: "=", RightColumnID: badgeFields["player"]}}})

	svc := data.NewService(a.driver)
	insert := func(table string, values map[string]interface{}) string {
		row, err := svc.InsertRow(ctx, table, values)
		require.NoError(t, err)
		return fmt.Sprint(row["id"])
	}
	insert("teams", map[string]interface{}{"name": "first team", "city": "c1"})
	second := insert("teams", map[string]interface{}{"name": "second team", "city": "c1"})
	p1 := insert("players", map[string]interface{}{"name": "p1", "city": "c1", "rating": 5})
	p2 := insert("players", map[string]interface{}{"name": "p2", "city": "c1", "rating": 9})
	insert("badges", map[string]interface{}{"player": "p1", "label": "gold"})
	insert("badges", map[string]interface{}{"player": "p2", "label": "silver"})

	expressions := []string{
		`teams::name`,                        // the layout's record
		`name & "/" & players::name`,         // the row, named alone or through its occurrence
		`badges::label`,                      // through the row
		`List ( badges::label )`,             // every record related to the row
		`Count ( players::name )`,            // every row related to the layout's record
		`players::rating > 6`,                // a portal row's condition
		`Self = 9`,                           // Self, the row's field
		`teams::name = "second team"`,        // a hide condition of the layout's fields
		`Count ( badges::label )`,            // totalled through the row
		`GetNthRecord ( badges::label ; 1 )`, // one record through the row
		`Sum ( players::rating )`,            // every row, totalled
	}
	self := make([]*data.SelfField, len(expressions))
	self[6] = &data.SelfField{Occurrence: "players", Field: "rating"}
	env := data.EvaluationEnv{Self: self, Context: &data.ContextRecord{Occurrence: teamsOcc, RecordID: second}}

	got, err := svc.EvaluateExpressionsIn(ctx, "players", playersOcc, p1, expressions, "Text", env)
	require.NoError(t, err)
	assert.Equal(t, []string{"second team", "p1/p1", "gold", "gold", "2", "0", "0", "1", "1", "gold", "14"}, texts(t, got))

	got, err = svc.EvaluateExpressionsIn(ctx, "players", "players", p2, expressions, "Text", env)
	require.NoError(t, err)
	assert.Equal(t, []string{"second team", "p2/p2", "silver", "silver", "2", "1", "1", "1", "1", "silver", "14"}, texts(t, got))

	// No occurrence named is the table's own.
	got, err = svc.EvaluateExpressionsIn(ctx, "players", "", p1, expressions[:2], "Text", env)
	require.NoError(t, err)
	assert.Equal(t, []string{"second team", "p1/p1"}, texts(t, got))

	// A portal of the layout's own occurrence (a self-join): the row is that
	// occurrence's record.
	got, err = svc.EvaluateExpressionsIn(ctx, "players", playersOcc, p2, []string{`players::name`, `name`}, "Text",
		data.EvaluationEnv{Context: &data.ContextRecord{Occurrence: playersOcc, RecordID: p1}})
	require.NoError(t, err)
	assert.Equal(t, []string{"p2", "p2"}, texts(t, got))

	// Asked for as another solution's occurrence: this one is not.
	external := env
	external.External = true
	_, err = svc.EvaluateExpressionsIn(ctx, "players", playersOcc, p1, expressions, "Text", external)
	assert.True(t, errors.Is(err, data.ErrOccurrenceNotFound), "%v", err)

	// The rows of one request share their context: the layout's record and
	// its table's access are read once, whatever the number of rows; a row
	// gone answers nothing.
	asked := map[string]int{}
	counting := data.NewService(a.driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
		asked[table]++
		return true, nil
	})
	batch, err := counting.EvaluateExpressionsForRecords(ctx, "players", playersOcc, []string{p1, "gone", p2}, expressions, "Text", env)
	require.NoError(t, err)
	require.Len(t, batch, 3)
	assert.Equal(t, "p1/p1", texts(t, batch[0])[1])
	assert.Nil(t, batch[1])
	assert.Equal(t, "p2/p2", texts(t, batch[2])[1])
	assert.Equal(t, 1, asked["teams"], "the layout's table, for every row: %v", asked)
	assert.Equal(t, 1, asked["players"], "the rows' table: %v", asked)
	_, err = counting.EvaluateExpressionsForRecords(ctx, "players", playersOcc, []string{p1},
		expressions, "Text", data.EvaluationEnv{Context: &data.ContextRecord{Occurrence: teamsOcc, RecordID: "no-such-record"}})
	assert.True(t, errors.Is(err, data.ErrContextNotFound), "a layout record gone is the request's: %v", err)

	// Without it, the layout's field is read back from the row through the
	// relationship: the first team of the city, not the layout's.
	got, err = svc.EvaluateExpressions(ctx, "players", playersOcc, p1, []string{`teams::name`}, "Text")
	require.NoError(t, err)
	assert.Equal(t, []string{"first team"}, texts(t, got))

	// The occurrence must be one of the table evaluated, and the layout's
	// one that is there.
	_, err = svc.EvaluateExpressionsIn(ctx, "players", teamsOcc, p1, expressions, "Text", env)
	assert.True(t, errors.Is(err, data.ErrOccurrenceNotFound), "%v", err)
	_, err = svc.EvaluateExpressionsIn(ctx, "players", playersOcc, p1, expressions, "Text",
		data.EvaluationEnv{Context: &data.ContextRecord{Occurrence: "no such occurrence", RecordID: second}})
	assert.True(t, errors.Is(err, data.ErrOccurrenceNotFound), "%v", err)
	_, err = svc.EvaluateExpressionsIn(ctx, "players", playersOcc, p1, expressions, "Text",
		data.EvaluationEnv{Context: &data.ContextRecord{Occurrence: teamsOcc, RecordID: "no-such-record"}})
	assert.True(t, errors.Is(err, data.ErrContextNotFound), "%v", err)
	assert.False(t, errors.Is(err, data.ErrRecordNotFound), "not a row gone: %v", err)

	// Every table read must be readable: the layout's, the row's, and the
	// ones a formula reads through the row.
	denying := func(denied string) *data.Service {
		return data.NewService(a.driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
			return table != denied, nil
		})
	}
	_, err = denying("teams").EvaluateExpressionsIn(ctx, "players", playersOcc, p1, expressions, "Text", env)
	assert.True(t, errors.Is(err, data.ErrNoPermission), "the layout's record: %v", err)
	_, err = denying("players").EvaluateExpressionsIn(ctx, "players", playersOcc, p1, expressions, "Text", env)
	assert.True(t, errors.Is(err, data.ErrNoPermission), "the row: %v", err)
	got, err = denying("badges").EvaluateExpressionsIn(ctx, "players", playersOcc, p1, expressions, "Text", env)
	require.NoError(t, err)
	require.Error(t, got[2].Err)
	assert.Contains(t, got[2].Err.Error(), "<No Access>")
	assert.Equal(t, "second team", got[0].Text)
}

// The rows of a portal of another solution's table are read there, as the
// account the session group holds there, while the formula, which names
// this solution's occurrences, is evaluated here in the layout's record.
//
//	teams (solution A, layout) ──city── remote_players (external of B)
func TestEvaluateExpressions_ExternalPortalRowInTheLayoutsRecord(t *testing.T) {
	ctx, a, b := twoSolutions(t, "xevalctx_teams", "xevalctx_players")
	_, teamsOcc, teamFields := a.table(t, ctx, "teams", [2]string{"name", "TEXT"}, [2]string{"city", "TEXT"})
	b.table(t, ctx, "players", [2]string{"name", "TEXT"}, [2]string{"city", "TEXT"}, [2]string{"rating", "NUMBER"})
	ref, remote := a.external(t, ctx, "Players", "remote_players")
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "teams_remote_players", LeftOccurrenceID: teamsOcc, RightOccurrenceID: remote,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: teamFields["city"], Operator: "=", RightColumnName: "city"}}})
	a.bind(t, ctx, ref, b, map[string]string{"remote_players": "players"})

	teams, players := data.NewService(a.driver), data.NewService(b.driver)
	teams.WithPeers(staticPeers{b.name: players})
	insert := func(svc *data.Service, table string, values map[string]interface{}) string {
		row, err := svc.InsertRow(ctx, table, values)
		require.NoError(t, err)
		return fmt.Sprint(row["id"])
	}
	insert(teams, "teams", map[string]interface{}{"name": "first team", "city": "c1"})
	second := insert(teams, "teams", map[string]interface{}{"name": "second team", "city": "c1"})
	p1 := insert(players, "players", map[string]interface{}{"name": "p1", "city": "c1", "rating": 5})
	insert(players, "players", map[string]interface{}{"name": "p2", "city": "c1", "rating": 9})

	expressions := []string{`teams::name`, `name & "/" & remote_players::name`, `Count ( remote_players::name )`, `Self`}
	self := []*data.SelfField{nil, nil, nil, {Field: "rating"}}
	env := data.EvaluationEnv{Self: self, Context: &data.ContextRecord{Occurrence: teamsOcc, RecordID: second}, External: true}
	got, err := teams.EvaluateExpressionsIn(ctx, "players", remote, p1, expressions, "Text", env)
	require.NoError(t, err)
	assert.Equal(t, []string{"second team", "p1/p1", "2", "5"}, texts(t, got))

	// Asked for as this solution's table, as the table's route does, even
	// named otherwise than in the graph: refused, never read.
	for _, named := range []string{remote, " Remote_Players "} {
		local := env
		local.External = false
		_, err = teams.EvaluateExpressionsIn(ctx, "players", named, p1, expressions, "Text", local)
		assert.True(t, errors.Is(err, data.ErrOccurrenceElsewhere), "%q: %v", named, err)
	}

	// The account there may not read the table: the row is not read.
	denied := data.NewService(b.driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
		return table != "players", nil
	})
	refused := data.NewService(a.driver).WithPeers(staticPeers{b.name: denied})
	_, err = refused.EvaluateExpressionsIn(ctx, "players", remote, p1, expressions, "Text", env)
	assert.True(t, errors.Is(err, data.ErrExternalUnavailable), "%v", err)
	assert.True(t, errors.Is(err, data.ErrNoAccess), "%v", err)

	// Not signed in there: nothing is read.
	alone := data.NewService(a.driver).WithPeers(staticPeers{})
	_, err = alone.EvaluateExpressionsIn(ctx, "players", remote, p1, expressions, "Text", env)
	assert.True(t, errors.Is(err, data.ErrExternalUnavailable), "%v", err)
}

// A script run from a portal row's button evaluates on the layout's record
// with that row active (data.PortalRow): a field of the portal's
// occurrence reads the row and one through it the records related to the
// row, while the layout's record stays the current one and an aggregate of
// the portal's occurrence still covers every related record (#237).
//
//	teams (layout) ──city── players (portal) ──name── badges
func TestEvaluateExpressions_ActivePortalRow(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "xevalrow", "xevalrow_unused")
	_, teamsOcc, teamFields := a.table(t, ctx, "teams", [2]string{"name", "TEXT"}, [2]string{"city", "TEXT"})
	_, playersOcc, playerFields := a.table(t, ctx, "players",
		[2]string{"name", "TEXT"}, [2]string{"city", "TEXT"}, [2]string{"rating", "NUMBER"})
	_, badgesOcc, badgeFields := a.table(t, ctx, "badges", [2]string{"player", "TEXT"}, [2]string{"label", "TEXT"})
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "teams_players", LeftOccurrenceID: teamsOcc, RightOccurrenceID: playersOcc,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: teamFields["city"], Operator: "=", RightColumnID: playerFields["city"]}}})
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "players_badges", LeftOccurrenceID: playersOcc, RightOccurrenceID: badgesOcc,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: playerFields["name"], Operator: "=", RightColumnID: badgeFields["player"]}}})

	svc := data.NewService(a.driver)
	insert := func(table string, values map[string]interface{}) string {
		row, err := svc.InsertRow(ctx, table, values)
		require.NoError(t, err)
		return fmt.Sprint(row["id"])
	}
	team := insert("teams", map[string]interface{}{"name": "a team", "city": "c1"})
	insert("players", map[string]interface{}{"name": "p1", "city": "c1", "rating": 5})
	p2 := insert("players", map[string]interface{}{"name": "p2", "city": "c1", "rating": 9})
	insert("badges", map[string]interface{}{"player": "p1", "label": "gold"})
	insert("badges", map[string]interface{}{"player": "p2", "label": "silver"})

	expressions := []string{
		`name`,                    // the layout's record
		`teams::name`,             // the layout's record, through its occurrence
		`players::name`,           // the active row, not the first related
		`badges::label`,           // through the row
		`Count ( players::name )`, // every related record
		`Sum ( players::rating )`,
		`List ( badges::label )`, // every record related to the row
	}
	active := data.EvaluationEnv{PortalRow: &data.PortalRow{Occurrence: "players", RecordID: p2}}
	got, err := svc.EvaluateExpressionsIn(ctx, "teams", teamsOcc, team, expressions, "Text", active)
	require.NoError(t, err)
	assert.Equal(t, []string{"a team", "a team", "p2", "silver", "2", "14", "silver"}, texts(t, got))

	// By the portal occurrence's id too; and from the table's own
	// occurrence when the layout's is not named.
	byID := data.EvaluationEnv{PortalRow: &data.PortalRow{Occurrence: playersOcc, RecordID: p2}}
	got, err = svc.EvaluateExpressionsIn(ctx, "teams", "", team, expressions[2:4], "Text", byID)
	require.NoError(t, err)
	assert.Equal(t, []string{"p2", "silver"}, texts(t, got))

	// No row active: the first related record.
	got, err = svc.EvaluateExpressionsIn(ctx, "teams", teamsOcc, team, expressions[2:4], "Text", data.EvaluationEnv{})
	require.NoError(t, err)
	assert.Equal(t, []string{"p1", "gold"}, texts(t, got))

	// A row of the layout's own occurrence (as a portal of it, or a layout
	// that does not name its occurrence, resolved to the table's) pins
	// nothing: the layout's record, as without a row.
	for _, layout := range []string{teamsOcc, ""} {
		own := data.EvaluationEnv{PortalRow: &data.PortalRow{Occurrence: teamsOcc, RecordID: team}}
		got, err = svc.EvaluateExpressionsIn(ctx, "teams", layout, team, expressions[:4], "Text", own)
		require.NoError(t, err, layout)
		assert.Equal(t, []string{"a team", "a team", "p1", "gold"}, texts(t, got), layout)
	}

	// A row gone, an occurrence that is not there, no record id, or with a
	// context: the request's.
	for name, env := range map[string]data.EvaluationEnv{
		"gone":     {PortalRow: &data.PortalRow{Occurrence: "players", RecordID: "no-such-row"}},
		"nowhere":  {PortalRow: &data.PortalRow{Occurrence: "no such occurrence", RecordID: p2}},
		"no id":    {PortalRow: &data.PortalRow{Occurrence: "players"}},
		"in a row": {PortalRow: &data.PortalRow{Occurrence: "players", RecordID: p2}, Context: &data.ContextRecord{Occurrence: teamsOcc, RecordID: team}},
	} {
		_, err := svc.EvaluateExpressionsIn(ctx, "teams", teamsOcc, team, expressions, "Text", env)
		require.Error(t, err, name)
		switch name {
		case "gone":
			assert.True(t, errors.Is(err, data.ErrPortalRowNotFound), "%s: %v", name, err)
		case "nowhere":
			assert.True(t, errors.Is(err, data.ErrOccurrenceNotFound), "%s: %v", name, err)
		default:
			assert.True(t, errors.Is(err, data.ErrInvalidValue), "%s: %v", name, err)
		}
	}

	// The row's table must be readable.
	denied := data.NewService(a.driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
		return table != "players", nil
	})
	_, err = denied.EvaluateExpressionsIn(ctx, "teams", teamsOcc, team, expressions, "Text", active)
	assert.True(t, errors.Is(err, data.ErrNoPermission), "%v", err)
}

// The active row of a portal of another solution's table is read there,
// as the account the session group holds there.
func TestEvaluateExpressions_ExternalActivePortalRow(t *testing.T) {
	ctx, a, b := twoSolutions(t, "xevalrow_teams", "xevalrow_players")
	_, teamsOcc, teamFields := a.table(t, ctx, "teams", [2]string{"name", "TEXT"}, [2]string{"city", "TEXT"})
	b.table(t, ctx, "players", [2]string{"name", "TEXT"}, [2]string{"city", "TEXT"}, [2]string{"rating", "NUMBER"})
	ref, remote := a.external(t, ctx, "Players", "remote_players")
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "teams_remote_players", LeftOccurrenceID: teamsOcc, RightOccurrenceID: remote,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: teamFields["city"], Operator: "=", RightColumnName: "city"}}})
	a.bind(t, ctx, ref, b, map[string]string{"remote_players": "players"})

	teams, players := data.NewService(a.driver), data.NewService(b.driver)
	teams.WithPeers(staticPeers{b.name: players})
	team, err := teams.InsertRow(ctx, "teams", map[string]interface{}{"name": "a team", "city": "c1"})
	require.NoError(t, err)
	_, err = players.InsertRow(ctx, "players", map[string]interface{}{"name": "p1", "city": "c1", "rating": 5})
	require.NoError(t, err)
	p2, err := players.InsertRow(ctx, "players", map[string]interface{}{"name": "p2", "city": "c1", "rating": 9})
	require.NoError(t, err)

	expressions := []string{`teams::name`, `remote_players::name`, `remote_players::rating`, `Count ( remote_players::name )`}
	env := data.EvaluationEnv{PortalRow: &data.PortalRow{Occurrence: "remote_players", RecordID: fmt.Sprint(p2["id"])}}
	got, err := teams.EvaluateExpressionsIn(ctx, "teams", teamsOcc, fmt.Sprint(team["id"]), expressions, "Text", env)
	require.NoError(t, err)
	assert.Equal(t, []string{"a team", "p2", "9", "2"}, texts(t, got))

	// The account there may not read the table: the row is not read.
	denied := data.NewService(b.driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
		return table != "players", nil
	})
	refused := data.NewService(a.driver).WithPeers(staticPeers{b.name: denied})
	_, err = refused.EvaluateExpressionsIn(ctx, "teams", teamsOcc, fmt.Sprint(team["id"]), expressions, "Text", env)
	assert.True(t, errors.Is(err, data.ErrExternalUnavailable), "%v", err)

	// Not signed in there: nothing is read.
	alone := data.NewService(a.driver).WithPeers(staticPeers{})
	_, err = alone.EvaluateExpressionsIn(ctx, "teams", teamsOcc, fmt.Sprint(team["id"]), expressions, "Text", env)
	assert.True(t, errors.Is(err, data.ErrExternalUnavailable), "%v", err)
}

package transfer_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/file4base/file4base-app/server/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The acceptance fixture of #96, as the issue states it: two synthetic
// solutions, Dispatch and Results, with parent surveys and child answers
// keyed by stable business codes. Two surveys and their answers are sent
// across, one result is modified and returned, the transfer is replayed, and
// one row is rejected — all with French text, exact decimals and a container
// to prove nothing is mangled on the way.
//
// Everything here is synthetic. No credential is ever part of a definition or
// of a transferred record.
const (
	// A French title with accents and a non-breaking space before the colon,
	// which is how French typography writes one.
	frenchSurveyTitle = "Enquête de satisfaction : été 2026"
	frenchRespondent  = "Élodie Gaëlle Noël"

	// "hello" as base64, which is how a container value travels.
	containerValue = "aGVsbG8="

	// A decimal that a float would not hold exactly.
	exactScore = "12.5"
)

type solution struct {
	name      string
	driver    dbal.DatabaseDriver
	schemaSvc *schema.Service
	dataSvc   *data.Service
}

// newSolution creates a database of its own and provisions the catalog in it.
// A transfer crosses two solutions, and in File4Base a solution is a
// database, so the fixture needs two real ones.
func newSolution(t *testing.T, ctx context.Context, mgr *dbal.MultiDatabaseManager, name string) *solution {
	t.Helper()
	require.NoError(t, mgr.CreateDatabase(ctx, name))
	t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })

	driver, err := mgr.DriverFor(ctx, name)
	require.NoError(t, err)

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	return &solution{name: name, driver: driver, schemaSvc: schemaSvc, dataSvc: data.NewService(driver)}
}

type column struct {
	name      string
	label     string
	fieldType dbal.AgnosticFieldType
}

func (s *solution) createTable(t *testing.T, ctx context.Context, name string, columns ...column) {
	t.Helper()
	tbl, err := s.schemaSvc.CreateTable(ctx, name, name)
	require.NoError(t, err)
	for _, c := range columns {
		_, err := s.schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: c.name, DisplayName: c.label, FieldType: c.fieldType, IsNullable: true,
		})
		require.NoError(t, err, "adding %s.%s", name, c.name)
	}
}

func transferTestManager(t *testing.T) (*dbal.MultiDatabaseManager, context.Context) {
	t.Helper()
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	if err != nil {
		t.Skip("database not available:", err)
		return nil, nil
	}
	t.Cleanup(func() { _ = mgr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	if err := mgr.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return nil, nil
	}
	return mgr, ctx
}

// fixture is the whole acceptance scene, built once per test.
type fixture struct {
	dispatch *solution
	results  *solution
}

func newFixture(t *testing.T, ctx context.Context, mgr *dbal.MultiDatabaseManager) *fixture {
	t.Helper()
	stamp := uniqueTestStamp()

	dispatch := newSolution(t, ctx, mgr, fmt.Sprintf("f4b_dispatch_%d", stamp))
	results := newSolution(t, ctx, mgr, fmt.Sprintf("f4b_results_%d", stamp))

	// Dispatch: the central solution. A survey's code is its business key.
	dispatch.createTable(t, ctx, "surveys",
		column{"survey_code", "Survey Code", dbal.FieldTypeText},
		column{"title", "Title", dbal.FieldTypeText},
		column{"issued_on", "Issued On", dbal.FieldTypeDate},
		column{"quota", "Quota", dbal.FieldTypeNumber},
	)
	// The answers carry their parent's code, which is what keeps a child from
	// being orphaned when it crosses to another solution: the key travels
	// with the record rather than a row id that means nothing over there.
	//
	// score_text is deliberately text in Dispatch and a number in Results:
	// that mapping is where a row the destination cannot hold comes from, and
	// it is the commonest real one.
	dispatch.createTable(t, ctx, "answers",
		column{"answer_code", "Answer Code", dbal.FieldTypeText},
		column{"survey_code", "Survey Code", dbal.FieldTypeText},
		column{"respondent", "Respondent", dbal.FieldTypeText},
		column{"score_text", "Score", dbal.FieldTypeText},
		column{"attachment", "Attachment", dbal.FieldTypeContainer},
		column{"result_note", "Result Note", dbal.FieldTypeText},
	)

	// Results: the project solution. Its field names differ on purpose, so
	// the transfer has to carry an explicit mapping rather than matching
	// names and hoping.
	results.createTable(t, ctx, "project_surveys",
		column{"code", "Code", dbal.FieldTypeText},
		column{"label", "Label", dbal.FieldTypeText},
		column{"issued_on", "Issued On", dbal.FieldTypeDate},
		column{"quota", "Quota", dbal.FieldTypeNumber},
	)
	results.createTable(t, ctx, "project_answers",
		column{"code", "Code", dbal.FieldTypeText},
		column{"survey_code", "Survey Code", dbal.FieldTypeText},
		column{"respondent", "Respondent", dbal.FieldTypeText},
		column{"score", "Score", dbal.FieldTypeNumber},
		column{"attachment", "Attachment", dbal.FieldTypeContainer},
		column{"result_note", "Result Note", dbal.FieldTypeText},
	)

	f := &fixture{dispatch: dispatch, results: results}
	f.seed(t, ctx)
	return f
}

func (f *fixture) seed(t *testing.T, ctx context.Context) {
	t.Helper()
	insert := func(svc *data.Service, table string, record map[string]interface{}) {
		t.Helper()
		_, err := svc.InsertRow(ctx, table, record)
		require.NoError(t, err, "seeding %s", table)
	}

	insert(f.dispatch.dataSvc, "surveys", map[string]interface{}{
		"survey_code": "SV-001", "title": frenchSurveyTitle,
		"issued_on": "2026-03-04", "quota": exactScore,
	})
	insert(f.dispatch.dataSvc, "surveys", map[string]interface{}{
		"survey_code": "SV-002", "title": "Enquête de suivi",
		"issued_on": "2026-04-03", "quota": "40",
	})

	insert(f.dispatch.dataSvc, "answers", map[string]interface{}{
		"answer_code": "AN-001", "survey_code": "SV-001",
		"respondent": frenchRespondent, "score_text": exactScore,
		"attachment": containerValue,
	})
	insert(f.dispatch.dataSvc, "answers", map[string]interface{}{
		"answer_code": "AN-002", "survey_code": "SV-001",
		"respondent": "Jean Dupont", "score_text": "7",
	})
	// The rejected row: a score nobody filled in, written as words. The
	// destination field is a number, and "non renseigné" is not one.
	insert(f.dispatch.dataSvc, "answers", map[string]interface{}{
		"answer_code": "AN-003", "survey_code": "SV-002",
		"respondent": "Camille Roy", "score_text": "non renseigné",
	})
}

// surveyTransfer sends the parents: Dispatch.surveys → Results.project_surveys.
func (f *fixture) surveyTransfer(t *testing.T, ctx context.Context) *schema.TransferDefinition {
	t.Helper()
	def, err := f.dispatch.schemaSvc.CreateTransfer(ctx, schema.TransferInput{
		Name:        "Send surveys",
		Source:      schema.TransferEnd{Table: "surveys"},
		Destination: schema.TransferEnd{Database: f.results.name, Table: "project_surveys"},
		Mappings: []schema.TransferMapping{
			{SourceField: "survey_code", DestinationField: "code"},
			{SourceField: "title", DestinationField: "label"},
			{SourceField: "issued_on", DestinationField: "issued_on"},
			{SourceField: "quota", DestinationField: "quota"},
		},
		MatchFields: []string{"code"},
		Mode:        schema.TransferModeUpdateMatching,
	}, "dispatch-owner")
	require.NoError(t, err)
	return def
}

// answerTransfer sends the children. onRejected decides whether one row the
// destination cannot hold refuses the whole transfer or is quarantined.
func (f *fixture) answerTransfer(t *testing.T, ctx context.Context, name, onRejected string) *schema.TransferDefinition {
	t.Helper()
	def, err := f.dispatch.schemaSvc.CreateTransfer(ctx, schema.TransferInput{
		Name:        name,
		Source:      schema.TransferEnd{Table: "answers"},
		Destination: schema.TransferEnd{Database: f.results.name, Table: "project_answers"},
		Mappings: []schema.TransferMapping{
			{SourceField: "answer_code", DestinationField: "code"},
			{SourceField: "survey_code", DestinationField: "survey_code"},
			{SourceField: "respondent", DestinationField: "respondent"},
			{SourceField: "score_text", DestinationField: "score"},
			{SourceField: "attachment", DestinationField: "attachment"},
		},
		MatchFields: []string{"code"},
		Mode:        schema.TransferModeUpdateMatching,
		OnRejected:  onRejected,
		Sort:        []data.SortField{{Field: "answer_code"}},
	}, "dispatch-owner")
	require.NoError(t, err)
	return def
}

func (f *fixture) run(ctx context.Context, def *schema.TransferDefinition) (*transfer.Report, error) {
	return transfer.Run(ctx, def,
		f.dispatch.driver, f.results.driver, f.dispatch.name, f.results.name)
}

func rows(t *testing.T, ctx context.Context, svc *data.Service, table string) []map[string]interface{} {
	t.Helper()
	got, err := svc.ListRows(ctx, table, data.QueryOptions{Limit: 100, SortBy: "id", SortAsc: true})
	require.NoError(t, err)
	return got
}

// byKey indexes records by one field, which is how the assertions ask "what
// does the record with this business key now hold?".
func byKey(records []map[string]interface{}, field string) map[string]map[string]interface{} {
	indexed := map[string]map[string]interface{}{}
	for _, record := range records {
		indexed[fmt.Sprint(record[field])] = record
	}
	return indexed
}

func TestRun_TheAcceptanceFixtureOfIssue96(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newFixture(t, ctx, mgr)

	// ─── 1. The parents cross first ───────────────────────────────────────
	surveys := f.surveyTransfer(t, ctx)
	report, err := f.run(ctx, surveys)
	require.NoError(t, err)
	assert.True(t, report.Committed)
	assert.Equal(t, 2, report.Rows)
	assert.Equal(t, 2, report.Added)
	assert.Equal(t, 0, report.Updated)
	assert.Empty(t, report.Rejected)

	stored := byKey(rows(t, ctx, f.results.dataSvc, "project_surveys"), "code")
	require.Len(t, stored, 2)
	// French text, the exact decimal and the date all arrive as they were.
	assert.Equal(t, frenchSurveyTitle, stored["SV-001"]["label"])
	assert.Equal(t, exactScore, fmt.Sprint(stored["SV-001"]["quota"]))

	// ─── 2. A rejected child refuses the whole transfer ───────────────────
	strict := f.answerTransfer(t, ctx, "Send answers (strict)", schema.TransferRejectedAbort)
	report, err = f.run(ctx, strict)
	require.ErrorIs(t, err, transfer.ErrRejected)
	assert.False(t, report.Committed, "an aborted transfer writes nothing")
	assert.Equal(t, 3, report.Rows)
	assert.Equal(t, 0, report.Added)
	require.Len(t, report.Rejected, 1)
	rejected := report.Rejected[0]
	assert.Equal(t, 3, rejected.Row, "the third answer, in the sorted source order")
	assert.Equal(t, "score_text", rejected.SourceField)
	assert.Equal(t, "score", rejected.DestinationField)
	assert.Equal(t, "non renseigné", rejected.Value)
	assert.Contains(t, rejected.Reason, "not a number")

	// The destination is exactly as it was: the documented transaction
	// outcome is all-or-nothing, so the two good answers are not there
	// either, and the transfer can be replayed once the source is fixed.
	assert.Empty(t, rows(t, ctx, f.results.dataSvc, "project_answers"))

	// ─── 3. Quarantining the rejected row commits the rest ────────────────
	lenient := f.answerTransfer(t, ctx, "Send answers", schema.TransferRejectedSkip)
	report, err = f.run(ctx, lenient)
	require.NoError(t, err)
	assert.True(t, report.Committed)
	assert.Equal(t, 3, report.Rows)
	assert.Equal(t, 2, report.Added)
	assert.Equal(t, 0, report.Updated)
	require.Len(t, report.Rejected, 1)
	assert.Equal(t, "non renseigné", report.Rejected[0].Value)

	answers := byKey(rows(t, ctx, f.results.dataSvc, "project_answers"), "code")
	require.Len(t, answers, 2)
	assert.NotContains(t, answers, "AN-003", "the rejected answer stayed behind")
	assert.Equal(t, frenchRespondent, answers["AN-001"]["respondent"])
	assert.Equal(t, exactScore, fmt.Sprint(answers["AN-001"]["score"]))
	assert.Equal(t, containerValue, answers["AN-001"]["attachment"],
		"a container crosses as the same bytes")

	// No child is orphaned: every answer that arrived names a survey that
	// arrived with it.
	parents := byKey(rows(t, ctx, f.results.dataSvc, "project_surveys"), "code")
	for code, answer := range answers {
		_, ok := parents[fmt.Sprint(answer["survey_code"])]
		assert.True(t, ok, "answer %s names a survey that is here", code)
	}

	// ─── 4. A result is modified and returned ─────────────────────────────
	// The project fills in a note on one answer and sends it back by the same
	// business key. This is the leg that updates the central record.
	results := rows(t, ctx, f.results.dataSvc, "project_answers")
	var modified string
	for _, record := range results {
		if fmt.Sprint(record["code"]) == "AN-001" {
			modified = fmt.Sprint(record["id"])
		}
	}
	require.NotEmpty(t, modified)
	_, err = f.results.dataSvc.UpdateRow(ctx, "project_answers", modified,
		map[string]interface{}{"result_note": "Réponse validée", "score": "13.25"})
	require.NoError(t, err)

	returnDef, err := f.results.schemaSvc.CreateTransfer(ctx, schema.TransferInput{
		Name:        "Return answers",
		Source:      schema.TransferEnd{Table: "project_answers"},
		Destination: schema.TransferEnd{Database: f.dispatch.name, Table: "answers"},
		Mappings: []schema.TransferMapping{
			{SourceField: "code", DestinationField: "answer_code"},
			{SourceField: "result_note", DestinationField: "result_note"},
			{SourceField: "score", DestinationField: "score_text"},
		},
		MatchFields: []string{"answer_code"},
		Mode:        schema.TransferModeUpdateMatching,
		// A result that matches no central answer is not invented here: the
		// central solution owns which answers exist.
		OnUnmatched: schema.TransferUnmatchedSkip,
	}, "results-owner")
	require.NoError(t, err)

	report, err = transfer.Run(ctx, returnDef,
		f.results.driver, f.dispatch.driver, f.results.name, f.dispatch.name)
	require.NoError(t, err)
	assert.True(t, report.Committed)
	assert.Equal(t, 2, report.Updated, "both returned answers match a central record")
	assert.Equal(t, 0, report.Added, "nothing is invented in the central solution")

	central := byKey(rows(t, ctx, f.dispatch.dataSvc, "answers"), "answer_code")
	require.Len(t, central, 3, "the central answers are still the three that existed")
	assert.Equal(t, "Réponse validée", central["AN-001"]["result_note"])
	assert.Equal(t, "13.25", fmt.Sprint(central["AN-001"]["score_text"]))
	assert.Nil(t, central["AN-003"]["result_note"], "the rejected answer never left")

	// ─── 5. The transfer is replayed ──────────────────────────────────────
	// Running the same transfers again must change nothing structural: the
	// parents are updated in place, no second parent appears, and no answer
	// is orphaned.
	report, err = f.run(ctx, surveys)
	require.NoError(t, err)
	assert.Equal(t, 0, report.Added, "a replay adds no parent")
	assert.Equal(t, 2, report.Updated)

	report, err = f.run(ctx, lenient)
	require.NoError(t, err)
	assert.True(t, report.Committed)
	assert.Equal(t, 0, report.Added, "a replay adds no child")
	assert.Equal(t, 2, report.Updated)
	require.Len(t, report.Rejected, 1, "the same row is still rejected, and still only reported")

	assert.Len(t, rows(t, ctx, f.results.dataSvc, "project_surveys"), 2)
	assert.Len(t, rows(t, ctx, f.results.dataSvc, "project_answers"), 2)

	// The replay re-sent Dispatch's own values, and Dispatch's score is now
	// the 13.25 the project returned in step 4. So the number has gone
	// project → central → project and come back bit for bit: a replay is
	// idempotent in the values as well as in the record count.
	replayed := byKey(rows(t, ctx, f.results.dataSvc, "project_answers"), "code")
	assert.Equal(t, "13.25", fmt.Sprint(replayed["AN-001"]["score"]),
		"the replay sent the central value, which is the one the project returned")
	assert.Equal(t, frenchRespondent, replayed["AN-001"]["respondent"],
		"French text survives every leg of the round trip")
	assert.Equal(t, containerValue, replayed["AN-001"]["attachment"],
		"so does the container")
}

func TestRun_SkipsTheRecordsThatMatchNothingWhenToldTo(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newFixture(t, ctx, mgr)

	def, err := f.dispatch.schemaSvc.CreateTransfer(ctx, schema.TransferInput{
		Name:        "Update existing surveys only",
		Source:      schema.TransferEnd{Table: "surveys"},
		Destination: schema.TransferEnd{Database: f.results.name, Table: "project_surveys"},
		Mappings: []schema.TransferMapping{
			{SourceField: "survey_code", DestinationField: "code"},
			{SourceField: "title", DestinationField: "label"},
		},
		MatchFields: []string{"code"},
		Mode:        schema.TransferModeUpdateMatching,
		OnUnmatched: schema.TransferUnmatchedSkip,
	}, "")
	require.NoError(t, err)

	// Nothing is over there yet, so every record is skipped and the
	// destination stays empty.
	report, err := f.run(ctx, def)
	require.NoError(t, err)
	assert.True(t, report.Committed)
	assert.Equal(t, 2, report.Rows)
	assert.Equal(t, 0, report.Added)
	assert.Equal(t, 2, report.Skipped)
	assert.Empty(t, rows(t, ctx, f.results.dataSvc, "project_surveys"))
}

func TestRun_SendsOnlyTheFoundSet(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newFixture(t, ctx, mgr)

	def, err := f.dispatch.schemaSvc.CreateTransfer(ctx, schema.TransferInput{
		Name:        "Send one survey",
		Source:      schema.TransferEnd{Table: "surveys"},
		Destination: schema.TransferEnd{Database: f.results.name, Table: "project_surveys"},
		FoundSet: []data.FindRequest{{Criteria: []data.FindCriterion{
			{FieldName: "survey_code", Operator: "=", Value: "SV-002"},
		}}},
		Mappings: []schema.TransferMapping{
			{SourceField: "survey_code", DestinationField: "code"},
			{SourceField: "title", DestinationField: "label"},
		},
		MatchFields: []string{"code"},
		Mode:        schema.TransferModeUpdateMatching,
	}, "")
	require.NoError(t, err)

	report, err := f.run(ctx, def)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Rows)
	assert.Equal(t, 1, report.Added)
	stored := rows(t, ctx, f.results.dataSvc, "project_surveys")
	require.Len(t, stored, 1)
	assert.Equal(t, "SV-002", stored[0]["code"])
}

func TestRun_AddModeAddsEveryRecordEveryTime(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newFixture(t, ctx, mgr)

	def, err := f.dispatch.schemaSvc.CreateTransfer(ctx, schema.TransferInput{
		Name:        "Append surveys",
		Source:      schema.TransferEnd{Table: "surveys"},
		Destination: schema.TransferEnd{Database: f.results.name, Table: "project_surveys"},
		Mappings: []schema.TransferMapping{
			{SourceField: "survey_code", DestinationField: "code"},
			{SourceField: "title", DestinationField: "label"},
		},
		Mode: schema.TransferModeAdd,
	}, "")
	require.NoError(t, err)

	for run := 1; run <= 2; run++ {
		report, err := f.run(ctx, def)
		require.NoError(t, err)
		assert.Equal(t, 2, report.Added)
		assert.Equal(t, 0, report.Updated)
		// "add" is the mode that is deliberately not replayable: it is here
		// for an append-only log, and the parity matrix says so.
		assert.Len(t, rows(t, ctx, f.results.dataSvc, "project_surveys"), 2*run)
	}
}

func TestRun_RefusesAMappingNeitherTableHas(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newFixture(t, ctx, mgr)

	cases := []struct {
		why      string
		mappings []schema.TransferMapping
		keys     []string
	}{
		{
			why: "a source field the source table has not",
			mappings: []schema.TransferMapping{
				{SourceField: "survey_code", DestinationField: "code"},
				{SourceField: "headline", DestinationField: "label"},
			},
			keys: []string{"code"},
		},
		{
			why: "a destination field the destination table has not",
			mappings: []schema.TransferMapping{
				{SourceField: "survey_code", DestinationField: "code"},
				{SourceField: "title", DestinationField: "headline"},
			},
			keys: []string{"code"},
		},
	}

	for i, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			def, err := f.dispatch.schemaSvc.CreateTransfer(ctx, schema.TransferInput{
				Name:        fmt.Sprintf("Unresolved %d", i),
				Source:      schema.TransferEnd{Table: "surveys"},
				Destination: schema.TransferEnd{Database: f.results.name, Table: "project_surveys"},
				Mappings:    tc.mappings,
				MatchFields: tc.keys,
				Mode:        schema.TransferModeUpdateMatching,
			}, "")
			require.NoError(t, err, "the definition saves: the far end is only resolved at run time")

			report, err := f.run(ctx, def)
			require.ErrorIs(t, err, transfer.ErrTransfer)
			assert.False(t, report.Committed)
			assert.Empty(t, rows(t, ctx, f.results.dataSvc, "project_surveys"),
				"an unresolved mapping blocks before anything is written")
		})
	}
}

func TestRun_RefusesATableNeitherSolutionHas(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newFixture(t, ctx, mgr)

	def, err := f.dispatch.schemaSvc.CreateTransfer(ctx, schema.TransferInput{
		Name:        "Missing destination table",
		Source:      schema.TransferEnd{Table: "surveys"},
		Destination: schema.TransferEnd{Database: f.results.name, Table: "nowhere"},
		Mappings: []schema.TransferMapping{
			{SourceField: "survey_code", DestinationField: "code"},
		},
		MatchFields: []string{"code"},
		Mode:        schema.TransferModeUpdateMatching,
	}, "")
	require.NoError(t, err)

	report, err := f.run(ctx, def)
	require.ErrorIs(t, err, transfer.ErrTransfer)
	assert.False(t, report.Committed)
	assert.Contains(t, err.Error(), "nowhere")
}

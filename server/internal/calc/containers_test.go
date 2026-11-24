package calc

import "testing"

func containerContext() *RecordContext {
	return &RecordContext{
		Record: Record{
			"scan": Container{Table: "assets", Record: "r1", Field: "scan", Size: 2048,
				Describe: func(c *Container) {
					c.Filename, c.MimeType = "plan.pdf", "application/pdf"
				}},
			"scan[2]": &Container{Table: "assets", Record: "r1", Field: "scan[2]", Filename: "back.png", Size: 9},
			"label":   textValue("cover"),
		},
		Repeats: map[string]int{"scan": 2},
	}
}

func containerAnswer(t *testing.T, formula string, result ResultType) any {
	t.Helper()
	expr, err := Parse(formula)
	if err != nil {
		t.Fatalf("%s: %v", formula, err)
	}
	v, err := EvaluateWith(expr, containerContext(), result)
	if err != nil {
		t.Fatalf("%s: %v", formula, err)
	}
	return v
}

func TestContainerValues(t *testing.T) {
	for formula, want := range map[string]string{
		`GetAsText ( scan )`:                               "plan.pdf",
		`scan & "|" & scan[2]`:                             "plan.pdf|back.png",
		`IsEmpty ( scan )`:                                 "0",
		`IsEmpty ( nothing )`:                              "1",
		`GetContainerAttribute ( scan ; "filename" )`:      "plan.pdf",
		`GetContainerAttribute ( scan ; "fileSize" )`:      "2048",
		`GetContainerAttribute ( scan[2] ; "FILESIZE" )`:   "9",
		`GetContainerAttribute ( scan ; "width" )`:         "",
		`GetContainerAttribute ( label ; "filename" )`:     "",
		`GetContainerAttribute ( nothing ; "fileSize" )`:   "",
		`Length ( scan )`:                                  "8",
		`GetAsText ( GetRepetition ( scan ; 2 ) )`:         "back.png",
		`GetAsText ( If ( label = "cover" ; scan ; "" ) )`: "plan.pdf",
	} {
		got := containerAnswer(t, formula, ResultText)
		if got == nil {
			got = ""
		}
		if got != want {
			t.Errorf("%s = %q, want %q", formula, got, want)
		}
	}
}

func TestContainerResults(t *testing.T) {
	cases := map[string]string{
		`scan`: "scan",
		`If ( label = "cover" ; scan[2] ; scan )`: "scan[2]",
		`Case ( label = "x" ; scan ; scan[2] )`:   "scan[2]",
		`GetRepetition ( scan ; 2 )`:              "scan[2]",
		`Let ( f = scan ; f )`:                    "scan",
		`Choose ( 1 ; scan ; scan[2] )`:           "scan[2]",
		`scan[2]`:                                 "scan[2]",
	}
	for formula, field := range cases {
		got, ok := containerAnswer(t, formula, ResultContainer).(Container)
		if !ok || got.Table != "assets" || got.Record != "r1" || got.Field != field {
			t.Errorf("%s = %#v, want the container at %s", formula, got, field)
		}
		if got.Describe != nil {
			t.Errorf("%s: an answer carries no loader", formula)
		}
	}
	if got := containerAnswer(t, `scan`, ResultContainer).(Container); got.Filename != "plan.pdf" || got.MimeType != "application/pdf" || got.Size != 2048 {
		t.Errorf("the answer keeps what is known of the file: %#v", got)
	}
	// Text is not a file: a container result of text is empty.
	for _, formula := range []string{`"plan.pdf"`, `label`, `GetAsText ( scan )`, `""`} {
		if got := containerAnswer(t, formula, ResultContainer); got != nil {
			t.Errorf("%s = %#v, want nothing", formula, got)
		}
	}
	if ParseResultType("Container") != ResultContainer || ParseResultType("binary") != ResultContainer {
		t.Error("Container result types")
	}
}

func TestContainerIsDescribedOnlyWhenRead(t *testing.T) {
	calls := 0
	ctx := &RecordContext{Record: Record{
		"scan": Container{Table: "assets", Record: "r1", Field: "scan", Describe: func(c *Container) { calls++; c.Filename = "a.txt" }},
	}}
	expr, _ := Parse(`"no container here"`)
	if _, err := EvaluateWith(expr, ctx, ResultText); err != nil || calls != 0 {
		t.Fatalf("a formula that does not read the container described it %d times (%v)", calls, err)
	}
	expr, _ = Parse(`scan & scan`)
	if v, err := EvaluateWith(expr, ctx, ResultText); err != nil || v != "a.txta.txt" {
		t.Fatalf("got %v, %v", v, err)
	}
}

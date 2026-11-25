package schema

import "testing"

// A data source reads its password only from a variable with the data source
// prefix, so a database owner cannot have the server send another of its
// secrets to a host they chose, even through a source stored before the
// prefix was required or imported from a solution file.
func TestDataSourcePasswordReadsOnlyPrefixedVariables(t *testing.T) {
	t.Setenv("FILE4BASE_DATASOURCE_TEST_PW", "source-secret")
	t.Setenv("F4B_TEST_SERVER_SECRET", "server-secret")

	if got := (DataSource{PasswordEnv: "FILE4BASE_DATASOURCE_TEST_PW"}).Password(""); got != "source-secret" {
		t.Fatalf("prefixed variable: got %q", got)
	}
	if got := (DataSource{PasswordEnv: "F4B_TEST_SERVER_SECRET"}).Password(""); got != "" {
		t.Fatalf("a variable without the prefix was read")
	}
	if got := (DataSource{PasswordEnv: "F4B_TEST_SERVER_SECRET"}).Password("typed"); got != "typed" {
		t.Fatalf("a typed password must win: got %q", got)
	}

	if _, err := checkDataSource(DataSourceInput{Name: "x", Engine: "postgres", Host: "h", PasswordEnv: "DATABASE_URL"}); err == nil {
		t.Fatal("a variable without the prefix was accepted")
	}
	if _, err := checkDataSource(DataSourceInput{Name: "x", Engine: "postgres", Host: "h", PasswordEnv: "FILE4BASE_DATASOURCE_LEGACY"}); err != nil {
		t.Fatalf("a prefixed variable was refused: %v", err)
	}
}

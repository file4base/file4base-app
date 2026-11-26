package dbal

// CatalogColumnsSQL is a subquery of the physical columns of the database a
// connection works in, with the columns table_name, column_name,
// is_nullable ('YES' or 'NO') and column_default, as
// information_schema.columns gives them.
// SQLite has no information_schema: its tables and their pragma_table_info
// give the same rows.
func CatalogColumnsSQL(engine EngineType) string {
	switch engine {
	case EngineSQLite:
		return `(SELECT m.name AS table_name, p.name AS column_name, ` +
			`CASE WHEN p."notnull" THEN 'NO' ELSE 'YES' END AS is_nullable, p.dflt_value AS column_default ` +
			`FROM sqlite_schema AS m JOIN pragma_table_info(m.name) AS p ` +
			`WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite\_%' ESCAPE '\')`
	case EngineMariaDB:
		return `(SELECT table_name, column_name, is_nullable, column_default FROM information_schema.columns WHERE table_schema = DATABASE())`
	default:
		return `(SELECT table_name, column_name, is_nullable, column_default FROM information_schema.columns WHERE table_schema = current_schema())`
	}
}

// CatalogTablesSQL is a subquery of the physical tables of the database a
// connection works in, with the column table_name.
func CatalogTablesSQL(engine EngineType) string {
	switch engine {
	case EngineSQLite:
		return `(SELECT name AS table_name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\')`
	case EngineMariaDB:
		return `(SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE())`
	default:
		return `(SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema())`
	}
}

// RowLockClause locks the rows a SELECT in a transaction reads until it
// ends, so a read-then-write cannot race another one. On PostgreSQL the lock
// does not block the foreign keys that point at the rows. SQLite has no row
// locks and needs none: a write transaction holds the database's only
// writer lock from its BEGIN (IMMEDIATE) on.
func RowLockClause(engine EngineType) string {
	switch engine {
	case EnginePostgres:
		return " FOR NO KEY UPDATE"
	case EngineSQLite:
		return ""
	}
	return " FOR UPDATE"
}

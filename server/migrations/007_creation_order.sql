-- File4Base Migration 007: records in creation order
-- (PostgreSQL; the MariaDB statement is given below)
--
-- Every user table numbers its records in the order they are created, in a
-- column "f4b$creation_order" the engine fills (an identity on PostgreSQL,
-- AUTO_INCREMENT on MariaDB). It is not a field: it is not in sys_columns, no
-- client reads or writes it, and "$" is in no field name. It orders records
-- the way FileMaker lists them without a sort, by record ID: a found set, the
-- records of a relationship without "Sort records", and First, Last and
-- GetNthRecord across one; and it breaks the ties of a sort.
--
-- A table given the column numbers the records it holds in the order the
-- engine reads them: their physical order on PostgreSQL, close to the order
-- they were inserted in (a migration inserts a FileMaker file's records in
-- its record order), and their id order on MariaDB, whose tables are stored
-- in primary key order.
--
-- New tables are created with the column. The server gives it to the tables
-- of an existing catalog itself (schema.EnsureSystemTables, at sign-in and
-- on restore), reading the catalog first so an upgraded one is only read;
-- until then a table's records are ordered by id, as before.

DO $$
DECLARE
	t RECORD;
BEGIN
	FOR t IN
		SELECT s.name FROM sys_tables s
		WHERE EXISTS (SELECT 1 FROM information_schema.tables i
			WHERE i.table_schema = current_schema() AND i.table_name = s.name)
		AND NOT EXISTS (SELECT 1 FROM information_schema.columns c
			WHERE c.table_schema = current_schema() AND c.table_name = s.name
			AND c.column_name = 'f4b$creation_order')
	LOOP
		EXECUTE format('ALTER TABLE %I ADD COLUMN "f4b$creation_order" BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE', t.name);
	END LOOP;
END $$;

-- MariaDB, for each user table <name> without the column:
--
--   ALTER TABLE `<name>` ADD COLUMN `f4b$creation_order` BIGINT NOT NULL AUTO_INCREMENT UNIQUE;

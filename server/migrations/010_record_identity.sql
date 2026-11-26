-- File4Base Migration 010: record identity
-- (PostgreSQL; the MariaDB statements are given below)
--
-- A record has a record ID and a modification count, as FileMaker's do: the
-- Data API's recordId and modId, which Get ( RecordID ) and
-- Get ( RecordModificationCount ) answer.
--
-- The record ID is the creation order column of migration 007,
-- "f4b$creation_order": ascending, per table, and never given twice. The
-- server takes every new record's from the table's allocator before writing
-- it, so a create that fails spends its ID, as in FileMaker: on PostgreSQL
-- the column's identity sequence, on MariaDB a row of sys_record_ids.
--
-- The modification count is a column "f4b$mod_count", from 0, which every
-- write that changes a field stored with the record raises by one. Like the
-- creation order it is not a field: it is not in sys_columns, and "$" is in
-- no field name. A table given it counts no modification yet for the records
-- it holds, as FileMaker counts none for a migrated record; adding it does
-- not rewrite the table (PostgreSQL 11 and later, MariaDB 10.3 and later).
--
-- New tables are created with the column. The server gives it to the tables
-- of an existing catalog itself (schema.EnsureSystemTables, at sign-in and
-- on restore), reading the catalog first so an upgraded one is only read.

-- Every table is altered in one transaction: a table another session holds
-- would keep those altered before it locked while it waits. The wait is
-- bounded, and a run that times out changes nothing and can be run again.
SET lock_timeout = '5s';

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
			AND c.column_name = 'f4b$mod_count')
	LOOP
		EXECUTE format('ALTER TABLE %I ADD COLUMN "f4b$mod_count" BIGINT NOT NULL DEFAULT 0', t.name);
	END LOOP;
END $$;

RESET lock_timeout;

-- MariaDB:
--
--   CREATE TABLE IF NOT EXISTS sys_record_ids (
--       table_name VARCHAR(64) PRIMARY KEY,
--       last_id BIGINT NOT NULL
--   );
--
-- and, for each user table <name> without the column:
--
--   ALTER TABLE `<name>` WAIT 5 ADD COLUMN `f4b$mod_count` BIGINT NOT NULL DEFAULT 0, ALGORITHM=INSTANT;
--
-- (a table that cannot take the column instantly, a compressed one, refuses
-- it: add it without ALGORITHM=INSTANT in a maintenance window, as that
-- rebuilds the table)
--
-- A table's row of sys_record_ids is created the first time one of its
-- record IDs is taken, past every ID the table holds and every one its
-- AUTO_INCREMENT counter has given.

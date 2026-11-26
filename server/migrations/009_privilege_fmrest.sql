-- File4Base System Catalog Migration 009 (PostgreSQL & MariaDB)
--
-- Access through the FileMaker Data API at /fmi/data (#208), granted per
-- account role like the bulk capabilities of #39. It is FileMaker's fmrest
-- extended privilege.
--
--   sys_privileges.fmrest   whether accounts of the role may sign in to the
--                           Data API. FALSE for every role already stored, as
--                           for a role with no row: the Data API is a second
--                           way in, which no account gains by an upgrade. An
--                           owner holds it whatever is stored.
--
-- The server applies this itself (schema.EnsureSystemTables).

ALTER TABLE sys_privileges ADD COLUMN IF NOT EXISTS fmrest BOOLEAN NOT NULL DEFAULT FALSE;

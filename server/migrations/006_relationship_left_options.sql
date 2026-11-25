-- File4Base System Catalog Migration 006 (PostgreSQL & MariaDB)
--
-- Relationship options per side. In FileMaker each side of a relationship has
-- its own "Allow creation of records in this table via this relationship",
-- "Delete related records in this table when a record is deleted in the other
-- table" and "Sort records", each acting on the side it is set on.
--
--   sys_relationships.allow_creation,       the right side's options, as
--   sys_relationships.cascade_delete,       before: records of the right side
--   sys_relationships.sort_related          are created from, deleted with
--                                           and sorted for a record of the
--                                           left side.
--   sys_relationships.left_allow_creation,  the left side's, acting the other
--   sys_relationships.left_cascade_delete,  way round.
--   sys_relationships.left_sort_related
--
-- An existing relationship gets no option on its left side, so it creates,
-- deletes and sorts exactly as before.
--
-- The server applies this itself (schema.EnsureSystemTables, and the first
-- time a relationship is read or written on an older database), reading the
-- catalog first so an upgraded catalog is only read.

ALTER TABLE sys_relationships ADD COLUMN IF NOT EXISTS left_allow_creation BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE sys_relationships ADD COLUMN IF NOT EXISTS left_cascade_delete BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE sys_relationships ADD COLUMN IF NOT EXISTS left_sort_related TEXT;

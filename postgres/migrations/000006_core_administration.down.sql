DROP FUNCTION IF EXISTS amsonia.inspect_invitation(BYTEA);
DROP INDEX IF EXISTS amsonia.idx_amsonia_access_session_family;
DROP INDEX IF EXISTS amsonia.idx_amsonia_refresh_session_family;
ALTER TABLE amsonia.access_sessions DROP COLUMN session_id;
ALTER TABLE amsonia.refresh_sessions DROP COLUMN session_id;
ALTER TABLE amsonia.accounts DROP COLUMN display_name;

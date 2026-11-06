-- Daily account administration and stable session families.
ALTER TABLE amsonia.accounts ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE amsonia.access_sessions ADD COLUMN session_id TEXT;
ALTER TABLE amsonia.refresh_sessions ADD COLUMN session_id TEXT;

UPDATE amsonia.refresh_sessions
SET session_id = 'ses_' || encode(public.gen_random_bytes(18), 'hex');
UPDATE amsonia.access_sessions a
SET session_id = COALESCE(
    (SELECT r.session_id FROM amsonia.refresh_sessions r
      WHERE r.account_id = a.account_id AND r.created_at = a.created_at
      ORDER BY r.token_hash LIMIT 1),
    'ses_' || encode(public.gen_random_bytes(18), 'hex')
);
ALTER TABLE amsonia.access_sessions ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE amsonia.refresh_sessions ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE amsonia.access_sessions ALTER COLUMN session_id
    SET DEFAULT 'ses_' || encode(public.gen_random_bytes(18), 'hex');
ALTER TABLE amsonia.refresh_sessions ALTER COLUMN session_id
    SET DEFAULT 'ses_' || encode(public.gen_random_bytes(18), 'hex');
CREATE INDEX idx_amsonia_access_session_family ON amsonia.access_sessions(account_id, session_id);
CREATE INDEX idx_amsonia_refresh_session_family ON amsonia.refresh_sessions(account_id, session_id);

-- A high-entropy invitation token is the proof authorizing this narrow read.
CREATE FUNCTION amsonia.inspect_invitation(p_token_hash BYTEA)
RETURNS TABLE(tenant_id TEXT, invitation_id TEXT, display_email TEXT,
              tenant_name TEXT, expires_at TIMESTAMPTZ, account_exists BOOLEAN)
LANGUAGE SQL STABLE SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
    SELECT i.tenant_id, i.invitation_id, i.display_email, t.name, i.expires_at,
           EXISTS (SELECT 1 FROM amsonia.accounts a WHERE a.normalized_email = i.normalized_email)
      FROM amsonia.member_invitations i
      JOIN amsonia.tenants t ON t.tenant_id = i.tenant_id
     WHERE i.token_hash = p_token_hash AND t.state = 'active'
       AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now()
$$;
REVOKE ALL ON FUNCTION amsonia.inspect_invitation(BYTEA) FROM PUBLIC;

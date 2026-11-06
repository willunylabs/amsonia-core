-- Account credentials and their recovery tokens are global, like sessions.
-- Tenant membership and authorization are rechecked in the issuing transaction.
CREATE TABLE amsonia.member_password_recoveries (
    recovery_id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES amsonia.accounts(account_id) ON DELETE CASCADE,
    issued_tenant_id TEXT NOT NULL REFERENCES amsonia.tenants(tenant_id),
    created_by TEXT NOT NULL REFERENCES amsonia.accounts(account_id),
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at),
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
REVOKE ALL ON TABLE amsonia.member_password_recoveries FROM PUBLIC;
CREATE INDEX member_password_recoveries_pending ON amsonia.member_password_recoveries(account_id)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;
CREATE INDEX member_password_recoveries_expiry ON amsonia.member_password_recoveries(expires_at, recovery_id);
CREATE INDEX access_sessions_expiry ON amsonia.access_sessions(expires_at, token_hash);
CREATE INDEX refresh_sessions_expiry ON amsonia.refresh_sessions(expires_at, token_hash);

-- Amsonia PostgreSQL schema v1
-- Self-contained: installs on an empty database, requires no host tables.
-- All tenant-owned tables are RLS-protected with FORCE ROW LEVEL SECURITY.

-- The application role must NOT be the table owner. The migration owner
-- creates tables and policies; the application role is granted DML.

CREATE SCHEMA IF NOT EXISTS amsonia;

-- ---------------------------------------------------------------------------
-- amsonia.roles
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS amsonia.roles (
    tenant_id      TEXT        NOT NULL,
    role_id        TEXT        NOT NULL,
    name           TEXT        NOT NULL,
    description    TEXT        NOT NULL DEFAULT '',
    version        BIGINT      NOT NULL DEFAULT 1,
    deleted        BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_amsonia_roles_tenant_name
    ON amsonia.roles (tenant_id, name);

-- ---------------------------------------------------------------------------
-- amsonia.role_permission_grants
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS amsonia.role_permission_grants (
    tenant_id       TEXT NOT NULL,
    role_id         TEXT NOT NULL,
    permission_key  TEXT NOT NULL,
    scope           TEXT NOT NULL,
    workspace_roles TEXT[] NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, role_id, permission_key, scope, workspace_roles),
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES amsonia.roles (tenant_id, role_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_amsonia_role_perm_tenant_role
    ON amsonia.role_permission_grants (tenant_id, role_id);

-- ---------------------------------------------------------------------------
-- amsonia.subject_roles
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS amsonia.subject_roles (
    tenant_id          TEXT        NOT NULL,
    subject_id         TEXT        NOT NULL,
    role_id            TEXT        NOT NULL,
    grantor_subject_id TEXT        NOT NULL DEFAULT '',
    provenance         TEXT        NOT NULL DEFAULT 'delegated',
    granted_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, subject_id, role_id),
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES amsonia.roles (tenant_id, role_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_amsonia_subject_roles_tenant_subject
    ON amsonia.subject_roles (tenant_id, subject_id);

CREATE INDEX IF NOT EXISTS idx_amsonia_subject_roles_tenant_role
    ON amsonia.subject_roles (tenant_id, role_id);

-- ---------------------------------------------------------------------------
-- amsonia.grant_edges
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS amsonia.grant_edges (
    tenant_id  TEXT        NOT NULL,
    grantor_id TEXT        NOT NULL,
    grantee_id TEXT        NOT NULL,
    role_id    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, grantor_id, grantee_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_amsonia_grant_edges_tenant_grantor
    ON amsonia.grant_edges (tenant_id, grantor_id);

CREATE INDEX IF NOT EXISTS idx_amsonia_grant_edges_tenant_grantee
    ON amsonia.grant_edges (tenant_id, grantee_id);

-- ---------------------------------------------------------------------------
-- amsonia.role_versions (immutable snapshots)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS amsonia.role_versions (
    tenant_id           TEXT        NOT NULL,
    role_id             TEXT        NOT NULL,
    version             BIGINT      NOT NULL,
    name                TEXT        NOT NULL,
    description         TEXT        NOT NULL DEFAULT '',
    grants              JSONB       NOT NULL DEFAULT '[]'::jsonb,
    deleted             BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by_subject  TEXT        NOT NULL DEFAULT '',
    bootstrap_initiator TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, role_id, version),
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES amsonia.roles (tenant_id, role_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_amsonia_role_versions_tenant_role
    ON amsonia.role_versions (tenant_id, role_id, version DESC);

-- ---------------------------------------------------------------------------
-- amsonia.audit_events (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS amsonia.audit_events (
    id              BIGSERIAL   PRIMARY KEY,
    tenant_id       TEXT        NOT NULL,
    actor_subject   TEXT        NOT NULL DEFAULT '',
    host_initiator  TEXT        NOT NULL DEFAULT '',
    operation       TEXT        NOT NULL,
    phase           TEXT        NOT NULL DEFAULT 'result',
    target_type     TEXT        NOT NULL,
    target_id       TEXT        NOT NULL,
    outcome         TEXT        NOT NULL,
    reason_code     TEXT        NOT NULL DEFAULT '',
    request_id      TEXT        NOT NULL DEFAULT '',
    role_version    BIGINT      NOT NULL DEFAULT 0,
    at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_amsonia_audit_tenant_time
    ON amsonia.audit_events (tenant_id, at DESC);

-- ---------------------------------------------------------------------------
-- amsonia.tenant_state (bootstrap marker + permanent purge tombstone)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS amsonia.tenant_state (
    tenant_id           TEXT        PRIMARY KEY,
    bootstrapped        BOOLEAN     NOT NULL DEFAULT FALSE,
    bootstrap_initiator TEXT        NOT NULL DEFAULT '',
    bootstrap_at        TIMESTAMPTZ,
    purged              BOOLEAN     NOT NULL DEFAULT FALSE,
    purged_at           TIMESTAMPTZ
);

-- ---------------------------------------------------------------------------
-- amsonia.purge_ledger (NOT removed by tenant purge)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS amsonia.purge_ledger (
    tenant_id      TEXT        NOT NULL,
    request_id     TEXT        NOT NULL,
    operation      TEXT        NOT NULL,
    host_initiator TEXT        NOT NULL DEFAULT '',
    reason_code    TEXT        NOT NULL DEFAULT '',
    committed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, request_id)
);

-- ---------------------------------------------------------------------------
-- RLS: tenant context via set_config('amsonia.tenant_id', ..., true)
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION amsonia.tenant_id()
RETURNS TEXT
LANGUAGE SQL
STABLE
AS $$
    SELECT NULLIF(current_setting('amsonia.tenant_id', TRUE), '')
$$;

CREATE OR REPLACE FUNCTION amsonia.set_tenant(p_tenant_id TEXT)
RETURNS VOID
LANGUAGE SQL
AS $$
    SELECT set_config('amsonia.tenant_id', p_tenant_id, TRUE)
$$;

-- amsonia.roles
ALTER TABLE amsonia.roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.roles FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_roles_select ON amsonia.roles;
DROP POLICY IF EXISTS p_roles_insert ON amsonia.roles;
DROP POLICY IF EXISTS p_roles_update ON amsonia.roles;
DROP POLICY IF EXISTS p_roles_delete ON amsonia.roles;
CREATE POLICY p_roles_select ON amsonia.roles
    FOR SELECT USING (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_roles_insert ON amsonia.roles
    FOR INSERT WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_roles_update ON amsonia.roles
    FOR UPDATE USING (amsonia.tenant_id() = tenant_id)
    WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_roles_delete ON amsonia.roles
    FOR DELETE USING (amsonia.tenant_id() = tenant_id);

-- amsonia.role_permission_grants
ALTER TABLE amsonia.role_permission_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.role_permission_grants FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_rpg_select ON amsonia.role_permission_grants;
DROP POLICY IF EXISTS p_rpg_insert ON amsonia.role_permission_grants;
DROP POLICY IF EXISTS p_rpg_update ON amsonia.role_permission_grants;
DROP POLICY IF EXISTS p_rpg_delete ON amsonia.role_permission_grants;
CREATE POLICY p_rpg_select ON amsonia.role_permission_grants
    FOR SELECT USING (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_rpg_insert ON amsonia.role_permission_grants
    FOR INSERT WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_rpg_update ON amsonia.role_permission_grants
    FOR UPDATE USING (amsonia.tenant_id() = tenant_id)
    WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_rpg_delete ON amsonia.role_permission_grants
    FOR DELETE USING (amsonia.tenant_id() = tenant_id);

-- amsonia.subject_roles
ALTER TABLE amsonia.subject_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.subject_roles FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_sr_select ON amsonia.subject_roles;
DROP POLICY IF EXISTS p_sr_insert ON amsonia.subject_roles;
DROP POLICY IF EXISTS p_sr_update ON amsonia.subject_roles;
DROP POLICY IF EXISTS p_sr_delete ON amsonia.subject_roles;
CREATE POLICY p_sr_select ON amsonia.subject_roles
    FOR SELECT USING (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_sr_insert ON amsonia.subject_roles
    FOR INSERT WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_sr_update ON amsonia.subject_roles
    FOR UPDATE USING (amsonia.tenant_id() = tenant_id)
    WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_sr_delete ON amsonia.subject_roles
    FOR DELETE USING (amsonia.tenant_id() = tenant_id);

-- amsonia.grant_edges
ALTER TABLE amsonia.grant_edges ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.grant_edges FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_ge_select ON amsonia.grant_edges;
DROP POLICY IF EXISTS p_ge_insert ON amsonia.grant_edges;
DROP POLICY IF EXISTS p_ge_update ON amsonia.grant_edges;
DROP POLICY IF EXISTS p_ge_delete ON amsonia.grant_edges;
CREATE POLICY p_ge_select ON amsonia.grant_edges
    FOR SELECT USING (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_ge_insert ON amsonia.grant_edges
    FOR INSERT WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_ge_update ON amsonia.grant_edges
    FOR UPDATE USING (amsonia.tenant_id() = tenant_id)
    WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_ge_delete ON amsonia.grant_edges
    FOR DELETE USING (amsonia.tenant_id() = tenant_id);

-- amsonia.role_versions
ALTER TABLE amsonia.role_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.role_versions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_rv_select ON amsonia.role_versions;
DROP POLICY IF EXISTS p_rv_insert ON amsonia.role_versions;
DROP POLICY IF EXISTS p_rv_update ON amsonia.role_versions;
DROP POLICY IF EXISTS p_rv_delete ON amsonia.role_versions;
CREATE POLICY p_rv_select ON amsonia.role_versions
    FOR SELECT USING (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_rv_insert ON amsonia.role_versions
    FOR INSERT WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_rv_update ON amsonia.role_versions
    FOR UPDATE USING (amsonia.tenant_id() = tenant_id)
    WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_rv_delete ON amsonia.role_versions
    FOR DELETE USING (amsonia.tenant_id() = tenant_id);

-- amsonia.audit_events
ALTER TABLE amsonia.audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.audit_events FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_ae_select ON amsonia.audit_events;
DROP POLICY IF EXISTS p_ae_insert ON amsonia.audit_events;
CREATE POLICY p_ae_select ON amsonia.audit_events
    FOR SELECT USING (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_ae_insert ON amsonia.audit_events
    FOR INSERT WITH CHECK (amsonia.tenant_id() = tenant_id);

-- amsonia.tenant_state
ALTER TABLE amsonia.tenant_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.tenant_state FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_ts_select ON amsonia.tenant_state;
DROP POLICY IF EXISTS p_ts_insert ON amsonia.tenant_state;
DROP POLICY IF EXISTS p_ts_update ON amsonia.tenant_state;
DROP POLICY IF EXISTS p_ts_delete ON amsonia.tenant_state;
CREATE POLICY p_ts_select ON amsonia.tenant_state
    FOR SELECT USING (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_ts_insert ON amsonia.tenant_state
    FOR INSERT WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_ts_update ON amsonia.tenant_state
    FOR UPDATE USING (amsonia.tenant_id() = tenant_id)
    WITH CHECK (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_ts_delete ON amsonia.tenant_state
    FOR DELETE USING (amsonia.tenant_id() = tenant_id);

-- amsonia.purge_ledger: intentionally NOT RLS-protected by tenant predicate
-- alone; it is only accessed through the maintenance adapter boundary.
-- It must survive tenant purge, so it is excluded from purge DML.

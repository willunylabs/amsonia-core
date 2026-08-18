-- Example host-application table protected by Amsonia's signed tenant context.
-- Apply as the table owner after the Amsonia migrations and role provisioning:
--
--   psql "$AMSONIA_MIGRATION_DSN" \
--     --set=runtime_role=amsonia_runtime \
--     --file=examples/business-data-rls/schema.sql

\set ON_ERROR_STOP on

BEGIN;

CREATE SCHEMA IF NOT EXISTS app;
REVOKE ALL ON SCHEMA app FROM PUBLIC;

CREATE TABLE IF NOT EXISTS app.invoices (
    tenant_id   TEXT        NOT NULL,
    invoice_id  TEXT        NOT NULL,
    description TEXT        NOT NULL,
    amount_cents BIGINT     NOT NULL CHECK (amount_cents >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, invoice_id)
);

ALTER TABLE app.invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.invoices FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS invoice_tenant_isolation ON app.invoices;
CREATE POLICY invoice_tenant_isolation ON app.invoices
    USING (tenant_id = amsonia.tenant_id())
    WITH CHECK (tenant_id = amsonia.tenant_id());

REVOKE ALL ON app.invoices FROM PUBLIC;
GRANT USAGE ON SCHEMA app TO :"runtime_role";
GRANT SELECT, INSERT, UPDATE, DELETE ON app.invoices TO :"runtime_role";

COMMIT;

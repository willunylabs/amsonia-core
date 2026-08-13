DROP POLICY IF EXISTS p_pl_insert ON amsonia.purge_ledger;
DROP POLICY IF EXISTS p_pl_select ON amsonia.purge_ledger;
ALTER TABLE amsonia.purge_ledger DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS p_ae_delete ON amsonia.audit_events;

CREATE POLICY p_rv_update ON amsonia.role_versions
    FOR UPDATE USING (amsonia.tenant_id() = tenant_id)
    WITH CHECK (amsonia.tenant_id() = tenant_id);

DROP FUNCTION IF EXISTS amsonia.bind_tenant(TEXT, BIGINT, TEXT, TEXT);
DROP TABLE IF EXISTS amsonia.runtime_secrets;
CREATE OR REPLACE FUNCTION amsonia.tenant_id()
RETURNS TEXT
LANGUAGE SQL
STABLE
AS $$
    SELECT NULLIF(current_setting('amsonia.tenant_id', TRUE), '')
$$;
GRANT EXECUTE ON FUNCTION amsonia.set_tenant(TEXT) TO PUBLIC;

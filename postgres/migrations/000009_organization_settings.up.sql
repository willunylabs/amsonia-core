ALTER TABLE amsonia.tenants ADD COLUMN settings_version BIGINT NOT NULL DEFAULT 1 CHECK (settings_version > 0);

CREATE FUNCTION amsonia.organization_settings()
RETURNS TABLE(id TEXT, name TEXT, state TEXT, version BIGINT, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ)
LANGUAGE SQL STABLE SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
    SELECT t.tenant_id, t.name, t.state, t.settings_version, t.created_at, t.updated_at
    FROM amsonia.tenants t WHERE t.tenant_id = amsonia.tenant_id() AND t.state = 'active'
$$;

CREATE FUNCTION amsonia.rename_organization(p_actor TEXT, p_name TEXT, p_expected_version BIGINT)
RETURNS TABLE(id TEXT, name TEXT, state TEXT, version BIGINT, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
DECLARE
    bound_tenant TEXT := amsonia.tenant_id();
    current_row amsonia.tenants%ROWTYPE;
BEGIN
    IF bound_tenant IS NULL OR NOT EXISTS (
        SELECT 1 FROM amsonia.system_administrators sa
        JOIN amsonia.accounts a ON a.account_id = sa.account_id
        JOIN amsonia.tenant_memberships m ON m.account_id = a.account_id
        WHERE a.account_id = p_actor AND a.status = 'active'
          AND m.tenant_id = bound_tenant AND m.status = 'active'
    ) THEN
        RAISE EXCEPTION 'active system administrator membership required' USING ERRCODE = '42501';
    END IF;
    IF p_name IS NULL OR octet_length(p_name) NOT BETWEEN 1 AND 80 OR
       p_name ~ '[[:cntrl:]]' OR p_name ~ '^[[:space:]]*$' OR p_expected_version IS NULL OR p_expected_version < 1 THEN
        RAISE EXCEPTION 'invalid organization settings' USING ERRCODE = '22023';
    END IF;
    SELECT t.* INTO current_row FROM amsonia.tenants t
    WHERE t.tenant_id = bound_tenant AND t.state = 'active' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'organization not found' USING ERRCODE = 'P0002';
    END IF;
    IF current_row.settings_version <> p_expected_version THEN
        RAISE EXCEPTION 'organization settings changed' USING ERRCODE = '40001';
    END IF;
    IF current_row.name <> p_name THEN
        UPDATE amsonia.tenants t SET name = p_name, settings_version = t.settings_version + 1, updated_at = now()
        WHERE t.tenant_id = bound_tenant;
    END IF;
    RETURN QUERY SELECT * FROM amsonia.organization_settings();
END;
$$;

REVOKE ALL ON FUNCTION amsonia.organization_settings() FROM PUBLIC;
REVOKE ALL ON FUNCTION amsonia.rename_organization(TEXT, TEXT, BIGINT) FROM PUBLIC;

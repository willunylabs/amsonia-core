-- Resolve deployment configuration without exposing the global tenant directory.
CREATE FUNCTION amsonia.single_organization_id(p_selected TEXT)
RETURNS TEXT
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
DECLARE
    candidates TEXT[];
BEGIN
    IF NULLIF(p_selected, '') IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM amsonia.tenants WHERE tenant_id = p_selected AND state = 'active') THEN
            RAISE EXCEPTION 'configured organization is not active' USING ERRCODE = '22023';
        END IF;
        RETURN p_selected;
    END IF;
    SELECT array_agg(t.tenant_id) INTO candidates FROM (
        SELECT tenant_id FROM amsonia.tenants WHERE state = 'active' ORDER BY tenant_id LIMIT 2
    ) t;
    IF cardinality(candidates) > 1 THEN
        RAISE EXCEPTION 'multiple active organizations require explicit configuration' USING ERRCODE = '22023';
    END IF;
    RETURN candidates[1];
END;
$$;
REVOKE ALL ON FUNCTION amsonia.single_organization_id(TEXT) FROM PUBLIC;

-- Activate or fail only the tenant authorized by signed runtime context.

CREATE OR REPLACE FUNCTION amsonia.activate_tenant()
RETURNS BOOLEAN
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
DECLARE
    bound_tenant TEXT := amsonia.tenant_id();
BEGIN
    IF bound_tenant IS NULL THEN
        RETURN FALSE;
    END IF;
    UPDATE amsonia.tenants
       SET state = 'active', updated_at = now()
     WHERE tenant_id = bound_tenant AND state = 'pending';
    RETURN FOUND;
END;
$$;

CREATE OR REPLACE FUNCTION amsonia.fail_created_tenant(p_tenant_id TEXT)
RETURNS BOOLEAN
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
DECLARE
    bound_actor TEXT := amsonia.actor_id();
BEGIN
    IF bound_actor IS NULL OR p_tenant_id IS NULL OR p_tenant_id = '' THEN
        RETURN FALSE;
    END IF;
    UPDATE amsonia.tenants
       SET state = 'failed', updated_at = now()
     WHERE tenant_id = p_tenant_id
       AND created_by = bound_actor
       AND state = 'pending';
    RETURN FOUND;
END;
$$;

REVOKE ALL ON FUNCTION amsonia.activate_tenant() FROM PUBLIC;
REVOKE ALL ON FUNCTION amsonia.fail_created_tenant(TEXT) FROM PUBLIC;

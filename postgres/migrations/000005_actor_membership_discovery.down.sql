DROP POLICY IF EXISTS p_tm_select ON amsonia.tenant_memberships;
CREATE POLICY p_tm_select ON amsonia.tenant_memberships
    FOR SELECT USING (amsonia.tenant_visible(tenant_id));

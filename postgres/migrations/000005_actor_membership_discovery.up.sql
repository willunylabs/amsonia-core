-- Allow the signed account actor to discover only its own active tenant
-- memberships while retaining ordinary tenant-scoped access under FORCE RLS.

DROP POLICY IF EXISTS p_tm_select ON amsonia.tenant_memberships;
CREATE POLICY p_tm_select ON amsonia.tenant_memberships
    FOR SELECT USING (
        amsonia.tenant_visible(tenant_id)
        OR (
            amsonia.actor_id() IS NOT NULL
            AND account_id = amsonia.actor_id()
        )
    );

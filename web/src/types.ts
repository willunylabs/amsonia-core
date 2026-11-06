export type Account = {
  id: string;
  email: string;
  system_admin: boolean;
  created_at: string;
  last_login_at?: string;
};

export type Tenant = {
  id: string;
  name: string;
  state: string;
  created_at: string;
};

export type Role = {
  tenant_id: string;
  role_id: string;
  name: string;
  description: string;
  version: number;
};

export type Member = {
  account_id: string;
  email: string;
  status: string;
  joined_at: string;
};

export type Permission = { key: string; description: string };

export type Capabilities = { permissions: Record<string, boolean> };

export type AuditEvent = {
  id: number;
  actor_subject_id?: string;
  host_initiator?: string;
  request_id?: string;
  role_version?: number;
  tenant_id: string;
  operation: string;
  phase: string;
  target_type: string;
  target_id: string;
  outcome: string;
  reason_code?: string;
  at: string;
};

export type Session = {
  access_token: string;
  expires_at: string;
  account: Account;
};

export type Profile = {
  id: string;
  email: string;
  display_name: string;
  created_at: string;
};
export type AccountSession = {
  id: string;
  created_at: string;
  last_seen_at: string;
  expires_at: string;
  remote_address: string;
  user_agent: string;
  current: boolean;
};
export type Invitation = {
  id: string;
  email: string;
  created_at: string;
  expires_at: string;
  status: string;
};
export type InvitationPreview = {
  id: string;
  tenant_id: string;
  tenant_name: string;
  email: string;
  expires_at: string;
  account_exists: boolean;
};
export type ScopedPermission = {
  permission: string;
  scope: "tenant" | "own" | "workspace";
  workspace_roles: string[] | null;
};
export type RoleHistory = {
  version: number;
  name: string;
  description: string;
  grants: ScopedPermission[];
  created_at: string;
  created_by: string;
};
export type RoleDetail = {
  role: Role;
  grants: ScopedPermission[];
  subjects: string[];
  subject_count?: number;
  versions: RoleHistory[];
  next_version?: number;
};
export type AuditPage = { items: AuditEvent[]; next_cursor?: number };
export type AuditFilter = {
  from?: string;
  until?: string;
  q?: string;
  outcome?: string;
  operation?: string;
  before?: number;
  limit?: number;
};

export type DirectoryFilter = {
  q?: string;
  status?: string;
  after?: string;
  limit?: number;
};
export type DirectoryPage<T> = {
  items: T[];
  total: number;
  next_cursor?: string;
};

export type OrganizationSettings = Tenant & {
  version: number;
  updated_at: string;
  can_manage: boolean;
};

export type PasswordRecovery = { id: string; email: string; expires_at: string; token: string };
export type PasswordRecoveryPreview = { email: string; tenant_name: string; expires_at: string };

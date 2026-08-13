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

export type AuditEvent = {
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

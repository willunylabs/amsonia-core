# Daily Core administration

Core provides a small, usable multi-tenant administration foundation. Open the
console using the existing local deployment workflow, bootstrap the first system
administrator with the CLI, and create a tenant. The tenant owner receives all
seven administration permissions.

## Account and sessions

Open **Account settings** beside the account avatar. Save a display name, change
your password using the current password, or revoke a signed-in device. Passwords
must contain 12–128 Unicode characters. A successful password change revokes all
access and refresh tokens and requires signing in again. Password verification
uses the existing account lockout policy. Profiles and sessions are always scoped
to the authenticated account; no account ID is accepted from the client.

A device session family keeps its stable ID across refresh rotations. Revoking
it invalidates both token types. The session list contains timestamps and browser
metadata, never token hashes or secrets. Remote addresses come from the API's
connection, so reverse-proxy addresses may appear. Activity updates are throttled
to once per minute.

The console restores a signed-in session from the HttpOnly refresh cookie when a
new tab has no access token. The optional `POST /api/v1/auth/restore` endpoint
returns 204 for an anonymous browser, without generating a normal sign-in error.
Temporary network or service failures retain the
credentials and offer retry; a definitive rejection returns to sign-in. Signing
out waits for server confirmation and revokes the entire device session family,
including older live access tokens from refresh rotations. Other device families
remain active. Revoked current refresh tokens are rejected without affecting
other devices; reuse of a token already consumed by rotation still invokes the
account-wide replay response.

Cookie-changing sign-in, refresh, and sign-out requests are ordered within each
page and, when available, across tabs using the browser's
[Web Locks API](https://developer.mozilla.org/en-US/docs/Web/API/Web_Locks_API).
Refresh responses arriving after a local sign-out cannot restore local
credentials. If a shared cookie now belongs to a different account, the console
switches account context and does not replay an earlier mutation as that account.
Bearer tokens remain in sessionStorage; no bearer token is placed in localStorage.

## Tenant selection and current access

System administrators can create additional tenants using the plus button beside
the tenant selector, including after their first tenant has been created.
The console remembers a tenant ID separately for each account in localStorage,
then validates it against the server's current membership list. This preference
never grants access. Switching tenants discards obsolete responses before they
can render another tenant's collections.

Use **Refresh workspace**, return focus to the window, or return from a hidden tab
to refresh memberships and capabilities. Member/role changes in this console also
refresh the workspace. If the selected membership disappears, old tenant data is
cleared and another accessible tenant is selected; if none remain, the console
shows the no-tenant state while keeping account settings available. A collection
permission denied during refresh triggers one bounded capability recheck.

## Members and invitations

In **Members**, choose **Invite member**, enter an email, and privately share the
one-time link. Core displays the secret once and stores only its hash. No email
is sent. A link expires after seven days; re-inviting an email revokes its earlier
pending link. Search all invitations by email or ID and filter pending, accepted,
revoked or expired links. Older pending invitations remain reachable through
pagination and can be revoked. The URL fragment carries the token so it does not
enter HTTP paths or referrers. Do not post invitation links publicly.

A new account chooses a password when accepting. An existing account proves its
current password; accepting never resets it. Suspended membership cannot be
bypassed by an invitation. A joined member starts without roles. Administrators
assign roles from the member's **Roles** action. A member with no read permissions
can still open the console and account settings without denied collection calls.

Suspend retains role bindings and blocks tenant access. Resume restores those
bindings. Remove deletes membership, subject-role bindings, and incoming grant
edges; the account and other tenant memberships remain. Rejoining requires a new
invitation. Another active administrator must exist before suspending/removing
the last tenant administrator. Removing a member does not revoke grants they
previously delegated to other members.

Member, invitation and role directories search on the server and show 30 rows
per page, matching totals, previous/next navigation, retry and empty results.
Filters stay applied while paging. Member status changes and workspace refreshes
update the current search; if its last page becomes empty, it returns to the first
page of that search. The member role picker uses the same searchable role pages;
assignments on other pages remain saved. Each checkbox saves immediately using
the displayed role version. The overview counts active memberships and all live
roles on the server; unreadable counts display a dash.

The Core HTTP list responses now include `items`, `total` and optional
`next_cursor`. Clients must follow `next_cursor` as `after` with unchanged `q`
and `status` to read the entire collection. `limit` defaults to 30 and accepts
1–100. Queries are trimmed literal, case-insensitive substrings up to 128 Unicode
characters; `%` and `_` have no wildcard meaning. Members and roles sort by
immutable ID ascending. Invitations sort by creation time and ID descending, so
newer inserts and editable role names cannot shift existing cursor boundaries.
A cursor from another tenant, collection or filter returns 400. Cursors contain
no authority or secrets; normal permission and membership checks still apply.
Totals and rows share a request snapshot; navigation is live, so edits, expiry,
and membership changes can alter matches between pages. Use an export workflow
when a fixed historical snapshot is required. Kernel APIs and existing Service
list method contracts are unchanged.

## Roles and audit

Use **Roles → View role** to edit details, add/revoke scoped grants, inspect
member binding counts, and read version snapshots. The history loads 20 versions
at a time. Each mutation uses the currently displayed role version; a conflict
asks the operator to refresh. Only empty, unassigned roles can be retired.

The role's **Role members** section lists assigned people and available tenant
members with search, status filters and 30-row cursor pages. Member identities
require both member-read and role-read access; users without member-read see only
the binding count. Read-only users can inspect the directory. Role assignment
controls require role-assign permission and use the currently displayed role
version. Assignment coverage and last-administrator checks use the existing
server mutation path. Suspended members remain visible; their bindings can be
removed, while new assignment requires resuming membership first.

Each binding change saves immediately and refreshes the count and current page.
An empty final page returns to the first page of the same search. Switching
assigned/available views starts a new search. A version conflict refreshes the
role data and asks the operator to try again deliberately; it does not replay the
write. To prepare a role for retirement, remove its bindings here, revoke its
grants, then confirm retirement. Other roles on each member are retained.

The console requests `GET /roles/{role_id}?summary=true`, which returns
`subject_count` and an empty `subjects` array without downloading every bound ID.
Default HTTP detail responses and the existing Service `RoleDetail` contract
continue to return the full subject-ID array. Role member identities use the
new `/roles/{role_id}/members` endpoint with `assignment=assigned|available` and
the existing directory filters. Its cursors are also bound to role and view.
Directory readers accept cursors up to 8192 bytes so valid escaped IDs and role
scope can round-trip without truncation.

Tenant administrative endpoints require tenant-wide grants. Own/workspace grants
are intended for host resource checks, not tenant collection management. Role
assignment requires actual coverage of the permissions delegated, and cycle and
last-administrator checks remain enforced by the server.

**Audit trail** supports text search, exact operation and outcome filters, date
ranges, event details, and stable ID cursor paging. The date inputs use the
browser's local timezone and are sent as RFC3339 UTC instants. `from` includes the
start and `until` excludes the end; both are optional, and a supplied start must
precede the end. Newer/older navigation preserves the applied filters. Changes in
the form take effect only after **Apply filters**.

**Export matching events** downloads all events matching the applied filters,
including when viewing an older page. It takes one read-only tenant snapshot at
download time; newly appended events may therefore differ from an earlier page.
The export uses the same tenant-wide `iam:audit:read` permission as the list.
`GET /api/v1/tenants/{tenant_id}/audit-events/export` accepts `q`, `operation`,
`outcome`, `from`, and `until`; it rejects pagination parameters. More than 10,000
matching events or 10 MiB returns JSON `422 audit_export_too_large` before any CSV
headers or bytes are sent. Narrow the filters and retry. Empty results produce a
header-only file. Downloads can be cancelled; tenant, account or access changes
invalidate pending downloads.

The CSV is UTF-8 with a BOM, descending event IDs, fixed column names, and UTC
RFC3339 timestamps. Quoted fields retain Unicode, commas, quotes and line breaks.
Following [OWASP's spreadsheet guidance](https://community.owasp.org/attacks/CSV_Injection),
formula-like or tab/line-break-leading text receives a leading tab inside the
quoted cell. This tab is part of the exported value; JSON retains the original
value. The server sends `Cache-Control: no-store` and a generated attachment
filename. No new migration or runtime database grant is needed.
Generated `X-Request-ID` values connect HTTP requests and mutation audit records.
Global account/profile/session changes are not tenant audit events. Kernel
mutation failures are audited; malformed requests and pre-authentication failures
are handled by HTTP logs rather than a tenant audit stream.

## Upgrade

Apply all pending migrations, including `000006_core_administration` and
`000007_context_transaction_errors`, then rerun
`ops/configure_database_roles.sql` before starting the new API. It grants the
narrow token-authorized invitation preview function. Startup readiness requires
an exact clean migration state. The migration adds display names and session
family IDs without invalidating existing credentials. Legacy refresh rotations
cannot always be reconstructed into one family: pre-upgrade live tokens may
appear as separate devices until expiry. Revoke each legacy entry or change the
password to invalidate all of them. New sessions and rotations share a family ID.
Migration 000007 preserves serialization/deadlock SQLSTATEs from signed tenant
and actor context verification so normal retryable conflicts are not masked as
RLS denials. Invalid or malformed bindings still fail closed. It does not change
signatures, role grants, or the tenant-boundary predicates. Its down migration
restores the previous exception handling. Migration 000006's down migration removes
the added fields and preview function; use matching older binaries only after
restoring role grants as appropriate.

## Scaffold comparison

Reviewed against primary project documentation on 2026-11-03. These projects are
workflow references; no source implementation was copied.

| Reference | Relevant reference capability | Core completion |
| --- | --- | --- |
| [Gin Vue Admin](https://github.com/flipped-aurora/gin-vue-admin/blob/main/README-en.md) | User, role, API administration and searchable lists | Member lifecycle, role composition, scoped grants and searchable audit |
| [Go Admin](https://github.com/go-admin-team/go-admin) | Administration starter with RBAC and tenant-oriented modules | Invitations, active memberships and explicit tenant isolation |
| [Nunu](https://github.com/go-nunu/nunu) | Layered Go project foundation | HTTP/service/storage boundaries and tested workflows |

Core's selected scope is accounts, invitations, members, roles/permissions,
sessions, and audit. Menus, code generation, dictionaries, files, scheduling,
billing, password recovery, and outbound email remain outside this iteration.

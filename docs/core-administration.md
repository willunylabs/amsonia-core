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

## Members and invitations

In **Members**, choose **Invite member**, enter an email, and privately share the
one-time link. Core displays the secret once and stores only its hash. No email
is sent. A link expires after seven days; re-inviting an email revokes its earlier
pending link. The latest 200 invitation states are visible and pending links can
be revoked. The URL fragment carries the token so it does not enter HTTP paths
or referrers. Do not post invitation links publicly.

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

## Roles and audit

Use **Roles → View role** to edit details, add/revoke scoped grants, inspect
member binding counts, and read version snapshots. The history loads 20 versions
at a time. Each mutation uses the currently displayed role version; a conflict
asks the operator to refresh. Only empty, unassigned roles can be retired.

Tenant administrative endpoints require tenant-wide grants. Own/workspace grants
are intended for host resource checks, not tenant collection management. Role
assignment requires actual coverage of the permissions delegated, and cycle and
last-administrator checks remain enforced by the server.

**Audit trail** supports text search, exact operation and outcome filters, event
details, and stable ID cursor paging. Newer/older navigation preserves the filter.
Generated `X-Request-ID` values connect HTTP requests and mutation audit records.
Global account/profile/session changes are not tenant audit events. Kernel
mutation failures are audited; malformed requests and pre-authentication failures
are handled by HTTP logs rather than a tenant audit stream.

## Upgrade

Apply migration `000006_core_administration`, then rerun
`ops/configure_database_roles.sql` before starting the new API. It grants the
narrow token-authorized invitation preview function. Startup readiness requires
an exact clean migration state. The migration adds display names and session
family IDs without invalidating existing credentials. Legacy refresh rotations
cannot always be reconstructed into one family: pre-upgrade live tokens may
appear as separate devices until expiry. Revoke each legacy entry or change the
password to invalidate all of them. New sessions and rotations share a family ID.
The down migration removes these fields and the preview function; use matching
older binaries only after restoring role grants as appropriate.

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

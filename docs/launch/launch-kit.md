# Amsonia Core launch kit

This document keeps promotion accurate, reproducible, and useful to engineers.
It is not a list of claims to broadcast before the corresponding proof ships.

## Positioning

**Category:** self-hosted multi-tenant authorization foundation for Go and
PostgreSQL.

**One sentence:** Amsonia Core combines delegated tenant RBAC, administrator
sessions, policy audit, and signed PostgreSQL RLS in a source-owned Go stack.

**Wedge:** most tools stop at a policy decision. Core also publishes and tests
the transaction boundary that prevents one tenant from reading or writing
another tenant's rows.

**Honest boundary:** Core is not an identity provider or a general-purpose
relationship graph. The host owns request authentication, active-tenant
selection, business-resource loading, and the mapping from an operation to a
permission. Amsonia Platform modules are not Core features.

## Proof before distribution

Do not launch a claim until a new user can verify it without private context:

- `make demo` succeeds from a clean clone.
- The local stack starts from a clean clone and its sample credentials are
  documented beside the command that creates them.
- The business-table RLS example shows two tenants, no tenant predicate in the
  select query, a rejected cross-tenant write, and a negative forged-context
  integration test.
- README, website, tagged release, OpenAPI contract, and local stack describe the same
  product boundary.
- A maintainer is ready to answer issues for at least 48 hours after launch.

## Launch sequence

### Week 1 — make evaluation boring

1. Merge and tag the adoption release.
2. Test the clean-clone path on macOS and Linux.
3. Publish a short screen recording: clone, `make demo`, sign in, inspect a
   denied permission, run the RLS proof.
4. Add GitHub topics that describe the actual system: `go`, `postgresql`,
   `multi-tenant`, `authorization`, `rbac`, `row-level-security`, `saas`, and
   `self-hosted`.

### Week 2 — launch to practitioners

Publish one primary post and answer every technical question with a link to
code or a test. Start with Go communities, then submit Show HN only after the
clean-clone flow has been independently verified. Do not cross-post identical
copy everywhere on the same day.

### Weeks 3–4 — teach the hard parts

Publish the signed-RLS article and the delegation-cycle article below. Use
questions and failed setup steps to improve docs before creating more keyword
pages. Invite design criticism explicitly; early technical objections are more
valuable than generic praise.

## Ready-to-edit launch copy

### Show HN

**Title**

> Show HN: Amsonia Core – tenant authorization for Go, enforced in PostgreSQL

**Body**

> I built Amsonia Core after seeing multi-tenant authorization split across
> handler checks, role tables, sessions, and easy-to-miss tenant filters. It is
> an Apache-2.0 Go/PostgreSQL stack with delegated RBAC, versioned roles,
> administrator sessions, audit events, a React console, and signed
> transaction-local tenant bindings for `FORCE ROW LEVEL SECURITY`.
>
> The part I most want feedback on is the database boundary. The included
> invoice example queries without a tenant `WHERE` clause; PostgreSQL exposes
> only the signed tenant's row and rejects a cross-tenant insert. Integration
> tests also try forged session settings.
>
> You can run the complete Core stack locally with `make demo`. The hosted commercial product demo is not evidence of Core scope.
> This is not an IdP or a Zanzibar-style relationship graph. It is aimed at Go
> SaaS teams that want tenant RBAC and PostgreSQL isolation in source they own.
> I would especially value review of the RLS model and the boundaries that
> should stay outside Core.

### r/golang / Go community post

**Title**

> I open-sourced the tenant authorization boundary from my Go/PostgreSQL SaaS stack

**Body**

> Amsonia Core is an Apache-2.0 authorization foundation for multi-tenant Go
> applications. It includes the policy kernel, PostgreSQL adapter and
> migrations, least-privileged roles, admin sessions, OpenAPI contract, and a
> React console.
>
> The unusual part is a signed transaction-local tenant context used by forced
> PostgreSQL RLS. The repo includes a two-tenant business-table example and
> negative tests for forged context and cross-tenant writes. A clean clone runs
> with `make demo`.
>
> I am looking for concrete feedback: Is the `RunTenant` boundary natural in a
> Go service? Which integration example would make evaluation easier? Where is
> the current API too opinionated?

### 中文技术社区

**标题**

> 我把 Go SaaS 里的租户授权与 PostgreSQL RLS 边界开源了

**正文要点**

- 不是“又一个后台模板”，而是把租户成员、委托式 RBAC、管理员会话、审计和数据库行隔离放进同一个可测试边界。
- 核心证据是一个双租户发票表示例：查询不写租户过滤条件，RLS 仍只返回当前签名租户的数据；跨租户写入直接由 PostgreSQL 拒绝。
- `make demo` 可从干净仓库启动完整 Core 栈；商业产品的线上 Demo 不属于 Core 的能力范围。
- 希望获得的反馈：事务边界是否符合 Go 服务习惯、接入现有项目还缺什么、哪些能力不应该进入 Core。

## First two engineering articles

### 1. Why a tenant header is not a database boundary

Show the threat model, unsigned-GUC failure, signed transaction payload,
non-owner runtime role, `FORCE RLS`, `USING` versus `WITH CHECK`, and the
negative integration tests. Include failure cases and operational assumptions;
do not present RLS as a replacement for authorization.

### 2. Preventing privilege cycles in delegated SaaS RBAC

Start with a concrete tenant-owner → administrator → role-manager delegation
chain. Explain immutable role versions, expected-version writes, graph-cycle
rejection, transaction serialization, and the audit record. Contrast the model
with flat role-name checks without claiming universal superiority.

## Metrics that can change the product

Review weekly for the first month:

- clean-clone attempts that reach a healthy local stack;
- docs-to-GitHub and release-to-GitHub CTA clicks;
- unique repository cloners and repeat visitors;
- setup failures grouped by step;
- issues or discussions containing a real integration question;
- the percentage of launch questions answerable by an existing public test or
  document.

Stars and impressions are useful reach signals, but they do not prove adoption.
Prioritize successful evaluations, technical conversations, and external
examples over raw totals.

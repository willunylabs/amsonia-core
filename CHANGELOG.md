# Changelog

## 0.2.0-preview.3

- Fix the packaged console failing to render its sign-in page because the
  production image was missing required link configuration.
- Verify first login on desktop and mobile Chromium before staging an archive,
  and verify the published archive separately through its public download URLs.

## 0.2.0-preview.2

- Standalone deployments default to one organization. Multi mode is opt-in;
  existing databases with multiple active organizations need explicit selection
  or multi mode before upgrading.
- The console includes dedicated member, invitation, role, account and
  organization forms, session controls, and filtered audit export.
- Preview builds produce a checksummed Linux amd64 bundle with prebuilt
  API, console and PostgreSQL images. The install path needs Docker Compose
  rather than Go or Node.js on the target host.
- Operators can recover an active system administrator without a mail service.
  Recovery invalidates the old password and all earlier sessions and writes an
  owner-only recovery record.
- The opt-in invoice example demonstrates a form, an API permission check,
  and PostgreSQL row-level isolation. It adds no routes to the default console.
- PostgreSQL CI exercises backup, restore to an empty database, and upgrade
  from the pinned schema 9 application baseline. It checks identity,
  organization settings, audit continuity and read-only permission enforcement.

Upgrade the API, CLI and console together. Apply all pending migrations,
including migration 10, reapply the runtime grants, and follow the
[self-hosting runbook](docs/self-hosting.md). The last public tag's scope is
described in its [release history](https://github.com/willunylabs/amsonia-core/releases).

## Release checklist

- Choose one commit and check its OpenAPI and package version metadata against
  the intended release tag.
- Verify clean installation from that exact revision and restore/upgrade from
  the prior supported application release. The PostgreSQL CI job checks the
  pinned migration 9 baseline; test additional supported sources as needed.
  Record prerequisites and incompatible configuration changes.
- Publish release notes with the supported installation path and links to
  operator recovery, upgrade, and application extension documentation.
- Run the **Preview bundle** workflow on the exact release commit. Download
  its archive and checksum only after the archive installation and browser
  sign-in checks pass.
- Create a draft GitHub prerelease at the revision recorded in the bundle.
  Run **Stage preview draft** with the successful workflow run ID to transfer
  verified assets on GitHub. Review the draft and publish separately.
- After publishing, dispatch **Public preview install** from `main`. Run
  it once with the published tag alone for a clean install and again with
  `from_tag` set to the prior supported preview for an in-place upgrade.
  For example, the preview.2 to preview.3 verification uses:

  ```sh
  gh workflow run public-preview.yml --ref main -f tag=v0.2.0-preview.3
  gh workflow run public-preview.yml --ref main -f tag=v0.2.0-preview.3 -f from_tag=v0.2.0-preview.2
  ```

  Confirm both runs succeeded before linking them from the release notes.
  Record the upgrade source and the platform tested.

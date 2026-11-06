# Administrator recovery validation

- PostgreSQL 16 integration/race tests cover an active, locked administrator,
  ordinary member refusal, least-privilege runtime refusal with rollback, both
  session token types, refresh replay after recovery, and audit attribution.
- A native drill built the preceding application CLI at d0f90fb, bootstrapped an
  administrator on migration 9, dumped the database and restored it to an empty
  database in the test cluster. The new CLI applied migration 10. The new API
  accepted the restored administrator and returned organization settings.
- The interactive recovery CLI ran against the restored database. An earlier
  bearer and the previous password were rejected; the replacement password
  worked. This drill reused cluster roles. It did not test provisioning those
  roles on a separate server or upgrading a Docker installation.
- The existing host-table example ran under the non-owner runtime role on the
  restored database. Each tenant saw only its own invoice and PostgreSQL
  rejected a cross-tenant insert.

Local test evidence is under the ignored output/admin-recovery-validation
directory. Test backups contain temporary identity data and are removed after
verification.

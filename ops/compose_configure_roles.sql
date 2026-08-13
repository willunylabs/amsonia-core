\set ON_ERROR_STOP on

\set runtime_role amsonia_runtime
\set maintenance_role amsonia_maintenance
\getenv runtime_password AMSONIA_POSTGRES_RUNTIME_PASSWORD
\getenv maintenance_password AMSONIA_POSTGRES_MAINTENANCE_PASSWORD
\getenv runtime_secret_hex AMSONIA_RUNTIME_SECRET_HEX
\getenv maintenance_secret_hex AMSONIA_MAINTENANCE_SECRET_HEX

SELECT format(
    'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT',
    :'runtime_role', :'runtime_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'runtime_role')
\gexec

SELECT format(
    'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT',
    :'maintenance_role', :'maintenance_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'maintenance_role')
\gexec

\i /ops/configure_database_roles.sql

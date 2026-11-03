CREATE TABLE amsonia.administrator_recoveries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id TEXT NOT NULL,
    database_operator NAME NOT NULL DEFAULT session_user,
    recovered_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
REVOKE ALL ON TABLE amsonia.administrator_recoveries FROM PUBLIC;
REVOKE ALL ON SEQUENCE amsonia.administrator_recoveries_id_seq FROM PUBLIC;

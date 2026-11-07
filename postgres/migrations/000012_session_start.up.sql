ALTER TABLE amsonia.access_sessions ADD COLUMN session_started_at TIMESTAMPTZ;
ALTER TABLE amsonia.refresh_sessions ADD COLUMN session_started_at TIMESTAMPTZ;

-- Older installations can only recover the earliest retained family record.
WITH family_start AS (
    SELECT account_id, session_id, min(created_at) AS started_at
    FROM (
        SELECT account_id, session_id, created_at FROM amsonia.access_sessions
        UNION ALL
        SELECT account_id, session_id, created_at FROM amsonia.refresh_sessions
    ) sessions
    GROUP BY account_id, session_id
), backfill_access AS (
    UPDATE amsonia.access_sessions AS s
    SET session_started_at = f.started_at
    FROM family_start AS f
    WHERE s.account_id = f.account_id AND s.session_id = f.session_id
)
UPDATE amsonia.refresh_sessions AS s
SET session_started_at = f.started_at
FROM family_start AS f
WHERE s.account_id = f.account_id AND s.session_id = f.session_id;

ALTER TABLE amsonia.access_sessions ALTER COLUMN session_started_at SET NOT NULL;
ALTER TABLE amsonia.refresh_sessions ALTER COLUMN session_started_at SET NOT NULL;

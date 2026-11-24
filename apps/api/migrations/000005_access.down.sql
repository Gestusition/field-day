-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

DROP TABLE access_events;
DROP TABLE access_grants;
DROP TABLE access_group_members;
DROP TABLE access_groups;

COMMIT;

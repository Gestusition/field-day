-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

DROP TABLE reader_commands;
DROP TABLE reader_enrollments;
DROP TABLE readers;

COMMIT;

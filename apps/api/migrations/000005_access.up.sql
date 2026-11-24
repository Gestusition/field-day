-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE access_groups (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER access_groups_set_updated_at
    BEFORE UPDATE ON access_groups
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE access_group_members (
    group_id   uuid NOT NULL REFERENCES access_groups (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX access_group_members_user_id_idx ON access_group_members (user_id);

-- A grant lets a user, or every member of a group, into a zone.
-- weekdays uses ISO numbering (1 = Monday ... 7 = Sunday). The time window is
-- in the site time zone; NULL start/end means all day; end < start wraps
-- past midnight.
CREATE TABLE access_grants (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    zone_id     uuid NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
    user_id     uuid REFERENCES users (id) ON DELETE CASCADE,
    group_id    uuid REFERENCES access_groups (id) ON DELETE CASCADE,
    weekdays    smallint[] NOT NULL DEFAULT '{1,2,3,4,5,6,7}',
    start_time  time,
    end_time    time,
    valid_from  timestamptz,
    valid_until timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT access_grants_one_subject CHECK (num_nonnulls(user_id, group_id) = 1),
    CONSTRAINT access_grants_weekdays_valid
        CHECK (cardinality(weekdays) > 0 AND weekdays <@ '{1,2,3,4,5,6,7}'::smallint[]),
    CONSTRAINT access_grants_window_pair CHECK ((start_time IS NULL) = (end_time IS NULL)),
    CONSTRAINT access_grants_validity_order
        CHECK (valid_from IS NULL OR valid_until IS NULL OR valid_from < valid_until)
);

CREATE INDEX access_grants_zone_id_idx ON access_grants (zone_id);
CREATE INDEX access_grants_user_id_idx ON access_grants (user_id);
CREATE INDEX access_grants_group_id_idx ON access_grants (group_id);

-- Every tap is recorded, granted or not.
CREATE TABLE access_events (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    reader_id       uuid NOT NULL REFERENCES readers (id) ON DELETE CASCADE,
    zone_id         uuid REFERENCES zones (id) ON DELETE SET NULL,
    credential_id   uuid REFERENCES credentials (id) ON DELETE SET NULL,
    user_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    presented_uid   text NOT NULL,
    decision        text NOT NULL CHECK (decision IN ('granted', 'denied', 'enrolled')),
    reason          text NOT NULL,
    -- Reader-generated id so a reader can safely retry a tap.
    device_event_id uuid,
    occurred_at     timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX access_events_device_event_key
    ON access_events (reader_id, device_event_id) WHERE device_event_id IS NOT NULL;
CREATE INDEX access_events_reader_id_idx ON access_events (reader_id, id DESC);
CREATE INDEX access_events_user_id_idx ON access_events (user_id, id DESC);
CREATE INDEX access_events_zone_id_idx ON access_events (zone_id, id DESC);

COMMIT;

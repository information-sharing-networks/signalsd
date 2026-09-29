-- +goose Up

-- adds the event content kind
--
-- Event signals
--
-- An event is a json signal that records that a process waypoint has been reached for an entity (e.g. "export health certificate approved" for a consignment).
-- The signal type is the event type (e.g. ehc-approved/v1.0.0) and the content is validated against its schema, as for json signals.
-- Events are immutable: the correlation_id is required, the content must include an occurred_at timestamp,
-- and a resubmitted event is either unchanged or rejected (it never creates a new version).
-- -------------------------------------------------------------------------

ALTER TABLE signal_types
    DROP CONSTRAINT valid_content_kind,
    ADD CONSTRAINT valid_content_kind CHECK (content_kind IN ('json', 'document', 'event'));

-- +goose Down

ALTER TABLE signal_types
    DROP CONSTRAINT valid_content_kind,
    ADD CONSTRAINT valid_content_kind CHECK (content_kind IN ('json', 'document'));

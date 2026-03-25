-- +goose Up

-- One rule set per (signal_type, json field) pair.
-- currently only one mapping field per signal type is supported
CREATE TABLE signal_routing_rules (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    signal_type_id  UUID        NOT NULL,
    routing_field   TEXT        NOT NULL,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL,

    CONSTRAINT fk_signal_routing_rules_signal_type
        FOREIGN KEY (signal_type_id)
        REFERENCES signal_types(id)
        ON DELETE CASCADE,

    CONSTRAINT unique_routing_rule_per_signal_field
        UNIQUE (signal_type_id, routing_field)
);

CREATE TABLE signal_routing_mappings (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    signal_routing_rule_id UUID        NOT NULL,
    match_pattern   TEXT        NOT NULL,       -- regex matched against the routing_field value
    notes           TEXT        NOT NULL,       -- user-friendly description of the mapping
    isn_id          UUID        NOT NULL,
    rule_sequence   INT         NOT NULL,       -- user-defined
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL,

    CONSTRAINT fk_signal_routing_mappings_rule
        FOREIGN KEY (signal_routing_rule_id)
        REFERENCES signal_routing_rules(id)
        ON DELETE CASCADE,

    CONSTRAINT fk_signal_routing_mappings_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE,

    CONSTRAINT unique_sequence_per_rule
        UNIQUE (signal_routing_rule_id, rule_sequence)
);

CREATE INDEX idx_signal_routing_mappings_rule_sequence
    ON signal_routing_mappings(signal_routing_rule_id, rule_sequence);

-- +goose Down
DROP TABLE IF EXISTS signal_routing_mappings CASCADE;
DROP TABLE IF EXISTS signal_routing_rules CASCADE;
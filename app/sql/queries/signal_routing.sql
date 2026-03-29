-- name: CreateSignalRoutingRule :one
INSERT INTO signal_routing_rules (
    signal_type_id,
    routing_field,
    created_at,
    updated_at
) VALUES ($1, $2, NOW(), NOW())
RETURNING id, signal_type_id, routing_field, created_at, updated_at;

-- name: CreateSignalRoutingMapping :one
INSERT INTO signal_routing_mappings (
    signal_routing_rule_id,
    match_pattern,
    notes,
    isn_id,
    rule_sequence,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
RETURNING id, signal_routing_rule_id, match_pattern, notes, isn_id, rule_sequence, created_at, updated_at;
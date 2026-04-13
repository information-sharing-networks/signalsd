-- name: CreateSignalRoutingConfig :one
-- Creates the routing field entry for a signal_type path.
-- Note RouteConfigs are always loaded atomically - use DeleteSignalRoutingConfigBySignalTypeID to clear any previous config before using.
INSERT INTO signal_routing_configs (
    id,
    created_at,
    signal_type_id, 
    routing_field 
) VALUES (gen_random_uuid(), NOW(), $1, $2)
RETURNING id, signal_type_id, routing_field, created_at;

-- name: CreateIsnRoute :one
-- Create a route (match pattern -> isn) for a specific signal type routing field
INSERT INTO routing_rules (
    id,
    created_at,
    signal_routing_config_id,
    match_pattern,
    operator,
    is_case_insensitive,
    isn_id,
    rule_sequence
) VALUES (gen_random_uuid(), NOW(), $1, $2, $3, $4, $5, $6)
RETURNING id, signal_routing_config_id, match_pattern, operator, is_case_insensitive, isn_id, rule_sequence, created_at;

-- name: GetSignalRoutingConfigs :many
-- Loads all signal types that have a routing field and at least one route defined
SELECT
    st.slug AS signal_type_slug,
    st.sem_ver,
    irf.routing_field,
    ir.match_pattern,
    ir.operator,
    ir.is_case_insensitive,
    ir.isn_id,
    i.slug AS isn_slug,
    ir.rule_sequence
FROM signal_routing_configs irf
JOIN signal_types st ON st.id = irf.signal_type_id
JOIN routing_rules ir ON ir.signal_routing_config_id = irf.id
JOIN isn i ON i.id = ir.isn_id
ORDER BY st.slug, st.sem_ver, ir.rule_sequence ASC;

-- name: GetSignalRoutingConfigBySignalType :one
-- Finds the routing configuration for a specific signal identity
SELECT irf.*
FROM signal_routing_configs irf
JOIN signal_types st ON st.id = irf.signal_type_id
WHERE st.slug = sqlc.arg(signal_type_slug)
  AND st.sem_ver = sqlc.arg(sem_ver);

-- name: GetIsnRoutesByFieldID :many
-- Fetches all routes for a specific field ID, ordered by their evaluation sequence
SELECT 
    ir.id,
    ir.match_pattern,
    ir.operator,
    ir.is_case_insensitive,
    ir.rule_sequence,
    i.slug AS isn_slug
FROM routing_rules ir
JOIN isn i ON i.id = ir.isn_id
WHERE ir.signal_routing_config_id = $1
ORDER BY ir.rule_sequence ASC;

-- name: DeleteSignalRoutingConfigBySignalTypeID :execrows
-- Clears all routes for a specific signal type path -  used before applyin new route configs
-- Note the delete also removes all the routes previously added for the signal type path
-- (Requires ON DELETE CASCADE on routing_rules.signal_routing_config_id)
DELETE FROM signal_routing_configs 
WHERE signal_type_id = $1;
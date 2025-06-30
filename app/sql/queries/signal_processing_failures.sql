-- name: CreateSignalProcessingFailureDetail :one
INSERT INTO signal_processing_failures (
    signal_batch_id,
    signal_type_slug,
    signal_type_sem_ver,
    local_ref,
    error_code,
    error_message
) VALUES (
    $1, -- signal_batch_id
    $2, -- signal_type_slug
    $3, -- signal_type_sem_ver
    $4, -- local_ref
    $5, -- error_code
    $6  -- error_message
)
RETURNING *;

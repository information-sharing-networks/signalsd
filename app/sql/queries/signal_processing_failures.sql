-- name: CreateSignalProcessingFailureDetail :one
INSERT INTO signal_processing_failures (
    id,
    signal_batch_id,
    isn_slug,
    signal_type_slug,
    signal_type_sem_ver,
    local_ref,
    error_code,
    error_message
) VALUES (
   uuidv7(),
    sqlc.arg(signal_batch_id),
    sqlc.narg(isn_slug), -- NULL for signals the signal router could not route to an ISN
    sqlc.arg(signal_type_slug),
    sqlc.arg(signal_type_sem_ver),
    sqlc.arg(local_ref),
    sqlc.arg(error_code),
    sqlc.arg(error_message)
)
RETURNING *;

-- name: GetIsnAccountsByAccountID :many
SELECT ia.*, i.slug as isn_slug FROM isn_accounts ia
JOIN isn i 
ON i.id = ia.isn_id
WHERE account_id = $1;
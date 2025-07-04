// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0
// source: isn.sql

package database

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const CreateIsn = `-- name: CreateIsn :one

INSERT INTO isn (
    id,
    created_at,
    updated_at,
    user_account_id,
    title,
    slug,
    detail,
    is_in_use,
    visibility
) VALUES (gen_random_uuid(), now(), now(), $1, $2, $3, $4, $5, $6 )
RETURNING id, slug
`

type CreateIsnParams struct {
	UserAccountID uuid.UUID `json:"user_account_id"`
	Title         string    `json:"title"`
	Slug          string    `json:"slug"`
	Detail        string    `json:"detail"`
	IsInUse       bool      `json:"is_in_use"`
	Visibility    string    `json:"visibility"`
}

type CreateIsnRow struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
}

func (q *Queries) CreateIsn(ctx context.Context, arg CreateIsnParams) (CreateIsnRow, error) {
	row := q.db.QueryRow(ctx, CreateIsn,
		arg.UserAccountID,
		arg.Title,
		arg.Slug,
		arg.Detail,
		arg.IsInUse,
		arg.Visibility,
	)
	var i CreateIsnRow
	err := row.Scan(&i.ID, &i.Slug)
	return i, err
}

const ExistsIsnWithSlug = `-- name: ExistsIsnWithSlug :one

SELECT EXISTS
  (SELECT 1
   FROM isn
   WHERE slug = $1) AS EXISTS
`

func (q *Queries) ExistsIsnWithSlug(ctx context.Context, slug string) (bool, error) {
	row := q.db.QueryRow(ctx, ExistsIsnWithSlug, slug)
	var exists bool
	err := row.Scan(&exists)
	return exists, err
}

const GetForDisplayIsnBySlug = `-- name: GetForDisplayIsnBySlug :one
SELECT 
    id,
    created_at,
    updated_at,
    title,
    slug,
    detail,
    is_in_use,
    visibility
FROM isn 
WHERE slug = $1
`

type GetForDisplayIsnBySlugRow struct {
	ID         uuid.UUID `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Title      string    `json:"title"`
	Slug       string    `json:"slug"`
	Detail     string    `json:"detail"`
	IsInUse    bool      `json:"is_in_use"`
	Visibility string    `json:"visibility"`
}

func (q *Queries) GetForDisplayIsnBySlug(ctx context.Context, slug string) (GetForDisplayIsnBySlugRow, error) {
	row := q.db.QueryRow(ctx, GetForDisplayIsnBySlug, slug)
	var i GetForDisplayIsnBySlugRow
	err := row.Scan(
		&i.ID,
		&i.CreatedAt,
		&i.UpdatedAt,
		&i.Title,
		&i.Slug,
		&i.Detail,
		&i.IsInUse,
		&i.Visibility,
	)
	return i, err
}

const GetIsnByID = `-- name: GetIsnByID :one
SELECT i.id, i.created_at, i.updated_at, i.user_account_id, i.title, i.slug, i.detail, i.is_in_use, i.visibility 
FROM isn i
WHERE i.id = $1
`

func (q *Queries) GetIsnByID(ctx context.Context, id uuid.UUID) (Isn, error) {
	row := q.db.QueryRow(ctx, GetIsnByID, id)
	var i Isn
	err := row.Scan(
		&i.ID,
		&i.CreatedAt,
		&i.UpdatedAt,
		&i.UserAccountID,
		&i.Title,
		&i.Slug,
		&i.Detail,
		&i.IsInUse,
		&i.Visibility,
	)
	return i, err
}

const GetIsnBySignalTypeID = `-- name: GetIsnBySignalTypeID :one
SELECT i.id, i.created_at, i.updated_at, i.user_account_id, i.title, i.slug, i.detail, i.is_in_use, i.visibility 
FROM isn i
JOIN signal_types sd on sd.isn_id = i.id
WHERE sd.id = $1
`

func (q *Queries) GetIsnBySignalTypeID(ctx context.Context, id uuid.UUID) (Isn, error) {
	row := q.db.QueryRow(ctx, GetIsnBySignalTypeID, id)
	var i Isn
	err := row.Scan(
		&i.ID,
		&i.CreatedAt,
		&i.UpdatedAt,
		&i.UserAccountID,
		&i.Title,
		&i.Slug,
		&i.Detail,
		&i.IsInUse,
		&i.Visibility,
	)
	return i, err
}

const GetIsnBySlug = `-- name: GetIsnBySlug :one
SELECT i.id, i.created_at, i.updated_at, i.user_account_id, i.title, i.slug, i.detail, i.is_in_use, i.visibility 
FROM isn i
WHERE i.slug = $1
`

func (q *Queries) GetIsnBySlug(ctx context.Context, slug string) (Isn, error) {
	row := q.db.QueryRow(ctx, GetIsnBySlug, slug)
	var i Isn
	err := row.Scan(
		&i.ID,
		&i.CreatedAt,
		&i.UpdatedAt,
		&i.UserAccountID,
		&i.Title,
		&i.Slug,
		&i.Detail,
		&i.IsInUse,
		&i.Visibility,
	)
	return i, err
}

const GetIsns = `-- name: GetIsns :many
SELECT i.id, i.created_at, i.updated_at, i.user_account_id, i.title, i.slug, i.detail, i.is_in_use, i.visibility 
FROM isn i
`

func (q *Queries) GetIsns(ctx context.Context) ([]Isn, error) {
	rows, err := q.db.Query(ctx, GetIsns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Isn
	for rows.Next() {
		var i Isn
		if err := rows.Scan(
			&i.ID,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.UserAccountID,
			&i.Title,
			&i.Slug,
			&i.Detail,
			&i.IsInUse,
			&i.Visibility,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const UpdateIsn = `-- name: UpdateIsn :execrows
UPDATE isn SET (
    updated_at, 
    detail,
    is_in_use,
    visibility
) = (Now(), $2, $3, $4)
WHERE id = $1
`

type UpdateIsnParams struct {
	ID         uuid.UUID `json:"id"`
	Detail     string    `json:"detail"`
	IsInUse    bool      `json:"is_in_use"`
	Visibility string    `json:"visibility"`
}

func (q *Queries) UpdateIsn(ctx context.Context, arg UpdateIsnParams) (int64, error) {
	result, err := q.db.Exec(ctx, UpdateIsn,
		arg.ID,
		arg.Detail,
		arg.IsInUse,
		arg.Visibility,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

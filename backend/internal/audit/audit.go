package audit

import (
	"context"
	"database/sql"
	"time"
)

type Event struct {
	ID        int64     `json:"id"`
	ActorType string    `json:"actor_type"`
	ActorID   *string   `json:"actor_id"`
	Action    string    `json:"action"`
	Target    *string   `json:"target"`
	Detail    *string   `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}
type Repository struct{ db *sql.DB }

func New(db *sql.DB) *Repository { return &Repository{db: db} }
func (r *Repository) Record(ctx context.Context, actorType, actorID, action, target, detail string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO audit_events(actor_type,actor_id,action,target,detail,created_at) VALUES(?,?,?,?,?,?)`, actorType, actorID, action, target, detail, time.Now().UTC())
	return err
}
func (r *Repository) List(ctx context.Context, limit int) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,actor_type,actor_id,action,target,detail,created_at FROM audit_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &e.Target, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

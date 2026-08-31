package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"parfumes_finder/internal/domain"
)

type Session struct {
	ChatID  int64
	ID      string
	Stage   string
	Query   domain.SearchQuery
	Options []domain.SearchQuery
}

type sessionPayload struct {
	ID      string               `json:"id"`
	Query   domain.SearchQuery   `json:"query"`
	Options []domain.SearchQuery `json:"options,omitempty"`
}

type Sessions struct {
	db *sql.DB
}

func NewSessions(db *sql.DB) *Sessions {
	return &Sessions{db: db}
}

func (sessions *Sessions) Save(ctx context.Context, session Session) error {
	queryJSON, err := json.Marshal(sessionPayload{ID: session.ID, Query: session.Query, Options: session.Options})
	if err != nil {
		return fmt.Errorf("encode session query: %w", err)
	}
	_, err = sessions.db.ExecContext(ctx, `
        INSERT INTO telegram_sessions(chat_id, stage, query_json, updated_at)
        VALUES(?, ?, ?, ?)
        ON CONFLICT(chat_id) DO UPDATE SET
            stage = excluded.stage,
            query_json = excluded.query_json,
            updated_at = excluded.updated_at
    `, session.ChatID, session.Stage, queryJSON, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("save telegram session: %w", err)
	}
	return nil
}

func (sessions *Sessions) Load(ctx context.Context, chatID int64) (Session, bool, error) {
	var session Session
	var queryJSON []byte
	err := sessions.db.QueryRowContext(ctx, `SELECT chat_id, stage, query_json FROM telegram_sessions WHERE chat_id = ?`, chatID).Scan(&session.ChatID, &session.Stage, &queryJSON)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("load telegram session: %w", err)
	}
	var payload sessionPayload
	if err := json.Unmarshal(queryJSON, &payload); err != nil {
		return Session{}, false, fmt.Errorf("decode telegram session: %w", err)
	}
	session.ID, session.Query, session.Options = payload.ID, payload.Query, payload.Options
	return session, true, nil
}

func (sessions *Sessions) Delete(ctx context.Context, chatID int64) error {
	if _, err := sessions.db.ExecContext(ctx, `DELETE FROM telegram_sessions WHERE chat_id = ?`, chatID); err != nil {
		return fmt.Errorf("delete telegram session: %w", err)
	}
	return nil
}

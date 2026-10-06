package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ConversationRepo persists chat conversations.
type ConversationRepo struct {
	db *sql.DB
}

func NewConversationRepo(db *sql.DB) *ConversationRepo {
	return &ConversationRepo{db: db}
}

// ConversationPatch is a partial update for a conversation.
type ConversationPatch struct {
	Title     *string
	ProfileID *string
	ModelID   *string
	MemoryOff *bool
}

func (r *ConversationRepo) List(ctx context.Context) ([]contracts.Conversation, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, COALESCE(title,''), COALESCE(profile_id,''), COALESCE(model_id,''), memory_off, created_at, updated_at
		FROM conversations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contracts.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []contracts.Conversation{}
	}
	return out, rows.Err()
}

func (r *ConversationRepo) Get(ctx context.Context, id string) (contracts.Conversation, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(title,''), COALESCE(profile_id,''), COALESCE(model_id,''), memory_off, created_at, updated_at
		FROM conversations WHERE id = ?`, id)
	c, err := scanConversation(row)
	if err == sql.ErrNoRows {
		return contracts.Conversation{}, fmt.Errorf("conversation %q not found", id)
	}
	return c, err
}

func (r *ConversationRepo) Create(ctx context.Context, title, profileID, modelID string) (contracts.Conversation, error) {
	if title == "" {
		title = "New chat"
	}
	now := time.Now().UTC()
	c := contracts.Conversation{
		ID:        uuid.NewString(),
		Title:     title,
		ProfileID: profileID,
		ModelID:   modelID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO conversations (id, title, profile_id, model_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.Title, nullIfEmpty(c.ProfileID), nullIfEmpty(c.ModelID),
		c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (r *ConversationRepo) Update(ctx context.Context, id string, patch ConversationPatch) (contracts.Conversation, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return contracts.Conversation{}, err
	}
	if patch.Title != nil {
		existing.Title = *patch.Title
		if existing.Title == "" {
			existing.Title = "New chat"
		}
	}
	if patch.ProfileID != nil {
		existing.ProfileID = *patch.ProfileID
	}
	if patch.ModelID != nil {
		existing.ModelID = *patch.ModelID
	}
	if patch.MemoryOff != nil {
		existing.MemoryOff = *patch.MemoryOff
	}
	memoryOff := 0
	if existing.MemoryOff {
		memoryOff = 1
	}
	existing.UpdatedAt = time.Now().UTC()
	_, err = r.db.ExecContext(ctx, `
		UPDATE conversations
		SET title = ?, profile_id = ?, model_id = ?, memory_off = ?, updated_at = ?
		WHERE id = ?`,
		existing.Title, nullIfEmpty(existing.ProfileID), nullIfEmpty(existing.ModelID), memoryOff,
		existing.UpdatedAt.Format(time.RFC3339Nano), id)
	if err != nil {
		return contracts.Conversation{}, err
	}
	return existing, nil
}

func (r *ConversationRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("conversation %q not found", id)
	}
	return nil
}

func (r *ConversationRepo) AddMessage(ctx context.Context, conversationID, role, content string) (contracts.Message, error) {
	return r.AddMessageWithMeta(ctx, conversationID, role, content, nil)
}

// AddMessageWithMeta stores a message with the sources and steps behind it.
func (r *ConversationRepo) AddMessageWithMeta(ctx context.Context, conversationID, role, content string, meta *contracts.MessageMeta) (contracts.Message, error) {
	now := time.Now().UTC()
	m := contracts.Message{
		ID:             uuid.NewString(),
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		CreatedAt:      now,
		Meta:           meta,
	}
	var metaJSON any
	if meta != nil {
		b, _ := json.Marshal(meta)
		metaJSON = string(b)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO messages (id, conversation_id, role, content, meta_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, m.ConversationID, m.Role, m.Content, metaJSON, m.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return m, err
	}
	_, _ = r.db.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now.Format(time.RFC3339Nano), conversationID)
	return m, nil
}

// DeleteAll removes every conversation and message.
func (r *ConversationRepo) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM messages`); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM conversations`)
	return err
}

// ListMessages returns a conversation's messages in the order they were
// added. created_at can't order them: RFC3339Nano drops trailing zeros, so
// "05.12Z" sorts after "05.123Z" as text, and back-to-back messages can share
// a timestamp. AddMessageWithMeta is the only writer, so rowid is the order.
func (r *ConversationRepo) ListMessages(ctx context.Context, conversationID string) ([]contracts.Message, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, COALESCE(meta_json, ''), created_at
		FROM messages WHERE conversation_id = ? ORDER BY rowid ASC`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contracts.Message
	for rows.Next() {
		var m contracts.Message
		var created, meta string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &meta, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTime(created)
		if meta != "" {
			m.Meta = &contracts.MessageMeta{}
			if json.Unmarshal([]byte(meta), m.Meta) != nil {
				m.Meta = nil
			}
		}
		out = append(out, m)
	}
	if out == nil {
		out = []contracts.Message{}
	}
	return out, rows.Err()
}

type conversationScanner interface {
	Scan(dest ...any) error
}

func scanConversation(s conversationScanner) (contracts.Conversation, error) {
	var c contracts.Conversation
	var created, updated string
	var memoryOff int
	if err := s.Scan(&c.ID, &c.Title, &c.ProfileID, &c.ModelID, &memoryOff, &created, &updated); err != nil {
		return contracts.Conversation{}, err
	}
	c.MemoryOff = memoryOff != 0
	c.CreatedAt = parseTime(created)
	c.UpdatedAt = parseTime(updated)
	return c, nil
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

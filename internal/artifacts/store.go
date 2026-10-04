// Package artifacts keeps the files people attach to chats and the files the
// assistant produces, in one store (AI experience spec §28). The bytes live
// under the data directory; SQLite holds the index.
package artifacts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Producers.
const (
	ProducerUser      = "user"
	ProducerAssistant = "assistant"
)

// MaxBytes caps one file.
const MaxBytes = 25 << 20

// ErrNotFound is returned for an unknown artifact.
var ErrNotFound = contracts.NewError("FILE_NOT_FOUND", nil, errors.New("file not found"))

// Artifact is one stored file.
type Artifact struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Name           string `json:"name"`
	MimeType       string `json:"mime_type"`
	// Kind is document, spreadsheet, pdf, image, code, or other.
	Kind       string    `json:"kind"`
	Size       int64     `json:"size_bytes"`
	Producer   string    `json:"producer"`
	SourceTask string    `json:"source_task,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	path       string
}

// Input is a file to save.
type Input struct {
	ConversationID string
	Name           string
	Producer       string
	SourceTask     string
	Data           []byte
}

// Store saves and finds artifacts.
type Store struct {
	db  *sql.DB
	dir string
	now func() time.Time
}

// NewStore keeps files under dir.
func NewStore(db *sql.DB, dir string) *Store {
	return &Store{db: db, dir: dir, now: time.Now}
}

// CleanName keeps a file name safe to store and to offer as a download: no
// directories, no control characters, and a sensible length.
func CleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "file"
	}
	if r := []rune(name); len(r) > 120 {
		ext := filepath.Ext(name)
		name = string(r[:120-len([]rune(ext))]) + ext
	}
	return name
}

// MimeType guesses a file's type from its name.
func MimeType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".tsv":
		return "text/tab-separated-values; charset=utf-8"
	case ".jsonl":
		return "application/x-ndjson"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".webm":
		// A clip, or a voice recording from a browser; either plays as video.
		return "video/webm"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	if kindOf(name) == "code" {
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// IsAudio reports an audio file: one that can be played and transcribed.
// A .webm file counts: it is a video kind, but its sound can be transcribed.
func IsAudio(name string) bool {
	return kindOf(name) == "audio" || strings.EqualFold(filepath.Ext(name), ".webm")
}

func kindOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv", ".tsv", ".xlsx":
		return "spreadsheet"
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".wav", ".mp3", ".m4a", ".aac", ".ogg", ".flac":
		return "audio"
	case ".webm":
		return "video"
	case ".txt", ".md", ".markdown", ".html", ".htm", ".json", ".jsonl", ".log", ".docx":
		return "document"
	case ".py", ".js", ".ts", ".tsx", ".jsx", ".go", ".rs", ".java", ".kt", ".c", ".h", ".cpp", ".hpp", ".cs",
		".rb", ".php", ".swift", ".sh", ".sql", ".yaml", ".yml", ".toml", ".xml", ".css", ".ini":
		return "code"
	}
	return "other"
}

// Save stores a file and returns its record.
func (s *Store) Save(ctx context.Context, in Input) (Artifact, error) {
	if len(in.Data) > MaxBytes {
		return Artifact{}, fmt.Errorf("%s is larger than %d MB", in.Name, MaxBytes>>20)
	}
	name := CleanName(in.Name)
	producer := in.Producer
	if producer != ProducerAssistant {
		producer = ProducerUser
	}
	a := Artifact{
		ID: uuid.NewString(), ConversationID: in.ConversationID, Name: name, MimeType: MimeType(name),
		Kind: kindOf(name), Size: int64(len(in.Data)), Producer: producer, SourceTask: in.SourceTask,
		CreatedAt: s.now().UTC(),
	}
	folder := in.ConversationID
	if folder == "" {
		folder = "unfiled"
	}
	a.path = filepath.Join(folder, a.ID+filepath.Ext(name))
	full := filepath.Join(s.dir, a.path)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return Artifact{}, err
	}
	if err := os.WriteFile(full, in.Data, 0o600); err != nil {
		return Artifact{}, err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO artifacts (id, conversation_id, name, mime_type, kind, size_bytes, producer, source_task, path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, nullable(a.ConversationID), a.Name, a.MimeType, a.Kind, a.Size, a.Producer, nullable(a.SourceTask), a.path,
		a.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		_ = os.Remove(full)
		return Artifact{}, err
	}
	return a, nil
}

const columns = `id, COALESCE(conversation_id, ''), name, mime_type, kind, size_bytes, producer, COALESCE(source_task, ''), path, created_at`

func scan(row interface{ Scan(...any) error }) (Artifact, error) {
	var a Artifact
	var created string
	if err := row.Scan(&a.ID, &a.ConversationID, &a.Name, &a.MimeType, &a.Kind, &a.Size, &a.Producer, &a.SourceTask, &a.path, &created); err != nil {
		return Artifact{}, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return a, nil
}

// Get returns one artifact's record.
func (s *Store) Get(ctx context.Context, id string) (Artifact, error) {
	a, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM artifacts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

// Read returns an artifact's bytes.
func (s *Store) Read(ctx context.Context, id string) (Artifact, []byte, error) {
	a, err := s.Get(ctx, id)
	if err != nil {
		return Artifact{}, nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, a.path))
	if errors.Is(err, os.ErrNotExist) {
		return Artifact{}, nil, ErrNotFound
	}
	return a, data, err
}

// List returns a conversation's artifacts, oldest first.
func (s *Store) List(ctx context.Context, conversationID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM artifacts WHERE conversation_id = ? ORDER BY created_at`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Artifact{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Attach files an unfiled artifact under a conversation, such as a file
// uploaded before the chat it belongs to was created.
func (s *Store) Attach(ctx context.Context, id, conversationID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE artifacts SET conversation_id = ? WHERE id = ? AND conversation_id IS NULL`, conversationID, id)
	return err
}

// Delete removes one artifact and its file.
func (s *Store) Delete(ctx context.Context, id string) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, id); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.dir, a.path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// DeleteConversation removes a conversation's files. Their rows go with the
// conversation.
func (s *Store) DeleteConversation(ctx context.Context, conversationID string) error {
	if conversationID == "" || strings.ContainsAny(conversationID, `/\`) || strings.Contains(conversationID, "..") {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE conversation_id = ?`, conversationID); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.dir, conversationID))
}

// DeleteAll removes every artifact, for "delete all chats".
func (s *Store) DeleteAll(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artifacts`); err != nil {
		return err
	}
	return os.RemoveAll(s.dir)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type ctxKey struct{}

// WithAttachments carries the files attached to a chat message.
func WithAttachments(ctx context.Context, ids []string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, append([]string(nil), ids...))
}

// AttachmentsFrom returns the files attached to a chat message.
func AttachmentsFrom(ctx context.Context) []string {
	ids, _ := ctx.Value(ctxKey{}).([]string)
	return ids
}

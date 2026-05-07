package repo

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/domain/notepad"
	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/models"
)

var ErrNotFound = errors.New("notepad: not found")

type ListDocumentsParams struct {
	OwnerID uuid.UUID
	Page    int64
	PerPage int64
	Search  string
}

type ListDocumentsResult struct {
	Data    []models.NotepadDocument `json:"data"`
	Total   int64                    `json:"total"`
	Page    int64                    `json:"page"`
	PerPage int64                    `json:"per_page"`
}

type CreateDocumentParams struct {
	Title       string
	Description string
	OwnerID     uuid.UUID
	Content     string
	TemplateKey *string
	Widgets     json.RawMessage
}

type UpdateDocumentParams struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	Title         *string
	Description   *string
	Content       *string
	TemplateKey   *string
	Widgets       json.RawMessage
	LastIndexedAt *time.Time
}

type UpsertPresenceParams struct {
	DocumentID  uuid.UUID
	OwnerID     uuid.UUID
	UserID      uuid.UUID
	SessionID   string
	DisplayName string
	CursorLabel string
	Color       string
}

type NotepadRepository interface {
	ListDocuments(ctx context.Context, params ListDocumentsParams) (ListDocumentsResult, error)
	CreateDocument(ctx context.Context, params CreateDocumentParams) (models.NotepadDocument, error)
	GetDocument(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (models.NotepadDocument, bool, error)
	UpdateDocument(ctx context.Context, params UpdateDocumentParams) (models.NotepadDocument, bool, error)
	DeleteDocument(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (bool, error)
	ListPresence(ctx context.Context, documentID uuid.UUID, ownerID uuid.UUID) ([]models.NotepadPresence, error)
	UpsertPresence(ctx context.Context, params UpsertPresenceParams) (models.NotepadPresence, error)
}

type PostgresNotepadRepository struct{ Pool *pgxpool.Pool }

func NewPostgresNotepadRepository(pool *pgxpool.Pool) *PostgresNotepadRepository {
	return &PostgresNotepadRepository{Pool: pool}
}

func (r *PostgresNotepadRepository) ListDocuments(ctx context.Context, params ListDocumentsParams) (ListDocumentsResult, error) {
	page, perPage := normalizePage(params.Page, params.PerPage)
	pattern := "%" + params.Search + "%"
	out := ListDocumentsResult{Page: page, PerPage: perPage}
	if err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM notepad_documents WHERE owner_id = $1 AND (title ILIKE $2 OR description ILIKE $2)`, params.OwnerID, pattern).Scan(&out.Total); err != nil {
		return out, err
	}
	offset := (page - 1) * perPage
	rows, err := r.Pool.Query(ctx, `SELECT id, title, description, owner_id, content, template_key, widgets, last_indexed_at, created_at, updated_at
		FROM notepad_documents
		WHERE owner_id = $1 AND (title ILIKE $2 OR description ILIKE $2)
		ORDER BY updated_at DESC, created_at DESC
		LIMIT $3 OFFSET $4`, params.OwnerID, pattern, perPage, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return out, err
		}
		out.Data = append(out.Data, doc)
	}
	return out, rows.Err()
}

func (r *PostgresNotepadRepository) CreateDocument(ctx context.Context, params CreateDocumentParams) (models.NotepadDocument, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return models.NotepadDocument{}, err
	}
	widgets := params.Widgets
	if len(widgets) == 0 || string(widgets) == "null" {
		widgets = json.RawMessage(`[]`)
	}
	row := r.Pool.QueryRow(ctx, `INSERT INTO notepad_documents (id, title, description, owner_id, content, template_key, widgets)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, title, description, owner_id, content, template_key, widgets, last_indexed_at, created_at, updated_at`,
		id, params.Title, params.Description, params.OwnerID, params.Content, params.TemplateKey, string(widgets))
	return scanDocument(row)
}

func (r *PostgresNotepadRepository) GetDocument(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (models.NotepadDocument, bool, error) {
	row := r.Pool.QueryRow(ctx, `SELECT id, title, description, owner_id, content, template_key, widgets, last_indexed_at, created_at, updated_at FROM notepad_documents WHERE id = $1 AND owner_id = $2`, id, ownerID)
	doc, err := scanDocument(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.NotepadDocument{}, false, nil
	}
	return doc, err == nil, err
}

func (r *PostgresNotepadRepository) UpdateDocument(ctx context.Context, params UpdateDocumentParams) (models.NotepadDocument, bool, error) {
	row := r.Pool.QueryRow(ctx, `UPDATE notepad_documents
		SET title = COALESCE($3, title), description = COALESCE($4, description), content = COALESCE($5, content),
		    template_key = COALESCE($6, template_key), widgets = COALESCE($7, widgets), last_indexed_at = COALESCE($8, last_indexed_at), updated_at = NOW()
		WHERE id = $1 AND owner_id = $2
		RETURNING id, title, description, owner_id, content, template_key, widgets, last_indexed_at, created_at, updated_at`,
		params.ID, params.OwnerID, params.Title, params.Description, params.Content, params.TemplateKey, nullableJSON(params.Widgets), params.LastIndexedAt)
	doc, err := scanDocument(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.NotepadDocument{}, false, nil
	}
	return doc, err == nil, err
}

func (r *PostgresNotepadRepository) DeleteDocument(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (bool, error) {
	ct, err := r.Pool.Exec(ctx, `DELETE FROM notepad_documents WHERE id = $1 AND owner_id = $2`, id, ownerID)
	return ct.RowsAffected() > 0, err
}

func (r *PostgresNotepadRepository) ListPresence(ctx context.Context, documentID uuid.UUID, ownerID uuid.UUID) ([]models.NotepadPresence, error) {
	if err := r.cleanupPresence(ctx); err != nil {
		return nil, err
	}
	if _, ok, err := r.GetDocument(ctx, documentID, ownerID); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	rows, err := r.Pool.Query(ctx, `SELECT id, document_id, user_id, session_id, display_name, cursor_label, color, last_seen_at
		FROM notepad_presence WHERE document_id = $1 ORDER BY last_seen_at DESC`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.NotepadPresence{}
	for rows.Next() {
		p, err := scanPresence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PostgresNotepadRepository) UpsertPresence(ctx context.Context, params UpsertPresenceParams) (models.NotepadPresence, error) {
	if err := r.cleanupPresence(ctx); err != nil {
		return models.NotepadPresence{}, err
	}
	if _, ok, err := r.GetDocument(ctx, params.DocumentID, params.OwnerID); err != nil || !ok {
		if err != nil {
			return models.NotepadPresence{}, err
		}
		return models.NotepadPresence{}, ErrNotFound
	}
	id, err := uuid.NewV7()
	if err != nil {
		return models.NotepadPresence{}, err
	}
	row := r.Pool.QueryRow(ctx, `INSERT INTO notepad_presence (id, document_id, user_id, session_id, display_name, cursor_label, color)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (document_id, user_id, session_id)
		DO UPDATE SET display_name = EXCLUDED.display_name, cursor_label = EXCLUDED.cursor_label, color = EXCLUDED.color, last_seen_at = NOW()
		RETURNING id, document_id, user_id, session_id, display_name, cursor_label, color, last_seen_at`,
		id, params.DocumentID, params.UserID, params.SessionID, params.DisplayName, params.CursorLabel, params.Color)
	presence, err := scanPresence(row)
	if isForeignKeyViolation(err) {
		return models.NotepadPresence{}, ErrNotFound
	}
	return presence, err
}

func (r *PostgresNotepadRepository) cleanupPresence(ctx context.Context) error {
	_, err := r.Pool.Exec(ctx, notepad.CleanupStalePresenceSQL())
	return err
}

type InMemoryNotepadRepository struct {
	mu        sync.Mutex
	documents map[uuid.UUID]models.NotepadDocument
	presence  map[string]models.NotepadPresence
	now       func() time.Time
}

func NewInMemoryNotepadRepository() *InMemoryNotepadRepository {
	return &InMemoryNotepadRepository{documents: map[uuid.UUID]models.NotepadDocument{}, presence: map[string]models.NotepadPresence{}, now: func() time.Time { return time.Now().UTC() }}
}

func (r *InMemoryNotepadRepository) ListDocuments(_ context.Context, params ListDocumentsParams) (ListDocumentsResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	page, perPage := normalizePage(params.Page, params.PerPage)
	search := strings.ToLower(params.Search)
	matches := []models.NotepadDocument{}
	for _, doc := range r.documents {
		if doc.OwnerID != params.OwnerID {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(doc.Title), search) && !strings.Contains(strings.ToLower(doc.Description), search) {
			continue
		}
		matches = append(matches, doc)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if !matches[i].UpdatedAt.Equal(matches[j].UpdatedAt) {
			return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
		}
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})
	total := int64(len(matches))
	offset := int((page - 1) * perPage)
	if offset > len(matches) {
		offset = len(matches)
	}
	end := offset + int(perPage)
	if end > len(matches) {
		end = len(matches)
	}
	return ListDocumentsResult{Data: append([]models.NotepadDocument(nil), matches[offset:end]...), Total: total, Page: page, PerPage: perPage}, nil
}

func (r *InMemoryNotepadRepository) CreateDocument(_ context.Context, params CreateDocumentParams) (models.NotepadDocument, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, err := uuid.NewV7()
	if err != nil {
		return models.NotepadDocument{}, err
	}
	widgets := params.Widgets
	if len(widgets) == 0 || string(widgets) == "null" {
		widgets = json.RawMessage(`[]`)
	}
	now := r.now()
	doc := models.NotepadDocument{ID: id, Title: params.Title, Description: params.Description, OwnerID: params.OwnerID, Content: params.Content, TemplateKey: params.TemplateKey, Widgets: append(json.RawMessage(nil), widgets...), CreatedAt: now, UpdatedAt: now}
	r.documents[id] = doc
	return doc, nil
}

func (r *InMemoryNotepadRepository) GetDocument(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (models.NotepadDocument, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, ok := r.documents[id]
	if !ok || doc.OwnerID != ownerID {
		return models.NotepadDocument{}, false, nil
	}
	return doc, true, nil
}

func (r *InMemoryNotepadRepository) UpdateDocument(_ context.Context, params UpdateDocumentParams) (models.NotepadDocument, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, ok := r.documents[params.ID]
	if !ok || doc.OwnerID != params.OwnerID {
		return models.NotepadDocument{}, false, nil
	}
	if params.Title != nil {
		doc.Title = *params.Title
	}
	if params.Description != nil {
		doc.Description = *params.Description
	}
	if params.Content != nil {
		doc.Content = *params.Content
	}
	if params.TemplateKey != nil {
		doc.TemplateKey = params.TemplateKey
	}
	if len(params.Widgets) > 0 && string(params.Widgets) != "null" {
		doc.Widgets = append(json.RawMessage(nil), params.Widgets...)
	}
	if params.LastIndexedAt != nil {
		doc.LastIndexedAt = params.LastIndexedAt
	}
	doc.UpdatedAt = r.now()
	r.documents[params.ID] = doc
	return doc, true, nil
}

func (r *InMemoryNotepadRepository) DeleteDocument(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, ok := r.documents[id]
	if !ok || doc.OwnerID != ownerID {
		return false, nil
	}
	delete(r.documents, id)
	for key, presence := range r.presence {
		if presence.DocumentID == id {
			delete(r.presence, key)
		}
	}
	return true, nil
}

func (r *InMemoryNotepadRepository) ListPresence(_ context.Context, documentID uuid.UUID, ownerID uuid.UUID) ([]models.NotepadPresence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if doc, ok := r.documents[documentID]; !ok || doc.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	r.cleanupPresenceLocked()
	out := []models.NotepadPresence{}
	for _, p := range r.presence {
		if p.DocumentID == documentID {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out, nil
}

func (r *InMemoryNotepadRepository) UpsertPresence(_ context.Context, params UpsertPresenceParams) (models.NotepadPresence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if doc, ok := r.documents[params.DocumentID]; !ok || doc.OwnerID != params.OwnerID {
		return models.NotepadPresence{}, ErrNotFound
	}
	r.cleanupPresenceLocked()
	key := presenceKey(params.DocumentID, params.UserID, params.SessionID)
	p, ok := r.presence[key]
	if !ok {
		id, err := uuid.NewV7()
		if err != nil {
			return models.NotepadPresence{}, err
		}
		p.ID = id
		p.DocumentID = params.DocumentID
		p.UserID = params.UserID
		p.SessionID = params.SessionID
	}
	p.DisplayName = params.DisplayName
	p.CursorLabel = params.CursorLabel
	p.Color = params.Color
	p.LastSeenAt = r.now()
	r.presence[key] = p
	return p, nil
}

func (r *InMemoryNotepadRepository) cleanupPresenceLocked() {
	cutoff := r.now().Add(-5 * time.Minute)
	for key, p := range r.presence {
		if p.LastSeenAt.Before(cutoff) {
			delete(r.presence, key)
		}
	}
}

func normalizePage(page, perPage int64) (int64, int64) {
	if page < 1 {
		page = 1
	}
	if perPage == 0 {
		perPage = 20
	}
	if perPage < 1 {
		perPage = 1
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return string(raw)
}

func presenceKey(documentID, userID uuid.UUID, sessionID string) string {
	return documentID.String() + ":" + userID.String() + ":" + sessionID
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

type scanner interface{ Scan(dest ...any) error }

func scanDocument(row scanner) (models.NotepadDocument, error) {
	var doc models.NotepadDocument
	if err := row.Scan(&doc.ID, &doc.Title, &doc.Description, &doc.OwnerID, &doc.Content, &doc.TemplateKey, &doc.Widgets, &doc.LastIndexedAt, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
		return models.NotepadDocument{}, err
	}
	return doc, nil
}

func scanPresence(row scanner) (models.NotepadPresence, error) {
	var p models.NotepadPresence
	if err := row.Scan(&p.ID, &p.DocumentID, &p.UserID, &p.SessionID, &p.DisplayName, &p.CursorLabel, &p.Color, &p.LastSeenAt); err != nil {
		return models.NotepadPresence{}, err
	}
	return p, nil
}

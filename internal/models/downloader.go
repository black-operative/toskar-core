package models

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/events"
)

// Downloader handles resumable HTTP downloads with integrity verification.
type Downloader struct {
	storage *Storage
	bus     *events.Bus
	db      *sql.DB
	client  *http.Client

	// QuotaLimitBytes returns the soft storage cap for models (0 = unlimited).
	QuotaLimitBytes func(ctx context.Context) (uint64, error)

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewDownloader(storage *Storage, bus *events.Bus, db *sql.DB) *Downloader {
	return &Downloader{
		storage: storage,
		bus:     bus,
		db:      db,
		client:  &http.Client{Timeout: 0},
		cancels: make(map[string]context.CancelFunc),
	}
}

// Download starts or resumes a model download.
func (d *Downloader) Download(ctx context.Context, entry CatalogEntry) error {
	if err := d.storage.EnsureDirs(); err != nil {
		return err
	}

	avail, err := d.storage.AvailableDiskBytes()
	if err != nil {
		return err
	}
	need := entry.SizeBytes
	if need == 0 {
		need = 1
	}
	if avail < need {
		return fmt.Errorf("insufficient disk space: need %d bytes, have %d", need, avail)
	}
	if d.QuotaLimitBytes != nil {
		limit, qerr := d.QuotaLimitBytes(ctx)
		if qerr != nil {
			return qerr
		}
		if limit > 0 {
			used, uerr := d.storage.SumInstalledBytes(ctx)
			if uerr != nil {
				return uerr
			}
			if used+need > limit {
				return fmt.Errorf(
					"model storage limit reached: need %d more bytes, but only %d remain under your %d byte limit (using %d)",
					need, limit-used, limit, used,
				)
			}
		}
	}

	downloadID := uuid.NewString()
	tempPath := d.storage.TempPath(entry.ID)
	finalPath := d.storage.ModelPath(entry.ID)

	if err := os.MkdirAll(filepath.Dir(tempPath), 0o755); err != nil {
		return err
	}

	dctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.cancels[entry.ID] = cancel
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.cancels, entry.ID)
		d.mu.Unlock()
	}()

	_, _ = d.db.ExecContext(ctx, `
		INSERT INTO downloads (id, model_id, status, temp_path, bytes_total)
		VALUES (?, ?, 'downloading', ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		downloadID, entry.ID, tempPath, entry.SizeBytes)

	d.bus.Publish(events.New(events.ModelDownloadStarted, map[string]any{
		"model_id": entry.ID,
	}))

	var offset int64
	if st, err := os.Stat(tempPath); err == nil {
		offset = st.Size()
	}

	req, err := http.NewRequestWithContext(dctx, http.MethodGet, entry.Source.URL, nil)
	if err != nil {
		return d.fail(ctx, entry.ID, downloadID, err)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return d.fail(ctx, entry.ID, downloadID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return d.fail(ctx, entry.ID, downloadID, fmt.Errorf("download failed: HTTP %d", resp.StatusCode))
	}

	total := entry.SizeBytes
	if resp.ContentLength > 0 {
		total = uint64(resp.ContentLength) + uint64(offset)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 && resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		offset = 0
	}

	f, err := os.OpenFile(tempPath, flags, 0o644)
	if err != nil {
		return d.fail(ctx, entry.ID, downloadID, err)
	}
	defer f.Close()

	hasher := sha256.New()
	if offset > 0 {
		// Hash existing partial content.
		existing, err := os.ReadFile(tempPath)
		if err != nil {
			return d.fail(ctx, entry.ID, downloadID, err)
		}
		hasher.Write(existing)
	}

	buf := make([]byte, 32*1024)
	downloaded := uint64(offset)
	lastEmit := time.Now()

	for {
		select {
		case <-dctx.Done():
			return d.fail(ctx, entry.ID, downloadID, dctx.Err())
		default:
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return d.fail(ctx, entry.ID, downloadID, werr)
			}
			hasher.Write(buf[:n])
			downloaded += uint64(n)
			_, _ = d.db.ExecContext(ctx, `
				UPDATE downloads SET bytes_downloaded = ?, bytes_total = ?, updated_at = datetime('now') WHERE model_id = ?`,
				downloaded, total, entry.ID)
			if time.Since(lastEmit) > 250*time.Millisecond {
				d.emitProgress(entry.ID, downloaded, total)
				lastEmit = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return d.fail(ctx, entry.ID, downloadID, readErr)
		}
	}
	d.emitProgress(entry.ID, downloaded, total)

	sum := hex.EncodeToString(hasher.Sum(nil))
	if entry.Source.SHA256 != "" && sum != entry.Source.SHA256 {
		_ = os.Remove(tempPath)
		return d.fail(ctx, entry.ID, downloadID, fmt.Errorf("sha256 mismatch: expected %s got %s", entry.Source.SHA256, sum))
	}

	if err := os.Rename(tempPath, finalPath); err != nil {
		return d.fail(ctx, entry.ID, downloadID, err)
	}

	if err := d.storage.MarkInstalled(ctx, entry.ID, finalPath, sum, downloaded); err != nil {
		return err
	}
	_, _ = d.db.ExecContext(ctx, `UPDATE downloads SET status = 'completed', updated_at = datetime('now') WHERE model_id = ?`, entry.ID)
	d.bus.Publish(events.New(events.ModelDownloadCompleted, map[string]any{
		"model_id": entry.ID,
		"path":     finalPath,
	}))
	return nil
}

func (d *Downloader) Cancel(modelID string) {
	d.mu.Lock()
	cancel, ok := d.cancels[modelID]
	d.mu.Unlock()
	if ok {
		cancel()
	}
}

func (d *Downloader) emitProgress(modelID string, downloaded, total uint64) {
	pct := float64(0)
	if total > 0 {
		pct = float64(downloaded) / float64(total) * 100
	}
	d.bus.Publish(events.New(events.ModelDownloadProgress, map[string]any{
		"model_id":         modelID,
		"bytes_downloaded": downloaded,
		"bytes_total":      total,
		"percent":          pct,
	}))
}

func (d *Downloader) fail(ctx context.Context, modelID, downloadID string, err error) error {
	_, _ = d.db.ExecContext(ctx, `UPDATE downloads SET status = 'failed', error = ?, updated_at = datetime('now') WHERE model_id = ?`,
		err.Error(), modelID)
	d.bus.Publish(events.New(events.ModelDownloadFailed, map[string]any{
		"model_id": modelID,
		"error":    err.Error(),
	}))
	return err
}

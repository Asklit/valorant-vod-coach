package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func (s Store) SaveUpload(ctx context.Context, record app.UploadRecord) error {
	if s.DB == nil {
		return errors.New("postgres store requires DB")
	}
	if strings.TrimSpace(record.VOD.Label) == "" || strings.TrimSpace(record.VOD.OwnerID) == "" {
		return errors.New("upload label and owner ID are required")
	}
	media, err := json.Marshal(record.Media)
	if err != nil {
		return fmt.Errorf("marshal upload media: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertVOD(ctx, tx, record.VOD); err != nil {
		return err
	}
	createdAt := record.VOD.UploadedAt
	if createdAt.IsZero() {
		createdAt = s.now()
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO uploaded_vods (
  label, owner_id, video_path, video_object_key, video_filename, size_bytes, media, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, now())
ON CONFLICT (label) DO UPDATE SET
  owner_id = EXCLUDED.owner_id,
  video_path = EXCLUDED.video_path,
  video_object_key = EXCLUDED.video_object_key,
  video_filename = EXCLUDED.video_filename,
  size_bytes = EXCLUDED.size_bytes,
  media = EXCLUDED.media,
  updated_at = now()
`,
		record.VOD.Label,
		record.VOD.OwnerID,
		record.VideoPath,
		record.VideoObjectKey,
		record.VideoFilename,
		record.SizeBytes,
		string(media),
		createdAt.UTC(),
	); err != nil {
		return fmt.Errorf("upsert uploaded VOD: %w", err)
	}
	return tx.Commit()
}

func (s Store) ListUploads(ctx context.Context, ownerID string, includeAll bool) ([]app.UploadRecord, error) {
	if s.DB == nil {
		return nil, errors.New("postgres store requires DB")
	}
	rows, err := s.DB.QueryContext(ctx, uploadSelect+`
WHERE ($2 OR uploads.owner_id = $1)
ORDER BY uploads.created_at DESC, uploads.label
`, strings.TrimSpace(ownerID), includeAll)
	if err != nil {
		return nil, fmt.Errorf("list uploaded VODs: %w", err)
	}
	defer rows.Close()
	return scanUploadRows(rows)
}

func (s Store) FindUpload(ctx context.Context, label string, ownerID string, includeAll bool) (app.UploadRecord, bool, error) {
	if s.DB == nil {
		return app.UploadRecord{}, false, errors.New("postgres store requires DB")
	}
	row := s.DB.QueryRowContext(ctx, uploadSelect+`
WHERE uploads.label = $1 AND ($3 OR uploads.owner_id = $2)
`, strings.TrimSpace(label), strings.TrimSpace(ownerID), includeAll)
	record, err := scanUpload(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.UploadRecord{}, false, nil
	}
	if err != nil {
		return app.UploadRecord{}, false, fmt.Errorf("find uploaded VOD: %w", err)
	}
	return record, true, nil
}

func (s Store) DeleteUpload(ctx context.Context, label string, ownerID string, includeAll bool) error {
	if s.DB == nil {
		return errors.New("postgres store requires DB")
	}
	result, err := s.DB.ExecContext(ctx, `
DELETE FROM vods
WHERE label = $1
  AND EXISTS (
    SELECT 1 FROM uploaded_vods uploads
    WHERE uploads.label = vods.label
      AND ($3 OR uploads.owner_id = $2)
  )
`, strings.TrimSpace(label), strings.TrimSpace(ownerID), includeAll)
	if err != nil {
		return fmt.Errorf("delete uploaded VOD: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return errors.New("uploaded VOD not found")
	}
	return nil
}

const uploadSelect = `
SELECT
  vods.label,
  vods.video_id,
  vods.rank,
  vods.source_url,
  vods.title,
  vods.channel,
  vods.manifest_duration_seconds,
  vods.map_name,
  vods.agent,
  vods.owner_id,
  vods.source_type,
  vods.original_filename,
  vods.uploaded_at,
  uploads.video_path,
  uploads.video_object_key,
  uploads.video_filename,
  uploads.size_bytes,
  uploads.media,
  uploads.updated_at
FROM uploaded_vods uploads
JOIN vods ON vods.label = uploads.label
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUpload(scanner rowScanner) (app.UploadRecord, error) {
	var record app.UploadRecord
	var rank string
	var uploadedAt sql.NullTime
	var media []byte
	err := scanner.Scan(
		&record.VOD.Label,
		&record.VOD.VideoID,
		&rank,
		&record.VOD.SourceURL,
		&record.VOD.Title,
		&record.VOD.Channel,
		&record.VOD.ManifestDurationSeconds,
		&record.VOD.Map,
		&record.VOD.Agent,
		&record.VOD.OwnerID,
		&record.VOD.SourceType,
		&record.VOD.OriginalFilename,
		&uploadedAt,
		&record.VideoPath,
		&record.VideoObjectKey,
		&record.VideoFilename,
		&record.SizeBytes,
		&media,
		&record.UpdatedAt,
	)
	if err != nil {
		return app.UploadRecord{}, err
	}
	record.VOD.Rank = domain.Rank(rank)
	if uploadedAt.Valid {
		record.VOD.UploadedAt = uploadedAt.Time.UTC()
	}
	if err := json.Unmarshal(media, &record.Media); err != nil {
		return app.UploadRecord{}, fmt.Errorf("decode upload media: %w", err)
	}
	return record, nil
}

func scanUploadRows(rows *sql.Rows) ([]app.UploadRecord, error) {
	records := make([]app.UploadRecord, 0)
	for rows.Next() {
		record, err := scanUpload(rows)
		if err != nil {
			return nil, fmt.Errorf("scan uploaded VOD: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate uploaded VODs: %w", err)
	}
	return records, nil
}

var _ app.UploadCatalog = Store{}

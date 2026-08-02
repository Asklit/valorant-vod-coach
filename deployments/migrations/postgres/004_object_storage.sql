ALTER TABLE uploaded_vods
  ADD COLUMN IF NOT EXISTS video_object_key text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS uploaded_vods_video_object_key_idx
  ON uploaded_vods (video_object_key)
  WHERE video_object_key <> '';

package sqlite

import (
	"context"
	"errors"

	"maragu.dev/glue/sql"

	"app/model"
)

// trackRow is the tracks table shape.
type trackRow struct {
	URI          model.ATURI
	DID          model.DID
	RKey         model.RecordKey
	CID          model.CID
	Title        string
	Description  string
	CreatedAt    model.Time `db:"created_at"`
	BlobCID      model.CID  `db:"blob_cid"`
	BlobMIMEType string     `db:"blob_mime_type"`
	BlobSize     int64      `db:"blob_size"`
	IndexedAt    model.Time `db:"indexed_at"`
}

func (r trackRow) track() model.Track {
	return model.Track{
		URI:         r.URI,
		DID:         r.DID,
		RecordKey:   r.RKey,
		CID:         r.CID,
		Title:       r.Title,
		Description: r.Description,
		Created:     r.CreatedAt,
		Audio:       model.Blob{CID: r.BlobCID, MIMEType: r.BlobMIMEType, Size: r.BlobSize},
		Indexed:     r.IndexedAt,
	}
}

// SaveTrack, the version of the record with the track's CID, and report whether anything changed. Saving
// the version already saved is a no-op, and another version of the same record replaces the saved one,
// with a new indexed time. The track's own indexed time is ignored.
func (d *Database) SaveTrack(ctx context.Context, t model.Track) (bool, error) {
	query := `
		insert into tracks (uri, did, rkey, cid, title, description, created_at, blob_cid, blob_mime_type, blob_size)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict (uri) do update set
			cid = excluded.cid,
			title = excluded.title,
			description = excluded.description,
			created_at = excluded.created_at,
			blob_cid = excluded.blob_cid,
			blob_mime_type = excluded.blob_mime_type,
			blob_size = excluded.blob_size,
			indexed_at = strftime('%Y-%m-%dT%H:%M:%fZ')
		where cid != excluded.cid
		returning uri`
	var uri model.ATURI
	err := d.H.Get(ctx, &uri, query, t.URI, t.DID, t.RecordKey, t.CID, t.Title, t.Description, t.Created,
		t.Audio.CID, t.Audio.MIMEType, t.Audio.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetTracksByDID of the tracks by the account with the given DID, newest first by their own createdAt.
func (d *Database) GetTracksByDID(ctx context.Context, did model.DID) ([]model.Track, error) {
	var rows []trackRow
	if err := d.H.Select(ctx, &rows, `select * from tracks where did = ? order by created_at desc, uri desc`, did); err != nil {
		return nil, err
	}
	tracks := make([]model.Track, 0, len(rows))
	for _, r := range rows {
		tracks = append(tracks, r.track())
	}
	return tracks, nil
}

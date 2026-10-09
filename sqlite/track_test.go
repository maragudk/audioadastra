package sqlite_test

import (
	"testing"
	"time"

	"maragu.dev/is"

	"app/model"
	"app/sqlitetest"
)

func TestDatabase_SaveTrack(t *testing.T) {
	t.Run("should save a new track and get it back", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)
		track := newTrack("3l2b5x3qe4c2a", "bafyreia", time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))

		changed, err := db.SaveTrack(t.Context(), track)
		is.NotError(t, err)
		is.True(t, changed)

		tracks, err := db.GetTracksByDID(t.Context(), aliceDID)
		is.NotError(t, err)
		is.Equal(t, 1, len(tracks))
		got := tracks[0]
		is.Equal(t, track.URI, got.URI)
		is.Equal(t, model.DID(aliceDID), got.DID)
		is.Equal(t, track.RecordKey, got.RecordKey)
		is.Equal(t, track.CID, got.CID)
		is.Equal(t, "Sounds of Earth", got.Title)
		is.Equal(t, "Whale song.", got.Description)
		is.True(t, track.Created.T.Equal(got.Created.T), got.Created.String())
		is.Equal(t, track.Audio, got.Audio)
		is.True(t, !got.Indexed.T.IsZero(), "no indexed time")
	})

	t.Run("should do nothing when saving the same version again", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)
		track := newTrack("3l2b5x3qe4c2a", "bafyreia", time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
		_, err := db.SaveTrack(t.Context(), track)
		is.NotError(t, err)
		before, err := db.GetTracksByDID(t.Context(), aliceDID)
		is.NotError(t, err)

		track.Title = "Changed without a new CID"
		changed, err := db.SaveTrack(t.Context(), track)
		is.NotError(t, err)
		is.True(t, !changed, "saved again")

		after, err := db.GetTracksByDID(t.Context(), aliceDID)
		is.NotError(t, err)
		is.Equal(t, 1, len(after))
		is.Equal(t, "Sounds of Earth", after[0].Title)
		is.Equal(t, before[0].Indexed, after[0].Indexed)
	})

	t.Run("should replace the saved version with a new version of the same record", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)
		track := newTrack("3l2b5x3qe4c2a", "bafyreia", time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
		_, err := db.SaveTrack(t.Context(), track)
		is.NotError(t, err)

		track.CID = "bafyreib"
		track.Title = "Sounds of Earth (edit)"
		changed, err := db.SaveTrack(t.Context(), track)
		is.NotError(t, err)
		is.True(t, changed)

		tracks, err := db.GetTracksByDID(t.Context(), aliceDID)
		is.NotError(t, err)
		is.Equal(t, 1, len(tracks))
		is.Equal(t, model.CID("bafyreib"), tracks[0].CID)
		is.Equal(t, "Sounds of Earth (edit)", tracks[0].Title)
	})
}

func TestDatabase_GetTracksByDID(t *testing.T) {
	t.Run("should list the account's tracks newest first, and no one else's", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)
		older := newTrack("3l2b5x3qe4c2a", "bafyreia", time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
		newer := newTrack("3l2b5x3qe4c2b", "bafyreib", time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
		other := newTrack("3l2b5x3qe4c2c", "bafyreic", time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
		other.DID = "did:plc:bobbobbobbobbobbobbobbob"
		other.URI = "at://did:plc:bobbobbobbobbobbobbobbob/com.audioadastra.track/3l2b5x3qe4c2c"
		for _, track := range []model.Track{older, other, newer} {
			_, err := db.SaveTrack(t.Context(), track)
			is.NotError(t, err)
		}

		tracks, err := db.GetTracksByDID(t.Context(), aliceDID)
		is.NotError(t, err)
		is.Equal(t, 2, len(tracks))
		is.Equal(t, newer.URI, tracks[0].URI)
		is.Equal(t, older.URI, tracks[1].URI)
	})

	t.Run("should list nothing for an account without tracks", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		tracks, err := db.GetTracksByDID(t.Context(), aliceDID)
		is.NotError(t, err)
		is.Equal(t, 0, len(tracks))
	})
}

func newTrack(rkey model.RecordKey, cid model.CID, created time.Time) model.Track {
	return model.Track{
		URI:         model.ATURI("at://" + aliceDID + "/com.audioadastra.track/" + rkey.String()),
		DID:         aliceDID,
		RecordKey:   rkey,
		CID:         cid,
		Title:       "Sounds of Earth",
		Description: "Whale song.",
		Created:     model.Time{T: created},
		Audio:       model.Blob{CID: "bafkreiaudio", MIMEType: "audio/flac", Size: 4242},
	}
}

package model

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Track is a com.audioadastra.track record as the app has indexed it.
type Track struct {
	URI       ATURI
	DID       DID
	RecordKey RecordKey
	// CID of the version of the record that was indexed.
	CID         CID
	Title       string
	Description string
	// Created is the record's createdAt, as its author declared it.
	Created Time
	// Audio is the original upload, the record's audio.original.
	Audio Blob
	// Indexed is when the app indexed this version of the record.
	Indexed Time
}

// TrackUpload is what a musician hands over to publish a track: the audio file, already on local disk,
// and the fields of the record.
type TrackUpload struct {
	Title       string
	Description string
	// AudioPath of the uploaded file on local disk.
	AudioPath string
}

// Limits of the track record's text fields, as the com.audioadastra.track lexicon sets them. The
// grapheme limits are the ones people see; the byte limits only bind for text heavy in multi-byte
// characters.
const (
	TrackTitleMaxGraphemes       = 300
	TrackTitleMaxBytes           = 3000
	TrackDescriptionMaxGraphemes = 5000
	TrackDescriptionMaxBytes     = 20000
)

// Normalized upload, with surrounding whitespace trimmed from the title and description, and checked
// against the lexicon's limits.
//
// Errors are [ErrorTrackTextInvalid], [ErrorTrackTitleMissing], [ErrorTrackTitleTooLong] and
// [ErrorTrackDescriptionTooLong].
func (u TrackUpload) Normalized() (TrackUpload, error) {
	u.Title = strings.TrimSpace(u.Title)
	u.Description = strings.TrimSpace(u.Description)

	switch {
	case !utf8.ValidString(u.Title) || !utf8.ValidString(u.Description):
		return u, ErrorTrackTextInvalid
	case u.Title == "":
		return u, ErrorTrackTitleMissing
	case len(u.Title) > TrackTitleMaxBytes || uniseg.GraphemeClusterCount(u.Title) > TrackTitleMaxGraphemes:
		return u, ErrorTrackTitleTooLong
	case len(u.Description) > TrackDescriptionMaxBytes || uniseg.GraphemeClusterCount(u.Description) > TrackDescriptionMaxGraphemes:
		return u, ErrorTrackDescriptionTooLong
	}
	return u, nil
}

// AudioInfo is what probing an audio file found out about it.
type AudioInfo struct {
	// Format of the container, by the names of its demuxer, such as "flac" or "mov,mp4,m4a,3gp,3g2,mj2".
	Format string
	// Codec of the audio stream, such as "flac" or "mp3".
	Codec      string
	Duration   time.Duration
	Channels   int
	SampleRate int
	// MIMEType for the format, always an audio type.
	MIMEType string
}

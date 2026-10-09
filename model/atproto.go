package model

import (
	"encoding/json/v2"
	"fmt"
)

// NSID is a namespaced identifier, naming a lexicon: a record collection, such as
// com.audioadastra.actor.profile, or an XRPC method.
type NSID string

// String satisfies [fmt.Stringer].
func (n NSID) String() string {
	return string(n)
}

var _ fmt.Stringer = NSID("")

// CollectionActorProfile is the NSID of the account profile record collection, whose one record has
// the record key [RecordKeySelf].
const CollectionActorProfile NSID = "com.audioadastra.actor.profile"

// CollectionTrack is the NSID of the track record collection, whose records have TID keys.
const CollectionTrack NSID = "com.audioadastra.track"

// ATURI names a record, such as at://did:plc:xj4bpglaht36jqc4dopoh3va/com.audioadastra.track/3l2b5x3qe4c2a.
type ATURI string

// String satisfies [fmt.Stringer].
func (u ATURI) String() string {
	return string(u)
}

var _ fmt.Stringer = ATURI("")

// CID is a content identifier, the hash of a record or a blob, in its string form.
type CID string

// String satisfies [fmt.Stringer].
func (c CID) String() string {
	return string(c)
}

var _ fmt.Stringer = CID("")

// RecordRef is where a record was written, and the CID of the version that was written.
type RecordRef struct {
	URI       ATURI
	RecordKey RecordKey
	CID       CID
}

// Blob is a file in an account's repository, as its PDS describes it. The MIME type is the one the PDS
// settled on, which may be its own reading of the bytes rather than what the uploader declared.
type Blob struct {
	CID      CID
	MIMEType string
	Size     int64
}

// MarshalJSON in the atproto data model's JSON form of a blob, so a [Blob] can stand in a record.
func (b Blob) MarshalJSON() ([]byte, error) {
	type link struct {
		Link string `json:"$link"`
	}
	return json.Marshal(struct {
		Type     string `json:"$type"`
		Ref      link   `json:"ref"`
		MIMEType string `json:"mimeType"`
		Size     int64  `json:"size"`
	}{Type: "blob", Ref: link{Link: b.CID.String()}, MIMEType: b.MIMEType, Size: b.Size})
}

// ServerDescription of a PDS, as far as the app needs it.
type ServerDescription struct {
	// BlobUploadLimit in bytes, or 0 when the PDS does not say.
	BlobUploadLimit int64
}

// RecordKey of a record within its collection in a repository.
type RecordKey string

// String satisfies [fmt.Stringer].
func (k RecordKey) String() string {
	return string(k)
}

var _ fmt.Stringer = RecordKey("")

// RecordKeySelf is the record key of a collection that holds one record per account, such as the
// profile.
const RecordKeySelf RecordKey = "self"

// DID is an atproto decentralized identifier, such as did:plc:xj4bpglaht36jqc4dopoh3va. It is the
// durable identity of an account; handles are mutable.
type DID string

// String satisfies [fmt.Stringer].
func (d DID) String() string {
	return string(d)
}

var _ fmt.Stringer = DID("")

// Handle of an atproto account, such as alice.bsky.social. Handles are mutable and verified against
// the DID they claim; one that does not verify is [HandleInvalid].
type Handle string

// HandleInvalid is the handle the atproto spec reserves for an account whose declared handle does not
// point back at its DID.
const HandleInvalid Handle = "handle.invalid"

// String satisfies [fmt.Stringer].
func (h Handle) String() string {
	return string(h)
}

var _ fmt.Stringer = Handle("")

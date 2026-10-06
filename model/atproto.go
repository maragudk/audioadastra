package model

import "fmt"

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

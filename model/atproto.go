package model

import "fmt"

// CollectionActorProfile is the NSID of the account profile record collection, whose one record has
// the fixed record key "self".
const CollectionActorProfile = "com.audioadastra.actor.profile"

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

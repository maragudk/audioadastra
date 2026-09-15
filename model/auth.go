package model

import (
	"fmt"

	"maragu.dev/glue/model"
)

type UserID = model.UserID

// DID is an atproto decentralized identifier, such as did:plc:abc123. It is the durable identity of a
// user; handles are mutable and never stored here.
type DID string

// String satisfies [fmt.Stringer].
func (d DID) String() string {
	return string(d)
}

var _ fmt.Stringer = DID("")

type User struct {
	ID      UserID
	Created Time
	Updated Time
	DID     DID
	Active  bool
}

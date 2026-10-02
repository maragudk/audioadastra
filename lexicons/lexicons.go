// Package lexicons holds the com.audioadastra lexicon schemas and validates records against them
// before they are written to a repository.
package lexicons

import (
	"embed"

	"github.com/bluesky-social/indigo/atproto/lexicon"
)

//go:embed com
var schemas embed.FS

// Catalog of every schema in this package.
type Catalog struct {
	base *lexicon.BaseCatalog
}

// NewCatalog with every schema in this package loaded.
func NewCatalog() (*Catalog, error) {
	base := lexicon.NewBaseCatalog()
	if err := base.LoadEmbedFS(schemas); err != nil {
		return nil, err
	}
	return &Catalog{base: base}, nil
}

// ValidateRecord against the schema of the given NSID, which the record's $type must match.
func (c *Catalog) ValidateRecord(record map[string]any, nsid string) error {
	return lexicon.ValidateRecord(c.base, record, nsid, 0)
}

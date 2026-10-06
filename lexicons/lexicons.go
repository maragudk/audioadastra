// Package lexicons holds the com.audioadastra lexicon schemas and validates records against them
// before they are written to a repository.
package lexicons

import (
	"embed"

	"github.com/bluesky-social/indigo/atproto/lexicon"

	"app/model"
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

// ValidateRecord against the schema of the given collection, which the record's $type must match as a
// string.
func (c *Catalog) ValidateRecord(record map[string]any, collection model.NSID) error {
	return lexicon.ValidateRecord(c.base, record, collection.String(), 0)
}

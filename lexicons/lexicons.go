// Package lexicons holds the com.audioadastra lexicon schemas and validates records against them
// before they are written to a repository.
package lexicons

import (
	"embed"
	"encoding/json/v2"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atdata"
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
// string. The record is validated in the JSON form it is written in, so values that marshal themselves,
// such as a [model.Blob], are checked as what they turn into.
func (c *Catalog) ValidateRecord(record map[string]any, collection model.NSID) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshaling record: %w", err)
	}
	data, err := atdata.UnmarshalJSON(raw)
	if err != nil {
		return fmt.Errorf("reading record as atproto data: %w", err)
	}
	return lexicon.ValidateRecord(c.base, data, collection.String(), 0)
}

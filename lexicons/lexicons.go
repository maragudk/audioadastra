// Package lexicons holds the com.audioadastra lexicon schemas and loads them into a catalog for
// validating records before they are written to a repository.
package lexicons

import (
	"embed"

	"github.com/bluesky-social/indigo/atproto/lexicon"
)

//go:embed com
var schemas embed.FS

// NewCatalog with every schema in this package loaded.
func NewCatalog() (*lexicon.BaseCatalog, error) {
	cat := lexicon.NewBaseCatalog()
	if err := cat.LoadEmbedFS(schemas); err != nil {
		return nil, err
	}
	return cat, nil
}

package html

import "context"

// Viewer is what the layout knows about the logged-in user: only the handle, which the nav renders.
type Viewer struct {
	Handle string
}

type contextKey string

const contextViewerKey = contextKey("viewer")

// ContextWithViewer for the layout to render, on pages of a logged-in user.
func ContextWithViewer(ctx context.Context, v Viewer) context.Context {
	return context.WithValue(ctx, contextViewerKey, v)
}

// viewerFromContext, and whether there is one, which there is not for logged-out pages and for pages
// rendered without a request context.
func viewerFromContext(ctx context.Context) (Viewer, bool) {
	if ctx == nil {
		return Viewer{}, false
	}
	v, ok := ctx.Value(contextViewerKey).(Viewer)
	return v, ok
}

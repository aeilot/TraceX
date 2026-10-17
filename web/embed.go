// Package web contains the offline profiler interface bundled into TraceX.
package web

import "embed"

// Files contains the complete client application; it needs no external assets.
//
//go:embed index.html app.js profile.js styles.css
var Files embed.FS

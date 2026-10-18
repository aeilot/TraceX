// Package web contains the offline profiler interface bundled into TraceX.
package web

import "embed"

// Files contains the complete client application; it needs no external assets.
//
//go:embed index.html i18n.js app.js profile.js styles.css lab.css experiments.js experiments.css experiment-model.js workspace.js workspace-model.js workspace.css motion.css motion.js insight-model.js insights.js insights.css
var Files embed.FS

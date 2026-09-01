// Package webproto embeds the canvas app so `cf serve` can serve the full
// GUI from the one binary — no python dev proxy required. The embed directive
// must live in this directory (go:embed cannot reach across package
// boundaries or use ".." paths), which is why this file sits among the assets
// it embeds rather than in cmd/cf.
package webproto

import "embed"

// Files holds the canvas app: index.html at the FS root plus its css/ and js/
// asset trees, embedded recursively. serve.py, README.md and
// prototype-source.html are deliberately NOT embedded — they are dev-workflow
// files (the standalone proxy stays supported for the edit-reload loop; see
// serve.py), not app assets a served canvas ever fetches.
//
//go:embed index.html css js
var Files embed.FS

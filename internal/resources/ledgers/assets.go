package ledgers

import (
	"embed"
)

//go:embed assets/reindex/v2.0.0/*.yaml
var reindexStreams embed.FS

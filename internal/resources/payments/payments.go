package payments

import (
	_ "embed"
)

//go:embed Caddyfile.gotpl
var Caddyfile string

//go:embed Caddyfile_v3.gotpl
var CaddyfileV3 string

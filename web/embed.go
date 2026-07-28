package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js force.js style.css vendor/cytoscape.min.js
var assets embed.FS

func FS() fs.FS { return assets }

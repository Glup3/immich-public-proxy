package public

import (
	"embed"
	"io/fs"
)

//go:embed **
var FS embed.FS

func Assets() (fs.FS, error) {
	return fs.Sub(FS, ".")
}

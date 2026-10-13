package gui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icon.png
var iconPNG []byte

func IconResource() fyne.Resource {
	return fyne.NewStaticResource("vclient.png", iconPNG)
}

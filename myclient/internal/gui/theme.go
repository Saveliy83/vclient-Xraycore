package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type fixedVariantTheme struct {
	fyne.Theme
	variant fyne.ThemeVariant
}

func (selected fixedVariantTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return selected.Theme.Color(name, selected.variant)
}

func ThemeFor(name string) fyne.Theme {
	switch name {
	case "dark":
		return fixedVariantTheme{Theme: theme.DefaultTheme(), variant: theme.VariantDark}
	case "light":
		return fixedVariantTheme{Theme: theme.DefaultTheme(), variant: theme.VariantLight}
	default:
		return theme.DefaultTheme()
	}
}

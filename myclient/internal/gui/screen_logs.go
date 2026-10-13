package gui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"example.com/vclient/internal/app"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type logsScreen struct {
	main    *MainWindow
	root    fyne.CanvasObject
	list    *widget.List
	level   *widget.Select
	search  *widget.Entry
	entries []app.LogEntry
}

func newLogsScreen(main *MainWindow) *logsScreen {
	screen := &logsScreen{main: main}
	screen.list = widget.NewList(
		func() int { return len(screen.entries) },
		func() fyne.CanvasObject {
			return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			label := object.(*widget.Label)
			if id < 0 || id >= len(screen.entries) {
				label.SetText("")
				return
			}
			entry := screen.entries[id]
			label.SetText(fmt.Sprintf("%s %-5s %s", entry.Time.Format("15:04:05"), strings.ToUpper(entry.Level), entry.Message))
		},
	)
	return screen
}

func (screen *logsScreen) build() fyne.CanvasObject {
	screen.level = widget.NewSelect([]string{"debug", "info", "warn", "error"}, func(string) { screen.refresh() })
	screen.level.SetSelected("info")
	screen.search = widget.NewEntry()
	screen.search.SetPlaceHolder("Поиск по тексту")
	screen.search.OnChanged = func(string) { screen.refresh() }
	toolbar := container.NewBorder(nil, nil,
		container.NewHBox(widget.NewLabel("Уровень"), screen.level), nil, screen.search)
	buttons := container.NewHBox(
		widget.NewButtonWithIcon("Очистить", theme.DeleteIcon(), func() {
			screen.main.service.ClearLogs()
			screen.refresh()
		}),
		widget.NewButtonWithIcon("Сохранить в файл", theme.DocumentSaveIcon(), screen.save),
	)
	screen.root = container.NewPadded(container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Логи", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), toolbar),
		buttons, nil, nil, screen.list,
	))
	screen.refresh()
	return screen.root
}

func (screen *logsScreen) refresh() {
	if screen.list == nil {
		return
	}
	minimum := "info"
	if screen.level != nil && screen.level.Selected != "" {
		minimum = screen.level.Selected
	}
	search := ""
	if screen.search != nil {
		search = screen.search.Text
	}
	screen.entries = FilterLogs(screen.main.service.Logs("debug"), minimum, search)
	screen.list.Refresh()
	if len(screen.entries) > 0 {
		screen.list.ScrollToBottom()
	}
}

func (screen *logsScreen) save() {
	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			screen.main.showError(err)
			return
		}
		if writer == nil {
			return
		}
		var writeErr error
		for _, entry := range FilterLogs(screen.main.service.Logs("debug"), "debug", screen.search.Text) {
			line := fmt.Sprintf("%s %-5s %s\n", entry.Time.Format(time.RFC3339), strings.ToUpper(entry.Level), entry.Message)
			if _, err := writer.Write([]byte(line)); err != nil {
				writeErr = fmt.Errorf("write log file: %w", err)
				break
			}
		}
		if err := writer.Close(); err != nil {
			closeErr := fmt.Errorf("close log file: %w", err)
			if writeErr != nil {
				writeErr = errors.Join(writeErr, closeErr)
			} else {
				writeErr = closeErr
			}
		}
		if writeErr != nil {
			screen.main.showError(writeErr)
		}
	}, screen.main.window)
	saveDialog.SetFileName("vclient.log")
	saveDialog.Show()
}

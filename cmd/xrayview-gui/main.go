package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("xrayview")
	w.SetContent(container.NewVBox(
		widget.NewLabel("xrayview GUI starting"),
		widget.NewButton("Open Image", func() {
			dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err != nil || reader == nil {
					return
				}
				fmt.Println(reader.URI().Path())
				reader.Close()
			}, w)
		}),
	))
	w.ShowAndRun()
}

package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("xrayview")
	pathLabel := widget.NewLabel("No image selected")
	preview := canvas.NewImageFromImage(nil)
	preview.FillMode = canvas.ImageFillContain
	preview.SetMinSize(fyne.NewSize(320, 240))
	preview.Hide()
	w.SetContent(container.NewVBox(
		widget.NewLabel("xrayview GUI starting"),
		pathLabel,
		preview,
		widget.NewButton("Open Image", func() {
			dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				if reader == nil {
					return
				}
				defer reader.Close()

				if _, _, err := image.DecodeConfig(reader); err != nil {
					dialog.ShowError(err, w)
					return
				}

				path := reader.URI().Path()
				fmt.Println(path)
				pathLabel.SetText(path)
				preview.File = path
				preview.Show()
				preview.Refresh()
			}, w)
		}),
	))
	w.ShowAndRun()
}

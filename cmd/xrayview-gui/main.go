package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/rymdport/portal/filechooser"
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
			go func() {
				uris, err := filechooser.OpenFile("", "Open Image", nil)
				if err != nil {
					fyne.Do(func() {
						dialog.ShowError(err, w)
					})
					return
				}
				if len(uris) == 0 {
					return
				}

				path, err := pickerPath(uris[0])
				if err != nil {
					fyne.Do(func() {
						dialog.ShowError(err, w)
					})
					return
				}

				file, err := os.Open(path)
				if err != nil {
					fyne.Do(func() {
						dialog.ShowError(err, w)
					})
					return
				}
				defer file.Close()

				if _, _, err := image.DecodeConfig(file); err != nil {
					fyne.Do(func() {
						dialog.ShowError(err, w)
					})
					return
				}

				fmt.Println(path)
				fyne.Do(func() {
					pathLabel.SetText(path)
					preview.File = path
					preview.Show()
					preview.Refresh()
				})
			}()
		}),
	))
	w.ShowAndRun()
}

func pickerPath(raw string) (string, error) {
	uri, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if uri.Scheme == "file" {
		return uri.Path, nil
	}
	return raw, nil
}

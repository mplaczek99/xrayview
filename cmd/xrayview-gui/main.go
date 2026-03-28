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
	"github.com/mplaczek99/xrayview/internal/imageio"
	"github.com/mplaczek99/xrayview/internal/pipeline"
	"github.com/rymdport/portal/filechooser"
)

func main() {
	a := app.New()
	w := a.NewWindow("xrayview")

	// Keep the selected path as explicit state instead of reading it back from the
	// label. The label is only for presentation, while later GUI actions will need
	// a stable value that represents the current selection.
	selectedPath := ""

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
			// The portal picker can block while waiting for the desktop environment.
			// Running it in a goroutine keeps the Fyne event loop responsive.
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

				// Fyne UI state should be updated on the GUI thread. fyne.Do keeps the
				// preview and labels synchronized with the result from the background picker.
				fyne.Do(func() {
					selectedPath = path
					pathLabel.SetText(path)
					preview.File = path
					preview.Show()
					preview.Refresh()
				})
			}()
		}),
		widget.NewButton("Process Image", func() {
			// Refusing to process without a selection gives immediate feedback and keeps
			// later processing code from needing to handle an impossible empty-input case.
			if selectedPath == "" {
				dialog.ShowError(fmt.Errorf("no image selected"), w)
				return
			}

			path := selectedPath

			// Loading and processing can take noticeable time for larger images, so the
			// work stays off the GUI thread and only the final widget update is marshaled
			// back through fyne.Do.
			go func() {
				img, _, err := imageio.Load(path)
				if err != nil {
					fyne.Do(func() {
						dialog.ShowError(err, w)
					})
					return
				}

				// The GUI deliberately reuses shared processing logic instead of embedding
				// filter knowledge here. That keeps the first GUI processing step aligned
				// with the project's default behavior.
				processed := pipeline.ProcessDefault(img)
				fmt.Println("process image clicked")

				// Updating the preview from memory avoids writing temporary files and keeps
				// this step focused on in-process integration with the existing logic.
				fyne.Do(func() {
					preview.File = ""
					preview.Image = processed
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

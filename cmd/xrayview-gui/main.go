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

	// Keep original and processed previews separate so the GUI can show a stable
	// before/after view without overwriting the user's source image preview.
	originalPreview := canvas.NewImageFromImage(emptyPreviewImage())
	originalPreview.FillMode = canvas.ImageFillContain
	originalPreview.SetMinSize(fyne.NewSize(320, 240))

	processedPreview := canvas.NewImageFromImage(emptyPreviewImage())
	processedPreview.FillMode = canvas.ImageFillContain
	processedPreview.SetMinSize(fyne.NewSize(320, 240))

	w.SetContent(container.NewVBox(
		widget.NewLabel("xrayview GUI starting"),
		pathLabel,
		container.NewGridWithColumns(2,
			container.NewVBox(
				widget.NewLabel("Original"),
				originalPreview,
			),
			container.NewVBox(
				widget.NewLabel("Processed"),
				processedPreview,
			),
		),
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
					originalPreview.Image = nil
					originalPreview.File = path
					originalPreview.Refresh()

					// Processing belongs to an explicit user action, so choosing a new file
					// resets the processed side back to an empty state until Process Image runs.
					processedPreview.File = ""
					processedPreview.Image = emptyPreviewImage()
					processedPreview.Refresh()
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

				// Updating the processed preview from memory avoids temporary files and keeps
				// the GUI path separate from export concerns. The shared pipeline is still
				// used so the image result matches the project's in-process default behavior.
				fyne.Do(func() {
					processedPreview.File = ""
					processedPreview.Image = processed
					processedPreview.Refresh()
				})
			}()
		}),
	))
	w.ShowAndRun()
}

func emptyPreviewImage() image.Image {
	// A transparent in-memory placeholder reserves preview space from the start so
	// the layout does not jump around while the user selects and processes images.
	return image.NewRGBA(image.Rect(0, 0, 1, 1))
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

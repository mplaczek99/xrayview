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
	// A larger default window gives the side-by-side previews enough room to be
	// useful immediately, so the user can inspect image detail without first having
	// to resize the app just to reach a comfortable starting layout.
	w.Resize(fyne.NewSize(960, 720))

	// Keep the selected path as explicit state instead of reading it back from the
	// label. The label is only for presentation, while later GUI actions will need
	// a stable value that represents the current selection.
	selectedPath := ""

	// Track the processed image separately from the preview widget so save behavior
	// can reuse the exact in-memory result that is currently shown, without having
	// to infer state back from UI objects.
	var processedImage image.Image

	// Control state lives separately from the processing call so the GUI can grow
	// incrementally. That keeps widget behavior easy to test visually before each
	// control is allowed to affect image results.
	invertValue := false
	brightnessValue := 0
	contrastValue := 1.0
	equalizeValue := false
	paletteValue := "none"

	pathLabel := widget.NewLabel("No image selected")
	brightnessValueLabel := widget.NewLabel("Brightness: 0")
	contrastValueLabel := widget.NewLabel("Contrast: 1.0")

	// Brightness uses a symmetric range around zero because zero naturally means
	// "leave the image unchanged" while negative and positive values map cleanly to
	// darker and brighter adjustments once the control is wired into processing.
	brightnessSlider := widget.NewSlider(-100, 100)
	brightnessSlider.Step = 1
	brightnessSlider.OnChanged = func(value float64) {
		brightnessValue = int(value)
		brightnessValueLabel.SetText(fmt.Sprintf("Brightness: %d", brightnessValue))
	}

	// Contrast defaults to 1.0 because that preserves the original tonal spread.
	// Values below and above that midpoint naturally represent softer and stronger
	// contrast without needing a separate enable/disable toggle.
	contrastSlider := widget.NewSlider(0.5, 2.0)
	contrastSlider.Step = 0.1
	contrastSlider.Value = 1.0
	contrastSlider.OnChanged = func(value float64) {
		contrastValue = value
		contrastValueLabel.SetText(fmt.Sprintf("Contrast: %.1f", contrastValue))
	}

	// Invert is another discrete processing choice, so a checkbox is the simplest
	// way to expose it without suggesting any intermediate values.
	invertCheckbox := widget.NewCheck("Invert", func(checked bool) {
		invertValue = checked
	})

	// Histogram equalization is a discrete on/off choice, so a checkbox expresses
	// the intent more clearly than another numeric control.
	equalizeCheckbox := widget.NewCheck("Equalize Histogram", func(checked bool) {
		equalizeValue = checked
	})

	// Start with one palette option beyond grayscale so the GUI can introduce color
	// mapping without expanding the shared pipeline surface too quickly.
	paletteSelect := widget.NewSelect([]string{"none", "hot"}, func(value string) {
		paletteValue = value
	})
	paletteSelect.SetSelected("none")

	// Keep original and processed previews separate so the GUI can show a stable
	// before/after view without overwriting the user's source image preview.
	originalPreview := canvas.NewImageFromImage(emptyPreviewImage())
	originalPreview.FillMode = canvas.ImageFillContain
	originalPreview.SetMinSize(fyne.NewSize(320, 240))

	processedPreview := canvas.NewImageFromImage(emptyPreviewImage())
	processedPreview.FillMode = canvas.ImageFillContain
	processedPreview.SetMinSize(fyne.NewSize(320, 240))

	var processButton *widget.Button
	var saveButton *widget.Button

	openButton := widget.NewButton("Open Image", func() {
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

				// Selecting a different source image makes any previously processed result
				// untrustworthy because that output was derived from older pixels. Clearing
				// the processed state and disabling save prevents exporting an image that no
				// longer matches the currently selected original preview.
				processedImage = nil
				processedPreview.File = ""
				processedPreview.Image = emptyPreviewImage()
				processedPreview.Refresh()
				processButton.Enable()
				saveButton.Disable()
			})
		}()
	})

	processButton = widget.NewButton("Process Image", func() {
		// Refusing to process without a selection gives immediate feedback and keeps
		// later processing code from needing to handle an impossible empty-input case.
		if selectedPath == "" {
			dialog.ShowError(fmt.Errorf("no image selected"), w)
			return
		}

		path := selectedPath
		// Snapshot the current slider value before leaving the GUI callback. Passing
		// explicit UI state into the shared pipeline keeps the GUI thin and avoids
		// teaching the pipeline package anything about widgets.
		invert := invertValue
		brightness := brightnessValue
		contrast := contrastValue
		equalize := equalizeValue
		palette := paletteValue

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
			// with the project's default behavior while still letting one UI control at
			// a time flow into the same in-process path. The pipeline remains centralized
			// so filter ordering does not drift between GUI code and shared logic. Palette
			// selection is passed in as plain state so the GUI stays responsible only for
			// user input while the shared pipeline owns all image transformation order.
			processed := pipeline.ProcessDefault(img, invert, brightness, contrast, equalize, palette)
			fmt.Println("process image clicked")

			// Updating the processed preview from memory avoids temporary files and keeps
			// the GUI path separate from export concerns. The shared pipeline is still
			// used so the image result matches the project's in-process default behavior.
			fyne.Do(func() {
				processedImage = processed
				processedPreview.File = ""
				processedPreview.Image = processed
				processedPreview.Refresh()

				// Save only becomes available after processing succeeds because disabled
				// actions communicate the required workflow earlier than an error dialog.
				// That keeps export tied to a real in-memory processed image instead of
				// inviting the user to click into a state the program already knows is invalid.
				saveButton.Enable()
			})
		}()
	})
	// Disabling Process Image until a valid file is selected is better than letting
	// the user click into a predictable error, because the button state itself shows
	// that opening an image is the required next step before processing can work.
	processButton.Disable()

	saveButton = widget.NewButton("Save Processed Image", func() {
		// Saving is separate from processing so the user can inspect the current
		// preview result before deciding whether it is worth exporting.
		if processedImage == nil {
			dialog.ShowError(fmt.Errorf("no processed image to save"), w)
			return
		}

		imageToSave := processedImage
		dialog.ShowFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if writer == nil {
				return
			}

			// Save uses the in-memory processed image directly so export writes exactly
			// what the user is looking at, without re-running the pipeline and risking
			// drift between preview state and saved output.
			path := writer.URI().Path()
			if err := writer.Close(); err != nil {
				dialog.ShowError(err, w)
				return
			}
			if err := imageio.SavePNG(path, imageToSave); err != nil {
				dialog.ShowError(err, w)
				return
			}
		}, w)
	})
	// Save starts disabled because exporting only makes sense after the GUI has a
	// fresh processed image in memory. Keeping it off beforehand avoids implying that
	// a selectable source file is already a valid export target.
	saveButton.Disable()

	// Grouping related controls into their own container makes the interface easier
	// to scan because previews stay visually separate from the inputs that change
	// them. This small layout cleanup is done after the button behavior is already
	// working so the UI can become more structured without mixing visual changes
	// into the earlier functionality steps.
	controlsSection := container.NewVBox(
		widget.NewLabel("Controls"),
		// Controls are added and wired one at a time so UI behavior can evolve in
		// tiny steps without making several processing changes harder to isolate.
		widget.NewLabel("Brightness"),
		brightnessSlider,
		brightnessValueLabel,
		widget.NewLabel("Contrast"),
		contrastSlider,
		contrastValueLabel,
		invertCheckbox,
		equalizeCheckbox,
		widget.NewLabel("Palette"),
		paletteSelect,
		openButton,
		processButton,
		saveButton,
	)
	// The preview area is the primary focus of this application because users spend
	// more time judging image changes than interacting with the controls. Putting
	// previews in the center region gives them the extra space, while the controls
	// stay below as a secondary section that remains visible but less dominant.
	previewsSection := container.NewGridWithColumns(2,
		container.NewVBox(
			widget.NewLabel("Original"),
			originalPreview,
		),
		container.NewVBox(
			widget.NewLabel("Processed"),
			processedPreview,
		),
	)
	headerSection := container.NewPadded(container.NewVBox(
		widget.NewLabel("xrayview GUI starting"),
		pathLabel,
	))
	paddedControlsSection := container.NewPadded(controlsSection)
	// Padding improves readability because the eye can separate the header,
	// previews, and controls more quickly when those regions are not pressed
	// directly against each other or against the window edges.
	//
	// This spacing pass happens after the Border layout is already settled so the
	// structure stays stable first, then visual breathing room can be tuned without
	// mixing polish work into the earlier layout and behavior steps.
	content := container.NewPadded(container.NewBorder(
		headerSection,
		paddedControlsSection,
		nil,
		nil,
		previewsSection,
	))

	w.SetContent(content)
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

package com.xrayview.frontend;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

import javafx.application.Application;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.Node;
import javafx.scene.Scene;
import javafx.scene.control.Button;
import javafx.scene.control.CheckBox;
import javafx.scene.control.ComboBox;
import javafx.scene.control.Label;
import javafx.scene.control.Slider;
import javafx.scene.image.Image;
import javafx.scene.image.ImageView;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.HBox;
import javafx.scene.layout.Priority;
import javafx.scene.layout.StackPane;
import javafx.scene.layout.VBox;
import javafx.stage.FileChooser;
import javafx.stage.Stage;

public final class XRayViewApp extends Application {
    // State is being centralized early so future backend requests can read one
    // stable object instead of pulling values back out of scattered controls.
    // This step deliberately does not connect the state to widgets yet, because
    // preserving current behavior while the structure improves keeps migration
    // risk low and makes later Java-to-Go integration changes safer to review.
    private final UiState uiState = new UiState();
    private final Label selectedPathLabel = new Label("No image selected yet");
    private final Label statusValueLabel = new Label("Ready");
    private final Label originalPlaceholderLabel = new Label("Preview placeholder");
    private final Label processedPlaceholderLabel = new Label("Preview placeholder");
    private final ImageView originalImageView = new ImageView();
    private final ImageView processedImageView = new ImageView();
    private final Button processImageButton = new Button("Process Image");
    private final Button saveProcessedImageButton = new Button("Save Processed Image");
    private File selectedImageFile;
    private File lastProcessedFile;

    @Override
    public void start(Stage stage) {
        // Building the layout before adding functionality keeps this migration
        // low-risk because the new frontend can match the intended structure
        // first, without mixing UI arrangement decisions with backend behavior.
        // Mirroring the current Go GUI also makes the transition easier to judge,
        // since both frontends can share the same mental model and screen shape.
        Label headerLabel = new Label("Image Visualization Tool");
        VBox headerSection = new VBox(4, headerLabel, selectedPathLabel);

        VBox originalSection = createOriginalPreviewSection();
        VBox processedSection = createProcessedPreviewSection();

        HBox previews = new HBox(16, originalSection, processedSection);
        HBox.setHgrow(originalSection, Priority.ALWAYS);
        HBox.setHgrow(processedSection, Priority.ALWAYS);

        VBox controlsSection = createControlsSection(stage);

        BorderPane root = new BorderPane();
        root.setPadding(new Insets(24));
        root.setTop(headerSection);
        root.setCenter(previews);
        root.setBottom(controlsSection);
        BorderPane.setMargin(headerSection, new Insets(0, 0, 16, 0));
        BorderPane.setMargin(previews, new Insets(0, 0, 16, 0));

        Scene scene = new Scene(root, 720, 480);

        stage.setTitle("XRayView");
        stage.setScene(scene);
        stage.show();
    }

    private VBox createControlsSection(Stage stage) {
        // The Java migration mirrors the Go GUI on purpose so each small step can
        // be compared against a working reference instead of inventing a second
        // desktop workflow. Matching the same labels and control order reduces
        // migration risk and makes later parity checks much more straightforward.
        //
        // The controls are added before wiring any functionality because layout
        // and interaction can be validated separately. That keeps this step about
        // screen structure only, while deliberately avoiding backend integration
        // until the Java frontend shape is stable enough to connect safely to Go.
        Slider brightnessSlider = new Slider(-100, 100, 0);
        Slider contrastSlider = new Slider(0.5, 2.0, 1.0);
        Label brightnessValueLabel = new Label();
        Label contrastValueLabel = new Label();

        // Visible value labels should stay synchronized with slider positions so
        // users can read exact settings instead of inferring them from thumb
        // location alone. Formatting is kept in the UI layer because display
        // precision is a presentation concern, while UiState keeps raw values for
        // later backend use. This remains a small behavior-preserving step because
        // it only updates on-screen text and does not change processing flow.
        updateBrightnessValueLabel(brightnessValueLabel, brightnessSlider.getValue());
        updateContrastValueLabel(contrastValueLabel, contrastSlider.getValue());

        CheckBox invertCheckBox = new CheckBox("Invert");
        CheckBox equalizeCheckBox = new CheckBox("Equalize Histogram");

        ComboBox<String> paletteComboBox = new ComboBox<>();
        paletteComboBox.getItems().addAll("none", "hot", "bone");
        paletteComboBox.setValue("none");

        // This step uses one-way binding from widgets into UiState first so the
        // state object can start collecting a backend-ready snapshot without yet
        // influencing what the user sees. The UI is still the source of truth at
        // this stage because the controls already define the visible behavior, and
        // mirroring their values into UiState prepares a safer handoff point for
        // later Java-to-Go integration without changing the current workflow.
        brightnessSlider.valueProperty().addListener((observable, oldValue, newValue) ->
        {
            double value = newValue.doubleValue();
            uiState.setBrightness(value);
            updateBrightnessValueLabel(brightnessValueLabel, value);
        });
        contrastSlider.valueProperty().addListener((observable, oldValue, newValue) ->
        {
            double value = newValue.doubleValue();
            uiState.setContrast(value);
            updateContrastValueLabel(contrastValueLabel, value);
        });
        invertCheckBox.selectedProperty().addListener((observable, oldValue, newValue) ->
                uiState.setInvert(newValue));
        equalizeCheckBox.selectedProperty().addListener((observable, oldValue, newValue) ->
                uiState.setEqualize(newValue));
        paletteComboBox.valueProperty().addListener((observable, oldValue, newValue) -> {
            if (newValue != null) {
                uiState.setPalette(newValue);
            }
        });

        Button openImageButton = new Button("Open Image");
        openImageButton.setOnAction(event -> handleOpenImage(stage));

        // Guarding invalid actions improves UX because users can see the expected
        // order of operations directly in button state instead of discovering it
        // through errors. This is done before backend integration so workflow
        // behavior is already stable once processing is wired in. Keeping this
        // limited to enable/disable transitions makes it a tiny, low-risk step.
        processImageButton.setDisable(true);
        saveProcessedImageButton.setDisable(true);
        processImageButton.setOnAction(event -> handleProcessImage());
        saveProcessedImageButton.setOnAction(event -> handleSaveProcessedImage(stage));

        return new VBox(8,
                new Label("Image Controls"),
                new Label("Brightness"),
                brightnessSlider,
                brightnessValueLabel,
                new Label("Contrast"),
                contrastSlider,
                contrastValueLabel,
                invertCheckBox,
                equalizeCheckBox,
                new Label("Palette"),
                paletteComboBox,
                openImageButton,
                processImageButton,
                saveProcessedImageButton,
                new Label("Status"),
                statusValueLabel);
    }

    private VBox createOriginalPreviewSection() {
        originalImageView.setPreserveRatio(true);
        originalImageView.setSmooth(true);

        StackPane placeholder = createPreviewFrame(originalPlaceholderLabel, originalImageView);

        originalImageView.fitWidthProperty().bind(placeholder.widthProperty().subtract(24));
        originalImageView.fitHeightProperty().bind(placeholder.heightProperty().subtract(24));

        VBox section = new VBox(8, new Label("Original Image"), placeholder);
        VBox.setVgrow(placeholder, Priority.ALWAYS);
        return section;
    }

    private VBox createProcessedPreviewSection() {
        processedImageView.setPreserveRatio(true);
        processedImageView.setSmooth(true);

        StackPane placeholder = createPreviewFrame(processedPlaceholderLabel, processedImageView);

        processedImageView.fitWidthProperty().bind(placeholder.widthProperty().subtract(24));
        processedImageView.fitHeightProperty().bind(placeholder.heightProperty().subtract(24));

        VBox section = new VBox(8, new Label("Processed Image"), placeholder);
        VBox.setVgrow(placeholder, Priority.ALWAYS);
        return section;
    }

    private void handleOpenImage(Stage stage) {
        // Showing the original preview before any backend processing is a safe
        // migration step because it proves the Java frontend can own desktop UI
        // work such as file selection and local preview rendering on its own.
        // This is still migration, not a new frontend design, because it follows
        // the same original/processed workflow the Go GUI already established.
        FileChooser fileChooser = new FileChooser();
        fileChooser.setTitle("Open Image");
        fileChooser.getExtensionFilters().add(
                new FileChooser.ExtensionFilter("Image Files", "*.png", "*.jpg", "*.jpeg", "*.PNG", "*.JPG", "*.JPEG"));

        File selectedFile = fileChooser.showOpenDialog(stage);
        if (selectedFile == null) {
            return;
        }

        Image image = new Image(selectedFile.toURI().toString());
        if (image.isError()) {
            return;
        }

        originalImageView.setImage(image);
        originalPlaceholderLabel.setVisible(false);
        selectedImageFile = selectedFile;
        lastProcessedFile = null;
        selectedPathLabel.setText(selectedFile.getAbsolutePath());
        statusValueLabel.setText("Image loaded");
        processImageButton.setDisable(false);
        saveProcessedImageButton.setDisable(true);
    }

    private void handleProcessImage() {
        if (selectedImageFile == null) {
            statusValueLabel.setText("Processing failed");
            return;
        }

        // The existing Go CLI is the first integration boundary because it
        // already defines processing behavior used by the current app. Using
        // ProcessBuilder is appropriate for this local handoff since Java can
        // invoke the CLI directly without introducing a new protocol yet. This
        // step stays intentionally synchronous so success and failure flow remain
        // easy to reason about while the migration is still proving parity.
        File repoRoot = resolveRepoRoot();
        File tempOutput;
        try {
            tempOutput = File.createTempFile("xrayview-processed-", ".png");
            tempOutput.deleteOnExit();
        } catch (IOException e) {
            statusValueLabel.setText("Processing failed");
            return;
        }

        List<String> command = buildCliCommand(repoRoot, selectedImageFile, tempOutput);
        ProcessBuilder processBuilder = new ProcessBuilder(command);
        processBuilder.directory(repoRoot);
        processBuilder.redirectOutput(ProcessBuilder.Redirect.DISCARD);
        processBuilder.redirectError(ProcessBuilder.Redirect.DISCARD);

        int exitCode;
        try {
            Process process = processBuilder.start();
            exitCode = process.waitFor();
        } catch (IOException e) {
            statusValueLabel.setText("Processing failed");
            return;
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            statusValueLabel.setText("Processing failed");
            return;
        }

        if (exitCode != 0) {
            statusValueLabel.setText("Processing failed");
            return;
        }

        Image processedImage = new Image(tempOutput.toURI().toString());
        if (processedImage.isError()) {
            statusValueLabel.setText("Processing failed");
            return;
        }

        processedImageView.setImage(processedImage);
        processedPlaceholderLabel.setVisible(false);
        // Reusing the processed temp file avoids re-running the CLI for save,
        // which keeps export tied to the exact result already shown in the UI.
        // Saving remains separate from processing so the user can inspect the
        // preview first, and keeping that handoff as a file reference preserves a
        // clean boundary between Java UI workflow and Go processing output.
        lastProcessedFile = tempOutput;
        saveProcessedImageButton.setDisable(false);
        statusValueLabel.setText("Image processed");
    }

    private void handleSaveProcessedImage(Stage stage) {
        if (lastProcessedFile == null) {
            return;
        }

        FileChooser fileChooser = new FileChooser();
        fileChooser.setTitle("Save Processed Image");
        fileChooser.getExtensionFilters().add(new FileChooser.ExtensionFilter("PNG Images", "*.png", "*.PNG"));

        File destinationFile = fileChooser.showSaveDialog(stage);
        if (destinationFile == null) {
            return;
        }

        try {
            Files.copy(lastProcessedFile.toPath(), destinationFile.toPath(), StandardCopyOption.REPLACE_EXISTING);
            statusValueLabel.setText("Image saved");
        } catch (IOException e) {
            // Keep this step minimal by leaving the current UI state alone when
            // the copy fails. The core save path still stays synchronous and easy
            // to reason about while export support is being introduced.
        }
    }

    // This optimization pass is being done in tiny slices so each cleanup stays
    // easy to verify against the current behavior. Preserving behavior matters
    // because the Go GUI is still the reference while the Java frontend catches
    // up, so structure can improve without moving the target. This helper was a
    // good first optimization because preview frame sizing and styling had begun
    // to duplicate across the Java UI, and extracting it reduces repetition with
    // essentially no workflow risk.
    private static StackPane createPreviewFrame(Node... children) {
        StackPane previewFrame = new StackPane(children);
        previewFrame.setMinSize(240, 220);
        previewFrame.setPrefSize(320, 240);
        previewFrame.setAlignment(Pos.CENTER);
        previewFrame.setStyle("-fx-border-color: gray; -fx-border-width: 1;");
        return previewFrame;
    }

    private static void updateBrightnessValueLabel(Label label, double value) {
        label.setText(String.format(Locale.US, "Brightness: %d", (int) Math.round(value)));
    }

    private static void updateContrastValueLabel(Label label, double value) {
        label.setText(String.format(Locale.US, "Contrast: %.1f", value));
    }

    private File resolveRepoRoot() {
        File currentDirectory = new File(System.getProperty("user.dir"));
        if (new File(currentDirectory, "cmd/xrayview").isDirectory()) {
            return currentDirectory;
        }

        File parentDirectory = currentDirectory.getParentFile();
        if (parentDirectory != null && new File(parentDirectory, "cmd/xrayview").isDirectory()) {
            return parentDirectory;
        }

        return currentDirectory;
    }

    private List<String> buildCliCommand(File repoRoot, File inputFile, File outputFile) {
        List<String> command = new ArrayList<>();

        File binary = new File(repoRoot, "xrayview");
        if (binary.isFile() && binary.canExecute()) {
            command.add(binary.getAbsolutePath());
        } else {
            command.add("go");
            command.add("run");
            command.add("./cmd/xrayview");
        }

        command.add("-input");
        command.add(inputFile.getAbsolutePath());
        command.add("-output");
        command.add(outputFile.getAbsolutePath());
        command.add("-invert=" + uiState.isInvert());
        command.add("-brightness=" + (int) Math.round(uiState.getBrightness()));
        command.add("-contrast=" + Double.toString(uiState.getContrast()));
        command.add("-equalize=" + uiState.isEqualize());
        command.add("-palette=" + uiState.getPalette());

        return command;
    }

    public static void main(String[] args) {
        launch(args);
    }
}

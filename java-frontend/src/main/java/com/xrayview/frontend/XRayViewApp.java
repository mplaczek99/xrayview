package com.xrayview.frontend;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
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
    // Keep processing settings separate from JavaFX widgets so backend calls can
    // read a stable snapshot of the current UI state.
    private final UiState uiState = new UiState();
    private final Label selectedPathLabel = new Label("No image selected yet");
    private final Label statusValueLabel = new Label("Ready");
    private final Label originalPlaceholderLabel = new Label("Preview placeholder");
    private final Label processedPlaceholderLabel = new Label("Preview placeholder");
    private final ImageView originalImageView = new ImageView();
    private final ImageView processedImageView = new ImageView();
    private final Button processImageButton = new Button("Process Image");
    private final Button saveProcessedImageButton = new Button("Save Processed Image");
    private final CliProcessor cliProcessor = new CliProcessor();
    private File selectedImageFile;
    private File lastProcessedFile;

    @Override
    public void start(Stage stage) {
        Label headerLabel = new Label("Image Visualization Tool");
        VBox headerSection = new VBox(4, headerLabel, selectedPathLabel);

        VBox originalSection = createPreviewSection("Original Image", originalImageView, originalPlaceholderLabel);
        VBox processedSection = createPreviewSection("Processed Image", processedImageView, processedPlaceholderLabel);

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

        // Size the window from the preferred content size instead of relying on a
        // hardcoded frame that can drift as the UI changes.
        Scene scene = new Scene(root);

        stage.setTitle("XRayView");
        stage.setScene(scene);
        stage.sizeToScene();
        // Keep the initial preferred size as the minimum so the controls and
        // preview panes cannot be collapsed into an unusable layout.
        stage.setMinWidth(stage.getWidth());
        stage.setMinHeight(stage.getHeight());
        stage.show();
    }

    private VBox createControlsSection(Stage stage) {
        Slider brightnessSlider = new Slider(-100, 100, 0);
        Slider contrastSlider = new Slider(0.5, 2.0, 1.0);
        Label brightnessValueLabel = new Label();
        Label contrastValueLabel = new Label();

        // Keep the value labels next to the sliders so the exact setting is
        // visible without relying on thumb position alone.
        updateBrightnessValueLabel(brightnessValueLabel, brightnessSlider.getValue());
        updateContrastValueLabel(contrastValueLabel, contrastSlider.getValue());

        CheckBox invertCheckBox = new CheckBox("Invert");
        CheckBox equalizeCheckBox = new CheckBox("Equalize Histogram");

        ComboBox<String> paletteComboBox = new ComboBox<>();
        paletteComboBox.getItems().addAll("none", "hot", "bone");
        paletteComboBox.setValue("none");

        // The widgets remain the source of truth; UiState just mirrors their
        // current values so processing can read one settings object.
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

        // Disable actions until the required input exists instead of relying on
        // error handling to teach the workflow.
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

    // Original and processed previews share the same structure, so keep their
    // sizing and bindings in one place.
    private VBox createPreviewSection(String title, ImageView imageView, Label placeholderLabel) {
        imageView.setPreserveRatio(true);
        imageView.setSmooth(true);

        StackPane placeholder = createPreviewFrame(placeholderLabel, imageView);

        imageView.fitWidthProperty().bind(placeholder.widthProperty().subtract(24));
        imageView.fitHeightProperty().bind(placeholder.heightProperty().subtract(24));

        VBox section = new VBox(8, new Label(title), placeholder);
        VBox.setVgrow(placeholder, Priority.ALWAYS);
        return section;
    }

    private void handleOpenImage(Stage stage) {
        // Load the original preview locally. The backend only needs to run when
        // the user asks for a processed result.
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
        // A processed preview only applies to the currently selected source.
        processedImageView.setImage(null);
        processedPlaceholderLabel.setVisible(true);
        lastProcessedFile = null;
        selectedPathLabel.setText(selectedFile.getAbsolutePath());
        statusValueLabel.setText("Image loaded");
        processImageButton.setDisable(false);
        saveProcessedImageButton.setDisable(true);
    }

    private void handleProcessImage() {
        if (selectedImageFile == null) {
            setProcessingFailedStatus();
            return;
        }

        File tempOutput;
        try {
            tempOutput = File.createTempFile("xrayview-processed-", ".png");
            tempOutput.deleteOnExit();
        } catch (IOException e) {
            setProcessingFailedStatus();
            return;
        }

        CliProcessor.ExecutionResult executionResult;
        try {
            executionResult = cliProcessor.run(selectedImageFile, tempOutput, uiState);
        } catch (IOException e) {
            setProcessingFailedStatus();
            return;
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            setProcessingFailedStatus();
            return;
        }

        if (executionResult.exitCode() != 0) {
            statusValueLabel.setText(formatProcessFailureStatus(executionResult.errorOutput()));
            return;
        }

        Image processedImage = new Image(tempOutput.toURI().toString());
        if (processedImage.isError()) {
            setProcessingFailedStatus();
            return;
        }

        processedImageView.setImage(processedImage);
        processedPlaceholderLabel.setVisible(false);
        // Save reuses the file produced by the CLI so export matches the preview
        // without running processing twice.
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
            // Leave the current UI state unchanged if the copy fails.
        }
    }

    // Keep preview sizing and border styling consistent across both panes.
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

    private void setProcessingFailedStatus() {
        statusValueLabel.setText("Processing failed");
    }

    private String formatProcessFailureStatus(String errorOutput) {
        String compactError = errorOutput.replaceAll("\\s+", " ").trim();
        if (compactError.isEmpty()) {
            return "Processing failed";
        }

        String statusText = "Processing failed: " + compactError;
        if (statusText.length() > 160) {
            return statusText.substring(0, 157) + "...";
        }

        return statusText;
    }

    public static void main(String[] args) {
        launch(args);
    }
}

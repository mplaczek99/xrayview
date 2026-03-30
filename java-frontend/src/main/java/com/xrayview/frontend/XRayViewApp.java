package com.xrayview.frontend;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
import java.util.Locale;

import javafx.application.Application;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.geometry.Rectangle2D;
import javafx.scene.Node;
import javafx.scene.Scene;
import javafx.scene.control.Button;
import javafx.scene.control.CheckBox;
import javafx.scene.control.ComboBox;
import javafx.scene.control.Label;
import javafx.scene.control.ScrollPane;
import javafx.scene.control.Slider;
import javafx.scene.image.Image;
import javafx.scene.image.ImageView;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.FlowPane;
import javafx.scene.layout.Priority;
import javafx.scene.layout.StackPane;
import javafx.scene.layout.VBox;
import javafx.stage.FileChooser;
import javafx.stage.Screen;
import javafx.stage.Stage;

public final class XRayViewApp extends Application {
    private static final double WINDOW_MARGIN = 40.0;
    private static final double DEFAULT_WINDOW_WIDTH = 860.0;
    private static final double DEFAULT_WINDOW_HEIGHT = 760.0;
    private static final double MIN_WINDOW_WIDTH = 520.0;
    private static final double MIN_WINDOW_HEIGHT = 480.0;
    private static final FileChooser.ExtensionFilter DICOM_EXTENSION_FILTER =
            new FileChooser.ExtensionFilter("DICOM Files", "*.dcm", "*.dicom", "*.DCM", "*.DICOM");

    // Current processing options.
    private final UiState uiState = new UiState();
    private final Label selectedPathLabel = new Label("No DICOM study selected yet");
    private final Label statusValueLabel = new Label("Ready");
    private final Label originalPlaceholderLabel = new Label("Preview placeholder");
    private final Label processedPlaceholderLabel = new Label("Preview placeholder");
    private final ImageView originalImageView = new ImageView();
    private final ImageView processedImageView = new ImageView();
    private final Button processImageButton = new Button("Process DICOM");
    private final Button saveProcessedImageButton = new Button("Save Processed DICOM");
    private final CliProcessor cliProcessor = new CliProcessor();
    private File selectedImageFile;
    private File lastProcessedFile;

    @Override
    public void start(Stage stage) {
        selectedPathLabel.setWrapText(true);
        selectedPathLabel.setMaxWidth(Double.MAX_VALUE);
        statusValueLabel.setWrapText(true);
        statusValueLabel.setMaxWidth(Double.MAX_VALUE);

        Label headerLabel = new Label("DICOM X-Ray Viewer");
        VBox headerSection = new VBox(4, headerLabel, selectedPathLabel);
        headerSection.setFillWidth(true);

        VBox originalSection = createPreviewSection("Original DICOM", originalImageView, originalPlaceholderLabel);
        VBox processedSection = createPreviewSection("Processed DICOM", processedImageView, processedPlaceholderLabel);
        originalSection.setMaxWidth(Double.MAX_VALUE);
        processedSection.setMaxWidth(Double.MAX_VALUE);

        FlowPane previews = new FlowPane(16, 16, originalSection, processedSection);
        previews.setAlignment(Pos.CENTER);
        previews.setPrefWrapLength(720);

        VBox controlsSection = createControlsSection(stage);
        controlsSection.setFillWidth(true);

        BorderPane root = new BorderPane();
        root.setPadding(new Insets(24));
        root.setTop(headerSection);
        root.setCenter(previews);
        root.setBottom(controlsSection);
        BorderPane.setMargin(headerSection, new Insets(0, 0, 16, 0));
        BorderPane.setMargin(previews, new Insets(0, 0, 16, 0));

        ScrollPane scrollPane = new ScrollPane(root);
        scrollPane.setFitToWidth(true);
        scrollPane.setPannable(true);

        // Let the scene determine the initial window size.
        Scene scene = new Scene(scrollPane);

        stage.setTitle("XRayView");
        stage.setScene(scene);
        stage.sizeToScene();
        fitStageToScreen(stage);
        stage.show();
    }

    private void fitStageToScreen(Stage stage) {
        Rectangle2D visualBounds = Screen.getPrimary().getVisualBounds();
        double availableWidth = Math.min(visualBounds.getWidth(), Math.max(320.0, visualBounds.getWidth() - WINDOW_MARGIN));
        double availableHeight = Math.min(visualBounds.getHeight(), Math.max(320.0, visualBounds.getHeight() - WINDOW_MARGIN));

        stage.setMinWidth(Math.min(MIN_WINDOW_WIDTH, availableWidth));
        stage.setMinHeight(Math.min(MIN_WINDOW_HEIGHT, availableHeight));

        double targetWidth = Math.max(stage.getWidth(), Math.min(DEFAULT_WINDOW_WIDTH, availableWidth));
        double targetHeight = Math.max(stage.getHeight(), Math.min(DEFAULT_WINDOW_HEIGHT, availableHeight));
        stage.setWidth(Math.min(targetWidth, availableWidth));
        stage.setHeight(Math.min(targetHeight, availableHeight));
        stage.centerOnScreen();
        stage.setX(Math.max(visualBounds.getMinX(), stage.getX()));
        stage.setY(Math.max(visualBounds.getMinY(), stage.getY()));
    }

    private VBox createControlsSection(Stage stage) {
        Slider brightnessSlider = new Slider(-100, 100, 0);
        Slider contrastSlider = new Slider(0.5, 2.0, 1.0);
        Label brightnessValueLabel = new Label();
        Label contrastValueLabel = new Label();

        // Show the exact slider values next to the controls.
        updateBrightnessValueLabel(brightnessValueLabel, brightnessSlider.getValue());
        updateContrastValueLabel(contrastValueLabel, contrastSlider.getValue());

        CheckBox invertCheckBox = new CheckBox("Invert");
        CheckBox equalizeCheckBox = new CheckBox("Equalize Histogram");

        ComboBox<String> paletteComboBox = new ComboBox<>();
        paletteComboBox.getItems().addAll("none", "hot", "bone");
        paletteComboBox.setValue("none");

        // Mirror widget changes into UiState.
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

        Button openImageButton = new Button("Open DICOM");
        openImageButton.setOnAction(event -> handleOpenImage(stage));

        // Actions stay disabled until a study is loaded.
        processImageButton.setDisable(true);
        saveProcessedImageButton.setDisable(true);
        processImageButton.setOnAction(event -> handleProcessImage());
        saveProcessedImageButton.setOnAction(event -> handleSaveProcessedImage(stage));

        return new VBox(8,
                new Label("DICOM Controls"),
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

    // Build both preview panes the same way.
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
        FileChooser fileChooser = new FileChooser();
        fileChooser.setTitle("Open DICOM");
        fileChooser.getExtensionFilters().add(DICOM_EXTENSION_FILTER);

        File selectedFile = fileChooser.showOpenDialog(stage);
        if (selectedFile == null) {
            return;
        }

        Image image = loadDicomPreview(selectedFile);
        if (image == null || image.isError()) {
            statusValueLabel.setText("DICOM load failed");
            return;
        }

        originalImageView.setImage(image);
        originalPlaceholderLabel.setVisible(false);
        selectedImageFile = selectedFile;
        // Clear any stale processed preview.
        processedImageView.setImage(null);
        processedPlaceholderLabel.setVisible(true);
        lastProcessedFile = null;
        selectedPathLabel.setText(selectedFile.getAbsolutePath());
        statusValueLabel.setText("DICOM loaded");
        processImageButton.setDisable(false);
        saveProcessedImageButton.setDisable(true);
    }

    private void handleProcessImage() {
        if (selectedImageFile == null) {
            setProcessingFailedStatus();
            return;
        }

        File tempPreviewOutput;
        File tempDicomOutput;
        try {
            tempPreviewOutput = File.createTempFile("xrayview-processed-", ".png");
            tempPreviewOutput.deleteOnExit();
            tempDicomOutput = File.createTempFile("xrayview-processed-", ".dcm");
            tempDicomOutput.deleteOnExit();
        } catch (IOException e) {
            setProcessingFailedStatus();
            return;
        }

        CliProcessor.ExecutionResult executionResult;
        try {
            executionResult = cliProcessor.processForUi(selectedImageFile, tempPreviewOutput, tempDicomOutput, uiState);
        } catch (IOException e) {
            setProcessingFailedStatus();
            return;
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            setProcessingFailedStatus();
            return;
        }

        if (executionResult.exitCode() != 0) {
            processedImageView.setImage(null);
            processedPlaceholderLabel.setVisible(true);
            lastProcessedFile = null;
            saveProcessedImageButton.setDisable(true);
            statusValueLabel.setText(formatProcessFailureStatus(executionResult.errorOutput()));
            return;
        }

        Image processedImage = new Image(tempPreviewOutput.toURI().toString());
        if (processedImage.isError()) {
            setProcessingFailedStatus();
            return;
        }

        processedImageView.setImage(processedImage);
        processedPlaceholderLabel.setVisible(false);
        // Keep the generated DICOM so save stays consistent with the preview.
        lastProcessedFile = tempDicomOutput;
        saveProcessedImageButton.setDisable(false);
        statusValueLabel.setText("DICOM processed");
    }

    private void handleSaveProcessedImage(Stage stage) {
        if (lastProcessedFile == null) {
            return;
        }

        FileChooser fileChooser = new FileChooser();
        fileChooser.setTitle("Save Processed DICOM");
        fileChooser.getExtensionFilters().add(DICOM_EXTENSION_FILTER);

        File destinationFile = fileChooser.showSaveDialog(stage);
        if (destinationFile == null) {
            return;
        }

        if (!isDicomFile(destinationFile)) {
            destinationFile = appendDefaultExtension(destinationFile, ".dcm");
        }

        try {
            Files.copy(lastProcessedFile.toPath(), destinationFile.toPath(), StandardCopyOption.REPLACE_EXISTING);
            statusValueLabel.setText("DICOM saved");
        } catch (IOException e) {
            statusValueLabel.setText("Save failed");
        }
    }

    // Shared preview frame styling.
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
        // Clear stale output after a failed run.
        processedImageView.setImage(null);
        processedPlaceholderLabel.setVisible(true);
        lastProcessedFile = null;
        saveProcessedImageButton.setDisable(true);
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

    private Image loadDicomPreview(File selectedFile) {
        File tempPreview;
        try {
            tempPreview = File.createTempFile("xrayview-preview-", ".png");
            tempPreview.deleteOnExit();
        } catch (IOException e) {
            return null;
        }

        try {
            CliProcessor.ExecutionResult executionResult = cliProcessor.renderPreview(selectedFile, tempPreview);
            if (executionResult.exitCode() != 0) {
                return null;
            }
        } catch (IOException e) {
            return null;
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            return null;
        }

        return new Image(tempPreview.toURI().toString());
    }

    private static boolean isDicomFile(File file) {
        String name = file.getName().toLowerCase(Locale.ROOT);
        return name.endsWith(".dcm") || name.endsWith(".dicom");
    }

    private static File appendDefaultExtension(File file, String extension) {
        File parentDirectory = file.getParentFile();
        if (parentDirectory == null) {
            return new File(file.getPath() + extension);
        }
        return new File(parentDirectory, file.getName() + extension);
    }

    public static void main(String[] args) {
        launch(args);
    }
}

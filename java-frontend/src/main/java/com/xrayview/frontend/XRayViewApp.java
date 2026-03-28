package com.xrayview.frontend;

import java.io.File;

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
    private final ImageView originalImageView = new ImageView();

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
        VBox processedSection = createPreviewPlaceholder("Processed Image");

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
                uiState.setBrightness(newValue.doubleValue()));
        contrastSlider.valueProperty().addListener((observable, oldValue, newValue) ->
                uiState.setContrast(newValue.doubleValue()));
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
        Button processImageButton = new Button("Process Image");
        Button saveProcessedImageButton = new Button("Save Processed Image");

        return new VBox(8,
                new Label("Image Controls"),
                new Label("Brightness"),
                brightnessSlider,
                new Label("Brightness: 0"),
                new Label("Contrast"),
                contrastSlider,
                new Label("Contrast: 1.0"),
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
        selectedPathLabel.setText(selectedFile.getAbsolutePath());
        statusValueLabel.setText("Image loaded");
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

    private static VBox createPreviewPlaceholder(String title) {
        Label titleLabel = new Label(title);
        Label placeholderLabel = new Label("Preview placeholder");

        StackPane placeholder = createPreviewFrame(placeholderLabel);

        VBox section = new VBox(8, titleLabel, placeholder);
        VBox.setVgrow(placeholder, Priority.ALWAYS);
        return section;
    }

    public static void main(String[] args) {
        launch(args);
    }
}

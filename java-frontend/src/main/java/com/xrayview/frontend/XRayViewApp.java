package com.xrayview.frontend;

import javafx.application.Application;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.Scene;
import javafx.scene.control.Button;
import javafx.scene.control.CheckBox;
import javafx.scene.control.ComboBox;
import javafx.scene.control.Label;
import javafx.scene.control.Slider;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.HBox;
import javafx.scene.layout.Priority;
import javafx.scene.layout.StackPane;
import javafx.scene.layout.VBox;
import javafx.stage.Stage;

public final class XRayViewApp extends Application {
    @Override
    public void start(Stage stage) {
        // Building the layout before adding functionality keeps this migration
        // low-risk because the new frontend can match the intended structure
        // first, without mixing UI arrangement decisions with backend behavior.
        // Mirroring the current Go GUI also makes the transition easier to judge,
        // since both frontends can share the same mental model and screen shape.
        Label headerLabel = new Label("Image Visualization Tool");

        VBox originalSection = createPreviewPlaceholder("Original Image");
        VBox processedSection = createPreviewPlaceholder("Processed Image");

        HBox previews = new HBox(16, originalSection, processedSection);
        HBox.setHgrow(originalSection, Priority.ALWAYS);
        HBox.setHgrow(processedSection, Priority.ALWAYS);

        VBox controlsSection = createControlsSection();

        BorderPane root = new BorderPane();
        root.setPadding(new Insets(24));
        root.setTop(headerLabel);
        root.setCenter(previews);
        root.setBottom(controlsSection);
        BorderPane.setMargin(headerLabel, new Insets(0, 0, 16, 0));
        BorderPane.setMargin(previews, new Insets(0, 0, 16, 0));

        Scene scene = new Scene(root, 720, 480);

        stage.setTitle("XRayView");
        stage.setScene(scene);
        stage.show();
    }

    private static VBox createControlsSection() {
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

        Button openImageButton = new Button("Open Image");
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
                new Label("Ready"));
    }

    private static VBox createPreviewPlaceholder(String title) {
        Label titleLabel = new Label(title);
        Label placeholderLabel = new Label("Preview placeholder");

        StackPane placeholder = new StackPane(placeholderLabel);
        placeholder.setMinSize(240, 220);
        placeholder.setPrefSize(320, 240);
        placeholder.setAlignment(Pos.CENTER);
        placeholder.setStyle("-fx-border-color: gray; -fx-border-width: 1;");

        VBox section = new VBox(8, titleLabel, placeholder);
        VBox.setVgrow(placeholder, Priority.ALWAYS);
        return section;
    }

    public static void main(String[] args) {
        launch(args);
    }
}

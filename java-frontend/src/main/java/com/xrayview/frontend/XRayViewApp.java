package com.xrayview.frontend;

import javafx.application.Application;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.Scene;
import javafx.scene.control.Label;
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

        VBox controlsSection = new VBox(8,
                new Label("Image Controls"),
                new Label("Controls coming next"));

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

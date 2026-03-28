package com.xrayview.frontend;

import javafx.application.Application;
import javafx.geometry.Insets;
import javafx.scene.Scene;
import javafx.scene.control.Label;
import javafx.scene.layout.StackPane;
import javafx.stage.Stage;

public final class XRayViewApp extends Application {
    @Override
    public void start(Stage stage) {
        Label label = new Label("XRayView Java frontend starting");

        StackPane root = new StackPane(label);
        root.setPadding(new Insets(24));

        Scene scene = new Scene(root, 420, 160);

        stage.setTitle("XRayView");
        stage.setScene(scene);
        stage.show();
    }

    public static void main(String[] args) {
        launch(args);
    }
}

package com.xrayview.frontend;

import java.io.File;
import java.io.IOException;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
import java.util.Locale;

import javafx.animation.FadeTransition;
import javafx.animation.ParallelTransition;
import javafx.animation.TranslateTransition;
import javafx.application.Application;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.geometry.Rectangle2D;
import javafx.scene.Node;
import javafx.scene.Scene;
import javafx.scene.control.Button;
import javafx.scene.control.CheckBox;
import javafx.scene.control.ComboBox;
import javafx.scene.control.ContentDisplay;
import javafx.scene.control.Label;
import javafx.scene.control.ScrollPane;
import javafx.scene.control.Slider;
import javafx.scene.control.ToggleButton;
import javafx.scene.control.ToggleGroup;
import javafx.scene.image.Image;
import javafx.scene.image.ImageView;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.FlowPane;
import javafx.scene.layout.HBox;
import javafx.scene.layout.Priority;
import javafx.scene.layout.Region;
import javafx.scene.layout.StackPane;
import javafx.scene.layout.VBox;
import javafx.stage.FileChooser;
import javafx.stage.Screen;
import javafx.stage.Stage;
import javafx.util.Duration;

public final class XRayViewApp extends Application {
    private static final double WINDOW_MARGIN = 40.0;
    private static final double DEFAULT_WINDOW_WIDTH = 1460.0;
    private static final double DEFAULT_WINDOW_HEIGHT = 940.0;
    private static final double MIN_WINDOW_WIDTH = 1120.0;
    private static final double MIN_WINDOW_HEIGHT = 720.0;
    private static final FileChooser.ExtensionFilter DICOM_EXTENSION_FILTER =
            new FileChooser.ExtensionFilter("DICOM Files", "*.dcm", "*.dicom", "*.DCM", "*.DICOM");

    private final UiState uiState = new UiState();
    private final CliProcessor cliProcessor = new CliProcessor();

    private final Label studyNameLabel = new Label("No DICOM study");
    private final Label studyMetaLabel = new Label("Open a .dcm or .dicom file to begin.");
    private final Label selectedPathLabel =
            new Label("A large-format preview and export workflow appear after a study is loaded.");
    private final Label sessionStatusLabel = new Label("Session ready");
    private final Label modeHelpLabel =
            new Label("Rendered output unlocks the compare view and DICOM export.");
    private final Label statusValueLabel = new Label("Open a study to start the visualization workflow.");
    private final Label outputPathLabel = new Label("Rendered DICOMs are exported on demand.");

    private final Label recipeMetricLabel = new Label("Neutral");
    private final Label recipeDetailLabel = new Label("Balanced grayscale settings for first-pass review.");
    private final Label toneMetricLabel = new Label("B +0 | C 1.0");
    private final Label toneDetailLabel = new Label("Invert off - Equalize off - Palette none");
    private final Label outputMetricLabel = new Label("No render yet");
    private final Label outputDetailLabel = new Label("Render output to unlock compare and export.");

    private final Label viewerTitleLabel = new Label("Original scan");
    private final Label viewerSubtitleLabel =
            new Label("Load a DICOM study to inspect a large-format preview.");
    private final Label profileChipLabel = new Label("PROFILE NEUTRAL");
    private final Label paletteChipLabel = new Label("PALETTE NONE");
    private final Label toneChipLabel = new Label("B +0 | C 1.0");
    private final Label statusChipLabel = new Label("READY");

    private final Label originalTileInfoLabel = new Label("Load a DICOM study");
    private final Label processedTileInfoLabel = new Label("Render output to compare");
    private final Label originalTilePlaceholderLabel = new Label("Original preview");
    private final Label processedTilePlaceholderLabel = new Label("Processed preview");
    private final Label viewerPlaceholderTitleLabel = new Label("Open a DICOM study");
    private final Label viewerPlaceholderSubtitleLabel = new Label(
            "Review the source image, render a tuned version, then compare and export the derived DICOM.");

    private final ImageView mainViewerImageView = new ImageView();
    private final ImageView originalCompareImageView = new ImageView();
    private final ImageView processedCompareImageView = new ImageView();
    private final ImageView originalTileImageView = new ImageView();
    private final ImageView processedTileImageView = new ImageView();

    private final StackPane viewerFrame = new StackPane();
    private final VBox viewerPlaceholderBox = new VBox(8, viewerPlaceholderTitleLabel, viewerPlaceholderSubtitleLabel);
    private final HBox comparisonPane = new HBox(16);

    private final ToggleGroup viewerModeGroup = new ToggleGroup();
    private final ToggleButton originalViewButton = new ToggleButton("Original");
    private final ToggleButton processedViewButton = new ToggleButton("Processed");
    private final ToggleButton compareViewButton = new ToggleButton("Compare");

    private final Button originalTileButton = new Button();
    private final Button processedTileButton = new Button();
    private final Button openImageButton = new Button("Open Study");
    private final Button processImageButton = new Button("Render Output");
    private final Button saveProcessedImageButton = new Button("Save DICOM");

    private final Slider brightnessSlider = new Slider(-100, 100, 0);
    private final Slider contrastSlider = new Slider(0.5, 2.0, 1.0);
    private final Label brightnessValueLabel = new Label();
    private final Label contrastValueLabel = new Label();
    private final CheckBox invertCheckBox = new CheckBox("Invert grayscale");
    private final CheckBox equalizeCheckBox = new CheckBox("Equalize histogram");
    private final ComboBox<String> paletteComboBox = new ComboBox<>();

    private File selectedImageFile;
    private File lastProcessedFile;
    private File lastSavedDestinationFile;
    private Image originalPreviewImage;
    private Image processedPreviewImage;
    private ViewerMode activeViewerMode = ViewerMode.ORIGINAL;
    private boolean renderDirty;

    private enum ViewerMode {
        ORIGINAL,
        PROCESSED,
        COMPARE
    }

    @Override
    public void start(Stage stage) {
        configureInterface(stage);

        VBox leftSidebar = createLeftSidebar();
        VBox centerColumn = createCenterColumn();
        VBox rightSidebar = createRightSidebar();

        HBox dashboardBody = new HBox(20, leftSidebar, centerColumn, rightSidebar);
        dashboardBody.getStyleClass().add("dashboard-body");
        dashboardBody.setAlignment(Pos.TOP_LEFT);
        HBox.setHgrow(centerColumn, Priority.ALWAYS);

        BorderPane root = new BorderPane();
        root.getStyleClass().add("app-shell");
        root.setPadding(new Insets(24));
        root.setTop(createHeaderBar());
        root.setCenter(dashboardBody);
        BorderPane.setMargin(dashboardBody, new Insets(20, 0, 0, 0));

        ScrollPane scrollPane = new ScrollPane(root);
        scrollPane.getStyleClass().add("dashboard-scroll");
        scrollPane.setFitToWidth(true);
        scrollPane.setPannable(true);

        Scene scene = new Scene(scrollPane);
        URL stylesheetUrl = XRayViewApp.class.getResource("app.css");
        if (stylesheetUrl != null) {
            scene.getStylesheets().add(stylesheetUrl.toExternalForm());
        }

        stage.setTitle("XRayView Workstation");
        stage.setScene(scene);
        stage.sizeToScene();
        fitStageToScreen(stage);
        stage.show();

        playEntranceAnimation(dashboardBody);
        refreshUi();
    }

    private void configureInterface(Stage stage) {
        configureLabels();
        configureButtons(stage);
        configureViewer();
        configureModeButtons();
        configurePreviewTiles();
        configureProcessingControls();
    }

    private void configureLabels() {
        Label[] wrappingLabels = {
                studyNameLabel,
                studyMetaLabel,
                selectedPathLabel,
                sessionStatusLabel,
                modeHelpLabel,
                statusValueLabel,
                outputPathLabel,
                recipeMetricLabel,
                recipeDetailLabel,
                toneMetricLabel,
                toneDetailLabel,
                outputMetricLabel,
                outputDetailLabel,
                viewerTitleLabel,
                viewerSubtitleLabel,
                profileChipLabel,
                paletteChipLabel,
                toneChipLabel,
                statusChipLabel,
                originalTileInfoLabel,
                processedTileInfoLabel,
                originalTilePlaceholderLabel,
                processedTilePlaceholderLabel,
                viewerPlaceholderTitleLabel,
                viewerPlaceholderSubtitleLabel,
                brightnessValueLabel,
                contrastValueLabel,
        };
        for (Label label : wrappingLabels) {
            label.setWrapText(true);
            label.setMaxWidth(Double.MAX_VALUE);
        }

        studyNameLabel.getStyleClass().add("study-title");
        studyMetaLabel.getStyleClass().add("study-meta");
        selectedPathLabel.getStyleClass().add("study-path");
        modeHelpLabel.getStyleClass().add("body-copy");
        statusValueLabel.getStyleClass().add("body-copy");
        outputPathLabel.getStyleClass().add("body-copy");

        recipeMetricLabel.getStyleClass().add("metric-value");
        toneMetricLabel.getStyleClass().add("metric-value");
        outputMetricLabel.getStyleClass().add("metric-value");
        recipeDetailLabel.getStyleClass().add("metric-detail");
        toneDetailLabel.getStyleClass().add("metric-detail");
        outputDetailLabel.getStyleClass().add("metric-detail");

        viewerTitleLabel.getStyleClass().add("viewer-title");
        viewerSubtitleLabel.getStyleClass().add("viewer-subtitle");
        profileChipLabel.getStyleClass().addAll("info-pill", "viewer-chip");
        paletteChipLabel.getStyleClass().addAll("info-pill", "viewer-chip");
        toneChipLabel.getStyleClass().addAll("info-pill", "viewer-chip");
        statusChipLabel.getStyleClass().addAll("info-pill", "viewer-chip");
        sessionStatusLabel.getStyleClass().addAll("info-pill", "session-pill");

        originalTileInfoLabel.getStyleClass().add("tile-info");
        processedTileInfoLabel.getStyleClass().add("tile-info");
        originalTilePlaceholderLabel.getStyleClass().add("thumbnail-placeholder");
        processedTilePlaceholderLabel.getStyleClass().add("thumbnail-placeholder");
        viewerPlaceholderTitleLabel.getStyleClass().add("viewer-placeholder-title");
        viewerPlaceholderSubtitleLabel.getStyleClass().add("viewer-placeholder-subtitle");

        brightnessValueLabel.getStyleClass().add("setting-value");
        contrastValueLabel.getStyleClass().add("setting-value");
        invertCheckBox.getStyleClass().add("option-check");
        equalizeCheckBox.getStyleClass().add("option-check");
    }

    private void configureButtons(Stage stage) {
        openImageButton.getStyleClass().addAll("toolbar-button", "toolbar-button-secondary");
        processImageButton.getStyleClass().addAll("toolbar-button", "toolbar-button-primary");
        saveProcessedImageButton.getStyleClass().addAll("toolbar-button", "toolbar-button-secondary");

        openImageButton.setOnAction(event -> handleOpenImage(stage));
        processImageButton.setOnAction(event -> handleProcessImage());
        saveProcessedImageButton.setOnAction(event -> handleSaveProcessedImage(stage));
    }

    private void configureViewer() {
        configureImageView(mainViewerImageView);
        configureImageView(originalCompareImageView);
        configureImageView(processedCompareImageView);
        configureImageView(originalTileImageView);
        configureImageView(processedTileImageView);

        mainViewerImageView.getStyleClass().add("viewer-image");
        mainViewerImageView.fitWidthProperty().bind(viewerFrame.widthProperty().subtract(44));
        mainViewerImageView.fitHeightProperty().bind(viewerFrame.heightProperty().subtract(44));

        comparisonPane.getStyleClass().add("comparison-pane");
        comparisonPane.setAlignment(Pos.CENTER);
        comparisonPane.setVisible(false);
        comparisonPane.managedProperty().bind(comparisonPane.visibleProperty());
        comparisonPane.getChildren().addAll(
                createComparisonSlot("Original", originalCompareImageView),
                createComparisonSlot("Processed", processedCompareImageView));

        viewerPlaceholderBox.getStyleClass().add("viewer-placeholder");
        viewerPlaceholderBox.setAlignment(Pos.CENTER);
        viewerPlaceholderBox.setMouseTransparent(true);

        viewerFrame.getStyleClass().add("viewer-stage");
        viewerFrame.setMinHeight(480);
        viewerFrame.setPrefHeight(580);
        viewerFrame.getChildren().addAll(mainViewerImageView, comparisonPane, viewerPlaceholderBox);
    }

    private void configureModeButtons() {
        for (ToggleButton button : new ToggleButton[] { originalViewButton, processedViewButton, compareViewButton }) {
            button.setToggleGroup(viewerModeGroup);
            button.getStyleClass().add("mode-toggle");
        }

        originalViewButton.setOnAction(event -> selectViewerMode(ViewerMode.ORIGINAL, true));
        processedViewButton.setOnAction(event -> selectViewerMode(ViewerMode.PROCESSED, true));
        compareViewButton.setOnAction(event -> selectViewerMode(ViewerMode.COMPARE, true));
        viewerModeGroup.selectToggle(originalViewButton);
    }

    private void configurePreviewTiles() {
        originalTileButton.getStyleClass().add("preview-tile");
        originalTileButton.setGraphic(createPreviewTileGraphic(
                "Original DICOM",
                originalTileImageView,
                originalTilePlaceholderLabel,
                originalTileInfoLabel));
        originalTileButton.setContentDisplay(ContentDisplay.GRAPHIC_ONLY);
        originalTileButton.setMaxWidth(Double.MAX_VALUE);
        originalTileButton.setOnAction(event -> selectViewerMode(ViewerMode.ORIGINAL, true));

        processedTileButton.getStyleClass().add("preview-tile");
        processedTileButton.setGraphic(createPreviewTileGraphic(
                "Processed DICOM",
                processedTileImageView,
                processedTilePlaceholderLabel,
                processedTileInfoLabel));
        processedTileButton.setContentDisplay(ContentDisplay.GRAPHIC_ONLY);
        processedTileButton.setMaxWidth(Double.MAX_VALUE);
        processedTileButton.setOnAction(event -> selectViewerMode(ViewerMode.PROCESSED, true));
    }

    private void configureProcessingControls() {
        brightnessSlider.getStyleClass().add("app-slider");
        brightnessSlider.setBlockIncrement(1.0);
        contrastSlider.getStyleClass().add("app-slider");
        contrastSlider.setBlockIncrement(0.1);

        paletteComboBox.getItems().addAll("none", "hot", "bone");
        paletteComboBox.setValue("none");
        paletteComboBox.getStyleClass().add("app-combo-box");
        paletteComboBox.setMaxWidth(Double.MAX_VALUE);

        updateBrightnessValueLabel(brightnessValueLabel, brightnessSlider.getValue());
        updateContrastValueLabel(contrastValueLabel, contrastSlider.getValue());

        brightnessSlider.valueProperty().addListener((observable, oldValue, newValue) -> {
            double value = newValue.doubleValue();
            uiState.setBrightness(value);
            updateBrightnessValueLabel(brightnessValueLabel, value);
            markRenderDirty();
            refreshUi();
        });

        contrastSlider.valueProperty().addListener((observable, oldValue, newValue) -> {
            double value = newValue.doubleValue();
            uiState.setContrast(value);
            updateContrastValueLabel(contrastValueLabel, value);
            markRenderDirty();
            refreshUi();
        });

        invertCheckBox.selectedProperty().addListener((observable, oldValue, newValue) -> {
            uiState.setInvert(newValue);
            markRenderDirty();
            refreshUi();
        });

        equalizeCheckBox.selectedProperty().addListener((observable, oldValue, newValue) -> {
            uiState.setEqualize(newValue);
            markRenderDirty();
            refreshUi();
        });

        paletteComboBox.valueProperty().addListener((observable, oldValue, newValue) -> {
            if (newValue != null) {
                uiState.setPalette(newValue);
                markRenderDirty();
                refreshUi();
            }
        });
    }

    private HBox createHeaderBar() {
        Label brandKicker = new Label("XRAYVIEW");
        brandKicker.getStyleClass().add("app-kicker");

        Label brandTitle = new Label("XRayView Workstation");
        brandTitle.getStyleClass().add("app-title");

        Label brandSubtitle = new Label(
                "A cleaner DICOM review desk with a larger canvas, faster compare mode, and clearer export feedback.");
        brandSubtitle.setWrapText(true);
        brandSubtitle.setMaxWidth(Double.MAX_VALUE);
        brandSubtitle.getStyleClass().add("app-subtitle");

        FlowPane topPills = new FlowPane(8, 8,
                createStaticPill("DICOM first"),
                sessionStatusLabel,
                createStaticPill("Visualization only"));
        topPills.setAlignment(Pos.CENTER_LEFT);

        VBox brandBlock = new VBox(8, brandKicker, brandTitle, brandSubtitle, topPills);
        brandBlock.setMaxWidth(620);

        FlowPane actions = new FlowPane(10, 10, openImageButton, processImageButton, saveProcessedImageButton);
        actions.setAlignment(Pos.CENTER_RIGHT);

        Region spacer = new Region();
        HBox.setHgrow(spacer, Priority.ALWAYS);

        HBox header = new HBox(20, brandBlock, spacer, actions);
        header.getStyleClass().add("top-bar");
        header.setAlignment(Pos.CENTER_LEFT);
        return header;
    }

    private VBox createLeftSidebar() {
        FlowPane studyTags = new FlowPane(8, 8,
                createMiniTag("DICOM"),
                createMiniTag("PNG preview"),
                createMiniTag("Derived export"));
        studyTags.setAlignment(Pos.CENTER_LEFT);

        FlowPane modeButtons = new FlowPane(8, 8, originalViewButton, processedViewButton, compareViewButton);
        modeButtons.setAlignment(Pos.CENTER_LEFT);

        Label workflowCopy = new Label(
                "1 Load the study  2 Tune the tonal curve  3 Render the derived DICOM  4 Save the approved output");
        workflowCopy.setWrapText(true);
        workflowCopy.setMaxWidth(Double.MAX_VALUE);
        workflowCopy.getStyleClass().add("body-copy");

        Label safetyCopy = new Label(
                "This workstation is for image visualization only and must not be used for diagnosis, treatment planning, or clinical decisions.");
        safetyCopy.setWrapText(true);
        safetyCopy.setMaxWidth(Double.MAX_VALUE);
        safetyCopy.getStyleClass().add("body-copy");

        VBox sidebar = new VBox(16,
                createCard(
                        "Study Deck",
                        "Keep the source file and preview status visible at a glance.",
                        studyNameLabel,
                        studyMetaLabel,
                        selectedPathLabel,
                        studyTags),
                createCard(
                        "View Modes",
                        "Switch between source, tuned output, and a side-by-side comparison.",
                        modeButtons,
                        modeHelpLabel),
                createCard(
                        "Workflow",
                        "The layout is optimized for a short, repeatable review loop.",
                        workflowCopy),
                createCard(
                        "Safety",
                        "Helpful UI, clear guardrails, no implied clinical AI.",
                        safetyCopy));

        sidebar.getStyleClass().add("sidebar-column");
        sidebar.setPrefWidth(260);
        sidebar.setMinWidth(260);
        return sidebar;
    }

    private VBox createCenterColumn() {
        HBox metricRow = new HBox(16,
                createMetricCard("Recipe", recipeMetricLabel, recipeDetailLabel),
                createMetricCard("Tone", toneMetricLabel, toneDetailLabel),
                createMetricCard("Output", outputMetricLabel, outputDetailLabel));
        metricRow.getStyleClass().add("metric-row");

        VBox viewerText = new VBox(6, viewerTitleLabel, viewerSubtitleLabel);

        FlowPane viewerChips = new FlowPane(8, 8, profileChipLabel, paletteChipLabel, toneChipLabel, statusChipLabel);
        viewerChips.setAlignment(Pos.CENTER_RIGHT);

        Region viewerSpacer = new Region();
        HBox.setHgrow(viewerSpacer, Priority.ALWAYS);

        HBox viewerHeader = new HBox(16, viewerText, viewerSpacer, viewerChips);
        viewerHeader.setAlignment(Pos.CENTER_LEFT);

        VBox viewerCard = new VBox(16, viewerHeader, viewerFrame);
        viewerCard.getStyleClass().addAll("panel-card", "viewer-card");
        viewerCard.setFillWidth(true);
        VBox.setVgrow(viewerFrame, Priority.ALWAYS);

        HBox previewRail = new HBox(16, originalTileButton, processedTileButton);
        previewRail.getStyleClass().add("preview-rail");
        HBox.setHgrow(originalTileButton, Priority.ALWAYS);
        HBox.setHgrow(processedTileButton, Priority.ALWAYS);

        VBox centerColumn = new VBox(16, metricRow, viewerCard, previewRail);
        centerColumn.getStyleClass().add("content-column");
        centerColumn.setFillWidth(true);
        VBox.setVgrow(viewerCard, Priority.ALWAYS);
        return centerColumn;
    }

    private VBox createRightSidebar() {
        Label quickRecipeLabel = new Label("Quick recipes");
        quickRecipeLabel.getStyleClass().add("setting-group-title");

        FlowPane quickRecipes = new FlowPane(8, 8,
                createProfileButton("Neutral", 0, 1.0, false, false, "none"),
                createProfileButton("Bone Focus", 10, 1.4, false, true, "bone"),
                createProfileButton("High Contrast", 0, 1.8, false, true, "none"));
        quickRecipes.setAlignment(Pos.CENTER_LEFT);

        Label quickRecipeHint = new Label(
                "Recipes snap the controls instantly. Manual edits keep the same workflow and show up as Custom.");
        quickRecipeHint.setWrapText(true);
        quickRecipeHint.setMaxWidth(Double.MAX_VALUE);
        quickRecipeHint.getStyleClass().add("body-copy");

        VBox controlsCard = createCard(
                "Processing Lab",
                "Shape the derived preview before generating a new DICOM export.",
                quickRecipeLabel,
                quickRecipes,
                quickRecipeHint,
                createSliderControl("Brightness", brightnessValueLabel, brightnessSlider),
                createSliderControl("Contrast", contrastValueLabel, contrastSlider),
                createPickerControl("Palette", paletteComboBox),
                invertCheckBox,
                equalizeCheckBox);

        VBox statusCard = createCard(
                "Render Status",
                "Keep the current review state visible while you work.",
                statusValueLabel,
                outputPathLabel);

        VBox sidebar = new VBox(16, controlsCard, statusCard);
        sidebar.getStyleClass().add("sidebar-column");
        sidebar.setPrefWidth(320);
        sidebar.setMinWidth(320);
        return sidebar;
    }

    private VBox createCard(String titleText, String descriptionText, Node... content) {
        Label title = new Label(titleText);
        title.getStyleClass().add("card-title");

        Label description = new Label(descriptionText);
        description.setWrapText(true);
        description.setMaxWidth(Double.MAX_VALUE);
        description.getStyleClass().add("card-description");

        VBox card = new VBox(12, title, description);
        card.getStyleClass().add("panel-card");
        card.setFillWidth(true);
        card.getChildren().addAll(content);
        return card;
    }

    private VBox createMetricCard(String labelText, Label valueLabel, Label detailLabel) {
        Label label = new Label(labelText);
        label.getStyleClass().add("metric-label");

        VBox card = new VBox(8, label, valueLabel, detailLabel);
        card.getStyleClass().addAll("panel-card", "metric-card");
        HBox.setHgrow(card, Priority.ALWAYS);
        return card;
    }

    private VBox createSliderControl(String titleText, Label valueLabel, Slider slider) {
        Label title = new Label(titleText);
        title.getStyleClass().add("setting-name");

        Region spacer = new Region();
        HBox.setHgrow(spacer, Priority.ALWAYS);

        HBox header = new HBox(10, title, spacer, valueLabel);
        header.getStyleClass().add("setting-header");
        header.setAlignment(Pos.CENTER_LEFT);

        VBox block = new VBox(8, header, slider);
        block.getStyleClass().add("setting-block");
        return block;
    }

    private VBox createPickerControl(String titleText, ComboBox<String> comboBox) {
        Label title = new Label(titleText);
        title.getStyleClass().add("setting-name");

        VBox block = new VBox(8, title, comboBox);
        block.getStyleClass().add("setting-block");
        return block;
    }

    private Button createProfileButton(
            String titleText,
            int brightness,
            double contrast,
            boolean invert,
            boolean equalize,
            String palette) {
        Button button = new Button(titleText);
        button.getStyleClass().add("profile-button");
        button.setOnAction(event -> applyQuickRecipe(brightness, contrast, invert, equalize, palette));
        return button;
    }

    private Node createPreviewTileGraphic(
            String titleText,
            ImageView imageView,
            Label placeholderLabel,
            Label infoLabel) {
        Label title = new Label(titleText);
        title.getStyleClass().add("tile-title");
        title.setWrapText(true);
        title.setMaxWidth(Double.MAX_VALUE);

        StackPane imageFrame = new StackPane(placeholderLabel, imageView);
        imageFrame.getStyleClass().add("thumbnail-frame");
        imageFrame.setMinHeight(158);
        imageFrame.setPrefHeight(172);

        imageView.fitWidthProperty().bind(imageFrame.widthProperty().subtract(24));
        imageView.fitHeightProperty().bind(imageFrame.heightProperty().subtract(24));

        VBox graphic = new VBox(10, title, imageFrame, infoLabel);
        graphic.getStyleClass().add("tile-graphic");
        graphic.setFillWidth(true);
        VBox.setVgrow(imageFrame, Priority.ALWAYS);
        return graphic;
    }

    private VBox createComparisonSlot(String titleText, ImageView imageView) {
        Label title = new Label(titleText);
        title.getStyleClass().add("comparison-title");

        StackPane frame = new StackPane(imageView);
        frame.getStyleClass().add("comparison-frame");
        frame.setMinHeight(420);

        imageView.fitWidthProperty().bind(frame.widthProperty().subtract(28));
        imageView.fitHeightProperty().bind(frame.heightProperty().subtract(28));

        VBox slot = new VBox(10, title, frame);
        slot.getStyleClass().add("comparison-slot");
        slot.setFillWidth(true);
        HBox.setHgrow(slot, Priority.ALWAYS);
        VBox.setVgrow(frame, Priority.ALWAYS);
        return slot;
    }

    private Label createStaticPill(String text) {
        Label pill = new Label(text);
        pill.getStyleClass().addAll("info-pill", "static-pill");
        return pill;
    }

    private Label createMiniTag(String text) {
        Label tag = new Label(text);
        tag.getStyleClass().add("mini-tag");
        return tag;
    }

    private void applyQuickRecipe(int brightness, double contrast, boolean invert, boolean equalize, String palette) {
        brightnessSlider.setValue(brightness);
        contrastSlider.setValue(contrast);
        invertCheckBox.setSelected(invert);
        equalizeCheckBox.setSelected(equalize);
        paletteComboBox.setValue(palette);
        setStatusMessage("Processing recipe updated. Render when you are ready to refresh the output.");
    }

    private void refreshUi() {
        refreshActionState();
        refreshStudyState();
        refreshProcessingInsights();
        refreshViewer();
        refreshPreviewTiles();
    }

    private void refreshActionState() {
        processImageButton.setDisable(selectedImageFile == null);
        saveProcessedImageButton.setDisable(lastProcessedFile == null || renderDirty);

        originalTileButton.setDisable(originalPreviewImage == null);
        processedTileButton.setDisable(processedPreviewImage == null);

        processedViewButton.setDisable(processedPreviewImage == null);
        compareViewButton.setDisable(originalPreviewImage == null || processedPreviewImage == null);

        if (selectedImageFile == null) {
            sessionStatusLabel.setText("Session ready");
            return;
        }
        if (renderDirty) {
            sessionStatusLabel.setText("Refresh needed");
            return;
        }
        if (lastProcessedFile != null) {
            sessionStatusLabel.setText("Output ready");
            return;
        }
        sessionStatusLabel.setText("Study loaded");
    }

    private void refreshStudyState() {
        if (selectedImageFile == null) {
            studyNameLabel.setText("No DICOM study");
            studyMetaLabel.setText("Open a .dcm or .dicom file to begin.");
            selectedPathLabel.setText("A large-format preview and export workflow appear after a study is loaded.");
            modeHelpLabel.setText("Rendered output unlocks the compare view and DICOM export.");
            return;
        }

        studyNameLabel.setText(selectedImageFile.getName());
        studyMetaLabel.setText(formatImageMetrics(originalPreviewImage) + " source preview loaded");
        selectedPathLabel.setText(selectedImageFile.getAbsolutePath());

        if (processedPreviewImage == null) {
            modeHelpLabel.setText("Render the tuned output to unlock compare mode and DICOM export.");
            return;
        }
        if (renderDirty) {
            modeHelpLabel.setText("Compare is still available, but export stays locked until you render the updated settings.");
            return;
        }
        modeHelpLabel.setText("Original, processed, and compare views are ready.");
    }

    private void refreshProcessingInsights() {
        String recipeName = inferRecipeName();
        int brightness = (int) Math.round(uiState.getBrightness());
        double contrast = uiState.getContrast();

        recipeMetricLabel.setText(recipeName);
        recipeDetailLabel.setText(describeRecipe(recipeName));
        toneMetricLabel.setText(String.format(Locale.US, "B %+d | C %.1f", brightness, contrast));
        toneDetailLabel.setText(formatToneDetail());

        profileChipLabel.setText("PROFILE " + recipeName.toUpperCase(Locale.US));
        paletteChipLabel.setText("PALETTE " + uiState.getPalette().toUpperCase(Locale.US));
        toneChipLabel.setText(String.format(Locale.US, "B %+d | C %.1f", brightness, contrast));

        if (selectedImageFile == null) {
            outputMetricLabel.setText("No render yet");
            outputDetailLabel.setText("Load a study, tune the controls, then render when ready.");
            outputPathLabel.setText("Rendered DICOMs are exported on demand.");
            statusChipLabel.setText("READY");
            return;
        }

        if (lastProcessedFile == null) {
            outputMetricLabel.setText("Waiting to render");
            outputDetailLabel.setText("Create the processed DICOM to unlock compare and export.");
            outputPathLabel.setText("Render output to unlock export.");
            statusChipLabel.setText("INPUT READY");
            return;
        }

        if (renderDirty) {
            outputMetricLabel.setText("Refresh needed");
            outputDetailLabel.setText("The visible processed preview no longer matches the current controls.");
            outputPathLabel.setText("Save is locked until you render the updated settings.");
            statusChipLabel.setText("RENDER AGAIN");
            return;
        }

        outputMetricLabel.setText("Output ready");
        if (lastSavedDestinationFile != null) {
            outputDetailLabel.setText("A saved copy exists and the current render still matches the active controls.");
            outputPathLabel.setText("Last saved to " + lastSavedDestinationFile.getAbsolutePath());
        } else {
            outputDetailLabel.setText("Compare the result or save the derived DICOM.");
            outputPathLabel.setText("Temporary render ready. Choose Save DICOM to export it.");
        }
        statusChipLabel.setText("OUTPUT READY");
    }

    private void refreshViewer() {
        boolean hasOriginal = originalPreviewImage != null;
        boolean hasProcessed = processedPreviewImage != null;

        if (activeViewerMode == ViewerMode.PROCESSED && !hasProcessed) {
            activeViewerMode = ViewerMode.ORIGINAL;
        }
        if (activeViewerMode == ViewerMode.COMPARE && !(hasOriginal && hasProcessed)) {
            activeViewerMode = hasProcessed ? ViewerMode.PROCESSED : ViewerMode.ORIGINAL;
        }

        switch (activeViewerMode) {
            case ORIGINAL:
                viewerModeGroup.selectToggle(originalViewButton);
                break;
            case PROCESSED:
                viewerModeGroup.selectToggle(processedViewButton);
                break;
            case COMPARE:
                viewerModeGroup.selectToggle(compareViewButton);
                break;
            default:
                break;
        }

        originalCompareImageView.setImage(originalPreviewImage);
        processedCompareImageView.setImage(processedPreviewImage);

        boolean showCompare = activeViewerMode == ViewerMode.COMPARE && hasOriginal && hasProcessed;
        Image imageToShow = null;

        if (activeViewerMode == ViewerMode.ORIGINAL) {
            imageToShow = originalPreviewImage;
            viewerTitleLabel.setText("Original scan");
            viewerSubtitleLabel.setText(hasOriginal
                    ? "Direct preview generated from the source DICOM study."
                    : "Load a DICOM study to inspect a large-format preview.");
        } else if (activeViewerMode == ViewerMode.PROCESSED) {
            imageToShow = processedPreviewImage;
            viewerTitleLabel.setText("Processed output");
            viewerSubtitleLabel.setText(renderDirty
                    ? "This processed preview is stale after recent control changes."
                    : "Rendered preview ready for compare and export.");
        } else {
            viewerTitleLabel.setText("Side-by-side compare");
            viewerSubtitleLabel.setText(renderDirty
                    ? "Compare is showing the last render. Render again to sync it with the current controls."
                    : "Source and processed views stay aligned for a faster review pass.");
        }

        comparisonPane.setVisible(showCompare);
        mainViewerImageView.setImage(showCompare ? null : imageToShow);
        mainViewerImageView.setVisible(!showCompare && imageToShow != null);
        originalCompareImageView.setVisible(showCompare && originalPreviewImage != null);
        processedCompareImageView.setVisible(showCompare && processedPreviewImage != null);

        if (!showCompare && imageToShow == null) {
            if (selectedImageFile == null) {
                viewerPlaceholderTitleLabel.setText("Open a DICOM study");
                viewerPlaceholderSubtitleLabel.setText(
                        "Review the source image, render a tuned version, then compare and export the derived DICOM.");
            } else if (activeViewerMode == ViewerMode.PROCESSED) {
                viewerPlaceholderTitleLabel.setText("Render the processed output");
                viewerPlaceholderSubtitleLabel.setText(
                        "The tuned preview appears here after you run Render Output.");
            } else {
                viewerPlaceholderTitleLabel.setText("Compare unlocks after render");
                viewerPlaceholderSubtitleLabel.setText(
                        "Generate the processed preview to open the side-by-side review mode.");
            }
        }
        viewerPlaceholderBox.setVisible(!showCompare && imageToShow == null);

        setPreviewTileActive(originalTileButton,
                activeViewerMode == ViewerMode.ORIGINAL || activeViewerMode == ViewerMode.COMPARE);
        setPreviewTileActive(processedTileButton,
                activeViewerMode == ViewerMode.PROCESSED || activeViewerMode == ViewerMode.COMPARE);
    }

    private void refreshPreviewTiles() {
        originalTileImageView.setImage(originalPreviewImage);
        originalTileImageView.setVisible(originalPreviewImage != null);
        originalTilePlaceholderLabel.setVisible(originalPreviewImage == null);

        if (originalPreviewImage == null) {
            originalTileInfoLabel.setText("Load a DICOM study");
        } else {
            originalTileInfoLabel.setText(formatImageMetrics(originalPreviewImage) + " input preview");
        }

        processedTileImageView.setImage(processedPreviewImage);
        processedTileImageView.setVisible(processedPreviewImage != null);
        processedTilePlaceholderLabel.setVisible(processedPreviewImage == null);

        if (processedPreviewImage == null) {
            processedTileInfoLabel.setText("Render output to compare");
            return;
        }

        if (renderDirty) {
            processedTileInfoLabel.setText(formatImageMetrics(processedPreviewImage) + " preview from previous settings");
            return;
        }

        processedTileInfoLabel.setText(formatImageMetrics(processedPreviewImage) + " ready to save");
    }

    private void handleOpenImage(Stage stage) {
        FileChooser fileChooser = new FileChooser();
        fileChooser.setTitle("Open DICOM");
        fileChooser.getExtensionFilters().add(DICOM_EXTENSION_FILTER);

        File candidateFile = fileChooser.showOpenDialog(stage);
        if (candidateFile == null) {
            return;
        }

        setStatusMessage("Loading DICOM preview...");

        Image image = loadDicomPreview(candidateFile);
        if (image == null || image.isError()) {
            setStatusMessage("DICOM load failed. Check that the file is readable and supported.");
            return;
        }

        selectedImageFile = candidateFile;
        originalPreviewImage = image;
        clearProcessedOutput();
        activeViewerMode = ViewerMode.ORIGINAL;
        setStatusMessage("Study loaded. Tune the processing lab and render when ready.");
        refreshUi();
        playViewerRefreshAnimation();
    }

    private void handleProcessImage() {
        if (selectedImageFile == null) {
            setStatusMessage("Load a DICOM study before rendering.");
            return;
        }

        setStatusMessage("Rendering processed DICOM...");

        File tempPreviewOutput;
        File tempDicomOutput;
        try {
            tempPreviewOutput = File.createTempFile("xrayview-processed-", ".png");
            tempPreviewOutput.deleteOnExit();
            tempDicomOutput = File.createTempFile("xrayview-processed-", ".dcm");
            tempDicomOutput.deleteOnExit();
        } catch (IOException e) {
            clearProcessedOutput();
            setStatusMessage("Rendering failed before output files could be prepared.");
            refreshUi();
            return;
        }

        CliProcessor.ExecutionResult executionResult;
        try {
            executionResult = cliProcessor.processForUi(selectedImageFile, tempPreviewOutput, tempDicomOutput, uiState);
        } catch (IOException e) {
            clearProcessedOutput();
            setStatusMessage("Rendering failed while the backend was starting.");
            refreshUi();
            return;
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            clearProcessedOutput();
            setStatusMessage("Rendering was interrupted before completion.");
            refreshUi();
            return;
        }

        if (executionResult.exitCode() != 0) {
            clearProcessedOutput();
            setStatusMessage(formatProcessFailureStatus(executionResult.errorOutput()));
            refreshUi();
            return;
        }

        Image processedImage = new Image(tempPreviewOutput.toURI().toString());
        if (processedImage.isError()) {
            clearProcessedOutput();
            setStatusMessage("Rendering finished, but the processed preview could not be loaded.");
            refreshUi();
            return;
        }

        processedPreviewImage = processedImage;
        lastProcessedFile = tempDicomOutput;
        lastSavedDestinationFile = null;
        renderDirty = false;
        activeViewerMode = ViewerMode.PROCESSED;
        setStatusMessage("Render complete. Compare the result or save the derived DICOM.");
        refreshUi();
        playViewerRefreshAnimation();
    }

    private void handleSaveProcessedImage(Stage stage) {
        if (lastProcessedFile == null) {
            setStatusMessage("Nothing to save yet. Render output first.");
            return;
        }
        if (renderDirty) {
            setStatusMessage("Settings changed after the last render. Render again before saving.");
            refreshUi();
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
            lastSavedDestinationFile = destinationFile;
            setStatusMessage("Derived DICOM saved.");
            refreshUi();
        } catch (IOException e) {
            setStatusMessage("Save failed. Check the destination and try again.");
        }
    }

    private void clearProcessedOutput() {
        processedPreviewImage = null;
        lastProcessedFile = null;
        lastSavedDestinationFile = null;
        renderDirty = false;
        if (activeViewerMode != ViewerMode.ORIGINAL) {
            activeViewerMode = ViewerMode.ORIGINAL;
        }
    }

    private void markRenderDirty() {
        if (lastProcessedFile == null || renderDirty) {
            return;
        }
        renderDirty = true;
        lastSavedDestinationFile = null;
        setStatusMessage("Settings changed. Render again to refresh the processed DICOM.");
    }

    private void setStatusMessage(String message) {
        statusValueLabel.setText(message);
    }

    private void selectViewerMode(ViewerMode mode, boolean animate) {
        activeViewerMode = mode;
        refreshViewer();
        if (animate) {
            playViewerRefreshAnimation();
        }
    }

    private void playEntranceAnimation(Node node) {
        node.setOpacity(0.0);
        node.setTranslateY(18.0);

        FadeTransition fade = new FadeTransition(Duration.millis(420), node);
        fade.setFromValue(0.0);
        fade.setToValue(1.0);

        TranslateTransition slide = new TranslateTransition(Duration.millis(520), node);
        slide.setFromY(18.0);
        slide.setToY(0.0);

        new ParallelTransition(fade, slide).play();
    }

    private void playViewerRefreshAnimation() {
        viewerFrame.setOpacity(0.74);
        FadeTransition fade = new FadeTransition(Duration.millis(240), viewerFrame);
        fade.setFromValue(0.74);
        fade.setToValue(1.0);
        fade.play();
    }

    private void configureImageView(ImageView imageView) {
        imageView.setPreserveRatio(true);
        imageView.setSmooth(true);
        imageView.setVisible(false);
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

    private String inferRecipeName() {
        int brightness = (int) Math.round(uiState.getBrightness());
        double contrast = uiState.getContrast();

        if (!uiState.isInvert() && !uiState.isEqualize() && brightness == 0 && approx(contrast, 1.0)
                && "none".equals(uiState.getPalette())) {
            return "Neutral";
        }
        if (!uiState.isInvert() && uiState.isEqualize() && brightness == 10 && approx(contrast, 1.4)
                && "bone".equals(uiState.getPalette())) {
            return "Bone Focus";
        }
        if (!uiState.isInvert() && uiState.isEqualize() && brightness == 0 && approx(contrast, 1.8)
                && "none".equals(uiState.getPalette())) {
            return "High Contrast";
        }
        return "Custom";
    }

    private String describeRecipe(String recipeName) {
        if ("Neutral".equals(recipeName)) {
            return "Balanced grayscale settings for first-pass review.";
        }
        if ("Bone Focus".equals(recipeName)) {
            return "A bone-toned look with extra punch and equalized detail.";
        }
        if ("High Contrast".equals(recipeName)) {
            return "Stronger tonal separation for a sharper grayscale preview.";
        }
        return "Manual tone shaping beyond the quick recipes.";
    }

    private String formatToneDetail() {
        return String.format(
                Locale.US,
                "Invert %s - Equalize %s - Palette %s",
                uiState.isInvert() ? "on" : "off",
                uiState.isEqualize() ? "on" : "off",
                uiState.getPalette());
    }

    private String formatImageMetrics(Image image) {
        if (image == null || image.getWidth() <= 0 || image.getHeight() <= 0) {
            return "Preview unavailable";
        }
        return String.format(Locale.US, "%.0f x %.0f", image.getWidth(), image.getHeight());
    }

    private boolean approx(double left, double right) {
        return Math.abs(left - right) < 0.05;
    }

    private void setPreviewTileActive(Button button, boolean active) {
        button.getStyleClass().remove("preview-tile-active");
        if (active && !button.isDisable()) {
            button.getStyleClass().add("preview-tile-active");
        }
    }

    private static void updateBrightnessValueLabel(Label label, double value) {
        label.setText(String.format(Locale.US, "%+d", (int) Math.round(value)));
    }

    private static void updateContrastValueLabel(Label label, double value) {
        label.setText(String.format(Locale.US, "%.1fx", value));
    }

    private String formatProcessFailureStatus(String errorOutput) {
        String compactError = errorOutput.replaceAll("\\s+", " ").trim();
        if (compactError.isEmpty()) {
            return "Rendering failed. Review the backend configuration and try again.";
        }

        String statusText = "Rendering failed: " + compactError;
        if (statusText.length() > 180) {
            return statusText.substring(0, 177) + "...";
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

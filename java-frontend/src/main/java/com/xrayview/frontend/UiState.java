package com.xrayview.frontend;

// UiState keeps processing settings separate from JavaFX widgets so backend
// calls can use a stable snapshot of the current selections.
public final class UiState {
    private double brightness;
    private double contrast;
    private boolean invert;
    private boolean equalize;
    private String palette;

    public UiState() {
        // Match the initial control defaults.
        this.brightness = 0.0;
        this.contrast = 1.0;
        this.invert = false;
        this.equalize = false;
        this.palette = "none";
    }

    public double getBrightness() {
        return brightness;
    }

    public void setBrightness(double brightness) {
        this.brightness = brightness;
    }

    public double getContrast() {
        return contrast;
    }

    public void setContrast(double contrast) {
        this.contrast = contrast;
    }

    public boolean isInvert() {
        return invert;
    }

    public void setInvert(boolean invert) {
        this.invert = invert;
    }

    public boolean isEqualize() {
        return equalize;
    }

    public void setEqualize(boolean equalize) {
        this.equalize = equalize;
    }

    public String getPalette() {
        return palette;
    }

    public void setPalette(String palette) {
        this.palette = palette;
    }
}

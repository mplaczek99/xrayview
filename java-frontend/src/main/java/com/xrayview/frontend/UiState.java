package com.xrayview.frontend;

// Centralizing UI state in one small object makes future backend integration
// safer because the frontend can pass a coherent snapshot of settings instead of
// reconstructing that data from individual widgets. The class starts with only
// default values on purpose so this optimization improves structure first while
// keeping behavior unchanged until later wiring steps are introduced.
public final class UiState {
    private double brightness;
    private double contrast;
    private boolean invert;
    private boolean equalize;
    private String palette;

    public UiState() {
        // These defaults intentionally match the current UI defaults so the new
        // state holder reflects the existing frontend behavior without changing it.
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

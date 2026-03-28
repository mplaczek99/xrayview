package com.xrayview.frontend;

import javafx.application.Application;

public final class XRayViewLauncher {
    private XRayViewLauncher() {
    }

    public static void main(String[] args) {
        Application.launch(XRayViewApp.class, args);
    }
}

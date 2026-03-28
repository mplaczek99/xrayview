package com.xrayview.frontend;

import java.io.File;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

// CliProcessor owns the Java-to-Go process boundary so command setup and error
// capture stay out of the UI layer.
public final class CliProcessor {
    private static final String BACKEND_PATH_PROPERTY = "xrayview.backend.path";
    private static final String BACKEND_PATH_ENV = "XRAYVIEW_BACKEND_PATH";
    private static final String BUNDLED_BACKEND_RELATIVE_PATH = "backend/xrayview";

    private final File projectRoot = resolveProjectRoot();

    public ExecutionResult run(File inputFile, File outputFile, UiState uiState) throws IOException, InterruptedException {
        List<String> command = buildCliCommand(inputFile, outputFile, uiState);

        ProcessBuilder processBuilder = new ProcessBuilder(command);
        processBuilder.directory(projectRoot);
        processBuilder.redirectOutput(ProcessBuilder.Redirect.DISCARD);

        Process process = processBuilder.start();
        String errorOutput = readProcessErrorOutput(process);
        int exitCode = process.waitFor();
        return new ExecutionResult(exitCode, errorOutput);
    }

    // Resolving the project root once keeps CLI execution from depending on the
    // process working directory, which can vary between Maven, IDE, and packaged runs.
    private File resolveProjectRoot() {
        File rootFromCodeSource = findProjectRoot(resolveCodeSourceLocation());
        if (rootFromCodeSource != null) {
            return rootFromCodeSource;
        }

        File currentDirectory = new File(System.getProperty("user.dir")).getAbsoluteFile();
        File rootFromWorkingDirectory = findProjectRoot(currentDirectory);
        if (rootFromWorkingDirectory != null) {
            return rootFromWorkingDirectory;
        }

        return currentDirectory;
    }

    private File resolveCodeSourceLocation() {
        try {
            return new File(CliProcessor.class.getProtectionDomain().getCodeSource().getLocation().toURI()).getAbsoluteFile();
        } catch (Exception e) {
            return new File(System.getProperty("user.dir")).getAbsoluteFile();
        }
    }

    private File findProjectRoot(File start) {
        File directory = start;
        if (directory.isFile()) {
            directory = directory.getParentFile();
        }

        while (directory != null) {
            if (new File(directory, "cmd/xrayview").isDirectory()) {
                return directory.getAbsoluteFile();
            }
            directory = directory.getParentFile();
        }

        return null;
    }

    private List<String> buildCliCommand(File inputFile, File outputFile, UiState uiState) {
        List<String> command = new ArrayList<>();

        File bundledBinary = resolveBundledBackendBinary();
        if (bundledBinary != null) {
            command.add(bundledBinary.getAbsolutePath());
        } else {
            File explicitBinary = resolveExplicitBackendBinary();
            if (explicitBinary != null) {
                command.add(explicitBinary.getAbsolutePath());
            } else {
                File binary = new File(projectRoot, "xrayview");
                if (isExecutableFile(binary)) {
                    command.add(binary.getAbsolutePath());
                } else {
                    command.add("go");
                    command.add("run");
                    command.add("./cmd/xrayview");
                }
            }
        }

        command.add("-input");
        command.add(inputFile.getAbsolutePath());
        command.add("-output");
        command.add(outputFile.getAbsolutePath());
        command.add("-invert=" + uiState.isInvert());
        command.add("-brightness=" + (int) Math.round(uiState.getBrightness()));
        command.add("-contrast=" + Double.toString(uiState.getContrast()));
        command.add("-equalize=" + uiState.isEqualize());
        command.add("-palette=" + uiState.getPalette());

        return command;
    }

    // jpackage app-images place extra input files next to the main jar under
    // lib/app, so a sibling backend path stays stable for local packaging.
    private File resolveBundledBackendBinary() {
        File codeSourceLocation = resolveCodeSourceLocation();
        File appDirectory = codeSourceLocation.isFile() ? codeSourceLocation.getParentFile() : codeSourceLocation;
        if (appDirectory == null) {
            return null;
        }

        File binary = new File(appDirectory, BUNDLED_BACKEND_RELATIVE_PATH).getAbsoluteFile();
        if (isExecutableFile(binary)) {
            return binary;
        }

        return null;
    }

    // Packaged desktop builds can point at a bundled backend without depending
    // on the source checkout layout.
    private File resolveExplicitBackendBinary() {
        String configuredPath = System.getProperty(BACKEND_PATH_PROPERTY);
        if (configuredPath == null || configuredPath.isBlank()) {
            configuredPath = System.getenv(BACKEND_PATH_ENV);
        }
        if (configuredPath == null || configuredPath.isBlank()) {
            return null;
        }

        File binary = new File(configuredPath).getAbsoluteFile();
        if (isExecutableFile(binary)) {
            return binary;
        }

        return null;
    }

    private boolean isExecutableFile(File file) {
        return file.isFile() && file.canExecute();
    }

    private String readProcessErrorOutput(Process process) throws IOException {
        return new String(process.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
    }

    public record ExecutionResult(int exitCode, String errorOutput) {
    }
}

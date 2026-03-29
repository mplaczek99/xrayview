package com.xrayview.frontend;

import java.io.File;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/** Runs the Go CLI backend from the Java frontend. */
public final class CliProcessor {
    private static final String BACKEND_PATH_PROPERTY = "xrayview.backend.path";
    private static final String BACKEND_PATH_ENV = "XRAYVIEW_BACKEND_PATH";
    private static final String BACKEND_DIRECTORY_NAME = "backend";
    private static final String BACKEND_BASE_NAME = "xrayview";
    private static final String WINDOWS_EXECUTABLE_SUFFIX = ".exe";

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

    // Resolve the project root once.
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

    private List<String> buildCliCommand(File inputFile, File outputFile, UiState uiState) throws IOException {
        List<String> command = new ArrayList<>();

        File explicitBinary = resolveExplicitBackendBinary();
        if (explicitBinary != null) {
            command.add(explicitBinary.getAbsolutePath());
        } else {
            File bundledBinary = resolveBundledBackendBinary();
            if (bundledBinary != null) {
                command.add(bundledBinary.getAbsolutePath());
            } else {
                File binary = resolveProjectBackendBinary();
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

    // Check for a backend bundled next to the app image.
    private File resolveBundledBackendBinary() {
        File codeSourceLocation = resolveCodeSourceLocation();
        File appDirectory = codeSourceLocation.isFile() ? codeSourceLocation.getParentFile() : codeSourceLocation;
        if (appDirectory == null) {
            return null;
        }

        for (String fileName : backendBinaryNames()) {
            File binary = new File(new File(appDirectory, BACKEND_DIRECTORY_NAME), fileName).getAbsoluteFile();
            if (isExecutableFile(binary)) {
                return binary;
            }
        }

        return null;
    }

    // Allow an explicit backend override.
    private File resolveExplicitBackendBinary() throws IOException {
        String configuredPath = System.getProperty(BACKEND_PATH_PROPERTY);
        if (configuredPath == null || configuredPath.isBlank()) {
            configuredPath = System.getenv(BACKEND_PATH_ENV);
        }
        if (configuredPath == null || configuredPath.isBlank()) {
            return null;
        }

        File binary = new File(configuredPath).getAbsoluteFile();
        if (!isExecutableFile(binary)) {
            throw new IOException("Configured backend binary is not runnable: " + binary.getAbsolutePath());
        }

        return binary;
    }

    private File resolveProjectBackendBinary() {
        for (String fileName : backendBinaryNames()) {
            File binary = new File(projectRoot, fileName).getAbsoluteFile();
            if (isExecutableFile(binary)) {
                return binary;
            }
        }

        return new File(projectRoot, preferredBackendBinaryName()).getAbsoluteFile();
    }

    private boolean isExecutableFile(File file) {
        if (!file.isFile()) {
            return false;
        }

        if (isWindows()) {
            return file.getName().toLowerCase(Locale.ROOT).endsWith(WINDOWS_EXECUTABLE_SUFFIX);
        }

        return file.canExecute();
    }

    private List<String> backendBinaryNames() {
        List<String> names = new ArrayList<>(2);
        names.add(preferredBackendBinaryName());
        String legacyName = BACKEND_BASE_NAME;
        if (!names.contains(legacyName)) {
            names.add(legacyName);
        }
        return names;
    }

    private String preferredBackendBinaryName() {
        if (isWindows()) {
            return BACKEND_BASE_NAME + WINDOWS_EXECUTABLE_SUFFIX;
        }
        return BACKEND_BASE_NAME;
    }

    private boolean isWindows() {
        String osName = System.getProperty("os.name", "");
        return osName.toLowerCase(Locale.ROOT).contains("win");
    }

    private String readProcessErrorOutput(Process process) throws IOException {
        return new String(process.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
    }

    public record ExecutionResult(int exitCode, String errorOutput) {
    }
}

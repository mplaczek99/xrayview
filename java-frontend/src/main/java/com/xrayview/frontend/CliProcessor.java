package com.xrayview.frontend;

import java.io.File;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

// This optimization pass is being done in tiny slices so structure can improve
// without disturbing the behavior that the Java frontend already relies on.
// Backend invocation is a good first extraction point because it is a distinct
// boundary between UI code and Go processing code, and keeping that boundary in
// one helper reduces responsibility in XRayViewApp without changing workflow.
// Behavior preservation matters here because the CLI call path is already the
// integration contract, so refactoring should clarify it rather than redefine it.
public final class CliProcessor {
    public ExecutionResult run(File inputFile, File outputFile, UiState uiState) throws IOException, InterruptedException {
        File repoRoot = resolveRepoRoot();
        List<String> command = buildCliCommand(repoRoot, inputFile, outputFile, uiState);

        ProcessBuilder processBuilder = new ProcessBuilder(command);
        processBuilder.directory(repoRoot);
        processBuilder.redirectOutput(ProcessBuilder.Redirect.DISCARD);

        Process process = processBuilder.start();
        String errorOutput = readProcessErrorOutput(process);
        int exitCode = process.waitFor();
        return new ExecutionResult(exitCode, errorOutput);
    }

    private File resolveRepoRoot() {
        File currentDirectory = new File(System.getProperty("user.dir"));
        if (new File(currentDirectory, "cmd/xrayview").isDirectory()) {
            return currentDirectory;
        }

        File parentDirectory = currentDirectory.getParentFile();
        if (parentDirectory != null && new File(parentDirectory, "cmd/xrayview").isDirectory()) {
            return parentDirectory;
        }

        return currentDirectory;
    }

    private List<String> buildCliCommand(File repoRoot, File inputFile, File outputFile, UiState uiState) {
        List<String> command = new ArrayList<>();

        File binary = new File(repoRoot, "xrayview");
        if (binary.isFile() && binary.canExecute()) {
            command.add(binary.getAbsolutePath());
        } else {
            command.add("go");
            command.add("run");
            command.add("./cmd/xrayview");
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

    private String readProcessErrorOutput(Process process) throws IOException {
        return new String(process.getErrorStream().readAllBytes(), StandardCharsets.UTF_8);
    }

    public record ExecutionResult(int exitCode, String errorOutput) {
    }
}

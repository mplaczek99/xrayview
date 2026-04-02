export type BusyAction = "opening" | "measuring" | "processing" | null;

export type ProcessingRunState =
  | { state: "idle" }
  | { state: "running" }
  | { state: "success"; outputPath: string }
  | { state: "error"; message: string };

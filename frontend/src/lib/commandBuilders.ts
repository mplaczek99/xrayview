import type { ProcessStudyCommand } from "./generated/contracts";
import type { ProcessingRequest } from "./types";

export function buildProcessStudyCommand(
  studyId: string,
  request: ProcessingRequest,
): ProcessStudyCommand {
  return {
    studyId,
    outputPath: request.outputPath,
    presetId: request.presetId,
    invert: request.controls.invert && !request.presetControls.invert,
    brightness:
      request.controls.brightness !== request.presetControls.brightness
        ? request.controls.brightness
        : null,
    contrast:
      request.controls.contrast !== request.presetControls.contrast
        ? request.controls.contrast
        : null,
    equalize: request.controls.equalize && !request.presetControls.equalize,
    compare: request.compare,
    palette:
      request.controls.palette !== request.presetControls.palette
        ? request.controls.palette
        : null,
  };
}

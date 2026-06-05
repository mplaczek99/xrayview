import { defaultSchemaPath, loadContractSchema, validateDefinition } from "./schema-tools.mjs";

const schema = loadContractSchema(defaultSchemaPath);

function expectValid(definitionName, value) {
  const errors = validateDefinition(schema, definitionName, value);
  if (errors.length > 0) {
    throw new Error(
      `${definitionName} should be valid:\n${errors.map((error) => `  ${error}`).join("\n")}`,
    );
  }
}

function expectInvalid(definitionName, value, expectedError) {
  const errors = validateDefinition(schema, definitionName, value);
  if (!errors.some((error) => error.includes(expectedError))) {
    throw new Error(
      `${definitionName} should reject ${expectedError}:\n${
        errors.length ? errors.map((error) => `  ${error}`).join("\n") : "  no errors"
      }`,
    );
  }
}

const processingControls = {
  brightness: 10,
  contrast: 1.4,
  invert: false,
  equalize: true,
  palette: "bone",
};

const processStudyCommand = {
  studyId: "study-1",
  presetId: "default",
  invert: false,
  brightness: 10,
  equalize: false,
  compare: false,
};

expectValid("ProcessingControls", processingControls);
expectInvalid(
  "ProcessingControls",
  { ...processingControls, contrast: Number.POSITIVE_INFINITY },
  "ProcessingControls.contrast: expected finite number",
);
expectInvalid(
  "ProcessingControls",
  { ...processingControls, brightness: 1.5 },
  "ProcessingControls.brightness: expected integer",
);

expectValid("ProcessStudyCommand", processStudyCommand);
expectInvalid(
  "ProcessStudyCommand",
  { ...processStudyCommand, brightness: 1.5 },
  "ProcessStudyCommand.brightness: value does not match any allowed type",
);

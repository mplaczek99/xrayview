import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import {
  defaultSchemaPath,
  getGeneratedTargetPaths,
  loadContractSchema,
  readFileIfPresent,
  renderGoValidationBindings,
  renderTypeScriptContracts,
  workspaceRoot,
  writeFileIfChanged,
} from "./schema-tools.mjs";

function parseArgs(argv) {
  const result = {
    check: false,
    stdout: null,
    schemaPath: defaultSchemaPath,
  };

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];

    if (arg === "--check") {
      result.check = true;
      continue;
    }

    if (arg === "--stdout") {
      result.stdout = argv[index + 1] ?? null;
      index += 1;
      continue;
    }

    if (arg === "--schema") {
      result.schemaPath = path.resolve(workspaceRoot, argv[index + 1] ?? "");
      index += 1;
      continue;
    }

    throw new Error(`unsupported argument: ${arg}`);
  }

  return result;
}

function relativeFromWorkspace(filePath) {
  return path.relative(workspaceRoot, filePath).replaceAll(path.sep, "/");
}

function buildOutputs(schemaPath) {
  const schema = loadContractSchema(schemaPath);
  const schemaSource = fs.readFileSync(schemaPath, "utf8");
  const targets = getGeneratedTargetPaths(schema);
  const schemaRelativePath = relativeFromWorkspace(schemaPath);

  return {
    schema,
    targets,
    typescript: renderTypeScriptContracts(schema, { schemaRelativePath }),
    goValidationBindings: renderGoValidationBindings(schema, schemaSource, {
      schemaRelativePath,
    }),
  };
}

function run() {
  const options = parseArgs(process.argv.slice(2));
  const outputs = buildOutputs(options.schemaPath);

  if (options.stdout === "typescript") {
    process.stdout.write(outputs.typescript);
    return;
  }

  if (options.stdout === "go") {
    process.stdout.write(outputs.goValidationBindings);
    return;
  }

  if (options.stdout !== null) {
    throw new Error(`unsupported stdout target: ${options.stdout}`);
  }

  if (options.check) {
    const drifted = [];

    if (readFileIfPresent(outputs.targets.typescript) !== outputs.typescript) {
      drifted.push(relativeFromWorkspace(outputs.targets.typescript));
    }

    if (readFileIfPresent(outputs.targets.goValidationBindings) !== outputs.goValidationBindings) {
      drifted.push(relativeFromWorkspace(outputs.targets.goValidationBindings));
    }

    if (drifted.length > 0) {
      throw new Error(
        `generated contract bindings are out of date: ${drifted.join(", ")}`,
      );
    }

    return;
  }

  writeFileIfChanged(outputs.targets.typescript, outputs.typescript);
  writeFileIfChanged(outputs.targets.goValidationBindings, outputs.goValidationBindings);

  process.stdout.write(
    `generated ${relativeFromWorkspace(outputs.targets.typescript)}\n`,
  );
  process.stdout.write(
    `generated ${relativeFromWorkspace(outputs.targets.goValidationBindings)}\n`,
  );
}

try {
  run();
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`error: ${message}\n`);
  process.exit(1);
}

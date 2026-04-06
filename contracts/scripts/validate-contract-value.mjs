import process from "node:process";
import { defaultSchemaPath, loadContractSchema, validateDefinition } from "./schema-tools.mjs";

async function readStdin() {
  const chunks = [];

  for await (const chunk of process.stdin) {
    chunks.push(chunk);
  }

  return Buffer.concat(chunks).toString("utf8");
}

async function run() {
  const definitionName = process.argv[2];

  if (!definitionName) {
    throw new Error("usage: validate-contract-value <definition-name>");
  }

  const input = await readStdin();
  const value = JSON.parse(input);
  const schema = loadContractSchema(defaultSchemaPath);
  const errors = validateDefinition(schema, definitionName, value);

  if (errors.length > 0) {
    throw new Error(errors.join("\n"));
  }
}

run().catch((error) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exit(1);
});

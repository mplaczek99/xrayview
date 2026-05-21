import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
export const workspaceRoot = path.resolve(scriptDir, "../..");
export const defaultSchemaPath = path.join(
  workspaceRoot,
  "contracts",
  "backend-contract-v1.schema.json",
);

function invariant(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function readJsonFile(filePath) {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function indent(text, level = 1) {
  const prefix = "  ".repeat(level);
  return text
    .split("\n")
    .map((line) => (line ? `${prefix}${line}` : line))
    .join("\n");
}

function quote(value) {
  return JSON.stringify(value);
}

function tsRefName(ref) {
  invariant(ref.startsWith("#/$defs/"), `unsupported schema ref: ${ref}`);
  return ref.slice("#/$defs/".length);
}

function hasOwn(object, key) {
  return Object.prototype.hasOwnProperty.call(object, key);
}

function isPlainObject(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function schemaEntries(schema) {
  const exportOrder = schema["x-export-order"];
  invariant(Array.isArray(exportOrder), "schema is missing x-export-order");
  const definitions = schema.$defs ?? {};
  return exportOrder.map((name) => {
    invariant(hasOwn(definitions, name), `schema is missing $defs.${name}`);
    return [name, definitions[name]];
  });
}

function renderTsProperty(name, propertySchema, required) {
  const optional = required.has(name) ? "" : "?";
  return `${name}${optional}: ${renderTsType(propertySchema)};`;
}

function renderTsInlineObject(schema) {
  const properties = schema.properties ?? {};
  const propertyEntries = Object.entries(properties);
  const required = new Set(schema.required ?? []);
  const renderedProperties = propertyEntries.map(([name, propertySchema]) =>
    renderTsProperty(name, propertySchema, required),
  );

  if (
    renderedProperties.length <= 2 &&
    renderedProperties.every((entry) => !entry.includes("\n")) &&
    renderedProperties.join(" ").length <= 72
  ) {
    return `{ ${renderedProperties.join(" ")} }`;
  }

  return `{\n${indent(renderedProperties.join("\n"))}\n}`;
}

function renderTsUnion(schemaList) {
  const rendered = schemaList.map((entry) => renderTsType(entry));
  if (rendered.every((entry) => !entry.includes("\n")) && rendered.join(" | ").length <= 80) {
    return rendered.join(" | ");
  }

  return `\n  | ${rendered.join("\n  | ")}`;
}

export function loadContractSchema(schemaPath = defaultSchemaPath) {
  const schema = readJsonFile(schemaPath);
  invariant(
    typeof schema["x-contract-version"] === "number",
    "schema is missing x-contract-version",
  );
  return schema;
}

export function getGeneratedTargetPaths(schema, root = workspaceRoot) {
  const targets = schema["x-generated-targets"] ?? {};
  invariant(typeof targets.typescript === "string", "schema is missing TypeScript target path");
  invariant(
    typeof targets.goValidationBindings === "string",
    "schema is missing Go validation target path",
  );

  return {
    typescript: path.join(root, targets.typescript),
    goValidationBindings: path.join(root, targets.goValidationBindings),
  };
}

function isTopLevelObjectDefinition(definition) {
  return !definition.enum && !definition.const && !definition.oneOf && !definition.anyOf && !definition.$ref
    && ((definition.type === "object") || isPlainObject(definition.properties));
}

function primitiveTsType(typeName) {
  switch (typeName) {
    case "string":
      return "string";
    case "number":
    case "integer":
      return "number";
    case "boolean":
      return "boolean";
    case "null":
      return "null";
    default:
      throw new Error(`unsupported schema type: ${typeName}`);
  }
}

function renderTsArray(itemsSchema) {
  const itemType = renderTsType(itemsSchema);
  if (itemType.includes("\n") || itemType.includes(" | ")) {
    return `(${itemType})[]`;
  }
  return `${itemType}[]`;
}

function renderTsType(schema) {
  if (schema.$ref) {
    return tsRefName(schema.$ref);
  }

  if (schema.const !== undefined) {
    return quote(schema.const);
  }

  if (Array.isArray(schema.enum)) {
    return schema.enum.map((entry) => quote(entry)).join(" | ");
  }

  if (Array.isArray(schema.oneOf)) {
    return renderTsUnion(schema.oneOf);
  }

  if (Array.isArray(schema.anyOf)) {
    return renderTsUnion(schema.anyOf);
  }

  if (Array.isArray(schema.type)) {
    return schema.type.map((entry) => primitiveTsType(entry)).join(" | ");
  }

  if (schema.type === "array") {
    invariant(schema.items, "array schema is missing items");
    return renderTsArray(schema.items);
  }

  if (schema.type === "object" || isPlainObject(schema.properties)) {
    return renderTsInlineObject(schema);
  }

  if (typeof schema.type === "string") {
    return primitiveTsType(schema.type);
  }

  throw new Error(`unsupported schema node: ${JSON.stringify(schema)}`);
}

function renderTsDefinition(name, definition) {
  if (isTopLevelObjectDefinition(definition)) {
    const properties = definition.properties ?? {};
    const required = new Set(definition.required ?? []);
    const lines = Object.entries(properties).map(([propertyName, propertySchema]) =>
      `  ${renderTsProperty(propertyName, propertySchema, required)}`,
    );
    return `export interface ${name} {\n${lines.join("\n")}\n}`;
  }

  const renderedType = renderTsType(definition);
  if (renderedType.startsWith("\n")) {
    return `export type ${name} =${renderedType};`;
  }
  return `export type ${name} = ${renderedType};`;
}

export function renderTypeScriptContracts(
  schema,
  {
    schemaRelativePath = path.relative(workspaceRoot, defaultSchemaPath).replaceAll(path.sep, "/"),
  } = {},
) {
  const sections = [
    `// This file is generated from \`${schemaRelativePath}\`.`,
    "// Run `npm run contracts:generate` after changing the schema.",
    `// Backend contract version: v${schema["x-contract-version"]}`,
    "",
    `export const BACKEND_CONTRACT_VERSION = ${schema["x-contract-version"]} as const;`,
    "",
  ];

  for (const [name, definition] of schemaEntries(schema)) {
    sections.push(renderTsDefinition(name, definition));
    sections.push("");
  }

  return `${sections.join("\n").trimEnd()}\n`;
}

function renderGoStringLiteral(value) {
  return JSON.stringify(value);
}

const goAcronyms = new Map([
  ["id", "ID"],
  ["ids", "IDs"],
  ["mm", "MM"],
]);

const goIntegerFields = new Set(["brightness", "percent"]);
const goUnsignedIntegerFields = new Set(["loadedWidth", "loadedHeight"]);

function splitIdentifierWords(value) {
  return (
    String(value)
      .replace(/[^A-Za-z0-9]+/g, " ")
      .trim()
      .match(/[A-Z]?[a-z0-9]+|[A-Z]+(?![a-z])/g) ?? []
  );
}

function renderGoIdentifier(value) {
  const words = splitIdentifierWords(value);
  invariant(words.length > 0, `cannot render Go identifier from ${value}`);
  return words
    .map((word) => {
      const lower = word.toLowerCase();
      return goAcronyms.get(lower) ?? `${lower.slice(0, 1).toUpperCase()}${lower.slice(1)}`;
    })
    .join("");
}

function renderGoEnumConstPrefix(name) {
  return name.endsWith("Name") ? name.slice(0, -"Name".length) : name;
}

function renderGoJsonTag(name, omitempty) {
  return `\`json:"${name}${omitempty ? ",omitempty" : ""}"\``;
}

function goRefName(ref) {
  invariant(ref.startsWith("#/$defs/"), `unsupported schema ref: ${ref}`);
  return ref.slice("#/$defs/".length);
}

function isNullSchema(schema) {
  return schema?.type === "null";
}

function withoutNullSchemas(schemaList) {
  return schemaList.filter((entry) => !isNullSchema(entry));
}

function isNullableSchema(schema) {
  if (Array.isArray(schema.type) && schema.type.includes("null")) {
    return true;
  }
  if (Array.isArray(schema.anyOf) && schema.anyOf.some(isNullSchema)) {
    return true;
  }
  if (Array.isArray(schema.oneOf) && schema.oneOf.some(isNullSchema)) {
    return true;
  }
  return false;
}

function renderGoPrimitiveType(typeName, propertyName = "") {
  switch (typeName) {
    case "string":
      return "string";
    case "number":
    case "integer":
      if (goUnsignedIntegerFields.has(propertyName)) {
        return "uint32";
      }
      if (goIntegerFields.has(propertyName)) {
        return "int";
      }
      return "float64";
    case "boolean":
      return "bool";
    default:
      throw new Error(`unsupported Go schema type: ${typeName}`);
  }
}

function renderGoType(schema, { propertyName = "" } = {}) {
  if (schema.$ref) {
    return goRefName(schema.$ref);
  }

  if (schema.const !== undefined) {
    return "string";
  }

  if (Array.isArray(schema.enum)) {
    return "string";
  }

  if (Array.isArray(schema.anyOf) || Array.isArray(schema.oneOf)) {
    const variants = withoutNullSchemas(schema.anyOf ?? schema.oneOf);
    if (variants.length === 1 && isNullableSchema(schema)) {
      return `*${renderGoType(variants[0], { propertyName })}`;
    }
    return "any";
  }

  if (Array.isArray(schema.type)) {
    const variants = schema.type.filter((entry) => entry !== "null");
    if (variants.length === 1 && schema.type.includes("null")) {
      return `*${renderGoPrimitiveType(variants[0], propertyName)}`;
    }
    return "any";
  }

  if (schema.type === "array") {
    invariant(schema.items, "array schema is missing items");
    return `[]${renderGoType(schema.items, { propertyName })}`;
  }

  if (schema.type === "object" || isPlainObject(schema.properties)) {
    return "map[string]any";
  }

  if (typeof schema.type === "string") {
    return renderGoPrimitiveType(schema.type, propertyName);
  }

  throw new Error(`unsupported Go schema node: ${JSON.stringify(schema)}`);
}

function renderGoEnumDefinition(name, definition) {
  const prefix = renderGoEnumConstPrefix(name);
  const lines = definition.enum.map((value) => {
    const constName = `${prefix}${renderGoIdentifier(value)}`;
    return `\t${constName} ${name} = ${renderGoStringLiteral(value)}`;
  });

  return `type ${name} string

const (
${lines.join("\n")}
)`;
}

function renderGoObjectDefinition(name, definition) {
  const properties = definition.properties ?? {};
  const required = new Set(definition.required ?? []);
  const lines = Object.entries(properties).map(([propertyName, propertySchema]) => {
    const fieldName = renderGoIdentifier(propertyName);
    const fieldType = renderGoType(propertySchema, { propertyName });
    const omitempty = !required.has(propertyName) || isNullableSchema(propertySchema);
    return `\t${fieldName} ${fieldType} ${renderGoJsonTag(propertyName, omitempty)}`;
  });

  return `type ${name} struct {
${lines.join("\n")}
}`;
}

function renderGoDefinition(name, definition) {
  if (Array.isArray(definition.enum)) {
    return renderGoEnumDefinition(name, definition);
  }

  if (isTopLevelObjectDefinition(definition)) {
    return renderGoObjectDefinition(name, definition);
  }

  if (name === "JobResult" && Array.isArray(definition.oneOf)) {
    return `type JobResult struct {
\tKind    JobKind \`json:"kind"\`
\tPayload any     \`json:"payload"\`
}`;
  }

  throw new Error(`unsupported Go definition ${name}: ${JSON.stringify(definition)}`);
}

export function renderGoValidationBindings(
  schema,
  schemaSource,
  {
    schemaRelativePath = path.relative(workspaceRoot, defaultSchemaPath).replaceAll(path.sep, "/"),
  } = {},
) {
  const goPackage = schema["x-go-package"];
  invariant(typeof goPackage === "string" && goPackage.length > 0, "schema is missing x-go-package");
  const normalizedSchemaSource = schemaSource.replaceAll("\r\n", "\n").replaceAll("\r", "\n");

  const definitionRefs = schemaEntries(schema)
    .map(([name]) => `\t${renderGoStringLiteral(name)}: ${renderGoStringLiteral(`#/$defs/${name}`)},`)
    .join("\n");
  const typeDefinitions = schemaEntries(schema)
    .map(([name, definition]) => renderGoDefinition(name, definition))
    .join("\n\n");

  return `// Code generated from ${schemaRelativePath}. DO NOT EDIT.

package ${goPackage}

const BackendContractVersion = ${schema["x-contract-version"]}

const BackendContractSchemaID = ${renderGoStringLiteral(schema.$id)}

// BackendContractSchemaJSON is the authoritative contract schema for future Go validation.
const BackendContractSchemaJSON = \`${normalizedSchemaSource.trimEnd()}\`

${typeDefinitions}

var DefinitionRefs = map[string]string{
${definitionRefs}
}
`;
}

function describeValue(value) {
  if (value === null) {
    return "null";
  }
  if (Array.isArray(value)) {
    return "array";
  }
  return typeof value;
}

function resolveRef(schema, ref) {
  const definitionName = tsRefName(ref);
  const definition = schema.$defs?.[definitionName];
  invariant(definition, `schema reference ${ref} does not exist`);
  return definition;
}

function validateType(typeName, value, at) {
  switch (typeName) {
    case "string":
      return typeof value === "string" ? [] : [`${at}: expected string, received ${describeValue(value)}`];
    case "number":
      return typeof value === "number" ? [] : [`${at}: expected number, received ${describeValue(value)}`];
    case "integer":
      return Number.isInteger(value)
        ? []
        : [`${at}: expected integer, received ${describeValue(value)}`];
    case "boolean":
      return typeof value === "boolean" ? [] : [`${at}: expected boolean, received ${describeValue(value)}`];
    case "null":
      return value === null ? [] : [`${at}: expected null, received ${describeValue(value)}`];
    case "object":
      return isPlainObject(value) ? [] : [`${at}: expected object, received ${describeValue(value)}`];
    case "array":
      return Array.isArray(value) ? [] : [`${at}: expected array, received ${describeValue(value)}`];
    default:
      throw new Error(`unsupported validator type: ${typeName}`);
  }
}

function validateSchemaNode(schema, node, value, at) {
  if (node.$ref) {
    return validateSchemaNode(schema, resolveRef(schema, node.$ref), value, at);
  }

  if (node.const !== undefined) {
    return value === node.const
      ? []
      : [`${at}: expected ${quote(node.const)}, received ${quote(value)}`];
  }

  if (Array.isArray(node.enum)) {
    return node.enum.includes(value)
      ? []
      : [`${at}: expected one of ${node.enum.map((entry) => quote(entry)).join(", ")}, received ${quote(value)}`];
  }

  if (Array.isArray(node.oneOf)) {
    const matchingBranches = node.oneOf.filter(
      (branch) => validateSchemaNode(schema, branch, value, at).length === 0,
    );
    if (matchingBranches.length === 1) {
      return [];
    }

    if (matchingBranches.length > 1) {
      return [`${at}: value matches multiple oneOf branches`];
    }

    return [`${at}: value does not match any oneOf branch`];
  }

  if (Array.isArray(node.anyOf)) {
    const accepted = node.anyOf.some(
      (branch) => validateSchemaNode(schema, branch, value, at).length === 0,
    );
    return accepted ? [] : [`${at}: value does not match any allowed shape`];
  }

  if (Array.isArray(node.type)) {
    const accepted = node.type.some(
      (typeName) => validateType(typeName, value, at).length === 0,
    );
    return accepted ? [] : [`${at}: value does not match any allowed type`];
  }

  if (node.type === "array") {
    const arrayErrors = validateType("array", value, at);
    if (arrayErrors.length > 0) {
      return arrayErrors;
    }

    const itemSchema = node.items ?? {};
    const errors = [];
    for (let index = 0; index < value.length; index += 1) {
      errors.push(...validateSchemaNode(schema, itemSchema, value[index], `${at}[${index}]`));
    }
    return errors;
  }

  if (node.type === "object" || isPlainObject(node.properties)) {
    const objectErrors = validateType("object", value, at);
    if (objectErrors.length > 0) {
      return objectErrors;
    }

    const properties = node.properties ?? {};
    const required = new Set(node.required ?? []);
    const errors = [];

    for (const propertyName of required) {
      if (!hasOwn(value, propertyName)) {
        errors.push(`${at}.${propertyName}: missing required property`);
      }
    }

    for (const [propertyName, propertyValue] of Object.entries(value)) {
      if (!hasOwn(properties, propertyName)) {
        if (node.additionalProperties === false) {
          errors.push(`${at}.${propertyName}: unexpected property`);
        }
        continue;
      }

      errors.push(
        ...validateSchemaNode(
          schema,
          properties[propertyName],
          propertyValue,
          `${at}.${propertyName}`,
        ),
      );
    }

    return errors;
  }

  if (typeof node.type === "string") {
    return validateType(node.type, value, at);
  }

  return [`${at}: unsupported schema node`];
}

export function validateDefinition(schema, definitionName, value) {
  const definition = schema.$defs?.[definitionName];
  invariant(definition, `schema does not export ${definitionName}`);
  return validateSchemaNode(schema, definition, value, definitionName);
}

export function writeFileIfChanged(filePath, contents) {
  if (readFileIfPresent(filePath) === contents) {
    return false;
  }

  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, contents);
  return true;
}

export function readFileIfPresent(filePath) {
  try {
    return fs.readFileSync(filePath, "utf8");
  } catch (error) {
    if (error && typeof error === "object" && error.code === "ENOENT") {
      return null;
    }
    throw error;
  }
}

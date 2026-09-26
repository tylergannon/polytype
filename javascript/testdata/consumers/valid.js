// A real ESM JavaScript consumer of the generated JSDoc types. Every value is
// valid, including a recursive Node chain, so strict allowJs/checkJs has
// nothing to report.

/**
 * A required-field object.
 *
 * @type {import('./types.js').Person}
 */
export const person = { name: "Ada", age: 36 };

/** @type {import('./types.js').Status} */
export const status = "ready";

/** @type {import('./types.js').Node} */
export const node = {
  value: "root",
  next: { value: "child", next: { value: "leaf" } },
};

/** @type {import('./types.js').Envelope} */
export const envelope = {
  person: { name: "Ada", age: 36 },
  event: { kind: "created", id: "1" },
};

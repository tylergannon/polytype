// A nested value and a value inside the recursive structure are both wrong.

/** @type {import('./types.js').Envelope} */
export const wrongNested = {
  person: { name: "Ada", age: "old" },
  event: { kind: "created", id: "1" },
};

/** @type {import('./types.js').Node} */
export const wrongRecursive = {
  value: "root",
  next: { value: 7 },
};

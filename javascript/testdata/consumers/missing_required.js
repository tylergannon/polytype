// The required `age` field is missing; a required-field object must still
// demand every non-optional property.

/** @type {import('./types.js').Person} */
export const missingRequired = { name: "Ada" };

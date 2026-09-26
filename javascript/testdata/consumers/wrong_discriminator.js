// The sealed union's discriminator only admits the variant tags.

/** @type {import('./types.js').Envelope} */
export const wrongDiscriminator = {
  person: { name: "Ada", age: 36 },
  event: { kind: "wrong", id: "1" },
};

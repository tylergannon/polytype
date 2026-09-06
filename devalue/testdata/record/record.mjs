// Maps every generated value through the pinned devalue package and writes the
// golden. Run after `go run ./devalue/testdata/record`; see README.md.
import { writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { stringify } from "devalue";

import { values } from "./values.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const seen = new Set();
const entries = values.map(([name, value]) => {
  if (seen.has(name)) throw new Error(`duplicate case name ${name}`);
  seen.add(name);
  return { name, devalue: stringify(value) };
});
writeFileSync(join(here, "..", "golden.json"), JSON.stringify(entries, null, 2) + "\n");
process.stdout.write(`wrote ${entries.length} cases\n`);

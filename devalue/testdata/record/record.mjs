// Maps every generated value through the pinned devalue package and writes the
// golden. Run after `go run ./devalue/testdata/record`; see README.md.
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { stringify, uneval } from "devalue";

import { values } from "./values.mjs";

const here = dirname(fileURLToPath(import.meta.url));

// The golden is only evidence of parity with the pinned release, so refuse to
// record from any other installed version.
const pinned = JSON.parse(readFileSync(join(here, "..", "..", "..", "package.json"), "utf8"))
  .devDependencies.devalue;
const installed = installedVersion();
if (installed !== pinned) {
  throw new Error(`installed devalue ${installed} is not the pinned ${pinned}; reinstall the repository root's packages`);
}

const seen = new Set();
const cases = values.map(([name, value]) => {
  if (seen.has(name)) throw new Error(`duplicate case name ${name}`);
  seen.add(name);
  return { name, devalue: stringify(value), uneval: uneval(value) };
});
writeFileSync(
  join(here, "..", "golden.json"),
  JSON.stringify({ devalue: installed, cases }, null, 2) + "\n",
);
process.stdout.write(`wrote ${cases.length} cases from devalue ${installed}\n`);

// installedVersion reads the version of the devalue package that the import
// above resolved to. Its exports map hides package.json, so walk up from the
// resolved entry point.
function installedVersion() {
  let dir = dirname(fileURLToPath(import.meta.resolve("devalue")));
  for (;;) {
    const file = join(dir, "package.json");
    if (existsSync(file)) {
      const pkg = JSON.parse(readFileSync(file, "utf8"));
      if (pkg.name === "devalue") return pkg.version;
    }
    const parent = dirname(dir);
    if (parent === dir) throw new Error("cannot find the installed devalue package.json");
    dir = parent;
  }
}

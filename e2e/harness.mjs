// A minimal test harness for driving the window in a real browser.
//
// Deliberately not @playwright/test: the suite has to run strictly in order
// against one shared application — the Go session holds the part tree, so two
// tests cutting at once would fight over it — and a serial runner that owns the
// ordering is less code than configuring a parallel one to stop being parallel.
//
// ponytail: own runner, no config file, no npm install. Move to @playwright/test
// if the suite ever wants sharding or a HTML report.

import { createRequire } from "node:module";
import { execSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import os from "node:os";
import { fileURLToPath } from "node:url";
import path from "node:path";

const require = createRequire(import.meta.url);

// Scratch space for files the suite makes the app write. Named, not deleted: when a
// 3MF assertion fails the file itself is the evidence.
export const scratch = mkdtempSync(path.join(os.tmpdir(), "stl-cutter-e2e-"));
let plateSeq = 1;

// unzip via the system tool rather than a hand-rolled central-directory parser. Node
// ships no zip reader, and forty lines of one is forty lines that can be wrong.
function unzipNames(file) {
  return execSync(`unzip -Z1 ${JSON.stringify(file)}`, { encoding: "utf8" }).trim().split("\n");
}

function unzipEntry(file, name) {
  return execSync(`unzip -p ${JSON.stringify(file)} ${JSON.stringify(name)}`, { encoding: "utf8" });
}

export const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
export const APP_URL = process.env.STL_CUTTER_URL || "http://localhost:34115";

// playwright is expected to be installed globally (npm i -g @playwright/cli),
// which is not on Node's resolution path for a project without a package.json.
function loadPlaywright() {
  const candidates = ["playwright"];
  try {
    const globalRoot = execSync("npm root -g", { encoding: "utf8" }).trim();
    candidates.push(
      path.join(globalRoot, "playwright"),
      path.join(globalRoot, "@playwright/cli/node_modules/playwright")
    );
  } catch {
    // npm missing is not fatal on its own; the bare specifier may still resolve.
  }
  for (const c of candidates) {
    try {
      return require(c);
    } catch {
      // try the next candidate
    }
  }
  throw new Error(
    "cannot find the playwright module. Install it with:\n" +
      "    npm install -g @playwright/cli\n" +
      `tried: ${candidates.join(", ")}`
  );
}

// Fixtures are generated rather than committed: cmd/genfixture is the same
// source the Go tests use, so the suite cannot drift from them.
export function fixture(name) {
  const file = path.join(repoRoot, "testdata", `${name}.stl`);
  if (!existsSync(file)) {
    execSync(`go run ./cmd/genfixture -name ${name} -out testdata/${name}.stl`, {
      cwd: repoRoot,
      stdio: "inherit",
    });
  }
  return file;
}

const tests = [];
let currentGroup = "";

export function group(name) {
  currentGroup = name;
}

export function test(name, fn) {
  tests.push({ group: currentGroup, name, fn });
}

class Failure extends Error {}

function fail(message) {
  throw new Failure(message);
}

export const expect = {
  equal(actual, wanted, what) {
    if (actual !== wanted) fail(`${what}: got ${JSON.stringify(actual)}, want ${JSON.stringify(wanted)}`);
  },
  near(actual, wanted, tol, what) {
    if (!(Math.abs(actual - wanted) <= tol)) {
      fail(`${what}: got ${actual}, want ${wanted} +/- ${tol}`);
    }
  },
  ok(cond, what) {
    if (!cond) fail(`expected ${what}`);
  },
  contains(haystack, needle, what) {
    if (!String(haystack).includes(needle)) {
      fail(`${what}: ${JSON.stringify(String(haystack))} does not contain ${JSON.stringify(needle)}`);
    }
  },
  absent(haystack, needle, what) {
    if (String(haystack).includes(needle)) {
      fail(`${what}: ${JSON.stringify(String(haystack))} should not contain ${JSON.stringify(needle)}`);
    }
  },
};

// Wails' own runtime throws once per page load reaching for something only the
// native webview provides, and a browser tab has no favicon. Neither is ours;
// everything else in the console is treated as a failure.
const EXPECTED_CONSOLE = [/ipc\.js/, /favicon\.ico/, /Cannot read properties of null \(reading 'nodes'\)/];

export async function run() {
  const { chromium } = loadPlaywright();

  const res = await fetch(APP_URL).catch(() => null);
  if (!res || !res.ok) {
    console.error(
      `${APP_URL} is not serving. Start the dev server first:\n` +
        `    wails dev -tags webkit2_41 &\n` +
        `    timeout 120 bash -c 'until curl -sf ${APP_URL} >/dev/null; do sleep 2; done'`
    );
    process.exit(1);
  }

  const filter = process.argv[2];
  const selected = filter
    ? tests.filter((t) => `${t.group} ${t.name}`.toLowerCase().includes(filter.toLowerCase()))
    : tests;

  // The globally installed playwright's bundled-browser revision does not always
  // match what is actually downloaded under ~/.cache/ms-playwright, so prefer
  // the system Chrome — which is what `playwright-cli` uses here too — and fall
  // back to the bundled build where there is no system one (CI).
  let browser;
  try {
    browser = await chromium.launch({ channel: "chrome" });
  } catch (err) {
    try {
      browser = await chromium.launch();
    } catch {
      throw err;
    }
  }
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });

  let consoleErrors = [];
  page.on("console", (m) => {
    if (m.type() !== "error") return;
    // A failed resource load says only "404 (Not Found)" in its text; which
    // resource is in the location. Both have to be matched, or the favicon
    // filter would swallow a genuinely missing module too.
    const text = `${m.text()} ${m.location()?.url || ""}`;
    if (EXPECTED_CONSOLE.some((re) => re.test(text))) return;
    consoleErrors.push(text);
  });
  page.on("pageerror", (e) => {
    if (EXPECTED_CONSOLE.some((re) => re.test(String(e)))) return;
    consoleErrors.push(String(e));
  });

  const app = makeApp(page);
  let passed = 0;
  const failures = [];
  let lastGroup = null;

  for (const t of selected) {
    if (t.group !== lastGroup) {
      console.log(`\n${t.group}`);
      lastGroup = t.group;
    }
    consoleErrors = [];
    try {
      await app.reload();
      await t.fn(app);
      if (consoleErrors.length) {
        fail(`unexpected console errors:\n      ${consoleErrors.join("\n      ")}`);
      }
      passed++;
      console.log(`  ok    ${t.name}`);
    } catch (err) {
      failures.push({ name: `${t.group} / ${t.name}`, err });
      console.log(`  FAIL  ${t.name}`);
      console.log(`        ${String(err.message).split("\n").join("\n        ")}`);
    }
  }

  await browser.close();

  console.log(`\n${passed}/${selected.length} passed`);
  if (failures.length) {
    console.log(`\n${failures.length} failure(s):`);
    for (const f of failures) console.log(`  ${f.name}`);
    process.exit(1);
  }
}

// makeApp wraps the page in the vocabulary of this application, so a test reads
// as what the user does rather than as a pile of selectors.
function makeApp(page) {
  const api = {
    page,

    async reload() {
      await page.goto(APP_URL, { waitUntil: "domcontentloaded" });
      // window.app is assigned at the end of ui.js, so its presence means the
      // whole module graph resolved. A missing import would hang here instead
      // of failing thirty lines later with something unrelated.
      await page.waitForFunction(() => window.app && window.app.openPath, null, { timeout: 15000 });
    },

    // open loads a fixture through the same sequence the Open button uses.
    // repair defaults to whatever the checkbox says, as a click would.
    async open(name, repair) {
      const file = fixture(name);
      await page.evaluate(
        ({ f, repair }) => (repair === undefined ? window.app.openPath(f) : window.app.openPath(f, repair)),
        { f: file, repair }
      );
      // Wait for the scale panel too, not just the tree. render() un-hides the tree
      // before it syncs the scale fields, so returning on the tree alone lets a test set
      // those fields and have syncScale overwrite them a moment later — which failed
      // about one full run in three while passing every time in isolation.
      await page.waitForFunction(
        () =>
          !document.getElementById("tree-panel").hidden &&
          !document.getElementById("scale-panel").hidden &&
          document.getElementById("scale-preview").textContent.length > 0
      );
    },

    async setRepairOnLoad(on) {
      await page.evaluate((v) => {
        document.getElementById("repair-on-load").checked = v;
      }, on);
    },

    async setPinStyle(value) {
      await page.evaluate((v) => {
        document.getElementById("pins-pegside").value = v;
      }, value);
    },

    text: (sel) => page.$eval(sel, (el) => el.textContent),
    hidden: (sel) => page.$eval(sel, (el) => el.hidden),
    disabled: (sel) => page.$eval(sel, (el) => el.disabled),
    value: (sel) => page.$eval(sel, (el) => el.value),
    status: () => api.text("#status"),
    messages: () => api.text("#messages"),

    // placePlane writes the gizmo's transform directly. The corner handles and
    // TransformControls need a real pointer and cannot be aimed to a coordinate;
    // the pointer group tests those separately.
    async placePlane({ rotation = [0, 0, 0], position, width, height }) {
      await page.evaluate(
        ({ rotation, position, width, height }) => {
          const g = window.app.gizmoGroup();
          g.rotation.set(...rotation);
          g.position.set(...position);
          g.updateMatrixWorld();
          if (width != null) window.app.setExtent(width, height);
        },
        { rotation, position, width, height }
      );
    },

    // A plane whose normal is +Y, which is what cuts across the U's arms.
    async planeAcrossTheArms({ position, width, height }) {
      await api.placePlane({ rotation: [-Math.PI / 2, 0, 0], position, width, height });
    },

    // setPlane types into the position, rotation and extent fields, which is how a plane
    // is placed exactly rather than by dragging.
    async setPlane(v) {
      await page.evaluate((v) => {
        const ids = { px: "plane-px", py: "plane-py", pz: "plane-pz", rx: "plane-rx", ry: "plane-ry", rz: "plane-rz" };
        for (const [k, id] of Object.entries(ids)) {
          if (v[k] === undefined) continue;
          const el = document.getElementById(id);
          el.value = String(v[k]);
          el.dispatchEvent(new Event("input"));
        }
      }, v);
    },

    planeFields: () =>
      page.evaluate(() =>
        ["plane-px", "plane-py", "plane-pz", "plane-rx", "plane-ry", "plane-rz"].map((id) =>
          Number(document.getElementById(id).value)
        )
      ),

    async setPins(spec) {
      await page.evaluate((s) => {
        document.getElementById("pins-enabled").checked = s.enabled !== false;
        for (const [k, v] of Object.entries(s)) {
          if (k === "enabled") continue;
          const el = document.getElementById(`pins-${k}`);
          if (!el) throw new Error(`no pin field pins-${k}`);
          el.value = String(v);
        }
      }, spec);
    },

    // exportPlates writes to a scratch path, since the save dialog belongs to the
    // native window and a browser tab cannot answer it.
    async exportPlates() {
      const out = path.join(scratch, `plates-${plateSeq++}.3mf`);
      return page.evaluate((p) => window.app.exportPlates(p), out);
    },

    // inspect3mf reads the archive back with Node, independent of anything the page
    // did, so a file that only looks right in the outcome object is still caught.
    async inspect3mf(file) {
      if (!existsSync(file)) throw new Error(`the export wrote no file at ${file}`);
      const settings = unzipEntry(file, "Metadata/model_settings.config");
      return {
        entries: unzipNames(file),
        bytes: readFileSync(file).length,
        plates: (settings.match(/key="plater_id"/g) || []).length,
        instances: (settings.match(/key="object_id"/g) || []).length,
      };
    },

    // selectPrinter picks a model by the name in its label and returns the bed size
    // the fields ended up with, so a test asserts on the effect rather than the value
    // attribute it just set.
    // Selects by option element and returns both the bed size and which printer the
    // select now reports. Setting sel.value used to be enough, but nine of the
    // fourteen machines share a bed size, so a size-valued select landed on the first
    // match — asking for H2C selected A2L, and the assertion passed because their beds
    // are identical. The chosen model is returned so a test can check it got the one
    // it asked for.
    async selectPrinter(name) {
      return page.evaluate((wanted) => {
        const sel = document.getElementById("printer");
        // Exact match on the model name, not a prefix: "A1" is a prefix of "A1 mini",
        // so a prefix match would quietly select the wrong machine.
        const opt = [...sel.options].find((o) => o.textContent.split(" — ")[0].trim() === wanted);
        if (!opt) {
          throw new Error(`no printer option named ${wanted}; have ${[...sel.options].map((o) => o.textContent)}`);
        }
        opt.selected = true;
        sel.dispatchEvent(new Event("change"));
        return {
          bed: ["bed-x", "bed-y", "bed-z"].map((id) => Number(document.getElementById(id).value)),
          chosen: sel.options[sel.selectedIndex].textContent,
          value: sel.value,
        };
      }, name);
    },

    // scale drives the Scale panel. percent or mm sets the X field and lets the
    // keep-proportions lock fill the rest; x/y/z with uniform false sets each.
    async scale(opts) {
      await page.evaluate((o) => {
        const mode = document.getElementById("scale-mode");
        // mm may be a single value (X, proportions kept) or simply a flag saying the
        // x/y/z below are millimetres rather than percentages.
        mode.value = o.mm !== undefined ? "mm" : "percent";
        mode.dispatchEvent(new Event("change"));

        const uniform = document.getElementById("scale-uniform");
        uniform.checked = o.uniform !== false;
        uniform.dispatchEvent(new Event("change"));

        const set = (id, v) => {
          const el = document.getElementById(id);
          el.value = String(v);
          el.dispatchEvent(new Event("input"));
        };
        if (o.percent !== undefined) set("scale-x", o.percent);
        else if (typeof o.mm === "number") set("scale-x", o.mm);
        else {
          set("scale-x", o.x);
          set("scale-y", o.y);
          set("scale-z", o.z);
        }
      }, opts);
      return api.act("do-scale");
    },

    scaleFields: () =>
      page.evaluate(() => ["scale-x", "scale-y", "scale-z"].map((id) => document.getElementById(id).value)),

    scalePreview: () => api.text("#scale-preview"),

    async setBed({ x, y, z }) {
      await page.evaluate(
        (b) => {
          document.getElementById("bed-x").value = String(b.x);
          document.getElementById("bed-y").value = String(b.y);
          document.getElementById("bed-z").value = String(b.z);
        },
        { x, y, z }
      );
    },

    // act clicks a button and waits for the command behind it to finish.
    // Every handler clears #messages first and every path — success, warning
    // and error alike — writes at least one message, so a non-empty #messages
    // means the round trip to Go is complete.
    async act(id) {
      // Clear first. Every handler clears #messages itself, but it does so after the
      // click has been awaited — so a second command in the same test could see the
      // first one's message still in the DOM and return before its own work had even
      // started. That made two different scale tests fail on alternate runs.
      await page.evaluate(() => document.getElementById("messages").replaceChildren());
      await page.click(`#${id}`);
      await page.waitForFunction(() => document.getElementById("messages").textContent.length > 0, null, {
        timeout: 60000,
      });
      return api.messages();
    },

    // cut adds a plane and then runs the plan, because that is what cutting is now:
    // both paths propose into an editable list and only Cut now acts. Kept as one
    // helper so the tests written before the plan existed still assert what they were
    // written to assert, rather than being rewritten around the new flow.
    async cut() {
      await api.act("do-cut");
      return api.act("do-execute");
    },

    // addPlane and execute for tests that care about the two halves separately.
    addPlane: () => api.act("do-cut"),
    execute: () => api.act("do-execute"),

    // autosplit plans and then runs, for the same reason.
    async autosplit() {
      const planned = await api.act("do-autosplit");
      // Nothing to run: either the model already fits, or planning was refused. Cut
      // now is disabled in both cases, so clicking it would hang the test rather than
      // fail it.
      if (await api.disabled("#do-execute")) return planned;
      return planned + (await api.act("do-execute"));
    },

    // planOnly stops after proposing, for asserting that nothing was cut.
    planFitToPrinter: () => api.act("do-autosplit"),

    plan: () => page.evaluate(() => window.app.plan()),

    planNames: () => page.evaluate(() => window.app.plan().cuts.map((c) => c.name)),

    // Undo reports nothing on success, so it waits on the tree instead.
    async undo() {
      const before = await api.status();
      await page.click("#do-undo");
      await page.waitForFunction((b) => document.getElementById("status").textContent !== b, before, {
        timeout: 30000,
      });
    },

    // parts returns one entry per leaf in the parts list, with the measurements
    // the sidebar shows for it. Selecting each leaf is the only way to read
    // them, which also exercises selection.
    async parts() {
      return page.evaluate(async () => {
        const seen = new Map();
        for (const li of document.querySelectorAll("#tree li")) {
          // renderTree marks split parts with .split and gives only leaves a
          // click listener, so a row without that class is a selectable leaf.
          const row = li.firstElementChild;
          if (!row || row.classList.contains("split")) continue;
          row.click();
          await new Promise((r) => setTimeout(r, 120));
          const info = document.getElementById("part-info");
          const val = (label) => {
            for (const dt of info.querySelectorAll("dt")) {
              if (dt.textContent === label) return dt.nextElementSibling.textContent;
            }
            return null;
          };
          seen.set(row.textContent, {
            label: row.textContent,
            size: val("Size"),
            volume: parseFloat(val("Volume")),
            tris: parseInt(val("Triangles"), 10),
            closed: val("Closed") === "yes",
            flagged: !!row.querySelector(".flag"),
            selected: row.classList.contains("selected"),
          });
        }
        return [...seen.values()];
      });
    },

    // camera returns the position and target, which is what an orbit, a pan and
    // a zoom each change in their own distinct way.
    async camera() {
      return page.evaluate(() => {
        const c = window.app.camera();
        const t = window.app.controls().target;
        return {
          pos: [c.position.x, c.position.y, c.position.z],
          target: [t.x, t.y, t.z],
          distance: c.position.distanceTo(t),
        };
      });
    },

    // cameraStill waits for OrbitControls to stop easing.
    //
    // Damping is on, so the camera keeps drifting for frames after frameAll().
    // Projecting a handle to a screen coordinate mid-drift aims the mouse at
    // where the handle *was*: a corner handle is only about 5px across, so a
    // 25px drift misses it entirely and the drag silently does nothing. This is
    // what made the rotated-plane test fail while the same gesture by hand
    // worked — the hand version happened to allow more settling time.
    async cameraStill(timeout = 3000) {
      const started = Date.now();
      let last = null;
      while (Date.now() - started < timeout) {
        const now = await api.camera();
        if (last && Math.hypot(...now.pos.map((v, i) => v - last.pos[i])) < 1e-4) return;
        last = now;
        await api.settle(3);
      }
    },

    // handleScreenPos projects a corner handle to a page coordinate so the
    // pointer tests can put the mouse on it.
    async handleScreenPos(corner) {
      await api.cameraStill();
      return page.evaluate((corner) => {
        const THREE = window.app.three;
        const g = window.app.gizmoGroup();
        const handle = g.children.find((c) => c.userData.corner === corner);
        if (!handle) throw new Error(`no corner handle ${corner}`);
        g.updateMatrixWorld(true);
        const p = handle.getWorldPosition(new THREE.Vector3()).project(window.app.camera());
        const canvas = window.app.canvas().getBoundingClientRect();
        return {
          x: canvas.left + ((p.x + 1) / 2) * canvas.width,
          y: canvas.top + ((1 - p.y) / 2) * canvas.height,
        };
      }, corner);
    },

    // emptySpot is a viewport point clear of both the model and the transform
    // gizmo, which sit together near the centre. A left-drag there orbits; the
    // same drag at the centre latches TransformControls and translates the
    // plane instead — correct, but not what an orbit test means to exercise.
    emptySpot: () =>
      page.$eval("#viewport", (el) => {
        const r = el.getBoundingClientRect();
        return { x: r.left + r.width * 0.1, y: r.top + r.height * 0.12 };
      }),

    // gizmoCentre is where TransformControls' own handles are, projected to the page.
    async gizmoCentre() {
      await api.cameraStill();
      return page.evaluate(() => {
        const THREE = window.app.three;
        const p = window.app.gizmoGroup().getWorldPosition(new THREE.Vector3()).project(window.app.camera());
        const c = window.app.canvas().getBoundingClientRect();
        return { x: c.left + ((p.x + 1) / 2) * c.width, y: c.top + ((1 - p.y) / 2) * c.height };
      });
    },

    planeOrigin: () => page.evaluate(() => window.app.planeInput().origin),

    async extent() {
      return page.evaluate(() => {
        const p = window.app.planeInput();
        return { width: p.width, height: p.height };
      });
    },

    async dragMouse(from, to, { button = "left", steps = 12 } = {}) {
      await page.mouse.move(from.x, from.y);
      await page.mouse.down({ button });
      for (let i = 1; i <= steps; i++) {
        await page.mouse.move(
          from.x + ((to.x - from.x) * i) / steps,
          from.y + ((to.y - from.y) * i) / steps
        );
      }
      await page.mouse.up({ button });
    },

    // The render loop needs a frame or two for damped controls to settle.
    settle: (frames = 4) =>
      page.evaluate(
        (n) =>
          new Promise((resolve) => {
            let left = n;
            const tick = () => (left-- > 0 ? requestAnimationFrame(tick) : resolve());
            tick();
          }),
        frames
      ),
  };
  return api;
}

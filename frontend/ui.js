import * as THREE from "three";
import { initViewer, showParts, frameAll, cameraRef, controlsRef, domElement } from "./viewer.js";
import {
  OpenModel, OpenPath, Select, Undo, ExportAll, SeparateBodies,
  ExportPlates, ExportPlatesTo,
  AddPlane, UpdatePlane, RenamePlane, DeletePlane, SetPlaneEnabled, ClearPlan,
  PlanFitToPrinter, ExecutePlan, Plan, ReorderPlan, ScaleModel,
} from "./wailsjs/go/main/App.js";
import { EventsOn } from "./wailsjs/runtime/runtime.js";
import { initGizmo, showGizmo, hideGizmo, setMode, setExtent, extent, onChange, planeInput, gizmoGroup, mode, placement, setPlacement } from "./gizmo.js";
import { renderTree, renderInfo, leavesOf } from "./tree.js";
import { renderPlan } from "./plan.js";
import { initScale, syncScale } from "./scale.js";
import { initPins, pinSpec, bedSpec, showPanels, reportPins, repairOnLoad } from "./pins.js";

const statusEl = document.getElementById("status");

initViewer(document.getElementById("viewport"));
initGizmo();
initPins();
initScale(applyScale);

const planeControls = document.getElementById("plane-controls");
const widthEl = document.getElementById("plane-width");
const heightEl = document.getElementById("plane-height");
// Position and rotation, so a plane can be placed by typing rather than only by dragging.
const placeEls = ["plane-px", "plane-py", "plane-pz", "plane-rx", "plane-ry", "plane-rz"].map((id) =>
  document.getElementById(id)
);
const treePanel = document.getElementById("tree-panel");
const treeEl = document.getElementById("tree");
const infoEl = document.getElementById("part-info");
const actionsEl = document.getElementById("actions");
const messagesEl = document.getElementById("messages");
const progressEl = document.getElementById("progress");
const progressBar = document.getElementById("progress-bar");
const cutBtn = document.getElementById("do-cut");
const planPanel = document.getElementById("plan-panel");
const planEl = document.getElementById("plan");
const savePlaneBtn = document.getElementById("save-plane");
const executeBtn = document.getElementById("do-execute");
const undoBtn = document.getElementById("do-undo");
const exportBtn = document.getElementById("do-export");

let currentTree = null;
let currentPlan = { cuts: [] };
// Which planned cut is selected. Only its plane is drawn: fifty rectangles at once
// would hide the model.
let selectedPlan = null;

document.getElementById("mode-translate").addEventListener("click", () => setMode("translate"));
document.getElementById("mode-rotate").addEventListener("click", () => setMode("rotate"));

widthEl.addEventListener("input", () => setExtent(parseFloat(widthEl.value), extent().height));
heightEl.addEventListener("input", () => setExtent(extent().width, parseFloat(heightEl.value)));

for (const el of placeEls) {
  el.addEventListener("input", () => {
    const n = placeEls.map((e) => parseFloat(e.value));
    setPlacement(n.slice(0, 3), n.slice(3));
  });
}

// Keep the number fields in step with the gizmo, whichever moved.
onChange((input) => {
  // Writing .value into a focused number field moves the caret to the end, so a
  // second keystroke can never land. Leave whichever field the user is typing in
  // alone; it is already showing what they typed.
  if (document.activeElement !== widthEl) widthEl.value = input.width.toFixed(2);
  if (document.activeElement !== heightEl) heightEl.value = input.height.toFixed(2);

  // Position and rotation follow the gizmo however it moved — dragged, typed, or loaded
  // from a planned cut — so the numbers always describe the plane on screen.
  const p = placement();
  const shown = [...p.position, ...p.rotation];
  for (const [i, el] of placeEls.entries()) {
    if (document.activeElement !== el) el.value = shown[i].toFixed(2);
  }
});

function message(text, kind = "warn") {
  const div = document.createElement("div");
  div.className = kind;
  div.textContent = text;
  messagesEl.appendChild(div);
}

function clearMessages() {
  messagesEl.replaceChildren();
}

// busy(true) disables every action while a command is in flight. busy(false)
// does not simply flip everything back on: Undo must only be enabled when
// currentTree.canUndo actually says so, or a busy(false) that runs right after
// a fresh Undo (which leaves nothing left to undo) would re-enable itself.
function busy(on) {
  cutBtn.disabled = on;
  exportBtn.disabled = on;
  undoBtn.disabled = on || !currentTree || !currentTree.canUndo;
}

// A cut on a large model runs for seconds. The library reports progress; show it
// rather than leaving the window looking frozen.
//
// Visibility belongs to the command that started the work, not to the cut:start
// and cut:done events. Go emits those two in order, but on an error path they
// leave microseconds apart and the bridge does not guarantee they are *delivered*
// in that order: when they arrived reversed, cut:start ran last and left the bar
// on screen for good, with nothing able to clear it. That failed about one run in
// four of the e2e suite. Bracketing the awaited call cannot get the order wrong,
// because there is only one order.
//
// cut:progress still drives the width — a message that arrives late or out of
// order only moves the bar, which the next one corrects.
// A cut is the one operation that knows how far along it is, so the moment it says so
// the spinner is joined by a real bar. Nothing goes back the other way within one
// command.
EventsOn("cut:progress", (f) => {
  if (busyEl.hidden) return; // a stray event outside a command must not draw anything
  progressEl.hidden = false;
  progressBar.style.width = `${Math.round(f * 100)}%`;
});

const busyEl = document.getElementById("busy");
const busyLabel = document.getElementById("busy-label");

// withBusy shows the indicator for the whole of one command and clears it however that
// command ends.
//
// Every slow path goes through here rather than each handler managing its own flag. That
// is the point, not tidiness: a stuck indicator is this feature's failure mode, and it
// has already happened once — the progress bar used to be driven by cut:start and
// cut:done, whose delivery order is not guaranteed, and when they arrived reversed the
// bar stayed on screen with nothing able to clear it. One wrapper is one place to get
// wrong.
//
// The label is set synchronously, before the first await, so a caller that does not await
// can still be observed to have started.
async function withBusy(label, fn) {
  busyLabel.textContent = label;
  busyEl.hidden = false;
  progressEl.hidden = true; // indeterminate until something says otherwise
  progressBar.style.width = "0%";
  busy(true);
  try {
    return await fn();
  } finally {
    busyEl.hidden = true;
    progressEl.hidden = true;
    busy(false);
  }
}

async function render(tree) {
  currentTree = tree;
  if (!tree) {
    statusEl.textContent = "No model loaded.";
    treePanel.hidden = true;
    planeControls.hidden = true;
    actionsEl.hidden = true;
    planPanel.hidden = true;
    syncScale(null);
    return;
  }
  const parts = leavesOf(tree.root);
  await showParts(parts, tree.selectedId);

  renderTree(treeEl, tree, async (id) => {
    try {
      await render(await Select(id));
    } catch (err) {
      statusEl.textContent = String(err);
    }
  });
  renderInfo(infoEl, tree);

  treePanel.hidden = false;
  planeControls.hidden = false;
  showPanels();
  actionsEl.hidden = false;
  undoBtn.disabled = !tree.canUndo;
  syncScale(tree);
  statusEl.textContent = `${tree.modelName} — ${parts.length} part(s)`;
}

// reportRepair says what load-time repair did, and — just as importantly — what
// it did not. Repair fills holes and drops degenerate triangles; it does not
// correct winding, so a mesh can come back genuinely improved and still not be a
// closed solid. Announcing the repair without that would read as a clean bill of
// health the model has not earned.
function reportRepair(tree) {
  const r = tree && tree.repair;
  if (!r) return;

  const did = [];
  if (r.holesFilled) {
    did.push(`filled ${r.holesFilled} hole(s) with ${r.trianglesAdded} triangle(s)`);
  }
  if (r.degenerateRemoved) {
    did.push(`removed ${r.degenerateRemoved} zero-area triangle(s)`);
  }
  if (r.shellsDropped) {
    // Debris is what real cut output actually suffers from, so it is worth naming
    // as debris rather than as an anonymous count of deleted triangles.
    did.push(
      `dropped ${r.shellsDropped} stray shell(s) enclosing no volume ` +
        `(${r.trianglesRemoved} triangle(s) of debris)`
    );
  }
  if (did.length) {
    message(`Repaired the mesh: ${did.join(", ")}.`, r.closed ? "ok" : "warn");
  } else {
    message(`Nothing to repair — ${r.before}. See below.`, "warn");
  }

  if (r.bodies > 1) {
    message(
      `This model contains ${r.bodies} separate bodies — use Separate bodies to split them.`,
      "ok"
    );
  }

  if (r.closed) return;

  // Naming the kind of defect is the difference between a user trying again and
  // a user knowing not to. Real exported models are almost always the
  // non-manifold case, which filling cannot touch.
  if (r.nonManifoldEdges) {
    // Where the touching surfaces are separate bodies, Separate bodies is the fix:
    // it changes no geometry and leaves each body a closed solid. Saying "repair
    // cannot help" and stopping would send the user to another tool for something
    // this application does in one click.
    const cure =
      r.bodies > 1
        ? `Separate bodies will fix it: the touching surfaces are ${r.bodies} separate ` +
          `solids, and split apart each one is a closed solid.`
        : `Filling cannot fix that, so repairing again will not help. Most slicers still ` +
          `print such a model; a mesh tool can separate the surfaces if yours refuses.`;
    message(
      `${r.nonManifoldEdges} edge(s) have more than two triangles meeting along them — ` +
        `two surfaces touching, not a hole. ${cure}`,
      "warn"
    );
  } else {
    message(
      `It is still not a closed solid — ${r.after}. Repair closes holes and drops ` +
        `degenerate triangles, but it does not turn backwards-facing triangles around.`,
      "warn"
    );
  }
}

// load is the whole open sequence given something that produces a tree, so the
// button and the headless handle below cannot drift apart.
async function load(loader, label) {
  const previousId = currentTree && currentTree.root ? currentTree.root.id : null;
  const tree = await withBusy(label || "Loading the model", loader);
  if (!tree) return; // nothing open, nothing to show
  // A previous cut's warnings do not describe the model now being opened.
  clearMessages();
  await render(tree);
  selectedPlan = null;
  showPlan(await Plan());
  reportRepair(tree);
  // OpenModel returns the current view unchanged when the dialog is cancelled,
  // so re-framing here would throw away a plane the user had just placed.
  if (tree.root && tree.root.id !== previousId) {
    frameAll();
    showGizmo();
  }
}

document.getElementById("open").addEventListener("click", async () => {
  try {
    await load(() => OpenModel(repairOnLoad()), "Opening a model");
  } catch (err) {
    statusEl.textContent = String(err);
  }
});

// A handle for driving the window without a mouse — see CLAUDE.md. The native
// file dialog cannot be answered from a browser tab, which otherwise leaves an
// automated run stuck on an empty sidebar, unable to reach the cut at all.
//
// These are the same functions the click handlers call, deliberately: a headless
// run has to exercise the real path, or it only proves that a parallel imitation
// of the app works.
// three is handed over whole rather than growing a helper per assertion. The
// pointer tests have to project a corner handle's world position to a screen
// coordinate before they can drag it, and every other maths helper they might
// want is already in here.
window.app = {
  // repair defaults to whatever the checkbox says, so a driven load behaves as a
  // clicked one; pass it explicitly to test the other setting.
  openPath: (path, repair = repairOnLoad()) =>
    load(() => OpenPath(path, repair), `Loading ${path.split("/").pop()}`),
  gizmoGroup,
  setExtent,
  planeInput,
  three: THREE,
  exportPlates,
  plan: () => currentPlan,
  selectPlanned,
  reorderPlan: async (ids) => showPlan(await ReorderPlan(ids)),
  mode,
  camera: cameraRef,
  controls: controlsRef,
  canvas: domElement,
};

// applyScale rescales the model. Scaling resets the tree and clears the plan, because
// both describe the model at its previous size, so both are redrawn afterwards.
async function applyScale(factors) {
  clearMessages();
  try {
    const had = currentPlan.cuts.length;
    const tree = await withBusy("Scaling the model", () => ScaleModel(factors));
    selectedPlan = null;
    await render(tree);
    showPlan(await Plan());
    // Re-frame, exactly as loading does. The model has changed size, so a camera and a
    // cutting plane left over from the old one describe nothing: scaling a 30mm model to
    // 600mm left the camera at 87 units aimed at (15,20,5), which is inside the new model
    // looking at a corner — the geometry had scaled and the window looked like it had not.
    frameAll();
    showGizmo();
    const size = tree.root.size.map((d) => d.toFixed(1)).join(" × ");
    message(`Scaled to ${size} mm.`, "ok");
    if (had > 0) {
      message(
        `The cut plan was cleared: its ${had} plane(s) named coordinates that no longer ` +
          `describe the model.`,
        "warn"
      );
    }
  } catch (err) {
    message(String(err), "err");
  }
}

// showPlan draws the list and keeps the panel and the Save plane button in step with
// whether an entry is selected.
function showPlan(plan) {
  // Defensive about cuts as well as plan: Go marshals a nil slice as null, and reading
  // .some() off null killed the module on load once already.
  currentPlan = { cuts: (plan && plan.cuts) || [] };
  if (selectedPlan && !currentPlan.cuts.some((c) => c.id === selectedPlan)) {
    selectedPlan = null; // it was deleted
  }
  planPanel.hidden = !currentTree;
  savePlaneBtn.hidden = selectedPlan === null;
  executeBtn.disabled = !currentPlan.cuts.some((c) => c.enabled);

  renderPlan(planEl, currentPlan, selectedPlan, {
    rename: (id, name) => planCommand(() => RenamePlane(id, name)),
    remove: (id) => planCommand(() => DeletePlane(id)),
    toggle: (id, on) => planCommand(() => SetPlaneEnabled(id, on)),
    select: (id) => selectPlanned(id),
    reorder: (ids) => planCommand(() => ReorderPlan(ids)),
  });
}

// planCommand runs one plan edit and redraws. Plan edits are cheap and touch no
// geometry, so they do not take the progress bar or disable the buttons.
async function planCommand(fn) {
  try {
    showPlan(await fn());
  } catch (err) {
    message(String(err), "err");
  }
}

// selectPlanned loads an entry's plane into the gizmo so it can be seen and moved.
// Only the selected entry is drawn; a fifty-entry plan drawn at once hides the model.
function selectPlanned(id) {
  const cut = currentPlan.cuts.find((c) => c.id === id);
  if (!cut) return;
  selectedPlan = id;
  loadPlaneIntoGizmo(cut.plane);
  showPlan(currentPlan);
}

// loadPlaneIntoGizmo is the reverse of planeInput(): it puts a stored plane back under
// the gizmo, so a planned cut can be re-positioned rather than deleted and redone.
function loadPlaneIntoGizmo(plane) {
  const g = gizmoGroup();
  const u = new THREE.Vector3(...plane.u);
  const v = new THREE.Vector3(...plane.v);
  const n = new THREE.Vector3(...plane.normal);
  if (u.lengthSq() > 0 && v.lengthSq() > 0 && n.lengthSq() > 0) {
    // The basis is orthonormal by construction, so it is a rotation matrix directly.
    const m = new THREE.Matrix4().makeBasis(u.normalize(), v.normalize(), n.normalize());
    g.quaternion.setFromRotationMatrix(m);
  }
  g.position.set(...plane.origin);
  g.updateMatrixWorld();
  setExtent(plane.width, plane.height);
  // false: reveal it where it is. Letting showGizmo re-frame and then restoring the
  // placement afterwards is what showed every planned cut lying flat — the rotation was
  // the one of the three things not put back.
  showGizmo(false);
}

cutBtn.addEventListener("click", async () => {
  if (!currentTree) return;
  clearMessages();
  try {
    const plan = await AddPlane(planeInput(), pinSpec());
    showPlan(plan);
    const added = plan.cuts[plan.cuts.length - 1];
    message(`Added ${added.name}. Nothing is cut until you press Cut now.`, "ok");
  } catch (err) {
    message(String(err), "err");
  }
});

savePlaneBtn.addEventListener("click", async () => {
  if (!selectedPlan) return;
  clearMessages();
  try {
    const plan = await UpdatePlane(selectedPlan, planeInput(), pinSpec());
    const saved = plan.cuts.find((c) => c.id === selectedPlan);
    showPlan(plan);
    message(`Moved ${saved ? saved.name : "the cut"}. Press Cut now to apply the plan.`, "ok");
  } catch (err) {
    message(String(err), "err");
  }
});

document.getElementById("clear-plan").addEventListener("click", () => {
  clearMessages();
  selectedPlan = null;
  planCommand(ClearPlan);
});

executeBtn.addEventListener("click", async () => {
  if (!currentTree) return;
  clearMessages();
  try {
    const out = await withBusy("Cutting", ExecutePlan);
    await render(out.tree);
    showPlan(out.plan);

    message(`Made ${out.cutsMade} cut(s) from the plan.`, "ok");

    // An entry that could not run has to be named. A plan that quietly does less than
    // it lists is the one outcome this application is not allowed to produce.
    for (const s of out.skipped || []) message(`Skipped ${s}`, "warn");
    for (const w of out.warnings || []) message(w, "warn");

    // Pins are per entry, so each is reported against the entry that asked for them,
    // with that entry's own spec — peg and dowel word themselves differently, and one
    // summed number would describe neither.
    for (const r of out.made || []) {
      if (r.pins && r.pins.enabled) {
        reportPins(
          { pinsPlaced: r.pinsPlaced, pinsRequested: r.pinsRequested, pinsSkipped: r.pinsSkipped },
          (text, kind) => message(`${r.name}: ${text}`, kind),
          r.pins
        );
      }
    }

    if (out.watertight) {
      message("All pieces are closed solids.", "ok");
    } else {
      message(
        "At least one piece is not a closed solid. Nudge the plane of the cut that " +
          "produced it and run the plan again, or check the flagged part before printing.",
        "warn"
      );
    }
  } catch (err) {
    message(String(err), "err");
  }
});

// exportPlates is shared by the button and the headless handle, so driving it
// exercises the real path. The save dialog belongs to the native window and cannot be
// answered from a browser tab, which is why the path-taking variant exists at all.
async function exportPlates(path) {
  clearMessages();
  try {
    const out = await withBusy("Working out orientations and writing the 3MF", () =>
      path ? ExportPlatesTo(path, bedSpec()) : ExportPlates(bedSpec())
    );
    if (out.cancelled) return out;

    message(`Wrote ${out.plates} plate(s) to ${out.path}.`, "ok");
    // Say what the orientation search did, per part. A silent "done" would leave the
    // user unable to tell a good orientation from none at all.
    for (const r of out.oriented || []) {
      const turned = r.rotated ? "rotated" : "left as it was";
      message(
        `Plate ${r.plate}: ${r.name} — ${turned}, ${r.height.toFixed(1)}mm tall, ` +
          `${r.overhangArea.toFixed(0)}mm² needing support on a ${r.baseArea.toFixed(0)}mm² base.`,
        "ok"
      );
    }
    for (const w of out.warnings || []) message(w, "warn");
    return out;
  } catch (err) {
    message(String(err), "err");
    throw err;
  }
}

document.getElementById("do-plates").addEventListener("click", () => {
  exportPlates(null).catch(() => {}); // already reported
});

document.getElementById("do-separate").addEventListener("click", async () => {
  if (!currentTree) return;
  clearMessages();
  try {
    const out = await withBusy("Separating bodies", () => SeparateBodies(currentTree.selectedId));
    await render(out.tree);
    // Separation is recorded as a plan entry, so the list has to be refreshed or it
    // would not show the step that just happened — and Cut now would look like it was
    // about to throw the separation away.
    showPlan(await Plan());

    if (out.bodies < 2) {
      message("This part is a single body — nothing to separate.", "ok");
    } else {
      message(`Separated into ${out.bodies} bodies.`, "ok");
    }
    // A body broken in its own right has to keep saying so, rather than being
    // hidden by the pieces around it having improved.
    for (const w of out.warnings || []) message(w, "warn");
  } catch (err) {
    message(String(err), "err");
  }
});

undoBtn.addEventListener("click", async () => {
  clearMessages();
  try {
    await render(await withBusy("Undoing", Undo));
  } catch (err) {
    message(String(err), "err");
  }
});

exportBtn.addEventListener("click", async () => {
  clearMessages();
  try {
    const out = await withBusy("Exporting the parts", ExportAll);
    // ExportAll always resolves to a real object, never null — it signals a
    // dismissed folder dialog with a flag instead, so this cannot throw.
    if (out.cancelled) return;
    message(`Wrote ${out.files.length} file(s) to ${out.dir}`, "ok");
    for (const f of out.notWatertight || []) {
      message(`${f} is not a closed solid and may not print correctly.`, "warn");
    }
  } catch (err) {
    message(String(err), "err");
  }
});

document.getElementById("do-autosplit").addEventListener("click", async () => {
  if (!currentTree) return;
  clearMessages();
  try {
    const before = currentPlan.cuts.length;
    const plan = await withBusy("Planning cuts to fit the printer", () => PlanFitToPrinter(bedSpec()));
    showPlan(plan);
    const added = plan.cuts.length - before;
    if (added === 0) {
      message("The model already fits the bed. Nothing to plan.", "ok");
    } else {
      message(`Planned ${added} cut(s). Nothing is cut until you press Cut now.`, "ok");
    }
  } catch (err) {
    message(String(err), "err");
  }
});

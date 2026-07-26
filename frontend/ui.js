import * as THREE from "three";
import { initViewer, showParts, frameAll, cameraRef, controlsRef, domElement } from "./viewer.js";
import { OpenModel, OpenPath, Select, Cut, Undo, ExportAll, AutoSplit } from "./wailsjs/go/main/App.js";
import { EventsOn } from "./wailsjs/runtime/runtime.js";
import { initGizmo, showGizmo, hideGizmo, setMode, setExtent, extent, onChange, planeInput, gizmoGroup, mode } from "./gizmo.js";
import { renderTree, renderInfo, leavesOf } from "./tree.js";
import { initPins, pinSpec, bedSpec, showPanels, reportPins, repairOnLoad } from "./pins.js";

const statusEl = document.getElementById("status");

initViewer(document.getElementById("viewport"));
initGizmo();
initPins();

const planeControls = document.getElementById("plane-controls");
const widthEl = document.getElementById("plane-width");
const heightEl = document.getElementById("plane-height");
const treePanel = document.getElementById("tree-panel");
const treeEl = document.getElementById("tree");
const infoEl = document.getElementById("part-info");
const actionsEl = document.getElementById("actions");
const messagesEl = document.getElementById("messages");
const progressEl = document.getElementById("progress");
const progressBar = document.getElementById("progress-bar");
const cutBtn = document.getElementById("do-cut");
const undoBtn = document.getElementById("do-undo");
const exportBtn = document.getElementById("do-export");

let currentTree = null;

document.getElementById("mode-translate").addEventListener("click", () => setMode("translate"));
document.getElementById("mode-rotate").addEventListener("click", () => setMode("rotate"));

widthEl.addEventListener("input", () => setExtent(parseFloat(widthEl.value), extent().height));
heightEl.addEventListener("input", () => setExtent(extent().width, parseFloat(heightEl.value)));

// Keep the number fields in step with the gizmo, whichever moved.
onChange((input) => {
  // Writing .value into a focused number field moves the caret to the end, so a
  // second keystroke can never land. Leave whichever field the user is typing in
  // alone; it is already showing what they typed.
  if (document.activeElement !== widthEl) widthEl.value = input.width.toFixed(2);
  if (document.activeElement !== heightEl) heightEl.value = input.height.toFixed(2);
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
EventsOn("cut:progress", (f) => {
  progressBar.style.width = `${Math.round(f * 100)}%`;
});

function progress(on) {
  progressEl.hidden = !on;
  if (on) progressBar.style.width = "0%";
}

async function render(tree) {
  currentTree = tree;
  if (!tree) {
    statusEl.textContent = "No model loaded.";
    treePanel.hidden = true;
    planeControls.hidden = true;
    actionsEl.hidden = true;
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

  if (r.closed) return;

  // Naming the kind of defect is the difference between a user trying again and
  // a user knowing not to. Real exported models are almost always the
  // non-manifold case, which filling cannot touch.
  if (r.nonManifoldEdges) {
    message(
      `${r.nonManifoldEdges} edge(s) have more than two triangles meeting along them — ` +
        `two surfaces touching, not a hole. Filling cannot fix that, so repairing again ` +
        `will not help. Most slicers still print such a model; a mesh tool can separate ` +
        `the surfaces if yours refuses.`,
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
async function load(loader) {
  const previousId = currentTree && currentTree.root ? currentTree.root.id : null;
  const tree = await loader();
  if (!tree) return; // nothing open, nothing to show
  // A previous cut's warnings do not describe the model now being opened.
  clearMessages();
  await render(tree);
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
    await load(() => OpenModel(repairOnLoad()));
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
  openPath: (path, repair = repairOnLoad()) => load(() => OpenPath(path, repair)),
  gizmoGroup,
  setExtent,
  planeInput,
  three: THREE,
  mode,
  camera: cameraRef,
  controls: controlsRef,
  canvas: domElement,
};

cutBtn.addEventListener("click", async () => {
  if (!currentTree) return;
  clearMessages();
  busy(true);
  const spec = pinSpec();
  progress(true);
  try {
    const outcome = await Cut(currentTree.selectedId, planeInput(), spec);
    await render(outcome.tree);

    // Warnings are shown whether or not the cut succeeded. A part that is not a
    // closed solid is still produced and still exported — the user has to be
    // told, not protected from the result.
    for (const w of outcome.warnings || []) message(w, "warn");
    if (!outcome.watertight) {
      message("At least one piece is not a closed solid. Nudge the plane slightly and cut again, or check the flagged part before printing.", "warn");
    } else if (!(outcome.warnings || []).length) {
      message("Cut complete. Both pieces are closed solids.", "ok");
    }
    reportPins(outcome, message, spec);
  } catch (err) {
    message(String(err), "err");
  } finally {
    progress(false);
    busy(false);
  }
});

undoBtn.addEventListener("click", async () => {
  clearMessages();
  busy(true);
  try {
    await render(await Undo());
  } catch (err) {
    message(String(err), "err");
  } finally {
    busy(false);
  }
});

exportBtn.addEventListener("click", async () => {
  clearMessages();
  busy(true);
  try {
    const out = await ExportAll();
    // ExportAll always resolves to a real object, never null — it signals a
    // dismissed folder dialog with a flag instead, so this cannot throw.
    if (out.cancelled) return;
    message(`Wrote ${out.files.length} file(s) to ${out.dir}`, "ok");
    for (const f of out.notWatertight || []) {
      message(`${f} is not a closed solid and may not print correctly.`, "warn");
    }
  } catch (err) {
    message(String(err), "err");
  } finally {
    busy(false);
  }
});

document.getElementById("do-autosplit").addEventListener("click", async () => {
  if (!currentTree) return;
  clearMessages();
  busy(true);
  progress(true);
  try {
    const out = await AutoSplit(bedSpec(), pinSpec());
    currentTree = out.tree;
    await render(currentTree);

    if (out.cutsMade === 0) {
      message("Every piece already fits the bed. Nothing to do.", "ok");
    } else {
      message(`Split into ${out.cutsMade + 1} pieces.`, "ok");
    }
    for (const w of out.warnings || []) message(w, "warn");
    for (const name of out.stillTooBig || []) {
      message(`${name} still does not fit the bed.`, "warn");
    }
  } catch (err) {
    message(String(err), "err");
  } finally {
    progress(false);
    busy(false);
  }
});

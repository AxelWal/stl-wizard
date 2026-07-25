import { initViewer, showParts, frameAll } from "./viewer.js";
import { OpenModel, Select, Cut, Undo, ExportAll } from "./wailsjs/go/main/App.js";
import { EventsOn } from "./wailsjs/runtime/runtime.js";
import { initGizmo, showGizmo, hideGizmo, setMode, setExtent, extent, onChange, planeInput } from "./gizmo.js";
import { renderTree, renderInfo, leavesOf } from "./tree.js";

const statusEl = document.getElementById("status");

initViewer(document.getElementById("viewport"));
initGizmo();

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
  widthEl.value = input.width.toFixed(2);
  heightEl.value = input.height.toFixed(2);
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
EventsOn("cut:start", () => {
  progressEl.hidden = false;
  progressBar.style.width = "0%";
});
EventsOn("cut:progress", (f) => {
  progressBar.style.width = `${Math.round(f * 100)}%`;
});
// Emitted from a defer in Go, so this always fires — including when Cut errors
// or panics — and the bar can never be left on screen.
EventsOn("cut:done", () => {
  progressEl.hidden = true;
});

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
  actionsEl.hidden = false;
  undoBtn.disabled = !tree.canUndo;
  statusEl.textContent = `${tree.modelName} — ${parts.length} part(s)`;
}

document.getElementById("open").addEventListener("click", async () => {
  try {
    const tree = await OpenModel();
    await render(tree);
    frameAll();
    showGizmo();
  } catch (err) {
    statusEl.textContent = String(err);
  }
});

cutBtn.addEventListener("click", async () => {
  if (!currentTree) return;
  clearMessages();
  busy(true);
  try {
    const outcome = await Cut(currentTree.selectedId, planeInput());
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
  } catch (err) {
    message(String(err), "err");
  } finally {
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

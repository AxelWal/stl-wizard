import { initViewer, showParts, frameAll } from "./viewer.js";
import { OpenModel, Select } from "./wailsjs/go/main/App.js";
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

document.getElementById("mode-translate").addEventListener("click", () => setMode("translate"));
document.getElementById("mode-rotate").addEventListener("click", () => setMode("rotate"));

widthEl.addEventListener("input", () => setExtent(parseFloat(widthEl.value), extent().height));
heightEl.addEventListener("input", () => setExtent(extent().width, parseFloat(heightEl.value)));

// Keep the number fields in step with the gizmo, whichever moved.
onChange((input) => {
  widthEl.value = input.width.toFixed(2);
  heightEl.value = input.height.toFixed(2);
});

async function render(tree) {
  if (!tree) {
    statusEl.textContent = "No model loaded.";
    treePanel.hidden = true;
    planeControls.hidden = true;
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

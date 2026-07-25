import { initViewer, showParts, frameAll } from "./viewer.js";
import { OpenModel } from "./wailsjs/go/main/App.js";
import { initGizmo, showGizmo, hideGizmo, setMode, setExtent, extent, onChange, planeInput } from "./gizmo.js";

const statusEl = document.getElementById("status");

initViewer(document.getElementById("viewport"));
initGizmo();

const planeControls = document.getElementById("plane-controls");
const widthEl = document.getElementById("plane-width");
const heightEl = document.getElementById("plane-height");

document.getElementById("mode-translate").addEventListener("click", () => setMode("translate"));
document.getElementById("mode-rotate").addEventListener("click", () => setMode("rotate"));

widthEl.addEventListener("input", () => setExtent(parseFloat(widthEl.value), extent().height));
heightEl.addEventListener("input", () => setExtent(extent().width, parseFloat(heightEl.value)));

// Keep the number fields in step with the gizmo, whichever moved.
onChange((input) => {
  widthEl.value = input.width.toFixed(2);
  heightEl.value = input.height.toFixed(2);
});

function leaves(part, out = []) {
  if (!part) return out;
  if (!part.children || part.children.length === 0) {
    out.push(part);
    return out;
  }
  for (const c of part.children) leaves(c, out);
  return out;
}

async function render(tree) {
  if (!tree) {
    statusEl.textContent = "No model loaded.";
    return;
  }
  const parts = leaves(tree.root);
  await showParts(parts, tree.selectedId);
  frameAll();
  statusEl.textContent = `${tree.modelName} — ${parts.length} part(s)`;

  planeControls.hidden = false;
  showGizmo();
}

document.getElementById("open").addEventListener("click", async () => {
  try {
    const tree = await OpenModel();
    await render(tree);
  } catch (err) {
    statusEl.textContent = String(err);
  }
});

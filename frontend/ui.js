import { initViewer, showParts, frameAll } from "./viewer.js";
import { OpenModel } from "./wailsjs/go/main/App.js";

const statusEl = document.getElementById("status");

initViewer(document.getElementById("viewport"));

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
}

document.getElementById("open").addEventListener("click", async () => {
  try {
    const tree = await OpenModel();
    await render(tree);
  } catch (err) {
    statusEl.textContent = String(err);
  }
});

export function leavesOf(part, out = []) {
  if (!part) return out;
  if (!part.children || part.children.length === 0) {
    out.push(part);
    return out;
  }
  for (const c of part.children) leavesOf(c, out);
  return out;
}

function fmt(n, digits = 1) {
  return Number(n).toFixed(digits);
}

// renderTree draws the whole tree, not just the leaves, so the history of how a
// model was divided stays visible. Only leaves are selectable — a split part has
// no geometry of its own.
export function renderTree(container, tree, onSelect) {
  container.replaceChildren();
  if (!tree || !tree.root) return;

  const build = (part) => {
    const li = document.createElement("li");

    const row = document.createElement("div");
    row.className = "row";
    const isLeaf = !part.children || part.children.length === 0;
    if (!isLeaf) row.classList.add("split");
    if (part.id === tree.selectedId) row.classList.add("selected");

    const name = document.createElement("span");
    name.textContent = part.name;
    if (isLeaf && !part.watertight) {
      const flag = document.createElement("span");
      flag.className = "flag";
      flag.textContent = " ⚠";
      flag.title = "This part is not a closed solid and may not print correctly.";
      name.appendChild(flag);
    }

    const size = document.createElement("span");
    size.textContent = isLeaf ? `${fmt(part.volume, 0)} mm³` : "";

    row.append(name, size);
    if (isLeaf) row.addEventListener("click", () => onSelect(part.id));
    li.appendChild(row);

    if (!isLeaf) {
      const ul = document.createElement("ul");
      for (const c of part.children) ul.appendChild(build(c));
      li.appendChild(ul);
    }
    return li;
  };

  container.appendChild(build(tree.root));
}

// renderInfo shows the measurements the spec asks for on the selected part.
export function renderInfo(container, tree) {
  container.replaceChildren();
  if (!tree || !tree.root) return;

  const selected = leavesOf(tree.root).find((p) => p.id === tree.selectedId);
  if (!selected) return;

  const dl = document.createElement("dl");
  const rows = [
    ["Size", `${fmt(selected.size[0])} × ${fmt(selected.size[1])} × ${fmt(selected.size[2])} mm`],
    ["Volume", `${fmt(selected.volume, 0)} mm³`],
    ["Triangles", String(selected.tris)],
    ["Closed", selected.watertight ? "yes" : "no — may not print"],
  ];
  for (const [k, v] of rows) {
    const dt = document.createElement("dt");
    dt.textContent = k;
    const dd = document.createElement("dd");
    dd.textContent = v;
    dl.append(dt, dd);
  }
  container.appendChild(dl);
}

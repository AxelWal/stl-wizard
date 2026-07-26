// Renders the cut plan: an ordered, editable list of cuts that nothing acts on until
// Cut now.
//
// The plan is the source of truth and the part tree is derived from it, so this list is
// the thing the user edits and the tree is only ever a result.

// renderPlan draws the list. Handlers are passed in rather than imported so this stays
// about drawing and ui.js stays about wiring commands.
export function renderPlan(container, plan, selectedID, on) {
  container.replaceChildren();
  if (!plan || !plan.cuts) return;

  for (const cut of plan.cuts) {
    const li = document.createElement("li");
    const row = document.createElement("div");
    row.className = "row";
    if (cut.id === selectedID) row.classList.add("selected");
    if (!cut.enabled) row.classList.add("split"); // greyed, as a split part is

    const tick = document.createElement("input");
    tick.type = "checkbox";
    tick.checked = cut.enabled;
    tick.title = "Include this cut when Cut now runs";
    tick.addEventListener("click", (e) => {
      e.stopPropagation(); // ticking is not selecting
      on.toggle(cut.id, tick.checked);
    });

    // contenteditable rather than an input: an input inside a clickable row swallows
    // the click that selects the entry, and the name is a label rather than a form
    // field.
    const name = document.createElement("span");
    name.className = "plan-name";
    name.textContent = cut.name;
    name.contentEditable = "true";
    name.spellcheck = false;
    name.addEventListener("click", (e) => e.stopPropagation());
    name.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        name.blur();
      }
    });
    name.addEventListener("blur", () => {
      const wanted = name.textContent.trim();
      if (wanted && wanted !== cut.name) on.rename(cut.id, wanted);
      else name.textContent = cut.name; // an empty name is refused; show what is still true
    });

    // What it cuts. A named target is a piece the user picked; an empty one means the
    // rectangle applies wherever it lands, which is what a fit-to-printer plan wants.
    const what = document.createElement("span");
    what.className = "plan-target";
    what.textContent = cut.target ? cut.target : "any piece it crosses";

    const del = document.createElement("button");
    del.className = "plan-delete";
    del.textContent = "✕";
    del.title = "Remove this cut from the plan";
    del.addEventListener("click", (e) => {
      e.stopPropagation();
      on.remove(cut.id);
    });

    // Dragging reorders, and order decides what each entry has to cut, so this
    // changes results rather than only the display.
    //
    // draggable goes on the row, not the name: the name is contenteditable, and a
    // draggable contenteditable cannot be selected with the mouse at all — the drag
    // wins over the caret.
    row.draggable = true;
    row.dataset.id = cut.id;
    row.addEventListener("dragstart", (e) => {
      dragging = cut.id;
      e.dataTransfer.effectAllowed = "move";
      // Firefox refuses to start a drag without data set.
      e.dataTransfer.setData("text/plain", cut.id);
    });
    row.addEventListener("dragend", () => {
      dragging = null;
      container.querySelectorAll(".drop-before, .drop-after").forEach((el) =>
        el.classList.remove("drop-before", "drop-after")
      );
    });
    row.addEventListener("dragover", (e) => {
      if (dragging === null || dragging === cut.id) return;
      e.preventDefault(); // without this the drop never fires
      const box = row.getBoundingClientRect();
      const after = e.clientY > box.top + box.height / 2;
      row.classList.toggle("drop-after", after);
      row.classList.toggle("drop-before", !after);
    });
    row.addEventListener("dragleave", () => {
      row.classList.remove("drop-before", "drop-after");
    });
    row.addEventListener("drop", (e) => {
      e.preventDefault();
      if (dragging === null || dragging === cut.id) return;
      const box = row.getBoundingClientRect();
      const after = e.clientY > box.top + box.height / 2;
      on.reorder(orderWith(plan, dragging, cut.id, after));
      dragging = null;
    });

    row.append(tick, name, what, del);
    row.addEventListener("click", () => on.select(cut.id));
    li.appendChild(row);
    container.appendChild(li);
  }
}

// dragging is the id being moved. Module state rather than dataTransfer, because
// dataTransfer.getData is empty during dragover in every browser — the data is only
// readable on drop — and the insertion marker has to be drawn while dragging over.
let dragging = null;

// orderWith returns the ids in the order they would be after moving one entry to just
// before or after another. Exported for the test, which asserts the arithmetic without
// having to simulate a drag.
export function orderWith(plan, movedID, overID, after) {
  const ids = plan.cuts.map((c) => c.id).filter((id) => id !== movedID);
  const at = ids.indexOf(overID);
  ids.splice(after ? at + 1 : at, 0, movedID);
  return ids;
}

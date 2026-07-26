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

    row.append(tick, name, what, del);
    row.addEventListener("click", () => on.select(cut.id));
    li.appendChild(row);
    container.appendChild(li);
  }
}

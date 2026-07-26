// Reads the pin and bed panels. Kept apart from ui.js so the controller stays
// about wiring commands rather than parsing form fields.

let els = {};

// initPrinters wires the printer select to the three bed fields.
//
// The figures in index.html come from Bambu Studio's own machine profiles — the
// printable_area and printable_height each printer inherits — not from marketing
// pages. Those disagree: the X1 Carbon is sold as 256 x 256 x 256, and the slicer
// will only accept 250 of height. The slicer's number is the one that decides
// whether a part actually slices, so it is the one used.
//
// Note for the five machines with a purge zone — P1P, P1S, X1, X1 Carbon, X1E — the
// front-left 18 x 28mm of the bed is excluded. A part filling the whole plate clashes
// with it. The bed is not shrunk for that, because doing so would split every model
// more than it needs; a part that large needs looking at on the plate anyway.
function initPrinters() {
  const sel = document.getElementById("printer");
  const fields = [els.bedX, els.bedY, els.bedZ];

  // Each option's value is the model, and the bed size is in data-bed.
  //
  // The size cannot be the value: nine of the fourteen machines share a bed with
  // another — five of them are 256 x 256 x 250 — so a size-valued select cannot say
  // which printer is chosen. Reading it back gave the first match, so H2C came back
  // as A2L, and a test that selected by value was silently exercising the wrong one.
  const apply = () => {
    const opt = sel.options[sel.selectedIndex];
    const bed = opt && opt.dataset.bed;
    if (!bed) return; // Custom: leave whatever is in the fields
    const dims = bed.split(",");
    fields.forEach((f, i) => (f.value = dims[i]));
  };
  sel.addEventListener("change", apply);

  // Typing a size by hand means the named printer no longer describes what is in the
  // fields, so the select has to stop claiming it does.
  for (const f of fields) {
    f.addEventListener("input", () => {
      if (sel.value !== "custom") sel.value = "custom";
    });
  }

  apply(); // start out agreeing with whichever printer is selected
}

export function initPins() {
  els = {
    enabled: document.getElementById("pins-enabled"),
    count: document.getElementById("pins-count"),
    diameter: document.getElementById("pins-diameter"),
    length: document.getElementById("pins-length"),
    clearance: document.getElementById("pins-clearance"),
    minwall: document.getElementById("pins-minwall"),
    pegside: document.getElementById("pins-pegside"),
    bedX: document.getElementById("bed-x"),
    bedY: document.getElementById("bed-y"),
    bedZ: document.getElementById("bed-z"),
    panel: document.getElementById("pin-controls"),
    bedPanel: document.getElementById("bed-controls"),
  };
  initPrinters();
}

// num reads a field, falling back to a default when it has been emptied mid-edit
// rather than sending NaN to Go, where it would be rejected with a less useful
// message than the field itself already conveys.
function num(el, fallback) {
  const v = parseFloat(el.value);
  return Number.isFinite(v) ? v : fallback;
}

export function pinSpec() {
  // The style select carries both the peg side and dowel mode, since they are one
  // choice for the user: either a peg goes on one of the two pieces, or neither
  // gets one and both are bored for their own dowel.
  const dowel = els.pegside.value === "dowel";
  return {
    enabled: els.enabled.checked,
    count: Math.max(1, Math.round(num(els.count, 4))),
    diameter: num(els.diameter, 4),
    length: num(els.length, 8),
    clearance: num(els.clearance, 0.15),
    minWall: num(els.minwall, 1),
    pegOnPart: parseInt(els.pegside.value, 10) === 1 ? 1 : 2,
    dowel,
  };
}

// repairOnLoad is read at the moment Open is clicked rather than held as state,
// matching OpenModel's parameter: what a load did stays a property of that load.
export function repairOnLoad() {
  return document.getElementById("repair-on-load").checked;
}

export function bedSpec() {
  return {
    x: num(els.bedX, 220),
    y: num(els.bedY, 220),
    z: num(els.bedZ, 250),
  };
}

export function showPanels() {
  els.panel.hidden = false;
  els.bedPanel.hidden = false;
}

// reportPins turns a cut's pin outcome into messages. A skipped pin is always
// surfaced with the clearance actually measured: the user needs to know their
// pieces will not locate against each other, and why.
export function reportPins(outcome, message, spec) {
  if (!outcome) return;
  const dowel = !!(spec && spec.dowel);
  const noun = dowel ? "dowel hole pair" : "alignment pin";
  if (outcome.pinsPlaced > 0) {
    // Count is a target: placement grids the cut face and stops when it runs
    // out of room, and a pin it never found a candidate for leaves no
    // SkippedPin behind to explain itself. Reporting only the placed count
    // would let the user read "Placed 1" as having got the 8 they asked for.
    const short = outcome.pinsRequested > outcome.pinsPlaced;
    let text = short
      ? `Placed ${outcome.pinsPlaced} of ${outcome.pinsRequested} ${noun}(s) — the cut face had room for no more.`
      : `Placed ${outcome.pinsPlaced} ${noun}(s).`;

    // In dowel mode the user has to cut the joining piece themselves, so the
    // message has to say what to cut. The hole is the stock plus clearance all
    // round, and there is one hole of that depth on each side.
    if (dowel) {
      const hole = spec.diameter + 2 * spec.clearance;
      const depth = spec.length + spec.clearance;
      text +=
        ` Each hole is ${hole.toFixed(2)}mm wide and ${depth.toFixed(2)}mm deep,` +
        ` so cut about ${(2 * depth).toFixed(1)}mm of ${spec.diameter}mm stock.`;
    }
    message(text, short ? "warn" : "ok");
  }
  for (const s of outcome.pinsSkipped || []) {
    message(
      `Skipped a pin at (${s.x.toFixed(1)}, ${s.y.toFixed(1)}, ${s.z.toFixed(1)}): ` +
        `${s.reason} — ${s.measured.toFixed(2)}mm available, ${s.required.toFixed(2)}mm needed.`,
      "warn"
    );
  }
}

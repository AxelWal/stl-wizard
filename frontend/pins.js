// Reads the pin and bed panels. Kept apart from ui.js so the controller stays
// about wiring commands rather than parsing form fields.

let els = {};

export function initPins() {
  const id = (name) => document.getElementById(name);
  els = {
    enabled: id("pins-enabled"),
    count: id("pins-count"),
    diameter: id("pins-diameter"),
    length: id("pins-length"),
    clearance: id("pins-clearance"),
    minwall: id("pins-minwall"),
    pegside: id("pins-pegside"),
    bedX: id("bed-x"),
    bedY: id("bed-y"),
    bedZ: id("bed-z"),
    panel: id("pin-controls"),
    bedPanel: id("bed-controls"),
  };
}

// num reads a field, falling back to a default when it has been emptied mid-edit
// rather than sending NaN to Go, where it would be rejected with a less useful
// message than the field itself already conveys.
function num(el, fallback) {
  const v = parseFloat(el.value);
  return Number.isFinite(v) ? v : fallback;
}

export function pinSpec() {
  return {
    enabled: els.enabled.checked,
    count: Math.max(1, Math.round(num(els.count, 4))),
    diameter: num(els.diameter, 4),
    length: num(els.length, 8),
    clearance: num(els.clearance, 0.15),
    minWall: num(els.minwall, 1),
    pegOnPart: parseInt(els.pegside.value, 10) === 1 ? 1 : 2,
  };
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
export function reportPins(outcome, message) {
  if (!outcome) return;
  if (outcome.pinsPlaced > 0) {
    message(`Placed ${outcome.pinsPlaced} alignment pin(s).`, "ok");
  }
  for (const s of outcome.pinsSkipped || []) {
    message(
      `Skipped a pin at (${s.x.toFixed(1)}, ${s.y.toFixed(1)}, ${s.z.toFixed(1)}): ` +
        `${s.reason} — ${s.measured.toFixed(2)}mm available, ${s.required.toFixed(2)}mm needed.`,
      "warn"
    );
  }
}

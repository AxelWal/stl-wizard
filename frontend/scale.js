// The Scale panel: percentage or target size, uniform or per axis.
//
// Factors sent to Go are always absolute, relative to the file as loaded, so applying the
// same numbers twice is idempotent and Reset is exact. Everything here is the conversion
// between what the user typed and those factors.

let els = {};
let original = [1, 1, 1]; // the file's own size
let scale = [1, 1, 1];

export function initScale(onApply) {
  els = {
    panel: document.getElementById("scale-panel"),
    mode: document.getElementById("scale-mode"),
    uniform: document.getElementById("scale-uniform"),
    x: document.getElementById("scale-x"),
    y: document.getElementById("scale-y"),
    z: document.getElementById("scale-z"),
    preview: document.getElementById("scale-preview"),
  };

  const fields = [els.x, els.y, els.z];
  for (const [i, f] of fields.entries()) {
    f.addEventListener("input", () => {
      // With proportions kept, whichever field was typed in drives all three. The factor
      // is taken from that axis, so a target width of 100mm on a 30mm-wide model gives
      // 333% everywhere rather than squashing the other two to 100mm as well.
      if (els.uniform.checked) {
        const factor = factorFrom(i, num(f));
        for (const [j, other] of fields.entries()) {
          if (j !== i) other.value = fieldValue(j, factor);
        }
      }
      showPreview();
    });
  }
  els.mode.addEventListener("change", () => {
    writeFields(currentFactors());
    showPreview();
  });
  els.uniform.addEventListener("change", showPreview);

  document.getElementById("do-scale").addEventListener("click", () => onApply(currentFactors()));
  document.getElementById("reset-scale").addEventListener("click", () => onApply([1, 1, 1]));
}

// sync is called whenever a tree arrives, so the fields describe the model on screen.
export function syncScale(tree) {
  if (!tree) {
    els.panel.hidden = true;
    return;
  }
  els.panel.hidden = false;
  original = tree.originalSize || [1, 1, 1];
  scale = tree.scale || [1, 1, 1];
  writeFields(scale);
  showPreview();
}

function num(el) {
  const v = parseFloat(el.value);
  return Number.isFinite(v) ? v : 0;
}

// factorFrom converts what is in field i into an absolute factor.
function factorFrom(i, value) {
  if (els.mode.value === "mm") {
    // A target size of 0 would be refused by Go anyway; guard the division here so the
    // other two fields do not fill with Infinity while it is being typed.
    return original[i] > 0 ? value / original[i] : 1;
  }
  return value / 100;
}

function fieldValue(i, factor) {
  const v = els.mode.value === "mm" ? original[i] * factor : factor * 100;
  return round(v);
}

function round(v) {
  return String(Math.round(v * 1000) / 1000);
}

function currentFactors() {
  return [factorFrom(0, num(els.x)), factorFrom(1, num(els.y)), factorFrom(2, num(els.z))];
}

function writeFields(f) {
  els.x.value = fieldValue(0, f[0]);
  els.y.value = fieldValue(1, f[1]);
  els.z.value = fieldValue(2, f[2]);
}

// showPreview states the size the model would end up, so the numbers in the fields never
// have to be interpreted.
function showPreview() {
  const f = currentFactors();
  const size = original.map((d, i) => d * f[i]);
  els.preview.textContent =
    `Result: ${size.map((d) => d.toFixed(1)).join(" × ")} mm ` +
    `(from ${original.map((d) => d.toFixed(1)).join(" × ")})`;
}

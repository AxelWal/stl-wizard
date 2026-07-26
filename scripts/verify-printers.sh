#!/usr/bin/env bash
# Check the printer sizes in frontend/index.html still match Bambu Studio's own
# machine profiles.
#
#     scripts/verify-printers.sh
#
# Fourteen hard-coded bed sizes are fourteen chances to be quietly wrong, and a wrong
# one means a part is either split more than it needs or reported as fitting a plate it
# does not. Bambu Studio ships the authoritative figures, so they can be diffed rather
# than trusted.
#
# Not a Go test: it needs the slicer installed, which no CI runner has. The profiles
# are also where the marketing figures and the real ones part company — the X1 Carbon
# is sold as 256 x 256 x 256 and the slicer accepts 250 of height. The slicer decides
# whether a part slices, so the slicer wins.
set -euo pipefail

cd "$(dirname "$0")/.."

profiles=$(find /var/lib/flatpak/app/com.bambulab.BambuStudio -type d -path '*profiles/BBL/machine' 2>/dev/null | head -1)
if [[ -z $profiles ]]; then
	echo "Bambu Studio's machine profiles were not found; cannot verify." >&2
	exit 3
fi

python3 - "$profiles" <<'PY'
import glob, json, os, re, sys

d = sys.argv[1]
byname = {}
for f in glob.glob(os.path.join(d, "*.json")):
    try:
        c = json.load(open(f))
    except Exception:
        continue
    if c.get("name"):
        byname[c["name"]] = c


def resolve(name, key, depth=0):
    if depth > 12 or name not in byname:
        return None
    c = byname[name]
    return c[key] if key in c else resolve(c.get("inherits", ""), key, depth + 1)


# model -> (x, y, z) straight from the slicer
truth = {}
for n in byname:
    if "nozzle" not in n:
        continue
    area, h = resolve(n, "printable_area"), resolve(n, "printable_height")
    if not area or not h:
        continue
    xs = [float(p.split("x")[0]) for p in area]
    ys = [float(p.split("x")[1]) for p in area]
    model = n.rsplit(" ", 2)[0].removeprefix("Bambu Lab ")
    truth[model] = (int(max(xs)), int(max(ys)), int(float(h)))

# model -> (x, y, z) as the UI offers it
ours = {}
html = open("frontend/index.html").read()
# Only the printer select. Without scoping this, the pin-style options get parsed too.
block = re.search(r'<select id="printer">(.*?)</select>', html, re.S)
if block is None:
    print("FAIL: frontend/index.html has no <select id=\"printer\">")
    sys.exit(1)
for value, label in re.findall(r'<option value="([^"]+)"[^>]*>([^<]+)</option>', block.group(1)):
    if value == "custom":
        continue
    model = label.split(" — ")[0].strip()
    ours[model] = tuple(int(v) for v in value.split(","))

bad = 0
for model, dims in sorted(ours.items()):
    want = truth.get(model)
    if want is None:
        print(f"FAIL {model}: offered by the UI but not in the slicer's profiles")
        bad += 1
    elif want != dims:
        print(f"FAIL {model}: UI says {dims}, the slicer says {want}")
        bad += 1
    else:
        print(f"ok   {model}: {dims[0]} x {dims[1]} x {dims[2]}")

for model in sorted(set(truth) - set(ours)):
    print(f"MISSING {model}: the slicer knows it, the UI does not offer it ({truth[model]})")
    bad += 1

print()
if bad:
    print(f"{bad} mismatch(es)")
    sys.exit(1)
print(f"all {len(ours)} printer sizes match Bambu Studio's profiles")
PY

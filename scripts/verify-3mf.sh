#!/usr/bin/env bash
# Verify a 3MF this application wrote is one Bambu Studio actually accepts, with the
# plates intact.
#
#     scripts/verify-3mf.sh plates.3mf
#
# Not a Go test: it needs Bambu Studio installed, which no CI runner has. The
# assertion it makes cannot be made any other way — whether a slicer honours the
# plate layout is a fact about the slicer, not about our code.
#
# --arrange 0 is not optional. Bambu's command line re-arranges on import by default
# and repacks every object onto plate 1, which looks exactly like our file being
# wrong. The GUI honours the stored layout; the CLI does not unless told to.
set -euo pipefail

file=${1:-}
if [[ -z $file || ! -f $file ]]; then
	echo "usage: $0 <file.3mf>" >&2
	exit 2
fi
file=$(realpath "$file")

if ! flatpak info com.bambulab.BambuStudio >/dev/null 2>&1; then
	echo "Bambu Studio is not installed as a flatpak; cannot verify." >&2
	exit 3
fi

# The flatpak is confined to $HOME, so work there whatever the input path.
work=$(mktemp -d "$HOME/.stl-wizard-verify-XXXXXX")
trap 'rm -rf "$work"' EXIT
cp "$file" "$work/in.3mf"

want=$(unzip -p "$work/in.3mf" Metadata/model_settings.config | grep -c 'key="plater_id"')
echo "our file declares $want plate(s)"

echo "--- what Bambu Studio reads ---"
flatpak run com.bambulab.BambuStudio "$work/in.3mf" --info 2>/dev/null |
	grep -E 'number_of_facets|manifold|volume' || true

# Before believing any failure below, check the slicer can read a project at all here.
# Bambu Studio 2.7.1.62 as a flatpak segfaults on every file it recognises as its own
# project — including one it exported itself — almost certainly the same headless-GL
# failure that stops the GUI starting. Files it treats as third-party geometry survive.
# Without this control a crash reads as "our file is malformed" when it means "this
# machine cannot check".
echo "--- can this environment read a Bambu project at all? ---"
control=$work/control.stl
cp "$(dirname "$0")/../testdata/u.stl" "$control"
if flatpak run com.bambulab.BambuStudio --export-3mf "$work/control.3mf" "$control" >/dev/null 2>&1 &&
	flatpak run com.bambulab.BambuStudio "$work/control.3mf" --info >/dev/null 2>&1; then
	echo "control: the slicer reads its own export, so a failure below is ours"
else
	echo "control: the slicer CRASHES on its own export in this environment." >&2
	echo "The plate layout cannot be checked here at all — see docs/manual-verification.md," >&2
	echo "which asks for the Bambu Studio GUI. Not treating this as a failure of our file." >&2
	exit 4
fi

echo "--- round trip with arrange disabled ---"
flatpak run com.bambulab.BambuStudio "$work/in.3mf" \
	--arrange 0 --export-3mf out.3mf --outputdir "$work" >/dev/null 2>&1

got=$(unzip -p "$work/out.3mf" Metadata/model_settings.config | grep -c 'key="plater_id"')
objects=$(unzip -p "$work/out.3mf" Metadata/model_settings.config | grep -c 'key="object_id"')
echo "after the round trip: $got plate(s), $objects object assignment(s)"

if [[ $got -ne $want ]]; then
	echo "FAIL: $want plate(s) went in and $got came out" >&2
	exit 1
fi
if [[ $objects -ne $want ]]; then
	echo "FAIL: $want plate(s) but $objects object assignment(s); one part per plate is the point" >&2
	exit 1
fi
echo "OK: $want plate(s) survived, one part each"

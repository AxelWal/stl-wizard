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

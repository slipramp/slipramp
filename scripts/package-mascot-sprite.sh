#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf 'mascot sprite packaging failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
usage: package-mascot-sprite.sh SOURCE.png OUTPUT.webp [X,Y X,Y X,Y X,Y]

Package a square 2-by-2 mascot source sheet into a horizontal four-frame,
lossless WebP strip. Optional offsets translate frames 1 through 4 on their
unchanged square canvases before packing. Positive values move art right/down.
EOF
  exit 2
}

test "$#" -eq 2 || test "$#" -eq 6 || usage

source_image=$1
output_image=$2
shift 2

test -f "$source_image" || fail "source does not exist: $source_image"
case "$output_image" in
  *.webp) ;;
  *) fail "output must use the .webp extension: $output_image" ;;
esac

command -v magick >/dev/null || fail "ImageMagick's magick command is required"
command -v cwebp >/dev/null || fail "cwebp is required"

if test "$#" -eq 0; then
  offsets=("0,0" "0,0" "0,0" "0,0")
else
  offsets=("$@")
fi

read -r source_width source_height < <(magick identify -format '%w %h\n' "$source_image")
test "$source_width" -eq "$source_height" ||
  fail "source must be square, got ${source_width}x${source_height}"
test $((source_width % 2)) -eq 0 || fail "source dimensions must be even"

cell_size=$((source_width / 2))
delivered_frame_size=288
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/slipramp-mascot.XXXXXX")

cleanup() {
  rm -rf "$temporary_directory"
}
trap cleanup EXIT

registered_frames=()

for index in 0 1 2 3; do
  source_x=$(((index % 2) * cell_size))
  source_y=$(((index / 2) * cell_size))
  source_frame="$temporary_directory/source-$index.png"
  registered_frame="$temporary_directory/registered-$index.png"

  IFS=, read -r offset_x offset_y <<<"${offsets[$index]}"
  [[ $offset_x =~ ^-?[0-9]+$ ]] || fail "invalid horizontal offset: ${offsets[$index]}"
  [[ $offset_y =~ ^-?[0-9]+$ ]] || fail "invalid vertical offset: ${offsets[$index]}"

  offset_x_geometry=$offset_x
  offset_y_geometry=$offset_y
  if ((offset_x >= 0)); then
    offset_x_geometry="+$offset_x"
  fi
  if ((offset_y >= 0)); then
    offset_y_geometry="+$offset_y"
  fi

  magick "$source_image" \
    -crop "${cell_size}x${cell_size}+${source_x}+${source_y}" \
    +repage \
    "$source_frame"
  magick \
    -size "${cell_size}x${cell_size}" \
    canvas:none \
    "$source_frame" \
    -geometry "${offset_x_geometry}${offset_y_geometry}" \
    -composite \
    "$registered_frame"

  registered_frames+=("$registered_frame")
done

packed_png="$temporary_directory/packed.png"
magick "${registered_frames[@]}" +append "$packed_png"

alpha_bounds=$(magick "$packed_png" -alpha extract -threshold 6% -format '%@' info:)
if [[ ! $alpha_bounds =~ ^[0-9]+x([0-9]+)[+-][0-9]+([+-][0-9]+)$ ]]; then
  fail "could not measure visible alpha bounds: $alpha_bounds"
fi

content_height=${BASH_REMATCH[1]}
content_y=${BASH_REMATCH[2]}
test "$content_height" -le "$cell_size" || fail "visible artwork exceeds its frame height"

centered_y=$(((cell_size - content_height) / 2))
center_offset_y=$((centered_y - content_y))
center_offset_y_geometry=$center_offset_y
if ((center_offset_y >= 0)); then
  center_offset_y_geometry="+$center_offset_y"
fi

centered_png="$temporary_directory/centered.png"
magick \
  -size "$((cell_size * 4))x${cell_size}" \
  canvas:none \
  "$packed_png" \
  -geometry "+0${center_offset_y_geometry}" \
  -composite \
  "$centered_png"

delivered_png="$temporary_directory/delivered.png"
magick \
  "$centered_png" \
  -filter Lanczos \
  -resize "$((delivered_frame_size * 4))x${delivered_frame_size}" \
  "$delivered_png"

cwebp \
  -quiet \
  -lossless \
  -exact \
  -m 6 \
  "$delivered_png" \
  -o "$output_image"

printf 'packaged %s (%dx%d, four %dx%d frames)\n' \
  "$output_image" \
  $((delivered_frame_size * 4)) \
  "$delivered_frame_size" \
  "$delivered_frame_size" \
  "$delivered_frame_size"

#!/usr/bin/env bash
# Replaces each ```gd++ code block in README.md and readme/*.md with its image,
# since GitHub can't highlight GD++. Each block is saved as readme/svg/ID.gd++,
# rendered by `gd++ cat --svg` as readme/svg/ID.svg, and replaced by the image,
# which links to the source. ID is a checksum of the code, so runs never clash.
# Each run also re-renders all readme/svg/*.gd++, so to change a snippet, edit
# its .gd++ file and run this again. Set GDPP to use another gd++ binary.
set -euo pipefail
cd "$(dirname "$0")/.."
gdpp=${GDPP:-gd++}
command -v "$gdpp" >/dev/null || { echo "There is no $gdpp to render with." >&2; exit 1; }
mkdir -p readme/svg
trap 'rm -f "${out:-}" "${code:-}"' EXIT

for md in README.md readme/*.md; do
  [[ -f $md ]] || continue
  # The image's path relative to the .md file.
  if [[ $(dirname "$md") == readme ]]; then rel=svg; else rel=readme/svg; fi
  out=$(mktemp) code=$(mktemp)
  fence='' indent='' snippet=0 changed=0
  while IFS= read -r line || [[ -n $line ]]; do
    if [[ -z $fence ]]; then
      # An opening fence: ``` or ~~~, at least 3, maybe indented, e.g. in a list.
      if [[ $line =~ ^(\ {0,3})(\`{3,}|~{3,})[[:space:]]*([^[:space:]\`]*) ]]; then
        indent=${BASH_REMATCH[1]} fence=${BASH_REMATCH[2]}
        case ${BASH_REMATCH[3]} in gd++ | gdpp | gg) snippet=1 && : >"$code" ;; *) snippet=0 ;; esac
        ((snippet)) || printf '%s\n' "$line" >>"$out"
      else
        printf '%s\n' "$line" >>"$out"
      fi
      continue
    fi
    # A closing fence: the same character, at least as long, with nothing after it.
    if [[ $line =~ ^\ {0,3}(\`{3,}|~{3,})[[:space:]]*$ && ${BASH_REMATCH[1]:0:1} == "${fence:0:1}" && ${#BASH_REMATCH[1]} -ge ${#fence} ]]; then
      if ((snippet)); then
        id=$(cksum <"$code" | cut -d' ' -f1)
        cp "$code" "readme/svg/$id.gd++"
        printf '%s<a href="%s/%s.gd++"><img src="%s/%s.svg" alt="GD++ code"></a>\n' "$indent" "$rel" "$id" "$rel" "$id" >>"$out"
        changed=1
      else
        printf '%s\n' "$line" >>"$out"
      fi
      fence=''
      continue
    fi
    if ((snippet)); then
      printf '%s\n' "${line#"$indent"}" >>"$code"
    else
      printf '%s\n' "$line" >>"$out"
    fi
  done <"$md"
  [[ -z $fence ]] || { echo "$md: a code block is never closed." >&2; exit 1; }
  # The .md changes only after the whole file was read, so a failure leaves it as it was.
  if ((changed)); then cat "$out" >"$md" && echo "Replaced the GD++ code in $md."; fi
  rm "$out" "$code"
done

for src in readme/svg/*.gd++; do
  [[ -f $src ]] || continue
  svg=${src%.gd++}.svg
  "$gdpp" cat --svg "$src" >"$svg.tmp" && mv "$svg.tmp" "$svg" || { rm -f "$svg.tmp"; exit 1; }
done

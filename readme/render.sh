#!/usr/bin/env bash
# Replaces each ```gd++:NAME code block in README.md and readme/*.md with its
# image, since GitHub can't highlight GD++. Each block is saved as
# readme/svg/NAME.gd++, rendered by `gd++ cat --svg` as readme/svg/NAME.svg, and
# replaced by the image, which links to the source. A block with the name of an
# earlier one replaces it. Each run also re-renders all readme/svg/*.gd++, so to
# change a snippet, edit its .gd++ file and run this again. Set GDPP to use
# another gd++ binary.
set -euo pipefail
cd "$(dirname "$0")/.."
gdpp=${GDPP:-gd++}
command -v "$gdpp" >/dev/null || { echo "There is no $gdpp to render with." >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail() { echo "$md:$n: $1" >&2; exit 1; }

# First, read every file, so that an error changes nothing.
mds=() outs=() names=' '
for md in README.md readme/*.md; do
  [[ -f $md ]] || continue
  # The image's path relative to the .md file.
  if [[ $(dirname "$md") == readme ]]; then rel=svg; else rel=readme/svg; fi
  out=$(mktemp "$tmp/XXXXXX.md")
  fence='' indent='' name='' n=0 changed=0
  while IFS= read -r line || [[ -n $line ]]; do
    n=$((n + 1))
    if [[ -z $fence ]]; then
      # An opening fence: ``` or ~~~, at least 3, maybe indented, e.g. in a list.
      if [[ $line =~ ^(\ {0,3})(\`{3,}|~{3,})[[:space:]]*([^[:space:]\`]*)(.*)$ ]]; then
        indent=${BASH_REMATCH[1]} fence=${BASH_REMATCH[2]} info=${BASH_REMATCH[3]} rest=${BASH_REMATCH[4]} name=''
        case ${info%%:*} in gd++ | gdpp | gg)
          [[ $info == *:* ]] || fail "A GD++ code block needs a name, e.g. \`\`\`gd++:player."
          name=${info#*:}${rest%"${rest##*[![:space:]]}"}
          [[ $name =~ ^[A-Za-z0-9_-]+$ ]] || fail "The block name \"$name\" may only have letters, digits, - and _."
          [[ $names != *" $name "* ]] || fail "The block name \"$name\" is used twice."
          names+="$name " && : >"$tmp/$name.gd++"
          continue ;;
        esac
      fi
      printf '%s\n' "$line" >>"$out"
      continue
    fi
    # A closing fence: the same character, at least as long, with nothing after it.
    if [[ $line =~ ^\ {0,3}(\`{3,}|~{3,})[[:space:]]*$ && ${BASH_REMATCH[1]:0:1} == "${fence:0:1}" && ${#BASH_REMATCH[1]} -ge ${#fence} ]]; then
      if [[ -n $name ]]; then
        printf '%s<a href="%s/%s.gd++"><img src="%s/%s.svg" alt="GD++ code: %s"></a>\n' "$indent" "$rel" "$name" "$rel" "$name" "$name" >>"$out"
        changed=1
      else
        printf '%s\n' "$line" >>"$out"
      fi
      fence=''
    elif [[ -n $name ]]; then
      printf '%s\n' "${line#"$indent"}" >>"$tmp/$name.gd++"
    else
      printf '%s\n' "$line" >>"$out"
    fi
  done <"$md"
  [[ -z $fence ]] || fail "A code block is never closed."
  if ((changed)); then mds+=("$md") outs+=("$out"); fi
done

# Then write the snippets, and the files that had any.
mkdir -p readme/svg
for name in $names; do cp "$tmp/$name.gd++" readme/svg/; done
for ((i = 0; i < ${#mds[@]}; i++)); do
  cat "${outs[$i]}" >"${mds[$i]}" # Keeps the file's mode, unlike mv.
  echo "Replaced the GD++ code in ${mds[$i]}."
done

for src in readme/svg/*.gd++; do
  [[ -f $src ]] || continue
  svg=${src%.gd++}.svg
  "$gdpp" cat --svg "$src" >"$tmp/svg" && mv "$tmp/svg" "$svg"
done

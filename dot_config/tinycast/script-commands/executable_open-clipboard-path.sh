#!/bin/bash
# Open the file or folder whose path is on the clipboard.
#
# Tinycast's file search only covers the indexed scopes (~), and pasting a path
# into the launcher runs a name search instead of opening it. This opens any
# path, such as one under /private/tmp, straight from the clipboard.
#
# Tinycast runs it as the custom command "Open Clipboard Path", which
# run_onchange_after_setup-tinycast adds to its store.

# First line only, without surrounding whitespace or quotes; ~ expands to $HOME.
p="$(pbpaste | head -n 1 | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e "s/^['\"]//" -e "s/['\"]\$//")"
p="${p/#\~/$HOME}"

if [ -e "$p" ]; then
  open "$p"
else
  echo "Not a path: $p"
  exit 1
fi

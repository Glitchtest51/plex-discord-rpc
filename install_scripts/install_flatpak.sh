#!/usr/bin/env bash
set -e

DEST="$HOME/.var/app/tv.plex.PlexDesktop/data/plex"
SCRIPT_OPTS="$DEST/script-opts"
SOURCE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "$DEST/scripts" "$SCRIPT_OPTS"

echo "Copying files..."

if [ -d "$SOURCE/scripts" ]; then
    cp -af "$SOURCE/scripts/." "$DEST/scripts/"
fi

if [ -d "$SOURCE/script-opts" ]; then
    cp -af "$SOURCE/script-opts/." "$SCRIPT_OPTS/"
fi

echo "Done. Files installed to $DEST"
read -r -p "Press Enter to exit..."
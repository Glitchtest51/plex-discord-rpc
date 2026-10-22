#!/usr/bin/env bash
set -e

DEST="$HOME/.var/app/tv.plex.PlexDesktop/data/plex"
SCRIPT_OPTS="$DEST/script-opts"
SOURCE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

read -r -p "This plugin needs Flatpak permission to work properly. Apply permissions to Plex? (y/n): " answer
case "$answer" in
    [Yy]|[Yy][Ee][Ss])
        flatpak override --user --filesystem=xdg-run/discord-ipc-0 tv.plex.PlexDesktop
        ;;
    [Nn]|[Nn][Oo])
        echo "Skipping Flatpak permission. Discord RPC may not work."
        ;;
    *)
        echo "Please answer y or n."
        exit 1
        ;;
esac

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
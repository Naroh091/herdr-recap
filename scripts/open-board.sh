#!/bin/sh
# Opens the board (split pane) entrypoint of this plugin, for clients like
# herdr-web-ui that cannot attach to session-modal popups (no pane ID).
# herdr's open answer names the focused pane, which is how the web finds it.
set -eu
exec "${HERDR_BIN_PATH:-herdr}" plugin pane open --plugin herdr-recap --entrypoint board

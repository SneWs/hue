# Hue

Control a local Hue Bridge from the Omarchy bar. Rooms open into their own panel for scenes, lights, switches, and sensors.

![Room list](docs/rooms.png)

The bar widget and the Go command live in this repository. The widget starts `hue serve`, which keeps one connection to the bridge open. The application key is stored in `~/.config/hue/bridge.json` and is not part of this repo. Connections check the bridge certificate against Signify's Hue Bridge CA and require the certificate subject to be that bridge's id before the application key is sent.

## Install

Manual setup. `omarchy plugin add` installs the bar widget only. It does not build or download the `hue` executable the panel runs, so the widget does nothing until that command is installed separately.

```sh
omarchy plugin add https://github.com/SneWs/hue.git --enable
sh ~/.config/omarchy/plugins/grenis.hue/install-hue.sh vX.Y.Z
```

Replace `vX.Y.Z` with the immutable release you want. `install-hue.sh` is one fail-closed script (`set -eu`) and requires the [GitHub CLI](https://cli.github.com/). It verifies that tag's signed release attestation, downloads `hue-linux-amd64` or `hue-linux-arm64`, checks the downloaded file against the same attestation, and runs `install` only after both checks succeed. A failed verification or download exits before anything is copied to `~/.local/bin/hue`.

Version tags published while release immutability is enabled on this repository get that attestation. The moving `latest` prerelease and any release without an attestation are refused. `v1.0.0` has no attestation.

To build from the checkout instead, Go 1.27 or newer is required. This repo pins 1.27.1 in `mise.toml` for [mise](https://mise.jdx.dev/) users.

```sh
cd ~/.config/omarchy/plugins/grenis.hue
go build -o ~/.local/bin/hue ./cmd/hue
```

Opening the panel registers with `hue serve`. Until the binary is on `~/.local/bin/hue`, the lightbulb has nothing to call.

## Usage

Click the lightbulb to open the panel. If no bridge is paired, pick the one on your network and press its link button.

Rooms with lamps show a count and a ›. Click the name to open that room's scenes, lights, switches, and sensors. The switch on the room row turns the whole room on or off. Escape returns from a room to the list, then closes the panel.

![A room's lights](docs/lights.png)

Click the gear to reorder. Up and down move a room in the list, or a light inside the open room. The order is saved in `~/.config/hue/order.json` and kept across restarts. New rooms and lights show up at the end until you move them.

![Reorder rooms](docs/reorder-rooms.png)

![Reorder lights](docs/reorder-lights.png)

## Configure

```sh
omarchy bar move grenis.hue --section right
```

## Remove

```sh
omarchy plugin remove grenis.hue
rm -f ~/.local/bin/hue
```

`omarchy plugin remove` deletes an installed git checkout. The saved bridge key stays in `~/.config/hue/bridge.json`. Delete that file too if you want to forget the bridge.

## This checkout

`~/.config/omarchy/plugins/grenis.hue` is a link to this repository, so the shell loads these files directly. `omarchy plugin remove grenis.hue` removes that link and leaves this folder in place. The shell watcher does not follow the link, so after a QML change run:

```sh
omarchy-shell shell rescanPlugins
```

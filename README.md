# whoisbutcooler

A terminal UI for IP address lookups. Type an IP or hostname and it draws a braille
world map, drops a pin on the address's geolocated position, and shows a seven-line
summary of the registry data that actually matters — not raw `whois` output.

## Why

`whois` buries the four facts most people want — where an address is, who runs it,
what network block it belongs to, and who to email about abuse — under nameservers,
raw registry timestamps, and a page of legal disclaimer. `whoisbutcooler` shows those
facts directly, on a map, in any terminal, including a plain SSH session with no
graphics protocol support.

## Install

```sh
go install github.com/PromDungeon/whoisbutcooler@latest
```

Requires Go 1.25 or later. No API key, no signup, no configuration: geolocation comes
from [ipwho.is](https://ipwho.is) and registry data from [rdap.org](https://rdap.org),
both free and keyless.

## Where your query goes

Every lookup asks two services about the address: ipwho.is for the location and
rdap.org for the registry record, both over HTTPS. If ipwho.is is unreachable,
geolocation falls back to [ip-api.com](https://ip-api.com), whose free tier has no TLS
— that request, and the address in it, cross the network in cleartext. The fallback is
never silent: whenever it answers, the status line says so, in the same frame as the
result. Private and reserved addresses are rejected locally and never leave the
machine.

## Usage

```sh
whoisbutcooler                # open the interactive prompt with nothing looked up yet
whoisbutcooler 8.8.8.8         # look it up immediately, then stay interactive
whoisbutcooler --once 8.8.8.8  # render a single frame to stdout and exit
```

An argument is a starting point, not a one-shot — this is a TUI, so it looks up the
address and then leaves you at the prompt to look up more. `--once` renders exactly
one frame and exits instead. Piping or redirecting stdout implies `--once`
automatically, since there's nobody there to type at an interactive prompt; braille is
ordinary text, so the output pipes and redirects cleanly with no ANSI escapes. Both
`--once` and a non-interactive stdout require a query — there's nothing to render and
no prompt to fall back to otherwise, so that combination exits non-zero with a usage
message on stderr.

## Keybindings

Focus alternates between the input prompt and the map, which resolves the collision
between arrow-key history recall and arrow-key panning.

| Key | Input focused | Map focused |
| --- | --- | --- |
| `tab` | focus map | focus input |
| `esc` | — | focus input |
| `enter` | submit lookup | — |
| `up` / `down` | history recall | pan |
| `left` / `right` | move cursor | pan |
| `h` `j` `k` `l` | (text) | pan |
| `+` / `-` | (text) | zoom in / out |
| `0` | (text) | reset to fit |
| `r` | (text) | retry last lookup |
| `q` | (text) | quit |
| `ctrl+c` | quit | quit |

## Map data

The coastlines are Natural Earth 110m land polygons (public domain), embedded in the
binary and reduced to polylines at build time — no download or database needed at
runtime.

## Not included

Raw `whois` passthrough, bulk or batch lookups, live traffic monitoring, and offline
operation are all out of scope. If you need the full record, use `whois`.

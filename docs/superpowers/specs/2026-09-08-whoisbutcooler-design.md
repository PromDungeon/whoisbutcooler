# whoisbutcooler — Design

**Date:** 2026-09-08
**Status:** Approved

## Summary

A terminal UI for IP address lookups. Instead of dumping raw whois output, it renders
a braille-vector world map, drops a pin at the address's geolocated position, and shows
a seven-line summary of the information that actually matters.

## Motivation

Nothing existing covers this. The closest tools each miss in a different direction:

| Tool | What it does | Why it isn't this |
| --- | --- | --- |
| [geotop](https://github.com/ozkanpakdil/geotop) | Live world map of incoming connections from a network interface or nginx logs | A monitor, not a lookup. No "type an IP" verb. |
| [Orbis Unum](https://pypi.org/project/orbis-unum/) | Maps IPs onto an interactive OpenStreetMap | Renders in a browser, not a terminal. |
| [IPGeolocation CLI](https://github.com/IPGeolocation/cli), `ipinfo` | Structured IP data for scripting | No map. |
| mapscii | Pannable braille world map in the terminal | No IP awareness. |

Standard `whois` output is also hostile to read: nameservers, raw registry timestamps, and
a dozen lines of legal disclaimer bury the four facts a person usually wants.

## Goals

- Type an IP or hostname, see where it is on a map, in any terminal.
- Show a radically condensed summary — location, operator, network block, abuse contact.
- Zero configuration. No API key, no signup, no database download before first use.
- Work over plain SSH. No terminal graphics protocol required.

## Non-goals

- Raw whois passthrough. Users wanting full records have `whois`.
- Bulk or batch lookups.
- Live traffic monitoring (geotop's job).
- Offline operation. Network is assumed; a local database is out of scope for v1.

## Decisions

Four forks were resolved before design:

1. **Braille vector map**, not image tiles or half-blocks. Works everywhere, including
   plain SSH and Terminal.app; roughly 2x4 sub-cell resolution; zoomable because the
   underlying data is vector, not raster.
2. **Keyless HTTP APIs**, not an API-token provider or a bundled MaxMind database.
   The tool must work the instant it's installed.
3. **Go + Bubble Tea**, not Rust + ratatui. ratatui ships a world-map canvas widget that
   would have been nearly free, but the surrounding toolchain, lint config, and release
   setup already exist in adjacent Go projects. The braille renderer is written by hand
   as a consequence — a contained, well-tested component, not a slog.
4. **Both invocation modes**, with an auto-fitting, pannable map.

### Provider: ipwho.is, not ip-api.com

ip-api.com was the initial pick for its keyless free tier, but **its free tier is HTTP-only** —
HTTPS requires a paid key. Every lookup would cross the network in cleartext: both the address
being investigated and the answer, readable by anyone on the path. For a tool whose entire
purpose is "tell me about this address," that is a poor default.

[ipwho.is](https://ipwho.is) is keyless, requires no signup, serves over **HTTPS**, and
returns the same fields. It becomes the primary provider. ip-api.com is retained only as a
fallback, and **its use is never silent** — see "Provider fallback" below.

## Architecture

```
main.go                    arg parsing, TTY detection, bootstrap
internal/
  canvas/    braille.go     dot buffer -> runes; Set(x,y), per-cell color, Render()
             coastline.go   embedded world polylines -> canvas
             world.bin      go:embed, Natural Earth 110m land (public domain)
  geo/       project.go     lat/lon <-> dot coordinates (equirectangular)
             viewport.go    center+zoom state, FitTo(pin), pan/zoom, clamping
  lookup/    lookup.go      Lookup(ctx, query) -> Result; hostname resolve, fan-out, merge
             ipwhois.go     ipwho.is client (primary geo provider)
             ipapi.go       ip-api.com client (fallback geo provider)
             rdap.go        RDAP client via rdap.org bootstrap
             result.go      the merged Result struct
  ui/        model.go       Bubble Tea Model/Update/View
             panel.go       the simplified whois panel
             input.go       query prompt + history
             styles.go      lipgloss theme
cmd/genworld/              regenerates world.bin from GeoJSON (committed, run rarely)
```

The boundaries are the point: `canvas` and `geo` know nothing about IP addresses, and
`lookup` knows nothing about terminals. Only `ui` depends on both. This means the map
math is unit-testable with no network, and the lookup merge is testable with no terminal.

### Why equirectangular projection

Braille resolution does not reward Mercator's detail-at-high-zoom, and Mercator renders
Antarctica as an infinite smear. Equirectangular reduces projection to two multiplications.
It also happens to fit the medium: a braille dot is 1/2 cell wide and 1/4 cell tall, and a
terminal cell is roughly 1:2 wide-to-tall, so a dot is very nearly square. The world renders
un-stretched with no aspect fudge factor.

## Data flow

```
query --> ui emits tea.Cmd (runs off the UI goroutine)
            |
            +--> ipwho.is (fallback: ip-api) --+
            +--> RDAP -------------------------+--> merge --> lookupMsg{Result, err}
                                                                |
                            Update: store result, viewport.FitTo(lat, lon)
                                                                |
                            View: canvas(coastlines + pin)  ||  panel(fields)
```

The two sources are independent and queried concurrently. **Partial success is the normal
case, not an error case** — RDAP failing must never blank the map.

## Components

### canvas

Each terminal cell holds eight braille dots, rendered as `U+2800 + bitmask`. For a dot at
column `x` in `{0,1}` and row `y` in `{0,1,2,3}` within a cell:

| | x=0 | x=1 |
| --- | --- | --- |
| **y=0** | `0x01` | `0x08` |
| **y=1** | `0x02` | `0x10` |
| **y=2** | `0x04` | `0x20` |
| **y=3** | `0x40` | `0x80` |

A canvas of `W` x `H` cells exposes a `2W` x `4H` dot grid. Color is tracked **per cell**,
not per dot, because that is the terminal's limit. Land renders in a muted color; the pin
in an accent color. When the pin and coastline share a cell, the accent color wins.

`coastline.go` walks embedded polylines, projects each vertex through `geo`, and rasterizes
segments between consecutive in-viewport vertices with Bresenham.

### World data

Natural Earth 110m land polygons (public domain), reduced to polylines of `float32`
longitude/latitude pairs and stored in a length-prefixed binary blob of roughly 80KB,
embedded with `go:embed`. Binary rather than GeoJSON so startup is a slice cast rather
than a JSON parse. `cmd/genworld` regenerates the blob from source GeoJSON and is committed
so the derivation is reproducible, but it runs rarely — the data does not change.

### geo

Viewport state is `centerLon`, `centerLat`, and `lonSpan` (degrees of longitude visible).

```
latSpan = lonSpan * (dotH / dotW)
west    = centerLon - lonSpan/2
north   = centerLat + latSpan/2
px      = (lon - west)  / lonSpan * dotW
py      = (north - lat) / latSpan * dotH
```

Zoom is a discrete ladder of `lonSpan` values: `360, 240, 120, 60, 30, 15, 8, 4, 2`.
`FitTo(lat, lon)` centers the pin and selects `60` — regional context, city roughly legible.
Latitude clamps so the viewport never runs past a pole; longitude wraps across the
antimeridian.

### lookup

```go
type Result struct {
    Query    string      // what the user typed
    IP       netip.Addr
    Lat, Lon float64

    City, Region, Country, CountryCode string  // geo provider
    ISP      string                            // geo provider, e.g. "Google LLC"
    ASN      string                            // geo provider, e.g. "AS15169"
    Timezone string                            // geo provider

    Network  string  // RDAP, e.g. "8.8.8.0/24"
    NetName  string  // RDAP, e.g. "GOGL"
    Abuse    string  // RDAP, e.g. "abuse@google.com"

    GeoSource string // "ipwho.is" or "ip-api.com" — drives the fallback indicator
}
```

A bare hostname resolves through `net.Resolver`; the first A or AAAA record is used.

**RDAP parsing.** `https://rdap.org/ip/{ip}` bootstraps to the correct RIR. `NetName` comes
from the response's `name`. `Network` prefers the `cidr0_cidrs` extension and falls back to
computing the prefix from `startAddress`/`endAddress`. `Abuse` requires walking `entities`
for one with the `abuse` role, then its jCard `vcardArray` for an `email` entry — the jCard
format is an awkward nested array and warrants its own tested function.

### Provider fallback

If ipwho.is fails, ip-api.com is tried. Because that fallback is HTTP-only, **the panel
displays the provider name whenever the fallback was used**, so a cleartext lookup is never
silent. `GeoSource` on `Result` carries this.

### The simplified whois panel

Seven lines, fixed:

```
  8.8.8.8
  -----------------------------
  Mountain View, California - US
  Google LLC - AS15169
  8.8.8.0/24 - GOGL
  abuse@google.com
  America/Los_Angeles
```

Fields unavailable from RDAP render as an em dash rather than vanishing, so the panel does
not reflow between lookups. Everything standard whois emits beyond these fields is dropped.

These seven lines are fixed. Transient state — the fallback-provider notice, errors, and
"looking up..." — renders on a **separate status line below the panel**, which is blank when
there is nothing to say. Keeping it out of the seven is what makes the panel's height
constant.

### Keybindings

Focus alternates between the input prompt and the map, which resolves the arrow-key
collision between history recall and panning.

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

### Invocation

- `whoisbutcooler` — opens the TUI with an empty prompt.
- `whoisbutcooler 8.8.8.8` — performs the lookup immediately, then **stays interactive**.
  It is a TUI; an argument is a starting point, not a one-shot.
- `whoisbutcooler --once 8.8.8.8`, or any invocation where **stdout is not a TTY** —
  renders a single frame to stdout and exits. Braille is ordinary text, so this pipes and
  redirects cleanly. Alt-screen is skipped in this mode.
- `--once` or a non-TTY stdout **with no query argument** is an error: there is nothing to
  render and no prompt to render it in. Exit non-zero with a usage message on stderr.

## Error handling

| Case | Behavior |
| --- | --- |
| Private/reserved IP (`10.x`, `192.168.x`, `127.x`, etc.) | Detected locally via `netip`, short-circuits with an explanatory message. No API call. |
| Malformed input | Inline panel error; map holds its last good state. |
| Geo provider timeout (5s) or error | Fall back to ip-api.com, flagged in the panel. If both fail: error plus `r` to retry. |
| Rate limited | Surface the provider's own message; back off. |
| RDAP failure | **Non-fatal.** Map and geo fields render; registry fields show an em dash. |
| Non-TTY stdout | Render one frame, skip alt-screen, exit. |

## Testing

| Package | Approach |
| --- | --- |
| `canvas` | Table tests over the bitmask math; render known shapes and assert exact output runes. |
| `geo` | Projection round-trips; clamping at the poles and across the antimeridian; `FitTo` framing. |
| `lookup` | `httptest` servers with recorded fixtures. Covers the merge, provider fallback, jCard abuse-email extraction, and the private-IP short circuit. |
| `ui` | `teatest` golden frames at fixed terminal dimensions. |

No test touches the live network.

## Build order

1. Braille canvas and its tests.
2. `cmd/genworld`, the embedded dataset, and a static world map render.
3. Projection, viewport, pan and zoom.
4. Lookup clients against fixtures — ipwho.is, ip-api fallback, RDAP.
5. Bubble Tea wiring: model, panel, input, history, keybindings.
6. One-shot and non-TTY modes; goreleaser configuration.

## Open questions

None. All forks resolved above.

# whoisbutcooler

A terminal UI for IP address lookups. Type an IP or hostname and it draws a braille
world map, drops a pin on the address's geolocated position, and shows a seven-line
summary of the registry data that actually matters — not raw `whois` output.

<img width="967" height="765" alt="Screenshot 2026-09-09 at 10 16 13 AM" src="https://github.com/user-attachments/assets/195adb89-b096-407b-9e59-6a4bd03693d9" />

## Why

`whois` buries the four things <em>most</em> people(me) want 95% of the time — 

-<b>where</b> an address is, <br> 
-<b>who</b> runs it, <br>
-<b>what</b> network block it belongs to, and , <br> 
-<b>who</b> to email about abuse — under nameservers,
raw registry timestamps, and a page of legal disclaimer. 

`whoisbutcooler` shows those
facts directly, on a map, in any terminal, including a plain SSH session with no
graphics protocol support.

## Install

Grab prebuilt binary from the
[latest release](https://github.com/PromDungeon/whoisbutcooler/releases/latest) — macOS,
Linux and Windows, on both Intel and ARM, no Go toolchain needed. Unpack it and put
`whoisbutcooler` anywhere on your `PATH`:

```sh
tar xzf whoisbutcooler_darwin_arm64.tar.gz
mv whoisbutcooler ~/go/bin/
```

Windows archives are `.zip` rather than `.tar.gz`. Releases ship with a
`checksums.txt`, so you can make sure I'm not trying to poison you:

```sh
shasum -a 256 -c checksums.txt --ignore-missing
```

Or build it from source, which needs Go 1.25 or later:

```sh
go install github.com/PromDungeon/whoisbutcooler@latest
```

No API key, no signup, no configuration: geolocation comes from
[ipwho.is](https://ipwho.is) and registry data from [rdap.org](https://rdap.org), both
free and keyless.

## Where your query goes

Every lookup asks two services about the address: ipwho.is for the location and
rdap.org for the registry record, both over HTTPS. If ipwho.is is unreachable,
geolocation falls back to [ip-api.com](https://ip-api.com), whose free tier has no TLS
— that request, and the address in it, cross the network in cleartext. The fallback is
never silent: whenever it answers, the status line says so, in the same frame as the
result. Private and reserved addresses are rejected locally, before either
provider is called. A hostname is resolved first, so its DNS lookup still goes
out even when it turns out to point somewhere private.

## Usage

```sh
whoisbutcooler                # open the interactive prompt with nothing looked up yet
whoisbutcooler 8.8.8.8         # look it up immediately, then stay interactive
whoisbutcooler --once 8.8.8.8  # render a single frame to stdout and exit
```

## Keybindings

Focus alternates between the input prompt and the map.

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

National borders come from Natural Earth's 110m admin_0 boundary lines, also public
domain, in a second 26KB blob. They are drawn in a dimmer grey beneath the coastline,
and only once the view is a continental one or tighter — about 120 degrees of longitude
across. Above that the map is being used to orient rather than to read a region, and
borders there cost ink without adding legibility. Every lookup lands well inside the
threshold, so in practice you see them whenever there is something to see.

## Not included

Raw `whois` passthrough, bulk or batch lookups, live traffic monitoring, and offline
operation are all out of scope. If you need the full record, use `whois`.

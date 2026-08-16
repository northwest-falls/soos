# Icon and tray

`icon.png` is the source art at 512, `icon.ico` carries 16, 24, 32, 48, 64, 128
and 256 for Windows, and `tray.png` is a 32 for the platforms that want a PNG in
the menu bar.

## Two build steps that are not automatic

**Resolved.** `go mod tidy` has been run, `go.sum` is committed, and the whole
module vets and tests clean.

**macOS cannot be cross-compiled.** `systray_darwin.m` is Objective-C, so that
target needs cgo and clang and must be built on a Mac. Windows and Linux build
from anywhere:

```
GOOS=windows GOARCH=amd64 go build ./cmd/soos
GOOS=linux   GOARCH=amd64 go build ./cmd/soos
```

**Do not pass `-s -w` to a Windows release build until it is signed.** An
unsigned stripped binary matches the heuristics antivirus products use for
packed malware. See the build notes in the top-level README.

**The Windows executable icon and its metadata are separate from the tray
icon.** The tray icon is embedded through `go:embed` and needs nothing. What
Explorer shows for `soos.exe`, both the icon and everything under Properties,
Details, comes from a resource object the linker picks up:

```
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
goversioninfo -o cmd/soos/resource_windows_amd64.syso versioninfo.json
```

`goversioninfo` rather than `rsrc`, because it does the icon **and** the
publisher fields in one pass, and the publisher fields matter more than they
look. An unsigned binary with a blank company and no description is the exact
shape of malware, and SmartScreen and every antivirus heuristic treat it that
way. Filling them in is worth doing long before there is a certificate.

Any `.syso` in the package directory is linked automatically. The name matters:
without the `_windows_amd64` suffix the Go tool would link it into every build,
including Linux and macOS, where it is meaningless.

**CompanyName is the operating name, not a legal entity.** The imprint says
registration is still in progress, and that a placeholder there would be worse
than an honest gap. The same applies to a binary: it must not claim a Ltd, a
GmbH or a B.V. that does not exist. On the day the entity is issued, update
`versioninfo.json`, `packaging/Info.plist` and the imprint in one commit, so
the three can never disagree about who published this.

## The other two platforms

`packaging/Info.plist` is the macOS bundle. `LSUIElement` is the load-bearing
key: it keeps Soos out of the Dock and out of the app switcher, which is right
for something that lives beside the clock. `CFBundleIdentifier` must stay equal
to `keychainService` in `internal/config/seal_darwin.go`, or the credential is
written under one name and looked for under another.

`packaging/soos.desktop` is the Linux entry, set to autostart. Somebody who
installed a background watcher meant it to be watching, and asking them to
launch it every morning is the fastest way to make them stop.

## Why the tray runs the loop rather than the other way round

`systray.Run` takes the calling goroutine and does not give it back, so the sync
loop runs in a goroutine beside it. Quitting from the menu closes `stopTray`,
which the same select in `signalContext` already watches, so the menu and Ctrl-C
reach one shutdown path. That matters because the index save is a deferred call:
a quit that bypassed it would throw away a scan somebody waited through.

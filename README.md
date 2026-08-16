# Soos

The Northwest Falls desktop app. It watches a folder and puts what lands there
in your vault.

It opens files read only. It never moves, renames, retags or writes anything in
the folders you point it at, and it never touches anything outside them.

Named after somebody who keeps the place running without being asked and
without making a thing of it. If you ever find yourself thinking about Soos
while you are working, something has gone wrong.

---

## Why this is readable

Everything on <https://northwestfalls.com/soos> is a claim about software that
runs on your own computer. "It only reads the folders you chose" and "nothing
about you is sent" are the sort of promises you should be able to check rather
than take on trust.

So the source is here, every release carries the SHA-256 of the exact file we
built, and you can compile it yourself and check the two match. If the code and
the page ever disagree, the code is the truth and the page is a bug we want
told about.

**Source-available, not open source**, and we would rather say that plainly
than use the more flattering word incorrectly. You may read it, build it, run
your own build, and publish whatever you find. You may not redistribute it or
reuse it in another product. See [LICENSE](LICENSE).

---

## What it does

A folder becomes a track. The files inside it become that track's versions,
oldest first, keeping the names you gave them.

```
Bounces/
  Nights Without You/     -> a track
    take 3.wav            -> v1
    idea2.wav             -> v2
    ahvbdghrkv2.wav       -> v3, current
```

Filenames are never interpreted. Ordering comes from modification time, which
beats reading `_FINAL_REAL` for meaning, but it is evidence rather than proof:
copying resets it, and restoring a backup flattens a decade into an afternoon.
So the order is proposed and you correct it.

Audio directly inside a track folder is a version. A nested folder is not, and
defaults to stems. Filing twelve stems as twelve drafts would destroy the
history this exists to build.

---

## What it will not do

**It only goes one way, and it only adds.**

- Deleting a file from your folder does **not** delete it from your vault. A
  client that mirrors deletions turns one bad afternoon into permanent loss.
- Renaming a file does **not** rename anything in your vault. That name is what
  a share link showed and what a collaborator was sent.
- Files that arrive from elsewhere do **not** appear in your folder. It never
  writes there, which is also why it cannot cause a sync conflict.
- Documents, contracts and video are **never** uploaded, whatever else you
  switch on. A bounce folder holds split sheets and invoices, and uploading one
  because it sat beside a song is not a risk worth taking.
- It **never** creates a share link. That is the one action that makes a
  private recording reachable by a stranger, so it stays something a person
  presses, signed in, rather than something a background process can do.

---

## What it sends

A bearer token once paired, and a version string so a bad release can be
identified in support. No machine id, no install id, no telemetry. Checking for
an update is an unauthenticated request for a static file carrying nothing, so
it cannot report who you are or that your computer is switched on.

A paired computer can add files and nothing else. It cannot read your
catalogue, delete anything, or change your account. Pairing happens in your
browser, so the app never sees a password, and a counterfeit build of it would
have nothing to collect.

---

## Building

```
go test ./...
go build ./cmd/soos
```

A release, every target plus checksums:

```
go run ./tools/release -version 0.1.0
```

Three things that are easy to get wrong and expensive to discover:

**Do not pass `-s -w` to a Windows build until it is signed.** Stripping the
symbol table and debug information is a common trait of packed malware, and
antivirus heuristics treat an unsigned stripped binary accordingly. It saves
about 30% of the file size and is not worth the risk.

**macOS cannot be cross-compiled.** The tray binds to Cocoa through
Objective-C, so that target needs cgo and clang and has to be built on a Mac.
Windows and Linux build from anywhere.

**The Windows icon and publisher fields come from a resource object**, not from
the build itself. See [internal/ui/README.md](internal/ui/README.md). A binary
with a blank company and no description is the exact shape of malware, and
every heuristic treats it that way.

---

## Status

Released, Windows and Linux. macOS needs a Mac to build on.

Open the downloaded file and he installs himself, asks for your account and
asks which folder to watch. There is nothing to type.

From a terminal, if you would rather:

```
soos install     put him in place, start him with your computer
soos pair        connect him to your account
soos add <folder>
```

Not signed yet, so Windows will say the publisher is unknown. Every release
carries `SHA256SUMS`, and you can build your own from the tag and compare.

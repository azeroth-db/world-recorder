# World Recorder uploader

The uploader sends what the World Recorder addon records to `https://wowdb.wiki`. It runs
on the player's computer next to World of Warcraft. It reads the addon's SavedVariables
file, converts it to JSON, compresses it and sends it when it has changed since the last
accepted send.

## How it works

The uploader looks for every file at
`<install>/<flavor>/WTF/Account/<account>/SavedVariables/WorldRecorder.lua`. The install
folder is the validated `--wow` choice or the saved `game-folder`. The root folder and
any client folder inside it (a name such as `_forever_`) are both accepted. With no saved choice, the program first reads
the folder Battle.net installed WoW Forever into, wherever that is: the Battle.net Agent's
`product.db` (`%ProgramData%\Battle.net\Agent\product.db` on Windows,
`/Users/Shared/Battle.net/Agent/product.db` on macOS) lists every installed product with its
product code and install folder (`productdb.go`), and the program keeps those of the products the
[client contract](#client-contract) lists. After it, macOS checks
`/Applications/World of Warcraft`; Windows checks the current-user and machine Blizzard
registry paths (both registry views, `InstallPath` / `InstallLocation`) and the actual
Windows Program Files locations. Every source gives candidates, not proof.

A folder is accepted with every client of the contract that `.build.info` lists as active at a build
the contract names (a beta and its launch client side by side are both read; setup and the window
name the first), whose folder's `.flavor.info` names the same product, with a client
executable. On Windows that is a `Wow*.exe` whose FileVersion matches the installation
metadata. An empty client folder, Retail, an older Classic beta or a missing/mismatched client
executable is rejected. A folder that holds no client the contract lists is named to the player
by version and folder, for example "This folder holds World of Warcraft 1.61.0.71000 in its
`_forever_` folder, which World Recorder does not read yet."; setup shows it in its dialog or
folder picker, the World Recorder window shows it as the folder state, `status` prints it, a pass
logs it and `run --once` exits with it. With no valid candidate,
more than one distinct valid installation, or an invalid saved/explicit path, setup
opens a native folder picker. Invalid selections return to the picker with the reason;
Cancel exits before addon replacement, account validation or login-item changes.

Only the validated choice receives the addon. Its normalized root is saved locally and
included in the login item's `--wow` argument; future setup and uploads retain that choice.
The save reader uses that client's Forever folder only.

### Client contract

`site/public/assets/world-recorder/clients.json`, which the program reads from `<site>/api/contrib/clients`, lists
each accepted client: `product` (the Battle.net product code), `flavor` (its folder), `builds`
(version prefixes such as `1.60.`) and `interfaces` (the numbers the addon's TOC names). The site's
ingest and realm logic read the same file as the host holds it now (`site/lib/forever/clients-live.ts`),
so a new product, folder, build or interface number is a change to that file, not a new program
or a new release ([launch client](../../../site/lib/contrib/README.mdx#launch-client)).
The program reads it at start, at the start of every pass and every 15 minutes (`clients.go`,
`daemon.go` `contractCheck`), even while the game writes nothing, and keeps the last valid copy as `clients.json` in its
folder for the times the site cannot be reached. A copy is valid only when every product is
`wow[a-z0-9_]*`, every folder `_name_` and every build prefix `<major>.<minor>.`, so a contract can
never point outside the game folder. Until a copy has been read, every folder is refused with
"World Recorder has not read the WoW Forever versions it supports from wowdb.wiki yet."

It makes a pass when it starts. It then waits on file change notifications from the
operating system (kqueue on macOS, `FindFirstChangeNotificationW` on Windows) and uses no
CPU while nothing changes. It watches each `WTF/Account` folder, each account folder, each
`SavedVariables` folder and each save file on macOS; on Windows it watches each
`WTF/Account` folder with every folder below it. Until the game creates `WTF/Account`, it
watches the nearest folder on the way to it. It also watches its configuration folder, so a
`link` run from a terminal takes effect at once. After every notification it builds the set
of watched folders again, so a new account folder is found without a restart, and it makes a
pass when a save file changed size or modification time, the stored key changed or the game
folder changed. The game writes the file on logout, on an interface reload (the addon's Save
button or `/wrsave`) and on exit. The uploader waits until the file has not changed for 5
seconds before it reads it.

A pass rereads the ledger, converts each save to JSON and compares its SHA-256 with the
ledger. When they differ, it sends the save. The ledger records the new hash only after the
server answers HTTP 200. A save that fails to send is sent again on the next pass. The
ledger belongs to the stored key: after a new link it starts empty, so every save goes to
the account that confirmed the link. While sending is paused (see
[Loopback API](#loopback-api)), changes wait until sending resumes or the player asks for a
send.

After every pass, and every 15 minutes, the uploader reads `<site>/assets/world-recorder/release.json`. When it names
a newer version and automatic updates are on, the uploader updates itself (see
[Updates](#updates)).

## Install

`setup` installs everything a player needs:

1. It copies the uploader into its configuration folder (see [Files](#files)). On macOS,
   when it runs from `World Recorder.app`, it copies the whole app. It writes the copy
   next to the old one and renames it into place, so a running uploader keeps its file.
2. It installs the World Recorder addon of the signed `release.json` (see
   [Addon package](#addon-package)) into
   `<validated install>/<client folder>/Interface/AddOns/WorldRecorder`, its TOC's `## Interface:`
   line set to the contract's interface numbers for that client and followed by `## X-Program: World Recorder`,
   which tells the addon this program sends its saves, so it does not name the program at login. A pass, and the check every 15 minutes, installs it again when the
   installed TOC differs from that, as after a game update or when an addon site replaced the copy, unless the player removed it, and installs
   it into a client folder the program sees for the first time, as when the contract adds WoW
   Forever's launch client (`clients-seen.json`). The client is
   revalidated immediately before replacement. Missing or ambiguous detection is resolved
   with the folder picker before setup makes changes.
3. It registers the copy to start at login with the `run` command and starts it. On
   macOS it writes `~/Library/LaunchAgents/wiki.wowdb.recorder-uploader.plist` with
   `KeepAlive`, so launchd starts it again whenever it stops, and loads it with `launchctl`. On Windows it ends running copies of the uploader, writes the
   value `WowdbRecorderUploader` under
   `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` and starts the copy; a copy that
   is stopped starts again at the next sign-in.
4. It runs `link` when no key is stored or the site refuses the stored one, and right after
   linking makes one pass, so the World Recorder page that confirmed the link shows the records
   while it is still open.
5. From a double-click, linking ends on the World Recorder page (`/account/world-recorder`) in
   the browser; an already linked computer opens the installed program's
   [World Recorder window](../desktop/README.mdx), or that page when the window cannot start.
   Only a failure shows a dialog.

Setup needs the network: it downloads the addon from wowdb.wiki, and linking needs the site too.

### Addon package

The program carries no addon. It installs the archive that the `addon` entry of the signed
[`release.json`](#updates) names, the same `WorldRecorder.zip` players download to install by hand:

1. When the configuration folder holds no usable copy, the program reads
   `<site>/assets/world-recorder/release.json` and refuses it unless it carries the release
   signature. It refuses a release older than the program.
2. It downloads the archive from the site's own origin and checks its SHA-256 against the
   signed `addon.sha256`. An archive that does not match is deleted and nothing is installed.
3. It keeps the archive as `addon.zip` and that `release.json` as `addon.json` (see
   [Files](#files)).
4. Before every install it checks the kept pair again: the signature of `addon.json`, its
   version against the program's, and the SHA-256 of `addon.zip` as opened. A pair that fails
   is downloaded again. Only the checked file is unpacked.

The kept copy serves the installs a pass and the 15-minute check make, so a new client folder
or an interface change needs no network once the addon was downloaded.

A double-click starts `setup`: the program starts with no arguments and without a
terminal. Players download `WorldRecorder-mac.zip`, unpack it and open
`World Recorder.app`, or download and open `WorldRecorderSetup.exe` on Windows. Both are
unsigned, so macOS and Windows show their warning for unsigned downloads first.

`install` does the same as `setup` and prints the result instead of showing a dialog. The
install scripts run it. `--wow` and `--endpoint` given to `setup` or `install` are kept in
the login item.

The macOS login item carries `ProcessType` `Background` and `AssociatedBundleIdentifiers`
`wiki.wowdb.recorder-uploader`, the app's `CFBundleIdentifier`, so the Login Items settings
show it under World Recorder.

To remove the login item, run the uploader with `uninstall`. On macOS it deletes the agent
file, then unloads the job by its label (`launchctl bootout gui/<uid>/wiki.wowdb.recorder-uploader`),
which also stops the running uploader and works when the file was already deleted. On
Windows it deletes the Run value, and the running uploader stops at the next logout.

`setup`, `install`, `link` and `status` work while the login item runs: none of them uses
the loopback API port.

## Link to an account

Every send carries the key of the player's wowdb.wiki account, so a moderator can stop
records from an account that sends false data. `link` gets the key:

1. It listens on `127.0.0.1` and sends `POST /api/contrib/link` with the computer name, the
   challenge of a random verifier and its port, then opens the address in the answer in the
   browser.
2. The player signs in on that page if needed and clicks Link this computer. The browser comes
   to the uploader's listener with a one-time grant.
3. The uploader trades grant and verifier for the key at `POST /api/contrib/link/token`, stores
   it in the file `key` with mode 0600, and sends the browser to `/account?linked=<key id>`, which
   opens the World Recorder page (`/account/world-recorder`). The
   key is never passed in an argument and never written to the log.

If it stops waiting first (quit, the 10-minute deadline of the request or a failure), it
withdraws the request at `POST /api/contrib/link/cancel`, proving it with the verifier, so the
page no longer offers the button.

Opened again from a double-click, setup asks the site whether the stored key still links the
computer; an unlinked key is dropped and linking starts again. An already linked computer opens
the World Recorder page. Only a failure shows a dialog.

On macOS the computer name is the one Finder shows (`scutil --get ComputerName`); on Windows it
is the host name. It is sent only here, as the key's name on the player's account page.

Server answers to a send:

| Answer | Effect |
|---|---|
| 200 | The ledger records the save as sent. |
| 401 | The server does not know the key or it was revoked. The uploader deletes `key`, keeps the save unsent and logs that this computer must be linked again with `setup` or `link`. |
| 403 | The account may not send records. The uploader logs it and sends nothing more in that pass; `run` keeps watching and tries again only when a save changes. `--once` exits with the error. |

Without a key, `run` keeps watching and logs that the computer is not linked. Storing a key
changes the configuration folder it watches, so a later `link` sends at once without a
restart.

## Loopback API

The running uploader (`run` without `--once`) serves the wowdb.wiki World Recorder page on
`127.0.0.1:47930`. The page reads the uploader's state and sends it commands from the
player's browser; nothing passes through wowdb.wiki. The contract, with the status fields and
every command, is [Loopback API, version 1](../../../docs/world-recorder-control.mdx#loopback-api-version-1).

Every request is checked in this order:

1. The `Host` header must be `127.0.0.1:47930`. Any other host answers 421, which stops DNS
   rebinding.
2. The `Origin` header must be the scheme and host of `--endpoint` (`https://wowdb.wiki` by
   default). Any other origin answers 403. `GET /v1/status` and `GET /v1/events` also answer
   a request without `Origin`, so a terminal can read them. A request without `Origin` whose
   `X-Recorder-Control` header equals the token in `control-token` comes from the
   [desktop app](../desktop/README.mdx) and may send every command; every other request
   without `Origin` answers 403.
3. An `OPTIONS` request answers 204 with `Access-Control-Allow-Methods: GET, POST`,
   `Access-Control-Allow-Headers: Content-Type` and, when the request carries
   `Access-Control-Request-Private-Network: true`, `Access-Control-Allow-Private-Network: true`.
4. A command must be a `POST` with `Content-Type: application/json` and a JSON object with
   only the fields the command takes; otherwise it answers 415 or 400.

Answers to the allowed origin carry `Access-Control-Allow-Origin` with that origin; every
answer carries `Vary: Origin`.

| Request | Effect |
|---|---|
| `GET /v1/status` | The status JSON. |
| `GET /v1/events` | Server-Sent Events: an event `status` with the status JSON at once and again after every change. |
| `GET /v1/saves/<id>` | The save whose path has this SHA-256, converted to JSON exactly as it is sent (`data` of the request). |
| `POST /v1/send` | Makes a pass now, also while paused. Answers 202. |
| `POST /v1/settings` | Stores `paused` and `autoUpdate` in `settings.json`. `startAtLogin` writes or removes the login item without stopping the running uploader; the status field reports whether the login item exists. |
| `POST /v1/folder` | Opens the folder picker at the current folder, validates the choice as setup does, saves it in `game-folder` and in the login item. Answers 409 when the player cancels. |
| `POST /v1/addon` | Installs the [addon package](#addon-package) into the chosen game folder. |
| `POST /v1/update` | Updates now when `release.json` names a newer version. Answers 202. |
| `POST /v1/link` | Starts the browser link flow (see [Link to an account](#link-to-an-account)). Answers 202; a new key sends at once unless sending is paused. |
| `POST /v1/uninstall` | Answers 202, removes the login item, then exits. |

One worker runs passes, updates, settings and the folder and addon commands one at a time,
and the status field `busy` names the one that runs. Linking waits for the browser beside the
worker, so passes and other commands go on; `busy` is `link` while nothing else runs. A
second folder, addon, update or link command while one of the same kind is queued or running
answers 409. A quit or termination signal during linking withdraws the request before the
program ends.

Sending stays paused across restarts: a paused program makes no pass when it starts, and only
`POST /v1/send` sends while paused.

Every request to wowdb.wiki uses Go's standard connect and TLS handshake limits and ends when
no byte arrives for 100 seconds, waiting for the answer or inside its body. 100 seconds is
how long Cloudflare waits for the origin before it answers 524, so no answer from the site can
come later. A save's `state` is the result of the last
pass while the file is unchanged, and `pending` once the file changed. `sends` lists every
send from `sends.jsonl`; `bytes` there is the compressed request body, and `bytes` of a save
is the size of its file.

When another program holds port 47930, such as a second copy of the uploader, `run` logs
it, keeps uploading and tries the port again after each later notification or command. It
does not exit, so launchd's `KeepAlive` never restarts it in a loop.

## Updates

`publish.sh` writes `release.json` next to the version folders:

```json
{ "version": "1.5.0",
  "darwin": { "url": "/assets/world-recorder/1.5.0/WorldRecorder-mac.zip", "sha256": "…" },
  "windows": { "url": "/assets/world-recorder/1.5.0/WorldRecorderSetup.exe", "sha256": "…" },
  "addon": { "url": "/assets/world-recorder/1.5.0/WorldRecorder.zip", "sha256": "…" },
  "signature": "…" }
```

`signature` is a base64 ed25519 signature over these seven fields, each followed by a newline,
in this order: `version`, `darwin.url`, `darwin.sha256`, `windows.url`, `windows.sha256`,
`addon.url`, `addon.sha256` (`manifest/manifest.go`). `publish.sh` signs with
`go run ./sign <key file>`, which reads the unsigned file on standard input and writes the
signed one; the signer is a separate command and is never built into the program. The private
key is `tools/world-recorder/.release-signing.key`, git-ignored and listed in the pack's local
`keys.md`; `publish.sh` stops before building when it is missing. The program carries the
public key (`update.go`, `releaseKey`) and refuses a `release.json` that is unsigned, carries
another signature or changes any signed field: it logs `release result=failed` with the
version it refused, and offers and installs nothing. Programs 1.6.19 and older ignore
`signature` and read the same file.

`addon` is the archive the program installs ([Addon package](#addon-package)) and players who
install the addon by hand download. The program's version is the addon's TOC version, which
`build.sh` sets at build time. An update runs on the worker, so never while a send is in flight:

1. It downloads the program file for this system from the site's own origin into the
   configuration folder and checks its SHA-256. A file that does not match is deleted and the
   update stops; automatic updates skip that version until the program starts again.
2. It records the version in `update-attempt`. Automatic updates never install that version
   again, so a `release.json` whose file does not raise the running version cannot start an
   update loop; `POST /v1/update` can still install it.
3. It swaps the program. On macOS it unpacks the app and puts it, or the bare binary, beside
   the installed one, then renames the installed one to `.old` and the new one into place. On
   Windows it renames the running `.exe` to `.exe.old`, which Windows allows, and moves the new
   file into place. When the second rename fails, the first is undone. It writes `update.json`
   with the new version and the path of the `.old` copy.
4. It restarts. On macOS the login item restarts with `launchctl kickstart -k`; a copy started
   another way runs the new file in its own place. On Windows it starts the new program with
   the same arguments and exits. When the restart fails, the `.old` copy is put back and the
   running program serves the port again.
5. The new program, once it serves the port, installs the [addon package](#addon-package)
   when the installed addon has another version, deletes the `.old` copy and then `update.json`, and
   starts its [desktop app](../desktop/README.mdx) in the tray. It
   logs when its version is not the one `update.json` names. On Windows the previous program
   can still be exiting; then the `.old` copy stays and the next start deletes it.

Each SHA-256 is covered by the signature, so a program file that does not match the signed
release is refused even when the site serves both. Signing the release proves that the key
holder published it; platform code signing of the programs themselves is the open decision in [owner decisions D1 and D2](../../../docs/world-recorder-transport.mdx#owner-decisions).

## What is sent

Each request is an HTTP `POST` with `Content-Type: application/json` and
`Content-Encoding: gzip`. The body holds one save:

```json
{"format": 1, "install": "<install id>", "uploader": "4", "saves": [{"flavor": "_forever_", "data": {}}]}
```

`data` holds every global the save file assigns, such as `WorldRecorderDB` and
`WorldRecorderUI`, converted without loss:

- A table whose keys are exactly 1 to n becomes an array. Any other table becomes an
  object, and its integer keys become decimal strings.
- A `nil` field is absent, as in Lua. A `nil` element inside a list leaves a gap, so
  that table becomes an object.
- An empty table becomes `{}`.
- Numbers keep their exact text from the file. Strings are unescaped exactly.
- Object keys are sorted, so the same content always gives the same JSON and hash.

Beside the addon's globals, `data` holds `WorldRecorderCombatLog` once the game's combat log gave the
uploader a creature (see [Combat log](#combat-log)).

The uploader refuses a file it cannot convert exactly, such as a string that is not
UTF-8, a number without a JSON form or a repeated key, and logs the reason.

Each request carries `Authorization: Bearer <key>`. The install id is a random 128-bit
number created on first run. The account folder name, file paths and the user name are not
sent and are not written to the log.

## Combat log

Forever's client withholds a creature's GUID inside dungeons, its health maximum and its casts from addon code
(`UnitGUID`, `UnitHealthMax` and `UNIT_SPELLCAST_*` return secret values; `COMBAT_LOG_EVENT_UNFILTERED` is
restricted). The game's combat log file keeps them. World Recorder turns advanced combat logging on inside a dungeon
or raid when the player has not, and off again when the player leaves. At the start of every pass the uploader reads
the lines each `<install>/<client folder>/Logs/WoWCombatLog*.txt` gained since the last pass (`combatlog.go`) and keeps,
under the install's build number:

| Kept | From |
|---|---|
| per creature id and level, the highest health maximum | the advanced block (`infoGUID`, `ownerGUID`, `currentHP`, `maxHP`, ..., `level` last) of a line whose `infoGUID` is the creature; the block's length (16 or 17 fields) is read from `SPELL_CAST_SUCCESS`, which it ends |
| per creature id, the maps its GUIDs name | field 4 of `Creature-0-<server>-<map>-<zone>-<id>-<spawn>` |
| per creature id, each spell id it cast (`SPELL_CAST_SUCCESS`, counted) or was seen with (`SPELL_*` as source, count 0) | the spell prefix |
| per encounter id, its map, starts, kills and the creatures that fought while it ran | `ENCOUNTER_START,id,"name",difficulty,size,map` and `ENCOUNTER_END,id,"name",difficulty,size,success` |

A unit is read only when its flags mark an outsider the game controls, of creature type (`0x8`, `0x200`, `0x800`):
players, their pets, guardians and totems are skipped, and no name, player GUID or position is kept. A line the game
has not finished writing waits for the next pass; a file shorter than its stored offset is read again from its start.
The uploader never deletes or changes a log file.

## Files

The uploader keeps its files in `wowdb-recorder` in the user configuration folder:
`~/Library/Application Support/wowdb-recorder` on macOS and
`%AppData%\wowdb-recorder` on Windows.

| File | Content |
|---|---|
| `install-id` | The install id. |
| `addon.zip` | The [addon package](#addon-package), downloaded from the site. |
| `addon.json` | The signed `release.json` that `addon.zip` came from; its `addon.sha256` is checked before every install. |
| `game-folder` | The validated Battle.net root selected during setup; a local path, never uploaded. |
| `clients.json` | The last valid [client contract](#client-contract) read from the site. |
| `clients-seen.json` | The client folders the program has seen; the first sight of a folder installs the addon there. Written without installing on its first run, so an addon removed before it existed stays removed. |
| `key` | The account key from `link`, mode 0600. |
| `World Recorder.app` or `wowdb-recorder` | The installed uploader on macOS. |
| `wowdb-recorder.exe` | The installed uploader on Windows. |
| `combatlog.json` | Per log file (keyed by the install folder and file name) the bytes read and the encounter running at that point; per build the facts [Combat log](#combat-log) lists. A local path is never uploaded. |
| `ledger.json` | The SHA-256 of the key it was written under and, for each save, the SHA-256 of its path mapped to the SHA-256 of the JSON last accepted. |
| `settings.json` | `paused` (default false) and `autoUpdate` (default true). |
| `update-attempt` | The release version the last update installed. |
| `update.json` | Written by an update and deleted by the new program once it has finished: the new version and the path of the previous program. |
| `sends.jsonl` | One line per send: time in milliseconds, the SHA-256 of the save's path, compressed size and result (`accepted`, `duplicate` or `failed`). |
| `control-token` | Written at each start, mode 0600: the token the desktop app's commands carry. |
| `World Recorder` or `World Recorder.exe` | The desktop app beside the bare macOS program or the Windows program, written by the program; inside `World Recorder.app` it is `Contents/MacOS/World Recorder`. |
| `uploader.log` | One line per pass, send, update and failure: time, flavor, uncompressed and compressed size, result. |

Delete `ledger.json` to send every save again.

## Commands and flags

| Command | Effect |
|---|---|
| none | `setup` when started without a terminal, otherwise `run`. |
| `setup` | Install the uploader and the addon, register the login item, link when no key is stored or the site refuses it, then open the World Recorder window; a failure shows a dialog. |
| `install` | `setup`, printing the result instead of a dialog. |
| `link` | Link this computer to a wowdb.wiki account and store the key. |
| `run` | Make a pass, then serve the loopback API, watch the saves and send until stopped. |
| `uninstall` | Remove the login item. |
| `status` | Print the install id, whether a key is stored, why the game folder holds no accepted client when it holds none and, for each save found, its flavor and whether it is sent. |

| Flag | Effect |
|---|---|
| `--wow <dir>` | Use this game root or Forever flavor folder; setup validates it and offers the picker if it is invalid. |
| `--endpoint <url>` | Send to this URL. Default: `https://wowdb.wiki/api/contrib/recorder`. `link` uses the scheme and host of this URL. |
| `--once` | Make one pass and exit. The exit status is 1 when any save was not sent. |

## Build

Go 1.26 or later, standard library only, on macOS (the script uses `lipo`, `codesign` and
`ditto`), with the [desktop app's build tools](../desktop/README.mdx#build):

```bash
tools/world-recorder/uploader/build.sh
```

It builds the desktop app into `control/` (the universal macOS app, signed ad hoc, and each of its slices, which the macOS
program of that architecture embeds; the Windows app, which the Windows program embeds) and
builds with `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false` and
`-X main.programVersion=<the addon's TOC version>`. `GOLDFLAGS` passes linker flags
to `go build`; a test build serves another port under another login item with
`GOLDFLAGS="-X main.apiAddress=127.0.0.1:<port> -X main.agentLabel=<label>"`. It writes to `dist/`:

| File | Content |
|---|---|
| `wowdb-recorder-darwin` | The macOS binary for Apple silicon and Intel, joined with `lipo` and signed ad hoc. |
| `World Recorder.app` | The app: that binary as `Contents/MacOS/wowdb-recorder`, the universal desktop app as `Contents/MacOS/World Recorder`, and an `Info.plist` with the addon's TOC version, `LSUIElement` set and an ad hoc signature over the bundle. |
| `WorldRecorder-mac.zip` | The app, zipped with `ditto`. |
| `WorldRecorderSetup.exe` | The Windows binary for x86-64. |

`dist/` and `control/` are not tracked. A plain `go build` needs `control/` from a previous
`build.sh` run and carries no version.

# World Recorder uploader

The uploader sends what the World Recorder addon records to `https://wowdb.wiki`. It runs
on the player's computer next to World of Warcraft. It reads the addon's SavedVariables
file, converts it to JSON, compresses it and sends it when it has changed since the last
accepted send.

## How it works

The uploader looks for every file at
`<install>/<flavor>/WTF/Account/<account>/SavedVariables/WorldRecorder.lua`. The install
folder is `--wow` when given. Otherwise it is `/Applications/World of Warcraft` on macOS.
On Windows it is the folder in the registry value `InstallPath` under
`HKLM\SOFTWARE\WOW6432Node\Blizzard Entertainment\World of Warcraft`, or its parent when
that value names a flavor folder, and `C:\Program Files (x86)\World of Warcraft`.

It makes a pass when it starts. It then checks every 10 seconds and makes another pass
when a World of Warcraft game process starts, when a save file changes size or
modification time, or when the stored key changes. The game writes the file on logout, on an interface reload (the
addon's Save button or `/wrsave`) and on exit. The uploader waits until the file has not
changed for 5 seconds before it reads it.

A pass rereads the ledger, converts each save to JSON and compares its SHA-256 with the
ledger. When they differ, it sends the save. The ledger records the new hash only after the
server answers HTTP 200. A save that fails to send is sent again on the next pass. The
ledger belongs to the stored key: after a new link it starts empty, so every save goes to
the account that confirmed the link.

## Install

`setup` installs everything a player needs:

1. It copies the uploader into its configuration folder (see [Files](#files)). On macOS,
   when it runs from `World Recorder.app`, it copies the whole app. It writes the copy
   next to the old one and renames it into place, so a running uploader keeps its file.
2. It installs the World Recorder addon, which is built into the binary, into
   `<install>/_classic_beta_/Interface/AddOns/WorldRecorder` of every install folder that
   has a WoW Forever client. It deletes the old addon folder first. It stops when no
   install folder has one.
3. It registers the copy to start at login with the `run` command and starts it. On
   macOS it writes `~/Library/LaunchAgents/wiki.wowdb.recorder-uploader.plist` and loads
   it with `launchctl`. On Windows it ends running copies of the uploader, writes the
   value `WowdbRecorderUploader` under
   `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` and starts the copy.
4. It runs `link` when no key is stored, and right after linking makes one pass, so the
   account page that confirmed the link shows the records while it is still open.
5. It shows one dialog with the result.

A double-click starts `setup`: the program starts with no arguments and without a
terminal. Players download `WorldRecorder-mac.zip`, unpack it and open
`World Recorder.app`, or download and open `WorldRecorderSetup.exe` on Windows. Both are
unsigned, so macOS and Windows show their warning for unsigned downloads first.

`install` does the same as `setup` and prints the result instead of showing a dialog. The
install scripts run it. `--wow` and `--endpoint` given to `setup` or `install` are kept in
the login item.

To remove the login item, run the uploader with `uninstall`. On macOS this also stops the
running uploader. On Windows the running uploader stops at the next logout.

## Link to an account

Every send carries the key of the player's wowdb.wiki account, so a moderator can stop
records from an account that sends false data. `link` gets the key:

1. It sends `POST /api/contrib/link` with `{"label": "<computer name>"}` to the site of
   `--endpoint` and opens the address in the answer in the browser. It also prints the
   code shown there.
2. The player signs in on that page and confirms the code.
3. The uploader asks `POST /api/contrib/link/poll` every 5 seconds, the default polling
   interval of RFC 8628 section 3.5, until the answer is `linked` or `expired`.
4. It stores the key in the file `key` with mode 0600. The key is never passed in an
   argument and never written to the log.

The computer name is sent only here, as the key's name on the player's account page.

Server answers to a send:

| Answer | Effect |
|---|---|
| 200 | The ledger records the save as sent. |
| 401 | The server does not know the key or it was revoked. The uploader deletes `key`, keeps the save unsent and logs that this computer must be linked again with `setup` or `link`. |
| 403 | The account may not send records. The uploader logs it and stops: `run` exits and `--once` sends nothing more. |

Without a key, `run` keeps watching and logs that the computer is not linked. It reads
`key` on every check, so a later `link` takes effect without a restart.

## What is sent

Each request is an HTTP `POST` with `Content-Type: application/json` and
`Content-Encoding: gzip`. The body holds one save:

```json
{"format": 1, "install": "<install id>", "uploader": "3", "saves": [{"flavor": "_classic_beta_", "data": {}}]}
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

The uploader refuses a file it cannot convert exactly, such as a string that is not
UTF-8, a number without a JSON form or a repeated key, and logs the reason.

Each request carries `Authorization: Bearer <key>`. The install id is a random 128-bit
number created on first run. The account folder name, file paths and the user name are not
sent and are not written to the log.

## Files

The uploader keeps its files in `wowdb-recorder` in the user configuration folder:
`~/Library/Application Support/wowdb-recorder` on macOS and
`%AppData%\wowdb-recorder` on Windows.

| File | Content |
|---|---|
| `install-id` | The install id. |
| `key` | The account key from `link`, mode 0600. |
| `World Recorder.app` or `wowdb-recorder` | The installed uploader on macOS. |
| `wowdb-recorder.exe` | The installed uploader on Windows. |
| `ledger.json` | The SHA-256 of the key it was written under and, for each save, the SHA-256 of its path mapped to the SHA-256 of the JSON last accepted. |
| `uploader.log` | One line per pass and per send: time, flavor, uncompressed and compressed size, result. |

Delete `ledger.json` to send every save again.

## Commands and flags

| Command | Effect |
|---|---|
| none | `setup` when started without a terminal, otherwise `run`. |
| `setup` | Install the uploader and the addon, register the login item, link when no key is stored and show the result in a dialog. |
| `install` | `setup`, printing the result instead of a dialog. |
| `link` | Link this computer to a wowdb.wiki account and store the key. |
| `run` | Make a pass, then watch and send until stopped. |
| `uninstall` | Remove the login item. |
| `status` | Print the install id, whether a key is stored and, for each save found, its flavor and whether it is sent. |

| Flag | Effect |
|---|---|
| `--wow <dir>` | Look for saves under this install folder only. |
| `--endpoint <url>` | Send to this URL. Default: `https://wowdb.wiki/api/contrib/recorder`. `link` uses the scheme and host of this URL. |
| `--once` | Make one pass and exit. The exit status is 1 when any save was not sent. |

## Build

Go 1.26 or later, standard library only, on macOS (the script uses `lipo`, `codesign` and
`ditto`):

```bash
tools/world-recorder/uploader/build.sh
```

It copies `../WorldRecorder` to `addon/`, which the binary embeds, and builds with
`CGO_ENABLED=0`, `-trimpath` and `-buildvcs=false`. It writes to `dist/`:

| File | Content |
|---|---|
| `wowdb-recorder-darwin` | The macOS binary for Apple silicon and Intel, joined with `lipo` and signed ad hoc. |
| `World Recorder.app` | The app: that binary as `Contents/MacOS/wowdb-recorder` and an `Info.plist` with the addon's TOC version, `LSUIElement` set and an ad hoc signature. |
| `WorldRecorder-mac.zip` | The app, zipped with `ditto`. |
| `WorldRecorderSetup.exe` | The Windows binary for x86-64. |

`dist/` and `addon/` are not tracked. A plain `go build` needs `addon/` from a previous
`build.sh` run.

# World Recorder

A WoW Forever addon and a desktop program for [azeroth.wiki](https://azeroth.wiki). The addon records
your loot, kills, quests, NPC services, fishing, auction prices and your characters while you play;
the program sends them to your azeroth.wiki account after every logout. See them on your
[World Recorder page](https://azeroth.wiki/account/world-recorder): items by quality, looted corpses
and objects, and the materials you gathered most.

![World Recorder showing recorded loot by quality, looted corpses and objects, and gathered materials](https://azeroth.wiki/cdn/guides/world-recorder/enUS/record-c01368854459.webp)

## Install with the program

The World Recorder program puts the addon into your WoW Forever client, keeps it up to date and
sends your records after every logout, so there is nothing to upload by hand. Its window and its
tray icon (the menu bar on macOS) show each step, from the addon to the last upload.

1. Download the setup for your computer from the [latest release](https://github.com/azeroth-db/world-recorder/releases/latest):
   [`WorldRecorderSetup.exe`](https://github.com/azeroth-db/world-recorder/releases/latest/download/WorldRecorderSetup.exe)
   for Windows, [`WorldRecorder-mac.zip`](https://github.com/azeroth-db/world-recorder/releases/latest/download/WorldRecorder-mac.zip)
   for macOS.
2. Run it. It finds your WoW Forever folder, installs the addon and opens azeroth.wiki in your browser
   to link this computer to your account.

   ![azeroth.wiki asks whether to link the computer that runs World Recorder to your account](https://azeroth.wiki/cdn/guides/world-recorder/enUS/approve-93c5004a1416.webp)
3. Play as usual. The program starts with your computer and sends each save the game writes.

The [World Recorder guide](https://azeroth.wiki/forever/guides/world-recorder) covers the warnings
Windows and macOS show for new downloads.

## Install the addon alone

1. Download `WorldRecorder-<version>.zip` from the [latest release](https://github.com/azeroth-db/world-recorder/releases/latest)
   and copy its `WorldRecorder` folder into `World of Warcraft/_forever_/Interface/AddOns/`.
2. Play, then log out or type `/wrsave`: the game writes the addon's save.
3. Open your [World Recorder page](https://azeroth.wiki/account/world-recorder) signed in and choose
   `World of Warcraft/_forever_/WTF/Account/<your account>/SavedVariables/WorldRecorder.lua`.

   ![The World Recorder page: Send a save file, with where the game keeps the save and the answer once it arrived](https://azeroth.wiki/cdn/guides/world-recorder/enUS/send-1ac111c2ecc6.webp)

Send the file after each session and before a game update: the records start over when the game
version changes. At each login the addon names the program, which sends the file for you.

## Edited saves

From version 1.7.5 every save carries a checksum of its records (`WorldRecorderSeal`). azeroth.wiki
refuses a save that was changed after the game wrote it, and the addon keeps such a save marked.
To record again, delete `WorldRecorder.lua` from the `SavedVariables` folder.

## Data sent to azeroth.wiki

The addon keeps its records on your computer and sends nothing by itself. The program sends them
to azeroth.wiki after you link it to your account; without the program, only the file you choose on
your World Recorder page is sent. Those records include loot, kills, quests, NPC services, fishing,
creature observations, auction prices and your characters' names, realms, guilds, equipment,
talents, professions and completed quests. Read the [privacy notice](https://azeroth.wiki/privacy)
before linking a computer.

## Build

```sh
./package.sh            # addon ZIP in dist/, after check-api.sh
uploader/build.sh       # macOS app and Windows setup in uploader/dist/
```

`check-api.sh <path to the client's Interface/AddOns>` checks calls against WoW Forever's
own interface. The program's source is in `uploader/` (Go: watching saves, uploading, linking,
installing the addon, updating itself) and `desktop/` (its Qt window and tray icon).

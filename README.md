# World Recorder

A WoW Forever addon for [wowdb.wiki](https://wowdb.wiki). See your recorded loot and gathering
on your [World Recorder page](https://wowdb.wiki/account/world-recorder): items by quality,
looted corpses and objects, and the materials you gathered most.

![World Recorder showing recorded loot by quality, looted corpses and objects, and gathered materials](https://wowdb.wiki/cdn/guides/world-recorder/enUS/record-c01368854459.webp)

## Install

Download the addon ZIP from [Releases](https://github.com/azeroth-db/world-recorder/releases)
and copy its `WorldRecorder` folder into your WoW Forever client's `Interface/AddOns/`.

For the companion uploader and setup instructions, open the
[World Recorder guide](https://wowdb.wiki/forever/guides/world-recorder).
The game writes the addon's saved data when you log out, exit or reload the interface.

## Data sent to wowdb.wiki

The addon keeps its records on your computer and sends nothing by itself. The companion
uploader sends them to wowdb.wiki after you link it to your account. Those records include
loot, kills, quests, NPC services, fishing, creature observations, auction prices and your
characters' names, realms, guilds, equipment, talents, professions and completed quests.
Read the [privacy notice](https://wowdb.wiki/privacy) before linking a computer.

## Build

```sh
./package.sh            # addon ZIP in dist/, after check-api.sh
uploader/build.sh       # macOS app and Windows setup in uploader/dist/
```

`check-api.sh <path to the client's Interface/AddOns>` checks calls against WoW Forever's
own interface. The uploader source is in `uploader/`.

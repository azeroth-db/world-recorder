# World Recorder

World Recorder is a WoW Forever addon that records what your client sees while you play:
drops, kills, skinning and gathering loot, quest givers and quest text, vendors, trainers,
flight routes and fishing catches. It also keeps a snapshot of each of your characters.
A companion uploader sends the saved data to [wowdb.wiki](https://wowdb.wiki), where your
[profile](https://wowdb.wiki/account) shows your characters and what you recorded.

- Install and downloads: [wowdb.wiki/news/wow-forever-world-recorder](https://wowdb.wiki/news/wow-forever-world-recorder)
- About the addon: [azeroth-db.github.io/world-recorder.html](https://azeroth-db.github.io/world-recorder.html)
- Releases: the addon zip, the macOS app and the Windows setup are attached to each [release](https://github.com/azeroth-db/world-recorder/releases).

## What it records

| Record | Kept |
|---|---|
| Loot | per creature or object: windows opened, money, every item with how often it showed and its total quantity |
| Kills | hostile and neutral creatures seen dead, the denominator of a drop rate |
| Loot after a cast | skinning, mining, herbalism and pick pocket loot apart from corpse loot |
| Quests | who gives and who takes each quest, the quest, objective, progress and completion text, rewards |
| NPC services | vendor wares and prices, trainer lessons, flight routes and fares, gossip options |
| Fishing | catches per zone and subzone |
| Creatures | level, classification, health and the spells they cast |
| Characters | name, realm, guild, level, equipment, talents, Legacy, professions and completed quests |

Quest text keeps your name as the game's own `$n` placeholder. The addon only listens to
events you cause and never acts for you.

## Saving

The game writes addon data when you log out, exit or reload the interface. Type `/wrsave`,
or click the Save button that `/worldrecorder button` shows, to write it without leaving the
world. `/worldrecorder` prints what has been recorded so far.

## Uploader

The uploader (`uploader/`, Go, standard library only) runs on your computer, watches the
addon's saved data and sends a changed save to wowdb.wiki with the key of the account you
linked. Unlink a computer on your wowdb.wiki profile to stop it. `uploader/README.md`
describes every command, file and request.

## Build

```sh
./package.sh            # addon zip in dist/, after check-api.sh
uploader/build.sh       # macOS app, universal binary and Windows setup in uploader/dist/
```

`check-api.sh <path to the client's Interface/AddOns>` refuses an addon that calls a
function WoW Forever's own interface never calls.

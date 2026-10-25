"""Writes resources/strings.json: every phrase the app shows, in the site's ten languages, from the site's own
interface resources, so the window and the profile page's "This computer" panel say the same thing."""
import glob
import json
import pathlib
import sys

DESKTOP = pathlib.Path(__file__).resolve().parent
SITE = DESKTOP.parents[2] / "site"
LOCALES = ["enUS", "deDE", "esES", "esMX", "frFR", "koKR", "ptBR", "ruRU", "zhCN", "zhTW"]
PHRASES = [
    "World Recorder", "Update to {version}", "Updating…", "Linked as", "Not linked", "Link this computer",
    "Linking…", "Game folder", "Choose the World of Warcraft folder.",
    "World Recorder does not read World of Warcraft {version} yet. It updates itself as soon as it can.", "Change", "AddOns", "Missing", "Install",
    "Update", "Saved records", "Saved {date}", "Log out, or type /reload or /wrsave in game.", "Upload", "Paused",
    "Waiting to upload", "Uploaded {date}", "Upload failed", "Refused", "Cannot be read", "Not linked", "Upload now",
    "Pause", "Update automatically", "Start at login", "Upload history", "Accepted", "Already uploaded", "Failed",
    "Remove from this computer", "Remove World Recorder from this computer?", "Keep", "Not running on this computer",
    "World Recorder could not do that.", "Your records", "Open", "Close",
]

sources = {}
for path in sorted(glob.glob(str(SITE / "i18n/features/*/phrases.json"))):
    sources.update(json.loads(pathlib.Path(path).read_text()))
for locale in LOCALES:
    for phrase, text in json.loads((SITE / "locales" / f"{locale}.json").read_text()).items():
        sources.setdefault(phrase, {}).setdefault(locale, text)

missing = [f"{phrase} [{locale}]" for phrase in PHRASES for locale in LOCALES if not sources.get(phrase, {}).get(locale)]
if missing:
    sys.exit("untranslated: " + ", ".join(missing))
table = {locale: {phrase: sources[phrase][locale] for phrase in dict.fromkeys(PHRASES)} for locale in LOCALES}
(DESKTOP / "resources/strings.json").write_text(json.dumps(table, ensure_ascii=False, indent=1) + "\n")

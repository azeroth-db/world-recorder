#!/bin/sh
# Refuses an addon that calls a function Forever's own interface never calls. Forever loads the mainline
# family plus its camelot folder (blizzard_uipanels_game.toc), so the Classic-flavor folders do not count.
# Exceptions: GetLootSourceInfo, absent from Blizzard's code and proven by recorded loot; C_QuestLog.GetAllCompletedQuestIDs,
# in the client's generated API documentation, called by no Blizzard frame and proven by a recorded character;
# C_AuctionHouse.ReplicateItems, GetNumReplicateItems and GetReplicateItemInfo, in the client's generated API documentation
# (auctionhousedocumentation.lua), called by no Blizzard frame: the whole-house list no Blizzard window shows.
# GetScriptBucketThrottleLimits, in framescriptdocumentation.lua, called by no Blizzard frame and proven by the
# whole-house reads of October 15, which saved the limits it returned (500 ms per second, 1000 ms burst).
# C_TooltipInfo.GetTrainerService, reached by the trainer window's GameTooltip:SetTrainerService through
# blizzard_sharedxmlgame/tooltip/tooltipdatahandler.lua's accessor table, which a text search cannot follow.
# C_Item.GetItemIDByGUID, in the client's generated API documentation (itemdocumentation.lua), called by no Blizzard
# frame: the item id of a container opened from the bags, whose GUID GetLootSourceInfo returns.
set -eu
cd "$(dirname "$0")"
ui=${1:-../../data/native-client/wow_classic_beta/1.60.1.70291/enUS/interface/addons}
missing=0
if [ ! -d "$ui" ]; then
  # WOW-48 retired the full interface dump. A presentation-only release can reuse
  # the published package's API proof when its complete call set is identical.
  # Any API change still requires the native interface sources below.
  python3 - <<'PY'
from pathlib import Path
import hashlib
import json
import re
import zipfile

root = Path('../..')
release = json.loads((root / 'site/public/assets/world-recorder/release.json').read_text())
archive = root / 'site/public' / release['addon']['url'].lstrip('/')
if hashlib.sha256(archive.read_bytes()).hexdigest() != release['addon']['sha256']:
    raise SystemExit('Published addon does not match its release SHA-256')
pattern = r'(?:^|[^.:A-Za-z0-9_])((?:C_[A-Za-z]+\.)?[A-Z][A-Za-z0-9_]*)\('
def calls(text):
    return set(re.findall(pattern, re.sub(r'--[^\n]*', '', text), re.M))
current, published = set(), set()
for file in Path('WorldRecorder').glob('*.lua'):
    current |= calls(file.read_text())
with zipfile.ZipFile(archive) as package:
    for name in package.namelist():
        if name.startswith('WorldRecorder/') and name.count('/') == 1 and name.endswith('.lua'):
            published |= calls(package.read(name).decode())
if current != published:
    raise SystemExit('Native API calls changed; supply the Forever interface source directory to check-api.sh')
print(f"Native API call set unchanged from SHA-verified published addon {release['version']}")
PY
  exit $?
fi
# Check every handwritten Lua module; generated data has no native API calls.
# The two exact WorldRecorderLookup* globals below are our binding entrypoints,
# not client APIs (the text scanner also sees their function declarations).
for call in $(find WorldRecorder -maxdepth 1 -name '*.lua' -type f -exec sed 's/--.*//' {} + | grep -oE '(^|[^.:A-Za-z0-9_])(C_[A-Za-z]+\.)?[A-Z][A-Za-z0-9_]*\(' | sed -E 's/^[^A-Za-z]*//; s/\($//' | sort -u); do
  case $call in WorldRecorderLookupToggle|WorldRecorderLookupInspect) continue ;; esac
  case $call in GetLootSourceInfo|C_QuestLog.GetAllCompletedQuestIDs|C_AuctionHouse.ReplicateItems|C_AuctionHouse.GetNumReplicateItems|C_AuctionHouse.GetReplicateItemInfo|GetScriptBucketThrottleLimits|C_TooltipInfo.GetTrainerService|C_Item.GetItemIDByGUID) continue ;; esac
  if ! rg -q -F --glob '*.lua' --glob '!**/blizzard_apidocumentationgenerated/**' --glob '!**/vanilla/**' --glob '!**/tbc/**' --glob '!**/wrath/**' --glob '!**/cata/**' --glob '!**/mists/**' --glob '!**/classic/**' "$call(" "$ui"; then
    echo "not called by Forever's interface: $call"
    missing=1
  fi
done
exit $missing

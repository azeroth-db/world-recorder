#!/bin/sh
# Refuses an addon that calls a function Forever's own interface never calls. Forever loads the mainline
# family plus its camelot folder (blizzard_uipanels_game.toc), so the Classic-flavor folders do not count.
# Exceptions: GetLootSourceInfo, absent from Blizzard's code and proven by recorded loot; C_QuestLog.GetAllCompletedQuestIDs,
# in the client's generated API documentation, called by no Blizzard frame and proven by a recorded character.
set -eu
cd "$(dirname "$0")"
ui=${1:-../../data/native-client/wow_classic_beta/1.60.1.70291/enUS/interface/addons}
missing=0
for call in $(sed 's/--.*//' WorldRecorder/WorldRecorder.lua | grep -oE '(^|[^.:A-Za-z0-9_])(C_[A-Za-z]+\.)?[A-Z][A-Za-z0-9_]*\(' | sed -E 's/^[^A-Za-z]*//; s/\($//' | sort -u); do
  case $call in GetLootSourceInfo|C_QuestLog.GetAllCompletedQuestIDs) continue ;; esac
  if ! rg -q -F --glob '*.lua' --glob '!**/blizzard_apidocumentationgenerated/**' --glob '!**/vanilla/**' --glob '!**/tbc/**' --glob '!**/wrath/**' --glob '!**/cata/**' --glob '!**/mists/**' --glob '!**/classic/**' "$call(" "$ui"; then
    echo "not called by Forever's interface: $call"
    missing=1
  fi
done
exit $missing

#!/bin/sh
# Puts the committed client contract (site/public/assets/world-recorder/clients.json) on the live wowdb.wiki release without a
# release, as on the day WoW Forever leaves beta: installed programs read it within 15 minutes and install the addon into the
# client folder it names, and the site accepts that client's saves and realms at once. Commit and push the file first, so the
# next release serves the same bytes.
set -eu
cd "$(dirname "$0")"
file=../../site/public/assets/world-recorder/clients.json
python3 -I - "$file" <<'EOF'
import json, re, sys
c = json.load(open(sys.argv[1]))
assert c["clients"] and c["realms"], "the contract lists no client or no realm"
for client in c["clients"]:
    assert re.fullmatch(r"wow[a-z0-9_]*", client["product"]) and re.fullmatch(r"_[a-z0-9]+(?:_[a-z0-9]+)*_", client["flavor"]), client
    assert client["builds"] and all(re.fullmatch(r"\d+\.\d+\.", b) for b in client["builds"]), client
    assert client["interfaces"] and all(isinstance(n, int) and n > 0 for n in client["interfaces"]), client
for realm in c["realms"]:
    assert realm["name"] and realm["kind"] in ("pve", "pvp"), realm
EOF
git diff --quiet HEAD -- "$file" || { echo "commit $file first"; exit 1; }
live=/root/world-of-warcraft/current/site/public/assets/world-recorder/clients.json
scp -q "$file" "devmeme:$live.next"
ssh devmeme "mv '$live.next' '$live'"
want=$(shasum -a 256 "$file" | cut -d' ' -f1)
got=$(curl -fsS https://wowdb.wiki/api/contrib/clients | shasum -a 256 | cut -d' ' -f1)
[ "$want" = "$got" ] || { echo "wowdb.wiki serves another contract ($got)"; exit 1; }
echo "wowdb.wiki serves the contract $want"

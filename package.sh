#!/bin/sh
# Stage a complete addon, leaving the source checkout and existing player saves alone.
# No production site build, client census, account or uploader is needed for this projection.
set -eu
caller=$(pwd)
cd "$(dirname "$0")"
root=$(cd ../.. && pwd)
version=$(sed -n 's/^## Version: //p' WorldRecorder/WorldRecorder.toc)
[ -n "$version" ] || { echo 'World Recorder has no package version' >&2; exit 1; }
mode=release
candidate=
if [ "$#" -gt 0 ]; then
  [ "$#" -eq 2 ] && [ "$1" = '--candidate' ] || { echo 'Usage: package.sh [--candidate <pack/.work/.../WorldRecorder-<version>-candidate.zip>]' >&2; exit 1; }
  mode=candidate
  candidate=$2
fi
archive=$(python3 - "$root" "$version" "$mode" "$candidate" "$caller" <<'PYMODE'
from pathlib import Path
import json
import re
import sys
root, version, mode, candidate, caller = Path(sys.argv[1]), *sys.argv[2:]
if not re.fullmatch(r'[0-9]+(?:\.[0-9]+){2}', version):
    raise SystemExit('World Recorder package version must be major.minor.patch')
if mode == 'candidate':
    target = (Path(caller) / candidate).resolve()
    if not target.is_relative_to(root / '.work') or target.name != f'WorldRecorder-{version}-candidate.zip':
        raise SystemExit('Candidate must be named WorldRecorder-<source-version>-candidate.zip inside the pack .work folder')
else:
    published = json.loads((root / 'site/public/assets/world-recorder/release.json').read_text(encoding='utf-8'))['version']
    if tuple(map(int, version.split('.'))) <= tuple(map(int, published.split('.'))):
        raise SystemExit(f'Refusing to replace published World Recorder {published}; use --candidate for isolated acceptance, then the normal versioned release workflow')
    target = root / 'tools/world-recorder/dist' / f'WorldRecorder-{version}.zip'
print(target)
PYMODE
)
./check-api.sh
data="$root/.work/world-recorder/LookupData"
python3 "$root/pipeline/package_recorder_lookup.py" --output "$data"
python3 "$root/pipeline/package_recorder_lookup.py" --output "$data" --check
mkdir -p "$root/.work/world-recorder" "$(dirname "$archive")"
stage=$(mktemp -d "$root/.work/world-recorder/package.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
cp -R WorldRecorder "$stage/WorldRecorder"
[ ! -e "$stage/WorldRecorder/LookupData" ] || { echo 'Unexpected source lookup data; regenerate in owned staging' >&2; exit 1; }
# The addon loads only the Lua files; manifest.json is the producer's own check input.
mkdir "$stage/WorldRecorder/LookupData"
cp "$data"/*.lua "$stage/WorldRecorder/LookupData/"
# A source-only installation can still inspect native IDs. Only a validated complete
# distribution adds the packaged data, immediately before its lookup consumer.
python3 - "$stage/WorldRecorder/WorldRecorder.toc" <<'PY'
from pathlib import Path
import sys
file = Path(sys.argv[1])
lines = file.read_text(encoding='utf-8').splitlines()
if lines.count('Lookup.lua') != 1 or any(line.startswith('LookupData/') for line in lines):
    raise SystemExit('Unexpected World Recorder lookup load order')
locales = ('enUS', 'deDE', 'esES', 'esMX', 'frFR', 'koKR', 'ptBR', 'ruRU', 'zhCN', 'zhTW')
at = lines.index('Lookup.lua')
# The client compiles a listed file before its GetLocale() guard runs; it skips a line gated to another text locale.
lines[at:at] = ['LookupData/Manifest.lua', 'LookupData/Casefold.lua', *(f'LookupData/{locale}.lua [AllowLoadTextLocale {locale}]' for locale in locales)]
file.write_text('\n'.join(lines) + '\n', encoding='utf-8')
PY
# Build the replacement fully before replacing an older archive; never leave a
# half-written file at the published package filename on failure.
python3 - "$stage" <<'PYZIP'
from pathlib import Path
import stat
import sys
import zipfile
root = Path(sys.argv[1])
with zipfile.ZipFile(root / 'WorldRecorder.zip', 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    for file in sorted((root / 'WorldRecorder').rglob('*')):
        if file.is_symlink():
            raise SystemExit(f'Unexpected package symlink: {file}')
        if file.is_file():
            info = zipfile.ZipInfo(file.relative_to(root).as_posix(), date_time=(1980, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = (stat.S_IFREG | 0o644) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, file.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
PYZIP
mv "$stage/WorldRecorder.zip" "$archive"
echo "$archive"

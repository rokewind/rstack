"""Install the build wrapper; optionally wrap tools and schedule macOS maintenance."""
import argparse
import json
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--shims', action='store_true')
parser.add_argument('--maintenance', action='store_true')
args = parser.parse_args()
if args.maintenance and sys.platform != 'darwin':
    parser.error('--maintenance requires macOS; otherwise schedule build-run --maintain yourself')
home = Path.home()
dest = home / '.local/share/build-guard'
bin_dir = home / '.local/bin'
cfg_path = home / '.config/build-guard.json'
for path in (dest, bin_dir, cfg_path.parent):
    path.mkdir(parents=True, exist_ok=True)
backup = dest / 'backups' / str(time.time_ns())
backup.mkdir(parents=True)
cfg = json.loads(cfg_path.read_text()) if cfg_path.exists() else {}
mapping = cfg.setdefault('binaries', {})
if args.shims:
    for name in ('go', 'cargo', 'npm', 'pnpm', 'terraform'):
        found = shutil.which(name)
        if found and Path(found).resolve() != dest / 'build_guard.py':
            original = Path(found)
            if original == bin_dir / name:
                saved = backup / 'original' / name
                saved.parent.mkdir(exist_ok=True)
                shutil.copy2(original, saved)
                original = saved
            mapping[name] = str(original)
if cfg_path.exists():
    shutil.copy2(cfg_path, backup / 'config.json')
cfg_path.write_text(json.dumps(cfg, indent=2) + '\n')
script = dest / 'build_guard.py'
if script.exists():
    shutil.copy2(script, backup / 'build_guard.py')
temp = dest / 'build_guard.py.new'
temp.write_text(Path(__file__).with_name('build_guard.py').read_text().replace(
    '#!/usr/bin/env python3', '#!' + sys.executable, 1))
temp.chmod(0o755)
temp.replace(script)
for name in ['build-run'] + (list(mapping) if args.shims else []):
    target = bin_dir / name
    if target.exists() or target.is_symlink():
        shutil.copy2(target, backup / name, follow_symlinks=False)
        target.unlink()
    target.symlink_to(script)
if args.maintenance:
    import os
    label = 'dev.toolkit.build-guard'
    plist = home / 'Library/LaunchAgents' / (label + '.plist')
    plist.parent.mkdir(parents=True, exist_ok=True)
    if plist.exists():
        shutil.copy2(plist, backup / plist.name)
    plist.write_bytes(plistlib.dumps(dict(Label=label, RunAtLoad=True,
        StartInterval=60, ProcessType='Background',
        ProgramArguments=[str(bin_dir / 'build-run'), '--maintain'])))
    domain = f'gui/{os.getuid()}'
    subprocess.run(['launchctl', 'bootout', domain + '/' + label],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    subprocess.run(['launchctl', 'bootstrap', domain, str(plist)], check=True)
print('Installed ~/.local/bin/build-run; backups:', backup)

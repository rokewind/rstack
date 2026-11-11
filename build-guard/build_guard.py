#!/usr/bin/env python3
"""Serialize builds, share caches, clean temporary outputs and protect disk space."""
import argparse
import contextlib
import fcntl
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time

GIB = 1024 ** 3
ROOT = Path.home() / '.local/state/build-guard'
CONFIG = Path.home() / '.config/build-guard.json'
DEFAULTS = dict(reserve_gib=5, buffer_gib=2, admission_gib=10,
                cache_limit_gib=8, poll_seconds=0.5, binaries={})


def config():
    cfg = DEFAULTS | (json.loads(CONFIG.read_text()) if CONFIG.exists() else {})
    if not (cfg['reserve_gib'] >= 5 and cfg['buffer_gib'] >= 2
            and cfg['admission_gib'] >= cfg['reserve_gib'] + cfg['buffer_gib']
            and 0 < cfg['poll_seconds'] <= 1):
        raise ValueError('Unsafe build guard thresholds')
    return cfg


def free_bytes():
    return shutil.disk_usage(ROOT).free


def initialize():
    ROOT.mkdir(parents=True, exist_ok=True, mode=0o700)
    for name in ('runs', 'cache'):
        (ROOT / name).mkdir(exist_ok=True)


def size(path):
    total = 0
    for base, dirs, files in os.walk(path):
        for name in files:
            try:
                total += (Path(base) / name).lstat().st_blocks * 512
            except FileNotFoundError:
                pass
    return total


def remove(path):
    if path.is_symlink():
        path.unlink()
    elif path.is_dir():
        shutil.rmtree(path)
    elif path.exists():
        path.unlink()


@contextlib.contextmanager
def lock(wait=True):
    with open(ROOT / 'build.lock', 'a+') as stream:
        warned = False
        while True:
            try:
                fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if not wait:
                    yield None
                    return
                if not warned:
                    print('build-run: waiting for the shared build slot', file=sys.stderr)
                    warned = True
                time.sleep(0.25)
        yield stream


def live_orphan():
    path = ROOT / 'active.json'
    if not path.exists():
        return False
    record = json.loads(path.read_text())
    if signal_group(record['pgid'], 0):
        return True
    path.unlink()
    return False


def cleanup(cfg):
    """Caller holds global lock. Never traverse user projects or Terraform state."""
    if live_orphan():
        raise RuntimeError('A previous build process group still exists; inspect active.json')
    for path in (ROOT / 'runs').iterdir():
        remove(path)
    cache = ROOT / 'cache'
    if size(cache) > cfg['cache_limit_gib'] * GIB or free_bytes() < cfg['admission_gib'] * GIB:
        for path in cache.iterdir():
            remove(path)


def nested():
    try:
        fd = int(os.environ['BUILD_GUARD_LOCK_FD'])
        s = os.fstat(fd)
        t = (ROOT / 'build.lock').stat()
        return (s.st_dev, s.st_ino) == (t.st_dev, t.st_ino)
    except (KeyError, ValueError, OSError):
        # Shells and build tools sometimes close inherited FDs; the live supervisor
        # still serializes and monitors their process group.
        try:
            a = json.loads((ROOT / 'active.json').read_text())
            return os.getpgrp() == a['pgid'] and os.environ.get('BUILD_GUARD_RUN') == a['run']
        except (OSError, ValueError, KeyError):
            return False


def environment(run, stream):
    env = os.environ.copy()
    cache = ROOT / 'cache'
    paths = {'GOCACHE': cache / 'go-build', 'npm_config_cache': cache / 'npm',
             'UV_CACHE_DIR': cache / 'uv', 'GOMODCACHE': Path.home() / 'go/pkg/mod',
             'TF_PLUGIN_CACHE_DIR': Path.home() / '.cache/terraform-plugin-cache',
             'TMPDIR': run / 'tmp', 'GOTMPDIR': run / 'tmp', 'BUILD_OUT': run / 'out'}
    for key, path in paths.items():
        path.mkdir(parents=True, exist_ok=True)
        env[key] = str(path)
    env['BUILD_GUARD_LOCK_FD'] = str(stream.fileno())
    env['BUILD_GUARD_RUN'] = str(run)
    return env


def signal_group(pgid, sig):
    try:
        os.killpg(pgid, sig)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        # Darwin can return EPERM for a group containing only unreaped zombies.
        # Do not suppress a genuine permissions failure for a living process.
        rows = subprocess.check_output(['/bin/ps', '-axo', 'pgid=,stat='], text=True)
        for row in rows.splitlines():
            fields = row.split()
            if len(fields) == 2 and int(fields[0]) == pgid and not fields[1].startswith('Z'):
                raise
        return False


def terminate(child):
    # Only the session created by this wrapper; never arbitrary host processes.
    if not signal_group(child.pid, signal.SIGTERM):
        child.poll()
        return
    try:
        child.wait(timeout=0.5)
    except subprocess.TimeoutExpired:
        pass
    signal_group(child.pid, signal.SIGKILL)
    child.wait()


def run(command, cfg):
    if nested():
        os.execvpe(command[0], command, os.environ)
    with lock() as stream:
        cleanup(cfg)
        if free_bytes() < cfg['admission_gib'] * GIB:
            print(f"build-run: refused; need {cfg['admission_gib']} GiB free", file=sys.stderr)
            return 75
        directory = Path(tempfile.mkdtemp(prefix='run-', dir=ROOT / 'runs'))
        child = None
        previous = {}
        try:
            env = environment(directory, stream)
            child = subprocess.Popen(command, env=env, start_new_session=True,
                                     pass_fds=(stream.fileno(),))
            (ROOT / 'active.json').write_text(json.dumps(dict(
                supervisor=os.getpid(), pgid=child.pid, run=str(directory),
                tool=Path(command[0]).name, started=time.time())))
            def interrupted(signum, frame):
                raise InterruptedError(signum)
            for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
                previous[sig] = signal.signal(sig, interrupted)
            while child.poll() is None:
                if free_bytes() < (cfg['reserve_gib'] + cfg['buffer_gib']) * GIB:
                    print('build-run: stopping build to protect the 5 GiB reserve', file=sys.stderr)
                    terminate(child)
                    return 75
                time.sleep(cfg['poll_seconds'])
            return child.returncode if child.returncode >= 0 else 128 - child.returncode
        except InterruptedError as exc:
            return 128 + exc.args[0]
        finally:
            for sig, handler in previous.items():
                signal.signal(sig, handler)
            if child is not None:
                # Also reap helpers left behind after the command finishes.
                terminate(child)
            (ROOT / 'active.json').unlink(missing_ok=True)
            remove(directory)


def shim(name, args, cfg):
    real = cfg['binaries'].get(name)
    if not real:
        raise RuntimeError(f'No real binary configured for {name}')
    commands = {
        'go': {'build', 'test', 'run', 'install', 'generate', 'get', 'mod', 'vet'},
        'cargo': {'build', 'test', 'run', 'check', 'clippy', 'install', 'fetch'},
        'npm': {'install', 'i', 'ci', 'rebuild', 'run', 'run-script', 'test', 'exec'},
        'pnpm': {'install', 'i', 'add', 'rebuild', 'run', 'test', 'exec', 'dlx', 'build'},
        'terraform': {'init', 'validate', 'plan', 'providers'},
    }
    subcommand = next((a for a in args if not a.startswith('-')), '')
    if subcommand in commands.get(name, set()):
        return run([real] + args, cfg)
    # In particular, never interrupt Terraform apply/destroy/state operations.
    os.execv(real, [real] + args)


def main():
    initialize()
    cfg = config()
    name = Path(sys.argv[0]).name
    if name not in ('build-run', 'build_guard.py'):
        return shim(name, sys.argv[1:], cfg)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--status', action='store_true')
    parser.add_argument('--maintain', action='store_true')
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.status:
        print(json.dumps(dict(free_gib=round(free_bytes()/GIB, 2),
              reserve_gib=cfg['reserve_gib'], stop_below_gib=cfg['reserve_gib']+cfg['buffer_gib'],
              start_above_gib=cfg['admission_gib'],
              active=json.loads((ROOT/'active.json').read_text()) if (ROOT/'active.json').exists() else None), indent=2))
        return 0
    if args.maintain:
        with lock(wait=False) as stream:
            if stream:
                cleanup(cfg)
        return 0
    command = args.command[1:] if args.command[:1] == ['--'] else args.command
    if not command:
        parser.error('supply a build command, --status or --maintain')
    return run(command, cfg)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, RuntimeError, ValueError) as exc:
        print(f'build-run: {exc}', file=sys.stderr)
        sys.exit(75)

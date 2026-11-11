import json
import multiprocessing
import os
from pathlib import Path
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

import build_guard as guard


def concurrent_run(root, output, label):
    guard.ROOT = Path(root)
    guard.initialize()
    script = ('import time; from pathlib import Path; '
              f'p=Path({str(output)!r}); '
              f'f=p.open("a"); f.write("{label}-start\\n"); f.flush(); '
              f'time.sleep(.25); f.write("{label}-end\\n"); f.close()')
    guard.run([sys.executable, '-c', script], guard.DEFAULTS)


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.old_root = guard.ROOT
        guard.ROOT = Path(self.tmp.name) / 'state'
        guard.initialize()
        self.cfg = guard.DEFAULTS | {'poll_seconds': .02}

    def tearDown(self):
        guard.ROOT = self.old_root
        self.tmp.cleanup()

    def test_refuses_before_launch_and_preserves_user_files(self):
        marker = Path(self.tmp.name) / 'never'
        with patch.object(guard, 'free_bytes', return_value=6 * guard.GIB):
            result = guard.run([sys.executable, '-c', f'open({str(marker)!r},"w")'], self.cfg)
        self.assertEqual(result, 75)
        self.assertFalse(marker.exists())

    def test_failure_cleans_temp_and_preserves_explicit_output(self):
        output = Path(self.tmp.name) / 'artifact'
        script = ('import os,sys; from pathlib import Path; '
                  'Path(os.environ["BUILD_OUT"],"temp").write_text("temporary"); '
                  f'Path({str(output)!r}).write_text(sys.argv[1]); sys.exit(23)')
        result = guard.run([sys.executable, '-c', script, 'literal $value with spaces'], self.cfg)
        self.assertEqual(result, 23)
        self.assertEqual(output.read_text(), 'literal $value with spaces')
        self.assertEqual(list((guard.ROOT / 'runs').iterdir()), [])
        self.assertFalse((guard.ROOT / 'active.json').exists())

    def test_stops_on_falling_space(self):
        readings = iter([20*guard.GIB, 20*guard.GIB, 6*guard.GIB])
        with patch.object(guard, 'free_bytes', side_effect=lambda: next(readings, 6*guard.GIB)):
            start = time.monotonic()
            result = guard.run([sys.executable, '-c', 'import time; time.sleep(30)'], self.cfg)
        self.assertEqual(result, 75)
        self.assertLess(time.monotonic() - start, 5)
        self.assertEqual(list((guard.ROOT / 'runs').iterdir()), [])

    def test_parallel_builds_are_serialized(self):
        output = Path(self.tmp.name) / 'order'
        children = [multiprocessing.Process(target=concurrent_run,
                    args=(str(guard.ROOT), str(output), label)) for label in ('a', 'b')]
        for child in children:
            child.start()
        for child in children:
            child.join(10)
            if child.is_alive():
                child.kill()
                self.fail('Lock deadlock')
            self.assertEqual(child.exitcode, 0)
        lines = output.read_text().splitlines()
        self.assertIn(lines, [['a-start', 'a-end', 'b-start', 'b-end'],
                             ['b-start', 'b-end', 'a-start', 'a-end']])

    def test_low_space_kills_descendants_ignoring_term(self):
        heartbeat = Path(self.tmp.name) / 'heartbeat'
        worker = ('import signal,time; from pathlib import Path; '
                  'signal.signal(signal.SIGTERM,signal.SIG_IGN); '
                  f'p=Path({str(heartbeat)!r}); '
                  '\nwhile True:\n p.write_text(str(time.time())); time.sleep(.02)')
        parent = ('import subprocess,sys,time; '
                  f'subprocess.Popen([sys.executable,"-c",{worker!r}]); time.sleep(30)')
        with patch.object(guard, 'free_bytes', side_effect=lambda:
                          (6 if heartbeat.exists() else 20) * guard.GIB):
            self.assertEqual(guard.run([sys.executable, '-c', parent], self.cfg), 75)
        first = heartbeat.read_text()
        time.sleep(.15)
        self.assertEqual(heartbeat.read_text(), first)

    def test_nested_run_does_not_deadlock(self):
        source = str(Path(guard.__file__).parent)
        script = (f'import sys;sys.path.insert(0,{source!r}); import build_guard as g; '
                  f'g.ROOT=g.Path({str(guard.ROOT)!r}); '
                  'g.run([sys.executable,"-c","print(123)"],g.DEFAULTS)')
        self.assertEqual(guard.run([sys.executable, '-c', script], self.cfg), 0)

    def test_maintenance_skips_active_lock(self):
        with guard.lock():
            with guard.lock(wait=False) as contender:
                self.assertIsNone(contender)


if __name__ == '__main__':
    unittest.main()

"""Isolated lifecycle test: EMILY_DEV_TEST_IMAGE=emily-development:local python3 ..."""
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
import uuid


@unittest.skipUnless(os.environ.get('EMILY_DEV_TEST_IMAGE'), 'set EMILY_DEV_TEST_IMAGE')
class SupervisionTests(unittest.TestCase):
    def docker(self, *args):
        return subprocess.check_output(['docker', *args], text=True).strip()

    def ctl(self, *args):
        return self.docker('exec', self.name, 'supervisorctl', '-c',
                           '/etc/supervisor/supervisord.conf', *args)

    def wait_for(self, predicate):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            try:
                result = predicate()
                if result:
                    return result
            except subprocess.CalledProcessError:
                pass
            time.sleep(0.2)
        self.fail('process lifecycle did not converge within 20 seconds')

    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        for directory in ('repo/bin', 'repo/var/log', 'state', 'ingress'):
            (root / directory).mkdir(parents=True)
        # A real child with an HTTP endpoint, but no project data or credentials.
        self.binary = root / 'repo/bin/jarvis-server'
        self.binary.write_text('''#!/usr/bin/python3
import http.server
import signal
import sys
signal.signal(signal.SIGUSR1, lambda *_: sys.exit(0))
http.server.ThreadingHTTPServer(('127.0.0.1', 18812), http.server.SimpleHTTPRequestHandler).serve_forever()
''')
        self.binary.chmod(0o755)
        self.name = 'emily-supervision-test-' + uuid.uuid4().hex[:8]
        self.addCleanup(subprocess.run, ['docker', 'rm', '-f', self.name],
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.docker('run', '-d', '--init', '--name', self.name,
                    '--user', f'{os.getuid()}:{os.getgid()}',
                    '-v', f'{root}/repo:/opt/jarvis',
                    '-v', f'{root}/state:/dev-state',
                    '-v', f'{root}/ingress:/run/emily-web',
                    os.environ['EMILY_DEV_TEST_IMAGE'])
        self.wait_for(lambda: 'RUNNING' in self.ctl('status', 'jarvis-server'))

    def test_exit_recovery_and_intentional_stop(self):
        for program in ('jarvis-server', 'ingress'):
            with self.subTest(program=program):
                old_pid = self.ctl('pid', program)
                self.docker('exec', self.name, 'kill', '-KILL', old_pid)
                self.wait_for(lambda: self.ctl('pid', program) not in ('0', old_pid))
                self.wait_for(lambda: self.docker(
                    'exec', self.name, 'curl', '--fail', '--silent', '--max-time', '1',
                    '--unix-socket', '/run/emily-web/web.sock', 'http://localhost/'))
        old_pid = self.ctl('pid', 'jarvis-server')
        self.docker('exec', self.name, 'kill', '-USR1', old_pid)
        self.wait_for(lambda: self.ctl('pid', 'jarvis-server') not in ('0', old_pid))
        # Explicit maintenance stop must remain stopped; deploy can start it again.
        self.ctl('stop', 'jarvis-server')
        time.sleep(2)
        with self.assertRaises(subprocess.CalledProcessError) as stopped:
            self.ctl('pid', 'jarvis-server')
        self.assertEqual(stopped.exception.returncode, 7)
        self.assertEqual(stopped.exception.output.strip(), '0')
        self.ctl('restart', 'jarvis-server')
        self.wait_for(lambda: self.ctl('pid', 'jarvis-server') != '0')
        # Restarting the container must also restore both processes and the socket.
        self.docker('restart', '-t', '35', self.name)
        self.wait_for(lambda: self.docker(
            'exec', self.name, 'curl', '--fail', '--silent', '--max-time', '1',
            '--unix-socket', '/run/emily-web/web.sock', 'http://localhost/'))


if __name__ == '__main__':
    unittest.main()

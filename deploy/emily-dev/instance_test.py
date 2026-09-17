import runpy
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
dev = runpy.run_path(str(ROOT/'scripts/emily-dev'))


class InstanceIsolationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_shared_directory_only_requires_okr_database(self):
        (self.root/'okr.db').touch()
        (self.root/'anything-already-in-okr').touch()
        dev['validate_okr_dir'](self.root)
        (self.root/'okr.db').unlink()
        with self.assertRaisesRegex(ValueError, 'contain okr.db'):
            dev['validate_okr_dir'](self.root)

    def test_mounts_product_data_instance_state_and_authoritative_git(self):
        shared = self.root/'shared'
        git_common = Path('/repo/main/.git')
        args = dev['container_args'](self.root, shared, self.root/'var/container',
                                     Path('/ingress'), Path('/bin/lark-cli'),
                                     Path('/lib/bytedcli'), Path('/lib/go'),
                                     git_common=git_common)
        self.assertEqual(args[args.index('--network') + 1], 'bridge')
        mounts = [args[i + 1] for i, a in enumerate(args) if a == '--mount']
        self.assertIn(f'type=bind,src={shared},dst=/opt/jarvis/data/okr', mounts)
        self.assertIn(f'type=bind,src={git_common},dst={git_common}', mounts)
        self.assertIn('type=bind,src=/bin/lark-cli,dst=/usr/local/bin/lark-cli,readonly', mounts)
        for forbidden in ('credentials', 'okr-chat/sessions', 'okr-chat/files', 'egress',
                          'docker.sock', 'auth.json', '/opt/jarvis/.git'):
            self.assertNotIn(forbidden, ' '.join(args))

    def test_resolves_real_linked_worktree_common_git_directory(self):
        repo = self.root/'repo'
        worktree = self.root/'worktree'
        subprocess.run(['git', 'init', '-q', repo], check=True)
        subprocess.run(['git', '-C', repo, 'config', 'user.email', 'test@example.test'], check=True)
        subprocess.run(['git', '-C', repo, 'config', 'user.name', 'Test'], check=True)
        (repo/'README.md').write_text('test\n')
        subprocess.run(['git', '-C', repo, 'add', 'README.md'], check=True)
        subprocess.run(['git', '-C', repo, 'commit', '-qm', 'initial'], check=True)
        subprocess.run(['git', '-C', repo, 'worktree', 'add', '-qb', 'development', worktree], check=True)

        self.assertEqual(dev['resolve_git_common_dir'](worktree), (repo/'.git').resolve())

    def test_image_does_not_disable_vcs_build_metadata(self):
        dockerfile = (ROOT/'deploy/emily-dev/Dockerfile').read_text()
        self.assertNotIn('buildvcs=false', dockerfile)

    def test_initialize_uses_defaults_and_refuses_to_overwrite_local_config(self):
        conf = self.root/'conf'
        conf.mkdir()
        shutil.copyfile(ROOT/'conf/config.yaml', conf/'config.yaml')
        dev['initialize'](self.root, 'https://example.test/dev/', 'developer@example.test')
        config = yaml.safe_load((conf/'config.runtime.yaml').read_text())
        self.assertEqual(config['auth']['principals'], ['developer@example.test'])
        self.assertFalse(config['capture']['enabled'])
        self.assertNotIn('sqlite', config)
        self.assertEqual(config['dailydigest']['git_author'], 'Emily development')
        module = yaml.safe_load((conf/'okr-module.runtime.yaml').read_text())
        self.assertEqual(module['chat'], {'enabled': True, 'runtime': 'local'})
        self.assertNotIn('feishu', module)
        self.assertEqual(module['identity']['token_dir'], '/dev-state/tokens')
        with self.assertRaisesRegex(ValueError, 'already exists'):
            dev['initialize'](self.root, 'https://other.test/', 'other')
        self.assertEqual(yaml.safe_load((conf/'config.runtime.yaml').read_text()), config)

    def test_live_test_helpers_require_an_explicit_instance(self):
        for name in ('okr-real-test-session', 'okr-real-readback', 'okr-real-acceptance'):
            with self.subTest(script=name):
                result = subprocess.run([str(ROOT/'scripts'/name)], env={'PATH': '/usr/bin:/bin'},
                                        cwd=self.root, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('OKR_REAL_', result.stderr)


if __name__ == '__main__':
    unittest.main()

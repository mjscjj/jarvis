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

    def test_shared_directory_accepts_live_sqlite_sidecars_but_not_private_state(self):
        (self.root/'okr.db').touch()
        (self.root/'okr.db-wal').touch()
        (self.root/'assets').mkdir()
        dev['validate_product_dir'](self.root)
        for name in ('feishu-tokens', 'directory-cache.json.user', 'agent-session', 'backups'):
            with self.subTest(name=name):
                private = self.root/name
                private.touch()
                with self.assertRaisesRegex(ValueError, 'private/runtime'):
                    dev['validate_product_dir'](self.root)
                private.unlink()
        (self.root/'assets'/'private-link').symlink_to('/tmp')
        with self.assertRaisesRegex(ValueError, 'must not link'):
            dev['validate_product_dir'](self.root)

    def test_mounts_only_product_data_programs_and_instance_state(self):
        shared = self.root/'shared'
        args = dev['container_args'](self.root, shared, self.root/'var/container',
                                     Path('/ingress'), Path('/bin/lark-cli'),
                                     Path('/lib/bytedcli'), Path('/lib/go'))
        self.assertEqual(args[args.index('--network') + 1], 'bridge')
        mounts = [args[i + 1] for i, a in enumerate(args) if a == '--mount']
        self.assertIn(f'type=bind,src={shared},dst=/opt/jarvis/data/okr', mounts)
        self.assertIn('type=bind,src=/bin/lark-cli,dst=/usr/local/bin/lark-cli,readonly', mounts)
        for forbidden in ('credentials', 'okr-chat/sessions', 'okr-chat/files', 'egress', 'docker.sock', 'auth.json'):
            self.assertNotIn(forbidden, ' '.join(args))
        (self.root/'data').mkdir()
        (self.root/'data/okr').symlink_to(shared)
        linked = dev['container_args'](self.root, shared, self.root/'var/container',
                                       Path('/ingress'), Path('/bin/lark-cli'),
                                       Path('/lib/bytedcli'), Path('/lib/go'))
        self.assertIn(f'type=bind,src={shared},dst={shared}', linked)

    def test_initialize_uses_defaults_and_refuses_to_overwrite_local_config(self):
        conf = self.root/'conf'
        conf.mkdir()
        shutil.copyfile(ROOT/'conf/config.yaml', conf/'config.yaml')
        dev['initialize'](self.root, 'https://example.test/dev/', 'developer@example.test')
        config = yaml.safe_load((conf/'config.development.yaml').read_text())
        self.assertEqual(config['auth']['principals'], ['developer@example.test'])
        self.assertEqual(config['extract']['principal_open_id'], '')
        self.assertEqual(config['card_approval']['relay_secret'], '')
        self.assertFalse(config['capture']['enabled'])
        self.assertEqual(config['sqlite']['path'], 'var/development.db')
        module = yaml.safe_load((conf/'okr-module.runtime.yaml').read_text())
        self.assertEqual(module['chat'], {'enabled': True, 'runtime': 'local'})
        self.assertNotIn('feishu', module)
        self.assertEqual(module['identity']['token_dir'], 'var/okr/feishu-tokens')
        with self.assertRaisesRegex(ValueError, 'already exists'):
            dev['initialize'](self.root, 'https://other.test/', 'other')
        self.assertEqual(yaml.safe_load((conf/'config.development.yaml').read_text()), config)

    def test_live_test_helpers_require_an_explicit_instance(self):
        for name in ('okr-real-test-session', 'okr-real-readback', 'okr-real-acceptance'):
            with self.subTest(script=name):
                result = subprocess.run([str(ROOT/'scripts'/name)], env={'PATH': '/usr/bin:/bin'},
                                        cwd=self.root, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('OKR_REAL_', result.stderr)


if __name__ == '__main__':
    unittest.main()

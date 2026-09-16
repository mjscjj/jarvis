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

    def test_shared_directory_preserves_known_private_entries(self):
        (self.root/'okr.db').touch()
        (self.root/'okr.db-wal').touch()
        (self.root/'assets').mkdir()
        dev['validate_product_dir'](self.root)
        for name, kind in dev['PRIVATE_ENTRIES'].items():
            path = self.root/name
            path.mkdir() if kind == 'directory' else path.write_text('private sentinel')
        dev['validate_product_dir'](self.root)
        self.assertEqual((self.root/'agent-session').read_text(), 'private sentinel')
        unknown = self.root/'unknown-private-state'
        unknown.touch()
        with self.assertRaisesRegex(ValueError, 'unclassified'):
            dev['validate_product_dir'](self.root)
        unknown.unlink()
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

    def test_private_overmounts_do_not_read_or_move_host_state(self):
        shared = self.root/'shared'
        shared.mkdir()
        (shared/'okr.db').touch()
        for name, kind in dev['PRIVATE_ENTRIES'].items():
            path = shared/name
            path.mkdir() if kind == 'directory' else path.write_text('host secret')
        state = self.root/'var/container'
        args = dev['container_args'](self.root, shared, state, Path('/ingress'),
                                     Path('/bin/lark-cli'), Path('/lib/bytedcli'), Path('/lib/go'))
        for name in dev['PRIVATE_ENTRIES']:
            self.assertIn(f'type=bind,src={state}/okr-private/{name},dst=/opt/jarvis/data/okr/{name}', args)
        self.assertEqual((shared/'agent-session').read_text(), 'host secret')
        self.assertFalse(state.exists())

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

import tempfile
import unittest
from pathlib import Path

from jarvis_mem0.settings import Settings


VALID_CONFIG = """
mem0:
  owner_id: owner
  qdrant_host: 127.0.0.1
  qdrant_port: 6333
  collection: jarvis_memories
  state_dir: var/mem0
  embedding_model: text-embedding-3-small
  embedding_dims: 1536
model:
  base_url: https://example.test/v1
  api_key: plaintext-key
  model: model-name
  is_reasoning_model: true
"""


class SettingsTest(unittest.TestCase):
    def test_loads_valid_config(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.yaml"
            path.write_text(VALID_CONFIG, encoding="utf-8")
            settings = Settings.from_file(path)
        self.assertEqual(settings.owner_id, "owner")
        self.assertEqual(settings.qdrant_port, 6333)
        self.assertEqual(settings.embedding_dims, 1536)
        self.assertTrue(settings.model_is_reasoning)

    def test_rejects_missing_model_key(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.yaml"
            path.write_text(VALID_CONFIG.replace("  api_key: plaintext-key\n", ""), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "api_key"):
                Settings.from_file(path)


if __name__ == "__main__":
    unittest.main()

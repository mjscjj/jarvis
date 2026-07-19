import tempfile
import unittest
from pathlib import Path

from jarvis_mem0.backend import Mem0Backend, build_mem0_config
from jarvis_mem0.settings import Settings


class FakeVectorClient:
    def get_collections(self):
        return []


class FakeMemory:
    def __init__(self):
        self.add_kwargs = None
        self.search_kwargs = None
        self.vector_store = type("VectorStore", (), {"client": FakeVectorClient()})()

    def add(self, messages, **kwargs):
        self.add_kwargs = kwargs
        return {"results": [{"memory": messages}]}

    def search(self, query, **kwargs):
        self.search_kwargs = kwargs
        return {"results": [{"memory": query}]}

    def delete(self, memory_id):
        return None

    def delete_all(self, **kwargs):
        self.delete_all_kwargs = kwargs


class BackendTest(unittest.TestCase):
    def test_owner_scope_cannot_be_overridden(self):
        memory = FakeMemory()
        backend = Mem0Backend(memory, "owner")
        backend.add("fact", {"source": "message"}, True)
        backend.search("query", {"user_id": "attacker"}, 20, 0.1, False)
        backend.delete_all()

        self.assertEqual(memory.add_kwargs["user_id"], "owner")
        self.assertEqual(memory.search_kwargs["filters"]["user_id"], "owner")
        self.assertEqual(memory.delete_all_kwargs["user_id"], "owner")

    def test_build_config_uses_server_qdrant(self):
        with tempfile.TemporaryDirectory() as directory:
            settings = Settings(
                owner_id="owner",
                qdrant_host="127.0.0.1",
                qdrant_port=6333,
                collection="jarvis_memories",
                state_dir=Path(directory),
                model_base_url="https://example.test/v1",
                model_api_key="key",
                model_name="model",
                model_is_reasoning=True,
                embedding_model="embed",
                embedding_dims=1536,
            )
            config = build_mem0_config(settings)

        vector = config["vector_store"]["config"]
        self.assertEqual(vector["host"], "127.0.0.1")
        self.assertEqual(vector["port"], 6333)
        self.assertNotIn("path", vector)
        self.assertEqual(config["embedder"]["config"]["embedding_dims"], 1536)
        self.assertTrue(config["llm"]["config"]["is_reasoning_model"])
        self.assertNotIn("temperature", config["llm"]["config"])


if __name__ == "__main__":
    unittest.main()

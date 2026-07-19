import unittest

from fastapi.testclient import TestClient

from jarvis_mem0.app import create_app


class FakeBackend:
    def __init__(self):
        self.added = []
        self.filters = None

    def add(self, messages, metadata, infer):
        self.added.append((messages, metadata, infer))
        return [{"id": "m1", "memory": "fact", "event": "ADD"}]

    def search(self, query, filters, top_k, threshold, rerank):
        self.filters = filters
        return [{"id": "m1", "memory": query, "score": 0.9}]

    def delete(self, memory_id):
        return None

    def delete_all(self):
        return None

    def health(self):
        return None


class AppTest(unittest.TestCase):
    def setUp(self):
        self.backend = FakeBackend()
        self.client = TestClient(create_app(backend=self.backend))

    def test_add(self):
        response = self.client.post(
            "/memories",
            json={"messages": "Alice: do it", "metadata": {"chat_id": "oc_a"}, "infer": True},
        )
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["results"][0]["id"], "m1")
        self.assertEqual(self.backend.added[0][0], "Alice: do it")

    def test_search(self):
        response = self.client.post(
            "/memories/search",
            json={"query": "deadline", "top_k": 5, "threshold": 0.2},
        )
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["results"][0]["memory"], "deadline")

    def test_health(self):
        response = self.client.get("/health")
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json(), {"status": "ok"})

    def test_rejects_unknown_fields(self):
        response = self.client.post("/memories", json={"messages": "x", "unexpected": True})
        self.assertEqual(response.status_code, 422)


if __name__ == "__main__":
    unittest.main()

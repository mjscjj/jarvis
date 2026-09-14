import json, runpy, sqlite3, unittest
from pathlib import Path
m=runpy.run_path(str(Path(__file__).with_name('okr-email-migrate')))
class MigrationTest(unittest.TestCase):
 def setUp(self):
  self.db=sqlite3.connect(':memory:');self.db.row_factory=sqlite3.Row
  self.db.execute("CREATE TABLE okr_workspace_kr_owner(kr_id TEXT, person_id INTEGER, owner_key TEXT, open_id TEXT, name TEXT, sort_order INTEGER, PRIMARY KEY(kr_id,person_id))")
  self.db.execute("INSERT INTO okr_workspace_kr_owner VALUES ('kr',1,'old','ou_a','Alice',0)")
  self.db.execute("CREATE TABLE okr_workspace_comment(id TEXT,mentions TEXT,content TEXT,author_open_id TEXT)")
  self.db.execute("INSERT INTO okr_workspace_comment VALUES ('c',?, '@Alice hello','ou_author')",(json.dumps([{'open_id':'ou_a','name':'Alice'}]),))
  self.e={'people':{'ou_a':{'email':'alice@EXAMPLE.TEST','name':'Alice','source_open_id':'ou_a','source_app_id':'cli_old'}}}
  self.db.commit()
 def tearDown(self):
  self.db.close()
 def test_idempotence_and_old_writer_guard(self):
  c=m['migrate'](self.db,self.e);self.assertEqual(c['okr_workspace_kr_owner'],1)
  row=self.db.execute('SELECT * FROM okr_workspace_kr_owner').fetchone();self.assertEqual(row['email'],'alice@example.test')
  comment=self.db.execute('SELECT * FROM okr_workspace_comment').fetchone();self.assertEqual(comment['author_open_id'],'ou_author');self.assertEqual(comment['content'],'@Alice hello')
  self.assertEqual(json.loads(comment['mentions'])[0],{'email':'alice@example.test','name':'Alice'})
  self.assertTrue(all(v==0 for v in m['migrate'](self.db,self.e).values()))
  with self.assertRaises(sqlite3.IntegrityError):self.db.execute("UPDATE okr_workspace_kr_owner SET open_id='ou_new'")
 def test_conflict_and_missing_mapping_fail_without_partial_commit(self):
  self.db.execute("INSERT INTO okr_workspace_kr_owner VALUES ('kr',2,'old2','ou_b','Bob',1)");self.db.commit()
  with self.assertRaises(ValueError):m['migrate'](self.db,self.e)
  self.e['people']['ou_b']={**self.e['people']['ou_a'],'source_open_id':'ou_b','name':'Bob'}
  self.db.execute('BEGIN')
  with self.assertRaises(ValueError):m['migrate'](self.db,self.e)
  self.db.rollback()
  self.assertEqual(self.db.execute('SELECT COUNT(*) FROM okr_workspace_kr_owner').fetchone()[0],2)
  self.assertNotIn('email',{r[1] for r in self.db.execute('PRAGMA table_info(okr_workspace_kr_owner)')})
if __name__=='__main__':unittest.main()

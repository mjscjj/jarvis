from pathlib import Path
import json, subprocess, xml.etree.ElementTree as E
p=Path('docs/summery/task-42-prd-review')
sections=json.loads(p.joinpath('section-patches.json').read_text())
doc='XTKAdu2JgoRsmdxg2rnmRqYiyl2'
def run(args,path):
 r=subprocess.run(['lark-cli',*args,'--as','user','--format','json'],text=True,capture_output=True)
 path.write_text(r.stdout if r.returncode==0 else json.dumps({'exit_code':r.returncode,'stdout':r.stdout,'stderr':r.stderr},ensure_ascii=False))
 if r.returncode: raise RuntimeError('exit='+str(r.returncode)+' stdout='+r.stdout+' stderr='+r.stderr)
 d=json.loads(r.stdout)
 if not d.get('ok'): raise RuntimeError(r.stdout)
 return d
current=run(['docs','+fetch','--doc',doc,'--detail','full'],p/'evidence/report-update-start.json')
for i,(heading,content) in enumerate(sections.items(),1):
 root=E.fromstring('<r>'+current['data']['document']['content']+'</r>')
 expected_text=''.join(E.fromstring('<r>'+content+'</r>').itertext())
 if expected_text in ''.join(root.itertext()):
  print('Already verified',heading,flush=True)
  continue
 children=list(root)
 begin=next(j for j,x in enumerate(children) if x.tag=='h1' and ''.join(x.itertext())==heading)
 end=next((j for j in range(begin+1,len(children)) if children[j].tag=='h1'),len(children))
 first=children[begin].get('id')
 lastnode=children[end] if end < len(children) else children[end-1]
 last=lastnode.get('id')
 if not last:
  identified=[x.get('id') for x in lastnode.iter() if x.get('id')]
  last=identified[-1]
 xml='<h1>'+heading+'</h1>'+content
 if end < len(children): xml += '<h1>'+''.join(children[end].itertext())+'</h1>'
 fp=p/f'section-{i}.xml';fp.write_text(xml)
 result=run(['docs','+update','--doc',doc,'--command','block_replace','--start-block-id',first,'--end-block-id',last,'--content','@./'+str(fp)],p/f'evidence/update-{i}.json')
 if result['data'].get('result')!='success' or result['data'].get('warnings'):
  raise RuntimeError(json.dumps(result,ensure_ascii=False))
 current=run(['docs','+fetch','--doc',doc,'--detail','full'],p/f'evidence/verify-{i}.json')
 actual=''.join(E.fromstring('<r>'+current['data']['document']['content']+'</r>').itertext())
 for node in E.fromstring('<r>'+xml+'</r>'):
  expected=''.join(node.itertext())
  if expected and expected not in actual: raise RuntimeError('readback mismatch: '+heading)
 print('Verified section',i,heading,flush=True)
p.joinpath('evidence/report-after.json').write_text(json.dumps(current,ensure_ascii=False,indent=2))

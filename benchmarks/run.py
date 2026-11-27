"""No-key benchmark: capture real catalogs, measure discovery and tokenized payloads.
This does NOT measure a model's reasoning, answer accuracy, or billed API usage.
"""
import argparse
import datetime
import hashlib
import importlib.metadata
import json
import pathlib
import statistics
import tempfile
import time

import tiktoken
from harness import BENCH,CLI,ROOT,Peer,brief,cli,compact,functions,replay
from metrics import aggregate,selection
from tasks import TASKS
from model_runner import ROUTER_TOOLS,SHELL_TOOLS,SYSTEM

def capture(config,destination):
    cfg=json.loads(config.read_text(encoding='utf-8'));catalogs={};probes=[]
    with Peer(config) as peer:
        for name in sorted(cfg['providers']):
            start=time.perf_counter();native=peer.request(action='catalog',provider=name);other=cli(config,['list',name])
            row={'provider':name,'native_ok':native['ok'],'cli_ok':other['response']['ok'],'elapsed_ms':round(1000*(time.perf_counter()-start),2)}
            if native['ok'] and other['response']['ok']:
                catalogs[name]=native['data'];row['tool_count']=len(native['data']);row['catalog_ids_equal']=set(c['id'] for c in native['data'])==set(c['id'] for c in other['response']['data'])
            else:row['native_error']=native.get('error');row['cli_error']=other['response'].get('error')
            probes.append(row);print('capture',name,row,flush=True)
    snapshot={'captured_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'config_sha256':hashlib.sha256(config.read_bytes()).hexdigest(),'providers':cfg['providers'],'catalogs':catalogs,'probes':probes,'specs':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in (BENCH/'specs').iterdir() if p.is_file()}}
    destination.write_text(json.dumps(snapshot,indent=2)+'\n',encoding='utf-8')
    if any(not r.get('catalog_ids_equal') for r in probes):raise RuntimeError('provider capture/parity failed; inspect the saved snapshot')
    return snapshot

def run(snapshot,destination):
    enc=tiktoken.get_encoding('o200k_base')
    def tokens(v):return len(enc.encode(v if isinstance(v,str) else compact(v)))
    all_caps=[c for caps in snapshot['catalogs'].values() for c in caps];by_id={c['id']:c for c in all_caps}
    for t in TASKS:
        if set(t['gold_tools'])-by_id.keys():raise RuntimeError('gold labels missing from captured catalog: '+t['id'])
    tools,_=functions(all_caps);native_tokens=tokens(tools)
    native_boot=tokens(SYSTEM)+native_tokens
    progressive_boot=tokens(SYSTEM)+tokens(ROUTER_TOOLS)+tokens('Use discover and describe to inspect MCP capabilities. Provider names: '+', '.join(sorted(snapshot['catalogs']))+'.')
    skill_text=(ROOT/'skills/skill-cli/SKILL.md').read_text(encoding='utf-8')
    rows=[]
    with tempfile.TemporaryDirectory(prefix='skill-cli-retrieval-') as temp,replay(snapshot,temp) as config,Peer(config) as peer:
        provider_listing=cli(config,['list'])['response'];cli_boot=tokens(SYSTEM)+tokens(skill_text)+tokens(SHELL_TOOLS)+tokens(provider_listing)
        for task in TASKS:
            # Native eager always exposes the full catalog; these are CONTEXT
            # relevance/coverage metrics, not predictions of what a model selects.
            rows.append(dict(task_id=task['id'],group=task['group'],arm='native_eager',selected=[c['id'] for c in all_caps],visible_component_tokens=native_boot+tokens(task['prompt']),**selection(by_id.keys(),task['gold_tools'])))
            query=task['query'];native=peer.request(action='search',query=query)
            prescribed=cli(config,['search',query]);literal=cli(config,['search',task['prompt']])
            if not native['ok'] or not prescribed['response']['ok'] or not literal['response']['ok']:raise RuntimeError('local replay discovery failed')
            n_ids={c['id'] for c in native['data']};c_ids={c['id'] for c in prescribed['response']['data']}
            if n_ids!=c_ids:raise RuntimeError('native/CLI search parity mismatch: '+task['id'])
            for arm,response,query_text in [('native_progressive_prescribed',native,query),('cli_prescribed',prescribed['response'],query),('cli_literal',literal['response'],task['prompt'])]:
                ids=[c['id'] for c in response['data']]
                boot=progressive_boot if arm.startswith('native_progressive') else cli_boot
                # Component tokens include instructions/provider list for CLI and
                # request+response payloads. They exclude model-specific framing.
                rows.append(dict(task_id=task['id'],group=task['group'],arm=arm,query=query_text,selected=ids,visible_component_tokens=boot+tokens(task['prompt'])+tokens({'action':'search','query':query_text})+tokens(response),**selection(ids,task['gold_tools'])))
            fallback=literal['response'];extra=0
            if not fallback['data']:
                fallback={'ok':True,'data':[]}
                for provider in snapshot['catalogs']:
                    listed=cli(config,['list',provider])['response'];fallback['data'].extend(listed['data']);extra+=tokens(listed)
            ids=[c['id'] for c in fallback['data']]
            rows.append(dict(task_id=task['id'],group=task['group'],arm='cli_literal_list_fallback',selected=ids,visible_component_tokens=cli_boot+tokens(task['prompt'])+tokens(literal['response'])+tokens({'action':'search','query':task['prompt']})+extra,**selection(ids,task['gold_tools'])))
    summary={}
    for arm in sorted({r['arm'] for r in rows}):
        subset=[r for r in rows if r['arm']==arm];summary[arm]=dict(**aggregate(subset),mean_visible_component_tokens=statistics.mean(r['visible_component_tokens'] for r in subset),by_group={group:aggregate([r for r in subset if r['group']==group]) for group in sorted({r['group'] for r in subset})})
    result={'measurement':'retrieval_context_relevance_and_tokenized_components_not_LLM_accuracy_or_billing','tokenizer':'o200k_base','tasks':len(TASKS),'catalog_size':len(all_caps),'skill_instruction_tokens':tokens(skill_text),'native_tool_schema_tokens':native_tokens,'cli_instruction_and_provider_list_tokens':cli_boot,'native_progressive_boot_tokens':progressive_boot,'component_counting_limits':'Includes system/skill/router or shell definitions, user prompt, prescribed search request and response; excludes tool framing, repeated conversation billing, describe followups and final model output. Native eager includes complete tool schemas. No model inference is included.','summaries':summary,'rows':rows,'synthetic_catalog_scaling':[{'factor':factor,'tools':len(all_caps)*factor,'schema_tokens':tokens([dict(t, function=dict(t['function'],name=t['function']['name']+'_copy_'+str(i))) for i in range(factor) for t in tools])} for factor in [1,10,100]]}
    result['package_versions']={p:importlib.metadata.version(p) for p in ['tiktoken','requests','PyYAML']}
    result['catalog_sha256']=hashlib.sha256(compact(snapshot['catalogs']).encode()).hexdigest()
    result['tasks_sha256']=hashlib.sha256(compact(TASKS).encode()).hexdigest()
    destination.write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8');print(json.dumps({k:v for k,v in result.items() if k not in ['rows','summaries']},indent=2));print(json.dumps(summary,indent=2))
    return result

def main():
    p=argparse.ArgumentParser();p.add_argument('--capture',action='store_true');p.add_argument('--config',type=pathlib.Path,default=BENCH/'public.json');p.add_argument('--snapshot',type=pathlib.Path,default=BENCH/'artifacts/catalog.json');p.add_argument('--output',type=pathlib.Path,default=BENCH/'artifacts/retrieval.json');a=p.parse_args()
    a.snapshot.parent.mkdir(parents=True,exist_ok=True);a.output.parent.mkdir(parents=True,exist_ok=True)
    snapshot=capture(a.config.resolve(),a.snapshot) if a.capture else json.loads(a.snapshot.read_text(encoding='utf-8'))
    run(snapshot,a.output)

if __name__=='__main__':main()

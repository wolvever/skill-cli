"""Ten-server frozen replay: lexical retrieval, transport parity and token components.
No model inference, tool execution, embedding service or upstream calls in run().
"""
import argparse
import copy
import datetime
import hashlib
import importlib.metadata
import json
import statistics
import tempfile
import time

import tiktoken
from harness import BENCH,ROOT,Peer,cli,compact,functions,replay
from metrics import aggregate,selection
from model_runner import SYSTEM,SHELL_TOOLS
from tasks_10 import TASKS

def evaluate(ids,gold):
    result=selection(ids,gold)
    result['reciprocal_rank']=next((1/(i+1) for i,item in enumerate(ids) if item in gold),0.) if gold else None
    return result

def run(snapshot,destination):
    enc=tiktoken.get_encoding('o200k_base')
    def tokens(value):return len(enc.encode(value if isinstance(value,str) else compact(value)))
    caps=[c for group in snapshot['catalogs'].values() for c in group];by_id={c['id']:c for c in caps}
    if len(by_id)!=len(caps):raise ValueError('duplicate capability IDs')
    for task in TASKS:
        if set(task['gold_tools'])-by_id.keys():raise ValueError('missing gold labels: '+task['id'])
    bare=copy.deepcopy(snapshot);bare['search_metadata']={};bare['providers']={}
    skill=(ROOT/'skills/skill-cli/SKILL.md').read_text(encoding='utf-8');native_schemas=tokens(functions(caps)[0])
    rows=[];latency=[];parity=0;listing={}
    # Both native controls use the actual SDK over the same local MCP replay.
    with tempfile.TemporaryDirectory(prefix='skill-cli-ranked-') as tmp, tempfile.TemporaryDirectory(prefix='skill-cli-bare-') as tmp2,replay(snapshot,tmp) as cfg,replay(bare,tmp2) as bare_cfg,Peer(cfg) as native,Peer(bare_cfg) as native_bare:
        providers=cli(cfg,['list'])['response']
        bootstrap=tokens(SYSTEM)+tokens(SHELL_TOOLS)+tokens(skill)+tokens(providers)
        for name in sorted(snapshot['catalogs']):
            short=cli(cfg,['list',name])['response'];full=cli(cfg,['list',name,'--full'])['response']
            assert short['ok'] and full['ok']
            listing[name]=dict(tools=len(short['data']),compact_tokens=tokens(short),full_tokens=tokens(full))
        for i,task in enumerate(TASKS):
            queries={'literal':task['prompt'],'prescribed':task['query']}
            for style,query in queries.items():
                start=time.perf_counter();ranked=cli(cfg,['search',query,'--limit','0'])['response'];latency.append(1000*(time.perf_counter()-start))
                substring=cli(cfg,['search',query,'--match','substring','--limit','0'])['response']
                control=native.request(action='search',query=query,match='ranked')
                without_metadata=native_bare.request(action='search',query=query,match='ranked')
                if not all(x['ok'] for x in [ranked,substring,control,without_metadata]):raise RuntimeError('replay failed')
                if [c['id'] for c in ranked['data']] != [c['id'] for c in control['data']]:raise RuntimeError('native ranked parity failed: '+task['id'])
                parity+=1
                for arm,response in [('cli_ranked',ranked),('cli_substring',substring),('native_ranked',control),('native_ranked_no_provider_metadata',without_metadata)]:
                    for k in [1,3,5,10]:
                        selected=response['data'][:k];ids=[c['id'] for c in selected]
                        request={'action':'search','query':query,'limit':k}
                        # Native compact control uses exactly CLI's output projection
                        # to avoid calling description truncation a transport gain.
                        visible={'ok':True,'data':ranked['data'][:k]} if arm=='native_ranked' else {'ok':True,'data':selected}
                        row=dict(task_id=task['id'],group=task['group'],style=style,arm=arm,k=k,selected=ids,response_tokens=tokens(visible),**evaluate(ids,task['gold_tools']))
                        if arm.startswith('cli_'):
                            row['visible_component_tokens']=bootstrap+tokens(task['prompt'])+tokens(request)+tokens(visible)
                            if ids:row['plus_top1_describe_tokens']=row['visible_component_tokens']+tokens({'action':'describe','target':ids[0]})+tokens({'ok':True,'data':by_id[ids[0]]})
                        rows.append(row)
            if (i+1)%10==0:print('evaluated',i+1,'/',len(TASKS),flush=True)
    summaries={}
    for arm in ['cli_ranked','cli_substring','native_ranked','native_ranked_no_provider_metadata']:
        for style in ['literal','prescribed']:
            for k in [1,3,5,10]:
                group=[r for r in rows if r['arm']==arm and r['style']==style and r['k']==k]
                positive=[r for r in group if r['reciprocal_rank'] is not None]
                summaries[f'{arm}/{style}@{k}']=dict(**aggregate(group),mrr=statistics.mean(r['reciprocal_rank'] for r in positive),mean_response_tokens=statistics.mean(r['response_tokens'] for r in group),mean_visible_component_tokens=statistics.mean(r['visible_component_tokens'] for r in group) if arm.startswith('cli_') else None,by_group={g:aggregate([r for r in group if r['group']==g]) for g in sorted({r['group'] for r in group})})
    eager=[dict(**evaluate(list(by_id),task['gold_tools'])) for task in TASKS]
    ordered=sorted(latency)
    result=dict(captured_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),measurement='retrieval_context_candidates_not_model_accuracy_or_billed_usage',catalog_size=len(caps),mcp_servers=sum(p['type']=='mcp' for p in snapshot['providers'].values()),openapi_providers=sum(p['type']=='openapi' for p in snapshot['providers'].values()),tasks=len(TASKS),task_groups={g:sum(t['group']==g for t in TASKS) for g in sorted({t['group'] for t in TASKS})},catalog_sha256=hashlib.sha256(compact(snapshot['catalogs']).encode()).hexdigest(),tasks_sha256=hashlib.sha256(compact(TASKS).encode()).hexdigest(),native_cli_ranked_parity_queries=parity,tokenizer='o200k_base',skill_tokens=tokens(skill),cli_bootstrap_tokens=bootstrap,eager_schema_tokens=native_schemas,eager_context=aggregate(eager),eager_mean_visible_component_tokens=statistics.mean(tokens(SYSTEM)+tokens(t['prompt'])+native_schemas for t in TASKS),listing=listing,local_replay_cli_search_ms=dict(p50=statistics.median(latency),p95=ordered[int(.95*(len(ordered)-1))],includes='CLI startup, config, 12 MCP replay connections/catalog transfers, index and search; not upstream latency'),actual_model_usage=None,usd=None,limits='Author-labeled development corpus, not held-out. Precision denominator is returned candidates, not fixed k. Native ranked shares implementation with CLI; parity controls transport, not independent ranking correctness. Metadata ablation also uses the native SDK; no-provider arm retains tool descriptions and argument names. No paid or local LLM was run. Token components exclude chat framing, repeated conversation and final answer; top1 describe counted separately per row. Native ranked output uses the same compact CLI projection.',summaries=summaries,rows=rows)
    result['package_versions']={p:importlib.metadata.version(p) for p in ['tiktoken','requests','PyYAML']}
    result['source_sha256']={path:hashlib.sha256((ROOT/path).read_bytes()).hexdigest() for path in ['internal/capability/search.go','skills/skill-cli/SKILL.md','benchmarks/tasks_10.py']}
    destination.write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8',newline='\n')
    print(json.dumps({key:value for key,value in result.items() if key not in ['rows','summaries']},indent=2))
    for key in ['cli_substring/literal@5','cli_ranked/literal@1','cli_ranked/literal@5','cli_ranked/prescribed@5','native_ranked_no_provider_metadata/literal@5']:print(key,json.dumps(summaries[key]))
    return result

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--capture',action='store_true');parser.add_argument('--snapshot',default=BENCH/'artifacts/catalog-10.json',type=type(BENCH));parser.add_argument('--output',default=BENCH/'artifacts/ranked-10.json',type=type(BENCH));args=parser.parse_args()
    if args.capture:
        from run import capture
        snapshot=capture((BENCH/'public.json').resolve(),args.snapshot)
    else:snapshot=json.loads(args.snapshot.read_text(encoding='utf-8'))
    run(snapshot,args.output)
if __name__=='__main__':main()

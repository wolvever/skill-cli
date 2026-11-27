"""Paired LLM capability-selection benchmark, with provider-reported usage.
Uses Chat Completions function schemas, as a native MCP client would register
MCP tools. It does not pretend to test a model vendor's hosted-MCP implementation.
All discovery uses frozen public catalogs; external operations are never invoked.
"""
import argparse
import collections
import json
import os
import pathlib
import random
import tempfile
import time

import requests
from harness import BENCH,ROOT,Peer,cli,compact,functions,replay
from metrics import aggregate,paired_bootstrap,selection
from tasks import TASKS

SYSTEM='''Select the minimum set of exact capability IDs needed for the user's request.
Do not execute external operations. Descriptions mentioning a topic do not mean a
tool performs an action on that topic. Select only operations actually present in
the available catalog. For an unavailable operation return an empty selection.
Finish with one JSON object: {"selected_tools":["provider.operation",...]}.'''

ROUTER_TOOLS=[
 {'type':'function','function':{'name':'discover','description':'Discover MCP tools by case-insensitive substring; empty query lists tools. An empty provider searches every provider.','parameters':{'type':'object','properties':{'query':{'type':'string'},'provider':{'type':'string'}},'required':['query']}}},
 {'type':'function','function':{'name':'describe','description':'Get the complete schema of one exact provider.tool ID.','parameters':{'type':'object','properties':{'tool':{'type':'string'}},'required':['tool']}}},
]
SHELL_TOOLS=[{'type':'function','function':{'name':'shell','description':'Run skill-cli with an argv array, starting with skill-cli. Config is supplied by the harness. Selection-only commands: list, search, describe, --help. No shell expansion or pipes. Example argv: ["skill-cli","search","search"].','parameters':{'type':'object','properties':{'argv':{'type':'array','items':{'type':'string'}}},'required':['argv']}}}]

class BudgetExceeded(RuntimeError):pass

class API:
    def __init__(self,url,key,model,max_output,rates=None,budget=None):
        self.url=url.rstrip('/')+'/chat/completions';self.key=key;self.model=model;self.max_output=max_output;self.rates=rates;self.budget=budget;self.spent=0.;self.calls=0;self.unknown_usage_requests=0
    def complete(self,messages,tools,arm):
        payload={'model':self.model,'messages':messages,'tools':tools,'max_completion_tokens':self.max_output,'tool_choice':'none' if arm=='native_eager' else 'auto'}
        if self.budget is not None:
            if self.rates is None:raise ValueError('budget requires explicit input, cached-input, and output token prices')
            # Conservative admission estimate, not an account-level hard limit.
            reserve=((len(compact(payload).encode())+8192)*self.rates[0]+self.max_output*self.rates[2])/1e6
            if self.spent+reserve>self.budget:raise BudgetExceeded('conservative token-price budget reservation exhausted')
        headers={'Content-Type':'application/json'}
        if self.key:headers['Authorization']='Bearer '+self.key
        try:response=requests.post(self.url,json=payload,headers=headers,timeout=120)
        except requests.RequestException:
            self.unknown_usage_requests+=1;raise
        # Never persist Authorization headers or echoed error bodies.
        if response.status_code!=200:self.unknown_usage_requests+=1;raise RuntimeError('model endpoint returned HTTP '+str(response.status_code))
        try:obj=response.json()
        except ValueError:self.unknown_usage_requests+=1;raise RuntimeError('model endpoint returned invalid JSON')
        self.calls+=1
        usage=obj.get('usage')
        if not usage or 'prompt_tokens' not in usage or 'completion_tokens' not in usage:self.unknown_usage_requests+=1;raise RuntimeError('endpoint did not return token usage; no billed-token result can be recorded')
        cached=(usage.get('prompt_tokens_details') or {}).get('cached_tokens',0);inp=usage['prompt_tokens'];out=usage['completion_tokens']
        if not (0<=cached<=inp):self.unknown_usage_requests+=1;raise RuntimeError('inconsistent cached token usage')
        cost=None
        if self.rates:
            cost=((inp-cached)*self.rates[0]+cached*self.rates[1]+out*self.rates[2])/1e6;self.spent+=cost
        return obj,dict(input_tokens=inp,cached_input_tokens=cached,output_tokens=out,reasoning_tokens=(usage.get('completion_tokens_details') or {}).get('reasoning_tokens',0),usd=cost)

def cli_dispatch(config,args):
    argv=args.get('argv')
    if not isinstance(argv,list) or not argv or any(not isinstance(v,str) for v in argv) or argv[0]!='skill-cli':raise ValueError('argv must start with skill-cli')
    if len(argv)<2 or argv[1] not in ['list','search','describe','--help']:raise ValueError('selection benchmark permits list/search/describe only')
    if any(v in ['--config','--gateway','--gateway-token-env','--timeout','--max-bytes'] or v.startswith(('--config=','--gateway=','--timeout=','--max-bytes=','--gateway-token-env=')) for v in argv):raise ValueError('provider configuration overrides are not permitted')
    if argv[1]=='--help':return {'commands':['list [provider]','search <substring> [--provider name]','describe <provider.operation>']}
    return cli(config,argv[1:])['response']

def dispatch(peer,config,name,args):
    if name=='shell':return cli_dispatch(config,args)
    if name=='discover':
        if not isinstance(args.get('query'),str):raise ValueError('query must be a string')
        return peer.request(action='search',query=args['query'],provider=args.get('provider',''))
    if name=='describe':return peer.request(action='describe',tool=args['tool'])
    raise ValueError('operation invocation disabled in selection benchmark')

def parse_selection(content):
    text=(content or '').strip()
    if text.startswith('```'):
        text=text.split('\n',1)[1].rsplit('```',1)[0].strip()
    obj=json.loads(text)
    if not isinstance(obj,dict) or not isinstance(obj.get('selected_tools'),list) or any(not isinstance(s,str) for s in obj['selected_tools']):raise ValueError('expected selected_tools string array')
    return obj['selected_tools']

def episode(api,arm,task,catalog,peer,config,max_turns=10):
    tools,_=functions(catalog) if arm=='native_eager' else (ROUTER_TOOLS,{}) if arm=='native_progressive' else (SHELL_TOOLS,{})
    extra=''
    if arm=='native_progressive':extra='\nUse discover and describe to inspect MCP capabilities. Provider names: '+', '.join(sorted({c['id'].split('.',1)[0] for c in catalog}))+'.'
    elif arm=='skill_cli':extra='\n'+(ROOT/'skills/skill-cli/SKILL.md').read_text(encoding='utf-8')+'\nRun skill-cli through shell; provider configuration is already supplied.'
    messages=[{'role':'system','content':SYSTEM+extra},{'role':'user','content':task['prompt']}]
    trace=[];total=dict(input_tokens=0,cached_input_tokens=0,output_tokens=0,reasoning_tokens=0,usd=0. if api.rates else None);selected=[];error=None;valid=False;start=time.perf_counter();unknown_before=getattr(api,'unknown_usage_requests',0)
    try:
        for turn in range(max_turns):
            response,usage=api.complete(messages,tools,arm)
            for key in total:
                if usage[key] is not None:total[key]+=usage[key]
            message=response['choices'][0]['message'];calls=message.get('tool_calls') or []
            entry=dict(turn=turn,model=response.get('model'),response_id=response.get('id'),system_fingerprint=response.get('system_fingerprint'),usage=usage,assistant=message)
            trace.append(entry)
            if not calls:
                selected=parse_selection(message.get('content'));valid=True;break
            messages.append({k:v for k,v in message.items() if k in ['role','content','tool_calls']})
            outputs=[]
            for call in calls:
                try:args=json.loads(call['function']['arguments']);result=dispatch(peer,config,call['function']['name'],args)
                except (ValueError,KeyError,RuntimeError) as exc:result={'ok':False,'error':str(exc)}
                outputs.append(result);messages.append({'role':'tool','tool_call_id':call['id'],'content':compact(result)})
            entry['tool_outputs']=outputs
        else:error='turn_limit'
    except BudgetExceeded as exc:error='BudgetExceeded: '+str(exc)
    except Exception as exc:error=type(exc).__name__+': '+str(exc)
    metric=selection(selected,task['gold_tools'])
    if not valid:
        metric.update(exact_match=False,precision=0.,negative_abstention=False if not task['gold_tools'] else None)
    return dict(task_id=task['id'],group=task['group'],arm=arm,selected=selected,gold_tools=task['gold_tools'],valid_output=valid,error=error,scorable=not(error and error.startswith('BudgetExceeded:')),usage_complete=getattr(api,'unknown_usage_requests',0)==unknown_before,elapsed_ms=round(1000*(time.perf_counter()-start),2),usage=total,trace=trace,**metric)

def summarize(rows):
    out={}
    for arm in sorted({r['arm'] for r in rows}):
        subset=[r for r in rows if r['arm']==arm]
        scored=[r for r in subset if r.get('scorable',True)]
        out[arm]=dict(**aggregate(scored),recorded_episodes=len(subset),budget_stopped_episodes=len(subset)-len(scored),usage_complete=all(r.get('usage_complete',True) for r in subset),valid_output_rate=sum(r['valid_output'] for r in scored)/len(scored) if scored else None,usage={key:sum(r['usage'][key] for r in subset) if subset[0]['usage'][key] is not None else None for key in subset[0]['usage']})
    # Aggregate repeated runs per task before bootstrapping: repeats are not
    # independent tasks. Only complete task pairs enter paired intervals.
    matched=collections.defaultdict(dict)
    for row in rows:
        if row.get('scorable',True) and row['arm'] in ['native_eager','skill_cli']:matched[(row['task_id'],row.get('repeat',0))][row['arm']]=row
    by_task=collections.defaultdict(list)
    for (task,_),pair in matched.items():
        if set(pair)=={'native_eager','skill_cli'}:by_task[task].append(pair)
    pairs=[{arm:sum(int(p[arm]['exact_match']) for p in repeats)/len(repeats) for arm in ['native_eager','skill_cli']} for repeats in by_task.values()]
    usage_delta={}
    for key in ['input_tokens','cached_input_tokens','output_tokens']:
        deltas=[sum(p['skill_cli']['usage'][key]-p['native_eager']['usage'][key] for p in repeats)/len(repeats) for repeats in by_task.values()]
        if deltas:usage_delta[key]=paired_bootstrap(deltas,[0.]*len(deltas))
    return dict(arms=out,paired_tasks=len(pairs),cli_minus_eager_exact_match=paired_bootstrap([p['skill_cli'] for p in pairs],[p['native_eager'] for p in pairs]) if pairs else None,cli_minus_eager_usage=usage_delta)

def main():
    p=argparse.ArgumentParser();p.add_argument('--model',required=True);p.add_argument('--base-url',default=os.environ.get('OPENAI_BASE_URL','https://api.openai.com/v1'));p.add_argument('--api-key-env',default='OPENAI_API_KEY');p.add_argument('--snapshot',type=pathlib.Path,default=BENCH/'artifacts/catalog.json');p.add_argument('--output',type=pathlib.Path,default=BENCH/'artifacts/model-results.json');p.add_argument('--task-limit',type=int,default=12);p.add_argument('--repeats',type=int,default=1);p.add_argument('--seed',type=int,default=7);p.add_argument('--max-turns',type=int,default=10);p.add_argument('--max-output-tokens',type=int,default=1200);p.add_argument('--budget-usd',type=float);p.add_argument('--input-usd-per-million',type=float);p.add_argument('--cached-input-usd-per-million',type=float);p.add_argument('--output-usd-per-million',type=float);a=p.parse_args()
    key=os.environ.get(a.api_key_env,'')
    from urllib.parse import urlparse
    endpoint=urlparse(a.base_url);local=endpoint.hostname in ['127.0.0.1','localhost','::1']
    if not key and not local:p.error('no API key found; provide a key or a local model endpoint. No benchmark was run.')
    rates=(a.input_usd_per_million,a.cached_input_usd_per_million,a.output_usd_per_million)
    if any(v is not None for v in rates) and not all(v is not None and v>=0 for v in rates):p.error('supply all three nonnegative token prices, or omit all prices')
    if a.budget_usd is not None and (a.budget_usd<=0 or any(v is None for v in rates)):p.error('a positive USD budget requires all three token prices')
    if a.task_limit<1 or a.repeats<1 or a.max_turns<1 or a.max_output_tokens<1:p.error('limits must be positive')
    snapshot=json.loads(a.snapshot.read_text(encoding='utf-8'));catalog=[c for caps in snapshot['catalogs'].values() for c in caps]
    api=API(a.base_url,key,a.model,a.max_output_tokens,rates if rates[0] is not None else None,a.budget_usd)
    rng=random.Random(a.seed);tasks=list(TASKS);rng.shuffle(tasks);tasks=tasks[:a.task_limit];rows=[];stopped=None
    schedule=[]
    for repeat in range(a.repeats):
        for task in tasks:
            arms=['native_eager','native_progressive','skill_cli'];rng.shuffle(arms)
            schedule.extend((repeat,task,arm) for arm in arms)
    a.output.parent.mkdir(parents=True,exist_ok=True)
    def save():a.output.write_text(json.dumps(dict(measurement='LLM_capability_selection_not_end_to_end_answer_accuracy',model=a.model,base_url_host=endpoint.hostname,seed=a.seed,repeats=a.repeats,planned_episodes=len(schedule),completed_episodes=len(rows),stopped=stopped,price_per_million=api.rates,summary=summarize(rows),rows=rows),indent=2)+'\n',encoding='utf-8')
    with tempfile.TemporaryDirectory(prefix='skill-cli-model-') as temp,replay(snapshot,temp) as config,Peer(config) as peer:
        for repeat,task,arm in schedule:
            row=episode(api,arm,task,catalog,peer,config,a.max_turns)
            row['repeat']=repeat;rows.append(row);save();print(task['id'],arm,row['exact_match'],row['usage'],flush=True)
            if row['error'] and row['error'].startswith('BudgetExceeded:'):stopped=row['error'];save();break
            if not row['usage_complete']:stopped='model request has unknown token usage; stopped to avoid an incomplete-cost comparison';save();break
    save()

if __name__=='__main__':main()

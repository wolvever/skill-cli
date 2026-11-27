"""Small read-only live invocation comparison, separate from retrieval quality."""
import datetime
import hashlib
import json
import pathlib
import re
import time

from harness import BENCH,Peer,cli,compact

CASES=[
 ('deepwiki.read_wiki_structure',{'repoName':'facebook/react'},'nonempty'),
 ('mslearn.microsoft_docs_search',{'query':'Azure Functions timeout limits'},'learn.microsoft.com'),
 ('cloudflare.search_cloudflare_documentation',{'query':'Workers KV get text value'},'developers.cloudflare.com'),
 ('petstore.getPetById',{'petId':1},'pet'),
 ('apisguru.getMetrics',{},'metrics'),
]
def normalize(result,tool,native):
    if tool.startswith(('petstore.','apisguru.')):
        return result.get('structuredContent') if native else result
    return result
def check(value,kind):
    if kind=='nonempty':return bool(value and value.get('content'))
    if kind=='pet':return value.get('status')==200 and isinstance(value.get('body'),dict) and value['body'].get('id')==1
    if kind=='metrics':return value.get('status')==200 and isinstance(value.get('body'),dict) and 'numAPIs' in value['body']
    return kind in compact(value)
def main():
    rows=[];config=BENCH/'public.json'
    with Peer(config) as peer:
        for tool,args,kind in CASES:
            start=time.perf_counter();native=peer.request(action='call',tool=tool,args=args);native_ms=1000*(time.perf_counter()-start)
            start=time.perf_counter();other=cli(config,['call',tool,'--args','-'],stdin=compact(args));cli_ms=1000*(time.perf_counter()-start)
            output={};values={}
            for name,envelope,native_flag in [('native',native,True),('skill_cli',other['response'],False)]:
                data=normalize(envelope.get('data') or {},tool,native_flag);serialized=compact(data)
                values[name]=data
                urls=sorted(set(re.findall(r'https?://[^\s"<>\\]+',serialized)))
                output[name]=dict(ok=envelope['ok'],criterion_pass=bool(envelope['ok'] and check(data,kind)),error=envelope.get('error'),result_bytes=len(serialized.encode()),result_sha256=hashlib.sha256(serialized.encode()).hexdigest(),url_count=len(urls),http_status=data.get('status') if isinstance(data,dict) else None)
            output.update(tool=tool,args=args,criterion=kind,native_elapsed_ms=round(native_ms,2),cli_elapsed_ms=round(cli_ms,2),normalized_json_equal=values['native']==values['skill_cli'])
            if output['native']['http_status']==404 and output['skill_cli']['http_status']==404:output['failure_class']='upstream_missing_record'
            rows.append(output);print(tool,output['native']['criterion_pass'],output['skill_cli']['criterion_pass'],flush=True)
    # Compatibility-only cases: preserve the failure instead of rewriting specs.
    import tempfile
    with tempfile.TemporaryDirectory() as tmp:
        cfg=pathlib.Path(tmp)/'config.json';cfg.write_text(json.dumps({'providers':{'openmeteo':{'type':'openapi','spec':str(BENCH/'specs/elevation.yaml'),'url':'https://api.open-meteo.com'}}}),encoding='utf-8')
        compatibility=cli(cfg,['list','openmeteo'])
    result=dict(captured_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),measurement='read_only_live_invocation_smoke_not_model_answer_precision',criteria='typed result checks; documentation checks verify usable text/domain, not semantic correctness',rows=rows,openmeteo_3_1_compatibility=compatibility)
    (BENCH/'artifacts/public-probes.json').write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
if __name__=='__main__':main()

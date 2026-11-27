"""Bounded, metadata-only probes of documented public MCP endpoints."""
import concurrent.futures
import datetime
import json
import pathlib
import subprocess
import tempfile
import time
from harness import BENCH,CREATE_FLAGS,PEER,compact

CANDIDATES=[
 ('context7','https://mcp.context7.com/mcp','https://github.com/upstash/context7'),
 ('grep','https://mcp.grep.app','https://vercel.com/blog/grep-a-million-github-repositories-via-mcp'),
 ('svelte','https://mcp.svelte.dev/mcp','https://svelte.dev/docs/mcp/remote-setup'),
 ('nuxt','https://nuxt.com/mcp','https://nuxt.com/docs/4.x/guide/ai/mcp'),
 ('astro','https://mcp.docs.astro.build/mcp','https://docs.astro.build/en/guides/build-with-ai/'),
 ('huggingface','https://huggingface.co/mcp','https://huggingface.co/docs/hub/en/hf-mcp-server'),
 ('cloudflare_blog','https://blog.mcp.cloudflare.com/mcp','https://github.com/cloudflare/mcp-server-cloudflare'),
]

def probe(candidate):
    name,url,source=candidate;start=time.perf_counter()
    row=dict(name=name,url=url,source=source)
    with tempfile.TemporaryDirectory(prefix='skill-cli-mcp-probe-') as tmp:
        cfg=pathlib.Path(tmp)/'config.json';cfg.write_text(compact({'providers':{name:{'type':'mcp','transport':'http','url':url}}}),encoding='utf-8')
        try:
            p=subprocess.run([str(PEER),'--config',str(cfg),'--timeout','12s'],input=compact({'action':'catalog','provider':name})+'\n',capture_output=True,text=True,encoding='utf-8',timeout=25,**CREATE_FLAGS)
            data=json.loads(p.stdout);row.update(ok=data['ok'],tools=data.get('data',[]),error=data.get('error'),exit_code=p.returncode)
        except (ValueError,subprocess.TimeoutExpired) as exc:row.update(ok=False,error=type(exc).__name__)
    row['elapsed_ms']=round(1000*(time.perf_counter()-start),2)
    return row

def main():
    rows=[]
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        for row in pool.map(probe,CANDIDATES):
            rows.append(row);print(row['name'],row['ok'],len(row.get('tools',[])),row.get('error'),flush=True)
    (BENCH/'artifacts/server-probes-10.json').write_text(json.dumps(dict(captured_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),method='one public unauthenticated MCP tools/list per candidate; no tools invoked',rows=rows),indent=2)+'\n',encoding='utf-8',newline='\n')
if __name__=='__main__':main()

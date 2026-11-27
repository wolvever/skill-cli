"""Benchmark plumbing: real CLI subprocesses, official-SDK peer, frozen catalogs."""
import contextlib
import json
import os
import pathlib
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer

ROOT=pathlib.Path(__file__).resolve().parent.parent
BENCH=ROOT/'benchmarks'
SUFFIX='.exe' if os.name=='nt' else ''
CLI=ROOT/'bin'/('skill-cli'+SUFFIX)
PEER=ROOT/'bin'/('mcp-peer'+SUFFIX)
CREATE_FLAGS={'creationflags':subprocess.CREATE_NO_WINDOW} if os.name=='nt' else {}

def compact(value):
    return json.dumps(value,separators=(',',':'),ensure_ascii=False)

class Peer:
    def __init__(self,config):
        self.process=subprocess.Popen([str(PEER),'--config',str(config),'--timeout','25s'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,encoding='utf-8',bufsize=1,**CREATE_FLAGS)
    def request(self,**request):
        self.process.stdin.write(compact(request)+'\n');self.process.stdin.flush()
        text=self.process.stdout.readline()
        if not text:
            raise RuntimeError('native peer closed without returning JSON')
        return json.loads(text)
    def close(self):
        self.process.stdin.close()
        try:self.process.wait(timeout=20)
        except subprocess.TimeoutExpired:self.process.kill();self.process.wait()
        self.process.stdout.close()
    def __enter__(self):return self
    def __exit__(self,*_):self.close()

def cli(config,argv,stdin=None,gateway=None):
    command=[str(CLI),'--config',str(config),'--timeout','25s']
    if gateway:command+=['--gateway',gateway]
    p=subprocess.run(command+argv,input=stdin,capture_output=True,text=True,encoding='utf-8',timeout=35,**CREATE_FLAGS)
    try:response=json.loads(p.stdout)
    except ValueError:raise RuntimeError('CLI returned invalid JSON: '+p.stderr[:300])
    return dict(exit_code=p.returncode,response=response,stdout=p.stdout)

def functions(catalog):
    tools=[];mapping={}
    for i,c in enumerate(sorted(catalog,key=lambda c:c['id'])):
        name='cap_'+str(i);mapping[name]=c['id']
        tools.append({'type':'function','function':{'name':name,'description':c['id']+'\n'+c.get('description',''),'parameters':c.get('inputSchema') or {'type':'object','properties':{}}}})
    return tools,mapping

def brief(c):
    return {k:v for k,v in c.items() if k not in ['inputSchema','outputSchema']}

@contextlib.contextmanager
def replay(snapshot,directory):
    """Expose captured public schemas as local MCP. No calls reach upstream."""
    catalogs=snapshot['catalogs']
    class Handler(BaseHTTPRequestHandler):
        def log_message(self,*_):pass
        def reply(self,status,data):
            payload=compact(data).encode();self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(payload)));self.end_headers();self.wfile.write(payload)
        def do_GET(self):self.reply(405,{'error':'no standalone stream'})
        def do_DELETE(self):self.reply(200,{})
        def do_POST(self):
            provider=self.path.strip('/');items=catalogs.get(provider)
            if items is None:self.reply(404,{'error':'unknown provider'});return
            request=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))));method=request.get('method')
            if 'id' not in request:self.reply(202,{});return
            out={'jsonrpc':'2.0','id':request['id']}
            if method=='initialize':out['result']={'protocolVersion':'2025-11-25','capabilities':{'tools':{}},'serverInfo':{'name':'public-catalog-replay','version':'1'}}
            elif method=='tools/list':out['result']={'tools':[dict(name=c['id'].split('.',1)[1],description=c.get('description',''),inputSchema=c.get('inputSchema') or {'type':'object'},**({'outputSchema':c['outputSchema']} if c.get('outputSchema') else {}),**({'annotations':c['annotations']} if c.get('annotations') else {})) for c in items]}
            elif method=='ping':out['result']={}
            else:out['error']={'code':-32601,'message':'selection-only replay: invocation unavailable'}
            self.reply(200,out)
    server=ThreadingHTTPServer(('127.0.0.1',0),Handler);server.daemon_threads=True
    thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
    metadata=snapshot.get('search_metadata',snapshot.get('providers',{}))
    providers={name:dict(type='mcp',transport='http',url='http://127.0.0.1:'+str(server.server_port)+'/'+name,**{key:metadata.get(name,{}).get(key) for key in ['description','keywords','skill_descriptions'] if metadata.get(name,{}).get(key)}) for name in catalogs}
    config=pathlib.Path(directory)/'replay.json';config.write_text(json.dumps({'providers':providers},indent=2),encoding='utf-8')
    try:yield config
    finally:server.shutdown();server.server_close();thread.join(timeout=2)

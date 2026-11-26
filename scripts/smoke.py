"""Optional compiled-binary smoke test. Build skill-cli and demo-server into bin first."""
import json
import os
import pathlib
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.request

ROOT=pathlib.Path(__file__).resolve().parent.parent
SUFFIX='.exe' if os.name=='nt' else ''
CLI=ROOT/'bin'/('skill-cli'+SUFFIX)
DEMO=ROOT/'bin'/('demo-server'+SUFFIX)

def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1',0))
        return s.getsockname()[1]

def wait_ready(url,headers=None):
    for _ in range(100):
        try:
            data=None if headers is None else b'{"action":"list"}'
            with urllib.request.urlopen(urllib.request.Request(url,data=data,headers=headers or {}),timeout=.2) as r:
                if r.status==200:
                    return
        except (OSError,TimeoutError):
            time.sleep(.05)
    raise RuntimeError('server did not become ready: '+url)

def main():
    flags={'creationflags':subprocess.CREATE_NO_WINDOW} if os.name=='nt' else {}
    with tempfile.TemporaryDirectory(prefix='skill-cli-smoke-') as tmp:
        tmp=pathlib.Path(tmp);base='http://127.0.0.1:'+str(port());gateway='http://127.0.0.1:'+str(port())
        config={'providers':{
            'demo':{'type':'mcp','transport':'http','url':base+'/mcp','allow':['add','echo','fail']},
            'legacy':{'type':'mcp','transport':'sse','url':base+'/sse'},
            'stdio':{'type':'mcp','transport':'stdio','command':[str(DEMO)]},
            'pets':{'type':'openapi','spec':str(ROOT/'examples/petstore.yaml'),'url':base},
            'rest':{'type':'http','url':base},
        }}
        path=tmp/'config.json';path.write_text(json.dumps(config),encoding='utf-8')
        env=dict(os.environ,SKILL_CLI_GATEWAY_TOKEN=secrets.token_urlsafe(32))
        server=subprocess.Popen([str(DEMO),'--listen',base.removeprefix('http://')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,**flags)
        service=None;rows=[]
        try:
            wait_ready(base+'/pets/42')
            def run(case,args,want=0,stdin=None):
                start=time.perf_counter();p=subprocess.run([str(CLI)]+args,input=stdin,text=True,encoding='utf-8',capture_output=True,timeout=20,env=env,**flags)
                output=json.loads(p.stdout)
                assert p.returncode==want,(case,p.returncode,p.stdout,p.stderr)
                assert output['ok']==(want==0),(case,output)
                rows.append(dict(case=case,exit_code=p.returncode,elapsed_ms=round(1000*(time.perf_counter()-start),2),output=output))
                return output
            common=['--config',str(path)]
            run('list',common+['list'])
            run('discover',common+['list','demo'])
            run('search',common+['search','add','--provider','demo'])
            run('describe',common+['describe','demo.add'])
            for provider in ['demo','legacy','stdio']:
                data=run(provider+'_call',common+['call',provider+'.add','--args',json.dumps({'a':2,'b':3})])
                assert data['data']['structuredContent']['sum']==5
            run('nested_stdin',common+['call','demo.echo','--args','-'],stdin=json.dumps({'message':'hello','payload':{'items':[1,True,None]}}))
            run('tool_error',common+['call','demo.fail'],want=1)
            run('invalid_input',common+['call','demo.add','--args','{"a":"bad"}'],want=1)
            run('openapi_call',common+['call','pets.getPet','--args',json.dumps({'id':'42','verbose':True})])
            run('raw_http',common+['request','rest','GET','/pets/42'])
            service=subprocess.Popen([str(CLI),'--config',str(path),'serve','--listen',gateway.removeprefix('http://')],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,**flags)
            wait_ready(gateway+'/v1/invoke',{'Authorization':'Bearer '+env['SKILL_CLI_GATEWAY_TOKEN']})
            thin=['--gateway',gateway,'--config',str(tmp/'absent.json')]
            run('thin_gateway_list',thin+['list'])
            for i in range(3):
                run('gateway_call_'+str(i),thin+['call','demo.add','--args',json.dumps({'a':2,'b':3})])
            run('gateway_allowlist',thin+['call','demo.denied'],want=1)
            destination=ROOT/'docs/research/skill-cli-smoke.json';destination.parent.mkdir(parents=True,exist_ok=True)
            destination.write_text(json.dumps(rows,indent=2),encoding='utf-8')
            print(str(len(rows))+' compiled CLI smoke cases passed')
        finally:
            for process in [service,server]:
                if process is not None:
                    process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill();process.wait()

if __name__=='__main__':
    main()

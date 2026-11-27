"""Harness tests use fake model responses; they are never model benchmark results."""
import copy
import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

from harness import BENCH,Peer,cli,replay
from metrics import aggregate,paired_bootstrap,selection
from model_runner import API,BudgetExceeded,cli_dispatch,episode,summarize

class MetricsTest(unittest.TestCase):
    def test_partial_selection_and_negative_conventions(self):
        p=selection(['a','wrong'],['a','b']);self.assertEqual((p['tp'],p['fp'],p['fn']),(1,1,1));self.assertEqual(p['precision'],.5);self.assertEqual(p['recall'],.5)
        n=selection([],[]);self.assertTrue(n['exact_match']);self.assertIsNone(n['recall']);self.assertTrue(n['negative_abstention'])
        a=aggregate([p,n]);self.assertEqual(a['micro_precision'],.5);self.assertEqual(a['micro_recall'],.5)
    def test_bootstrap_keeps_paired_differences(self):
        self.assertEqual(paired_bootstrap([2,3],[1,2])['ci95'],[1.,1.])
        with self.assertRaises(ValueError):paired_bootstrap([1],[])
    def test_unmatched_repeat_does_not_bias_pair(self):
        def row(arm,repeat,correct):return dict(task_id='x',arm=arm,repeat=repeat,valid_output=True,usage=dict(input_tokens=1,cached_input_tokens=0,output_tokens=1,reasoning_tokens=0,usd=None),**selection(['a'] if correct else [],['a']))
        result=summarize([row('native_eager',0,True),row('skill_cli',0,False),row('native_eager',1,False)])
        self.assertEqual(result['paired_tasks'],1);self.assertEqual(result['cli_minus_eager_exact_match']['mean'],-1.)

class APITest(unittest.TestCase):
    def test_usage_pricing_and_budget_preflight(self):
        class Response:
            status_code=200
            def json(self):return dict(usage=dict(prompt_tokens=100,completion_tokens=20,prompt_tokens_details={'cached_tokens':40}),choices=[{'message':{'role':'assistant','content':'{}'}}])
        api=API('http://127.0.0.1/v1','test-key','fixture',100,rates=(2.,1.,4.),budget=1.)
        with patch('model_runner.requests.post',return_value=Response()) as post:
            _,u=api.complete([],[],'native_eager');self.assertAlmostEqual(u['usd'],.00024);self.assertEqual(u['cached_input_tokens'],40);self.assertEqual(post.call_args.kwargs['json']['tool_choice'],'none')
            api.budget=api.spent
            with self.assertRaises(BudgetExceeded):api.complete([],[],'native_eager')
            self.assertEqual(post.call_count,1)
    def test_missing_usage_is_not_silently_estimated(self):
        class Response:
            status_code=200
            def json(self):return {'choices':[]}
        with patch('model_runner.requests.post',return_value=Response()):
            with self.assertRaisesRegex(RuntimeError,'token usage'):API('http://127.0.0.1/v1','','fixture',100).complete([],[],'native_eager')
        class NullableDetails:
            status_code=200
            def json(self):return {'usage':{'prompt_tokens':10,'completion_tokens':5,'prompt_tokens_details':None,'completion_tokens_details':None},'choices':[]}
        with patch('model_runner.requests.post',return_value=NullableDetails()):
            _,u=API('http://127.0.0.1/v1','','fixture',100).complete([],[],'native_eager');self.assertEqual(u['cached_input_tokens'],0);self.assertEqual(u['reasoning_tokens'],0)

class ReplayAndRunnerTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.snapshot={'catalogs':{'fixture':[{'id':'fixture.echo','description':'Echo a message','protocol':'mcp','inputSchema':{'type':'object','properties':{'message':{'type':'string'}},'required':['message']}}]}}
    def test_real_native_sdk_and_cli_share_catalog(self):
        with tempfile.TemporaryDirectory() as tmp,replay(self.snapshot,tmp) as cfg,Peer(cfg) as peer:
            native=peer.request(action='catalog',provider='fixture');other=cli(cfg,['describe','fixture.echo'])['response']
            self.assertTrue(native['ok']);self.assertEqual(native['data'][0]['inputSchema'],other['data']['inputSchema'])
            self.assertEqual(cli(cfg,['search','ECHO'])['response']['data'][0]['id'],'fixture.echo')
            denied=cli(cfg,['call','fixture.echo','--args','-'],stdin='{"message":"hi"}');self.assertNotEqual(denied['exit_code'],0)
    def test_cli_tool_cannot_change_config_or_invoke(self):
        for argv in [['skill-cli','call','fixture.echo'],['skill-cli','search','a','--config=bad'],['python','-c','code']]:
            with self.assertRaises(ValueError):cli_dispatch('unused',{'argv':argv})
    def test_fake_model_discovery_loop_and_parse_failure(self):
        class Fake:
            rates=None
            def __init__(self,messages):self.messages=iter(messages)
            def complete(self,*_):return {'model':'fake-test-only','choices':[{'message':next(self.messages)}]},dict(input_tokens=10,cached_input_tokens=0,output_tokens=5,reasoning_tokens=0,usd=None)
        messages=[{'role':'assistant','content':None,'tool_calls':[{'id':'1','type':'function','function':{'name':'shell','arguments':json.dumps({'argv':['skill-cli','search','echo']})}}]},{'role':'assistant','content':'{"selected_tools":["fixture.echo"]}'}]
        task=dict(id='test',group='exact',prompt='Select echo',gold_tools=['fixture.echo'])
        with tempfile.TemporaryDirectory() as tmp,replay(self.snapshot,tmp) as cfg,Peer(cfg) as peer:
            row=episode(Fake(messages),'skill_cli',task,self.snapshot['catalogs']['fixture'],peer,cfg);self.assertTrue(row['exact_match']);self.assertEqual(row['usage']['input_tokens'],20);self.assertEqual(len(row['trace']),2)
            bad=episode(Fake([{'role':'assistant','content':'not JSON'}]),'native_eager',dict(task,gold_tools=[]),self.snapshot['catalogs']['fixture'],peer,cfg);self.assertFalse(bad['valid_output']);self.assertFalse(bad['exact_match']);self.assertFalse(bad['negative_abstention'])
    def test_budget_stop_preserves_prior_turn_usage(self):
        class Fake:
            rates=(1.,1.,1.)
            def __init__(self):self.calls=0
            def complete(self,*_):
                self.calls+=1
                if self.calls>1:raise BudgetExceeded('test stop')
                return {'choices':[{'message':{'role':'assistant','content':None,'tool_calls':[{'id':'1','function':{'name':'discover','arguments':'{"query":"echo"}'}}]}}]},dict(input_tokens=100,cached_input_tokens=0,output_tokens=10,reasoning_tokens=0,usd=.001)
        with tempfile.TemporaryDirectory() as tmp,replay(self.snapshot,tmp) as cfg,Peer(cfg) as peer:
            row=episode(Fake(),'native_progressive',dict(id='test',group='exact',prompt='Select echo',gold_tools=['fixture.echo']),self.snapshot['catalogs']['fixture'],peer,cfg);self.assertEqual(row['usage']['input_tokens'],100);self.assertEqual(row['usage']['usd'],.001);self.assertIn('BudgetExceeded',row['error'])

if __name__=='__main__':unittest.main()

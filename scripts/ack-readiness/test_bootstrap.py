import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
import uuid
from unittest.mock import patch

import operations
from argv_validator import await_stable, expected_tokens, observe_process
from journal_validator import Rejected, validate_records
from schema3 import SchemaError
from schema5 import validate_bootstrap, bootstrap_outcome
from measurement import readiness_observation
from test_startup import entries, lines, scope5

def fixture(name):
    return json.loads((Path(os.environ['ACK_BOOTSTRAP_FIXTURE_DIR'])/(name+'.json')).read_text())

def response(e):return '[ACK-BOOTSTRAP] '+json.dumps(e,separators=(',',':'))

def sequence(name,success=False):
    startup=lines('success' if success else 'missing')
    split=next(i for i,l in enumerate(startup) if '"startup_stage":"authorization"' in l and '"startup_result":"begin"' in l)+1
    e=fixture(name)
    if name=='redirect_limit':
        rs=[response(dict(fixture('redirect'),ordinal=i)) for i in range(1,11)]+[response(e)]
    elif name.startswith('redirect_'):
        rs=[response(fixture('redirect')),response(dict(e,ordinal=2))]
    else:rs=[response(e)]
    return startup[:split]+rs+startup[split:]

class BootstrapTests(unittest.TestCase):
    def test_successful_bootstrap_through_first_snapshot(self):
        texts=sequence('normal',True)
        records,_=validate_records(entries(texts+[texts[-1]]*8),scope5())
        self.assertEqual('PASS',readiness_observation(records,30,schema=5)['LOG_VALIDATION'])
        outcome=bootstrap_outcome(records)
        self.assertEqual('BootstrapObserved',outcome['RESULT'])
        self.assertEqual('OK',outcome['BOOTSTRAP_RESPONSES'][-1]['client_config_parse_result'])
        self.assertFalse(outcome['READY']) # response evidence alone is insufficient

    def test_exact_go_positive_fixtures(self):
        for p in Path(os.environ['ACK_BOOTSTRAP_FIXTURE_DIR']).glob('*.json'):
            with self.subTest(p=p.stem):validate_bootstrap(json.loads(p.read_text()))

    def test_unknown_fields_numbers_types_enums_and_relationships(self):
        original=fixture('normal')
        changes=[{k:'SECRET_SENTINEL'} for k in ('url','query','body','headers','cookie','token','password','key','document_id','payload','client_config','raw_error')]
        changes += [dict(schema=4),dict(schema=True),dict(event='other'),dict(ordinal=0),dict(ordinal=12),
            dict(body_bytes_read=-1),dict(body_bytes_read=True),dict(http_status_code=99),dict(http_status_class='5XX'),
            dict(content_type_class='text/html;SECRET_SENTINEL'),dict(content_encoding_class='SECRET_SENTINEL'),
            dict(body_read_error_class='SECRET_SENTINEL'),dict(final_route_class='SECRET_SENTINEL'),
            dict(client_config_parse_result='SECRET_SENTINEL'),dict(response_result='SECRET_SENTINEL'),
            dict(client_config_matched=False),dict(client_config_match_count=0),dict(client_config_match_count_capped=True),
            dict(body_read_complete=False),dict(client_config_search_on_non2xx=True),dict(is_html=1),
            dict(final_route_class='YANDEX_CAPTCHA_OR_CHALLENGE')]
        for change in changes:
            with self.subTest(key=next(iter(change))):
                with self.assertRaises(SchemaError):validate_bootstrap(dict(original,**change))
        for key in original:
            bad=copy.deepcopy(original);del bad[key]
            with self.assertRaises(SchemaError):validate_bootstrap(bad)

    def test_all_response_fixtures_through_actual_journal_path(self):
        for p in Path(os.environ['ACK_BOOTSTRAP_FIXTURE_DIR']).glob('*.json'):
            with self.subTest(p=p.stem):
                records,_=validate_records(entries(sequence(p.stem)),scope5())
                outcome=bootstrap_outcome(records,True)
                self.assertEqual('DiagnosticStartupFailed',outcome['RESULT'])
                self.assertFalse(outcome['READY'])
                self.assertTrue(outcome['BOOTSTRAP_RESPONSES'])
                self.assertNotIn('SECRET_SENTINEL',json.dumps(outcome))

    def test_journal_rejects_malformed_unknown_and_mixed_schema(self):
        values=sequence('normal')
        pos=next(i for i,l in enumerate(values) if l.startswith('[ACK-BOOTSTRAP]'))
        for bad in ('[ACK-BOOTSTRAP] {bad',response(dict(fixture('normal'),schema=4)),
                    response(dict(fixture('normal'),url='SECRET_SENTINEL')),None,[255],
                    response(dict(fixture('normal'),body_bytes_read='12'))):
            with self.assertRaises(Rejected) as c:validate_records(entries(values[:pos]+[bad]+values[pos+1:]),scope5())
            self.assertNotIn('SECRET_SENTINEL',json.dumps(c.exception.safe))
        for altered in (values[:pos]+values[pos:pos+1]*2+values[pos+1:],values[:pos]+[response(dict(fixture('normal'),ordinal=2))]+values[pos+1:],values+[response(fixture('normal'))]):
            with self.assertRaises(Rejected):validate_records(entries(altered),scope5())

    def test_challenge_marker_is_required_and_incomplete_scan_is_explicit(self):
        self.assertFalse(fixture('429')['has_known_challenge_structure'])
        self.assertTrue(fixture('redirect_challenge')['has_known_challenge_structure'])
        self.assertEqual('YANDEX_CAPTCHA_OR_CHALLENGE',fixture('redirect_challenge')['final_route_class'])
        self.assertTrue(fixture('config_before_read_error')['client_config_matched'])
        self.assertFalse(fixture('config_before_read_error')['body_read_complete'])

    def test_schema4_remains_explicitly_supported_but_cannot_accept_new_event(self):
        old=[]
        for l in lines('missing'):
            e=json.loads(l.split(' ',1)[1]);e['schema']=4;old.append('[ACK-STARTUP] '+json.dumps(e))
        validate_records(entries(old),dict(scope5(),schema=4))
        with self.assertRaises(Rejected):validate_records(entries(old[:-1]+[response(fixture('normal'))]+old[-1:]),dict(scope5(),schema=4))

    def test_real_linux_proc_and_bootstrap_ingestion(self):
        for negative in (False,True):
            exe=str(Path(sys.executable).resolve())
            child=str(Path(__file__).with_name('bootstrap_simulated_invocation.py').resolve())
            texts=sequence('redirect_challenge')
            if negative:texts.insert(-1,response(dict(fixture('normal'),url='SECRET_SENTINEL')))
            p=subprocess.Popen([exe,'-u',child],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            inv=uuid.uuid4().hex;state=dict(MainPID=str(p.pid),InvocationID=inv,NRestarts='0',ActiveState='active',SubState='running')
            try:
                expected=expected_tokens(exe,['-u',child])
                _,proof=await_stable(lambda:observe_process(state,expected),dict(MainPID='0',InvocationID='old'),expected)
                self.assertTrue(proof['ARGV_MATCH'] and proof['MAINPID_STABLE'])
                p.stdin.write(json.dumps(texts)+'\n');p.stdin.flush()
                emitted=[p.stdout.readline().rstrip('\n') for _ in texts]
                scope=dict(scope5(),pid=str(p.pid),exe=exe,invocation=inv,uid=str(os.getuid()),gid=str(os.getgid()))
                es=entries(emitted)
                for e in es:e.update(_PID=scope['pid'],_EXE=exe,_SYSTEMD_INVOCATION_ID=inv,_UID=scope['uid'],_GID=scope['gid'])
                def command(*args):
                    self.assertIn('--all',args);self.assertIn('--after-cursor=before',args)
                    return '\n'.join(json.dumps(e) for e in es)
                with patch.object(operations,'command',command),patch.object(operations,'load',return_value=scope),patch.object(operations,'save'):
                    if negative:
                        with self.assertRaises(RuntimeError):operations.journal(inv)
                    else:
                        records=operations.journal(inv);outcome=bootstrap_outcome(records,True)
                        self.assertFalse(outcome['READY']);self.assertEqual(2,len(outcome['BOOTSTRAP_RESPONSES']))
                self.assertEqual(1,p.wait(timeout=5));self.assertEqual('',p.stderr.read())
            finally:
                if p.poll() is None:p.kill();p.wait(timeout=5)
                for stream in (p.stdin,p.stdout,p.stderr):stream.close()

if __name__=='__main__':unittest.main()

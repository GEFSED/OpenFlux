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
import schema6
from schema3 import SchemaError
from argv_validator import await_stable, expected_tokens, observe_process
from journal_validator import Rejected, validate_records
from measurement import readiness_observation
from test_startup import entries, lines, scope5

def fixture(name):
    return json.loads((Path(os.environ['ACK_STRUCTURE_FIXTURE_DIR'])/(name+'.json')).read_text())

def sequence(e,success=False):
    startup=lines('success' if success else 'missing',6)
    split=next(i for i,l in enumerate(startup) if '"startup_stage":"authorization"' in l and '"startup_result":"begin"' in l)+1
    return startup[:split]+['[ACK-BOOTSTRAP] '+json.dumps(e,separators=(',',':'))]+startup[split:]

class StructureTests(unittest.TestCase):
    def test_all_exact_go_structure_and_bootstrap_fixtures(self):
        for env in ('ACK_STRUCTURE_FIXTURE_DIR','ACK_BOOTSTRAP_FIXTURE_DIR'):
            for p in Path(os.environ[env]).glob('*.json'):
                with self.subTest(case=p.stem):schema6.validate_bootstrap(json.loads(p.read_text()))

    def test_fixture_matrix_through_fresh_journal_failure_path(self):
        for p in Path(os.environ['ACK_STRUCTURE_FIXTURE_DIR']).glob('*.json'):
            with self.subTest(case=p.stem):
                texts=sequence(fixture(p.stem))
                records,_=validate_records(entries(texts),dict(scope5(),schema=6))
                result=schema6.bootstrap_outcome(records,process_exited=True)
                self.assertEqual('DiagnosticStartupFailed',result['RESULT'])
                self.assertEqual('authorization',result['STARTUP_STAGE'])
                self.assertEqual('auth_client_config_missing',result['STARTUP_FAILURE_CLASS'])
                self.assertFalse(result['READY'])
                self.assertEqual(fixture(p.stem),result['BOOTSTRAP_RESPONSES'][0])

    def test_success_through_first_snapshot_and_readiness(self):
        texts=sequence(fixture('legacy'),True)
        records,_=validate_records(entries(texts+[texts[-1]]*8),dict(scope5(),schema=6))
        self.assertEqual('PASS',readiness_observation(records,30,schema=6)['LOG_VALIDATION'])
        self.assertFalse(schema6.bootstrap_outcome(records)['READY'])

    def test_incomplete_or_conflicting_scan_never_passes_readiness(self):
        for name in ('giant_json','excess_tokens','conflict'):
            texts=sequence(fixture(name),True)
            records,_=validate_records(entries(texts+[texts[-1]]*8),dict(scope5(),schema=6))
            with self.assertRaisesRegex(RuntimeError,'bootstrap_structure_not_conclusive'):
                readiness_observation(records,30,schema=6)

    def reject(self,v):
        with self.assertRaises(SchemaError) as err:schema6.validate_bootstrap(v)
        self.assertNotIn('FAKE_SECRET',str(err.exception))

    def test_unknown_raw_fields_never_accepted(self):
        fields=('url','path','script_src','dom_text','html','raw_json','document_id','token','cookie','authorization','credential','encryption_key','query','body','payload','headers','secret_extension')
        for key in fields:
            with self.subTest(field=key):self.reject(dict(fixture('public_root'),**{key:'FAKE_SECRET'}))
        for key in ('expected_docs_app_markers','static_resource_family_classes'):
            v=fixture('public_root');v[key]['FAKE_SECRET']='FAKE_SECRET';self.reject(v)

    def test_missing_fields_and_strict_types(self):
        for key in schema6.KEYS:
            v=fixture('public_root');del v[key];self.reject(v)
        for key in schema6.ENUMS:
            for value in ('https://FAKE_SECRET',[],None,False):
                v=fixture('public_root');v[key]=value;self.reject(v)
        for key in schema6.BOOLS:
            v=fixture('public_root');v[key]=1;self.reject(v)
        for key in schema6.COUNTS:
            for value in (-1,True,1.5,'1',65537):
                v=fixture('public_root');v[key]=value;self.reject(v)
        for value in (5,7,True):
            v=fixture('public_root');v['schema']=value;self.reject(v)

    def test_impossible_counts_scan_conflicts_shapes_and_page(self):
        changes=[dict(script_total_count=1),dict(script_inline_count=0),dict(script_json_count=3),
                 dict(script_module_count=2),dict(candidate_container_count=2),dict(structure_scan_complete=False),
                 dict(structure_scan_limit_reason='TOKEN_COUNT'),dict(page_structure_class='LEGACY_EDITOR_CLIENT_CONFIG'),
                 dict(structural_classification_conflict=True),dict(initial_store_present=False),
                 dict(other_inline_bootstrap_present=True),dict(browser_compatibility_marker='YES'),
                 dict(optional_ttl_field_present=True),dict(required_auth_field_shape_present=True,candidate_container_count=0)]
        for change in changes:self.reject(dict(fixture('public_root'),**change))
        v=fixture('public_root');v['static_resource_family_classes']['YANDEX_STATIC_PSF']=0;self.reject(v)
        v=fixture('conflict');v['structural_classification_conflict']=False;v['page_structure_class']='CHALLENGE_PAGE';self.reject(v)
        v=fixture('giant_json');v['structure_scan_complete']=True;self.reject(v)
        v=fixture('editor_required');v['required_auth_field_shape_present']=False;self.reject(v)
        v=fixture('browser');v['browser_compatibility_marker']='NO';self.reject(v)

    def test_journal_strict_privacy_mixed_schema_and_unknown_events(self):
        source=fixture('legacy')
        for e in (dict(source,url='FAKE_SECRET'),dict(source,schema=5),dict(source,event='unrecognized')):
            with self.assertRaises(Rejected) as err:validate_records(entries(sequence(e)),dict(scope5(),schema=6))
            self.assertNotIn('FAKE_SECRET',json.dumps(err.exception.safe))
        for e in (None,[255],'[ACK-BOOTSTRAP] {bad'):
            values=sequence(source);values.insert(-1,e)
            with self.assertRaises(Rejected):validate_records(entries(values),dict(scope5(),schema=6))

    def test_schema6_real_linux_process_and_operations_journal(self):
        for negative in (False,True):
            exe=str(Path(sys.executable).resolve());child=str(Path(__file__).with_name('bootstrap_simulated_invocation.py').resolve())
            e=fixture('public_root')
            if negative:e['raw_html']='FAKE_SECRET'
            texts=sequence(e)
            p=subprocess.Popen([exe,'-u',child],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            inv=uuid.uuid4().hex;state=dict(MainPID=str(p.pid),InvocationID=inv,NRestarts='0',ActiveState='active',SubState='running')
            try:
                expected=expected_tokens(exe,['-u',child])
                _,proof=await_stable(lambda:observe_process(state,expected),dict(MainPID='0',InvocationID='old'),expected)
                self.assertTrue(proof['ARGV_MATCH'] and proof['MAINPID_STABLE'])
                p.stdin.write(json.dumps(texts)+'\n');p.stdin.flush()
                emitted=[p.stdout.readline().rstrip('\n') for _ in texts]
                scope=dict(scope5(),schema=6,pid=str(p.pid),exe=exe,invocation=inv,uid=str(os.getuid()),gid=str(os.getgid()))
                es=entries(emitted)
                for item in es:item.update(_PID=scope['pid'],_EXE=exe,_SYSTEMD_INVOCATION_ID=inv,_UID=scope['uid'],_GID=scope['gid'])
                def command(*args):
                    self.assertIn('--all',args);self.assertIn('--after-cursor=before',args)
                    return '\n'.join(json.dumps(x) for x in es)
                with patch.object(operations,'command',command),patch.object(operations,'load',return_value=scope),patch.object(operations,'save'):
                    if negative:
                        with self.assertRaises(RuntimeError):operations.journal(inv)
                    else:
                        result=schema6.bootstrap_outcome(operations.journal(inv),True)
                        self.assertEqual('DiagnosticStartupFailed',result['RESULT']);self.assertFalse(result['READY'])
                self.assertEqual(1,p.wait(timeout=5));self.assertEqual('',p.stderr.read())
            finally:
                if p.poll() is None:p.kill();p.wait(timeout=5)
                for stream in (p.stdin,p.stdout,p.stderr):stream.close()

if __name__=='__main__':unittest.main()

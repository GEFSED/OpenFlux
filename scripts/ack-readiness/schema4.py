"""Schema 4: explicit startup envelope + frozen schema-3 numeric accounting."""
from schema3 import SchemaError, require, validate_snapshot as validate_v3

STAGES = frozenset('process_start diagnostic_init transport_start authorization relay_workers proxy_init snapshot_loop'.split())
CLASSES = frozenset('''none auth_client_config_missing auth_challenge_or_captcha_classified
auth_other transport_start_other relay_workers_failure proxy_init_failure
diagnostic_init_failure schema_emit_failure unknown_startup_failure'''.split())
FLAGS = ('transport_started', 'authorization_completed', 'relay_workers_started',
         'proxy_initialized', 'diagnostic_snapshot_loop_started')
KEYS = frozenset(('schema', 'event', 'ordinal', 'startup_stage', 'startup_result', 'failure_class', *FLAGS))
STAGE_FLAG = dict(zip(('transport_start', 'authorization', 'relay_workers', 'proxy_init', 'snapshot_loop'), FLAGS))
CLASS_STAGE = {'auth_client_config_missing':'authorization',
    'auth_challenge_or_captcha_classified':'authorization', 'auth_other':'authorization',
    'transport_start_other':'transport_start', 'relay_workers_failure':'relay_workers',
    'proxy_init_failure':'proxy_init', 'diagnostic_init_failure':'diagnostic_init'}

def validate_snapshot(v):
    require(type(v) is dict and type(v.get('schema')) is int and v['schema']==4, 'unsupported_schema_version')
    validate_v3(dict(v, schema=3))
    return v

def validate_startup(e):
    require(type(e) is dict and e.keys()==KEYS, 'unknown_or_missing_startup_field')
    require(type(e['schema']) is int and e['schema']==4 and e['event']=='startup', 'unsupported_startup_envelope')
    require(type(e['ordinal']) is int and 1<=e['ordinal']<=256, 'invalid_startup_ordinal')
    require(type(e['startup_stage']) is str and e['startup_stage'] in STAGES, 'unknown_startup_stage')
    require(type(e['startup_result']) is str and e['startup_result'] in ('begin','ok','failure'), 'unknown_startup_result')
    require(type(e['failure_class']) is str and e['failure_class'] in CLASSES, 'unknown_startup_failure_class')
    require(all(type(e[k]) is bool for k in FLAGS), 'invalid_startup_flag')
    require((e['startup_result']=='failure')==(e['failure_class']!='none'), 'startup_failure_mismatch')
    if e['failure_class'] in CLASS_STAGE:
        require(e['startup_stage']==CLASS_STAGE[e['failure_class']], 'startup_failure_stage_mismatch')
    return e

class StartupStream:
    def __init__(self):
        self.last=None
    def accept(self,e):
        validate_startup(e)
        previous=self.last
        require(e['ordinal']==(previous['ordinal']+1 if previous else 1), 'startup_event_gap_or_duplicate')
        require(not previous or previous['startup_result']!='failure', 'startup_event_after_failure')
        require(previous is not None or e['startup_stage']=='process_start', 'missing_process_start_event')
        flags={k:previous[k] if previous else False for k in FLAGS}
        if e['startup_result']=='ok' and e['startup_stage'] in STAGE_FLAG:
            flags[STAGE_FLAG[e['startup_stage']]]=True
        # A sink failure may be reported after completing the observed stage.
        if e['startup_result']=='failure' and e['failure_class'] in ('schema_emit_failure','diagnostic_init_failure'):
            key=STAGE_FLAG.get(e['startup_stage'])
            if key and e[key]:flags[key]=True
        require(all(e[k]==flags[k] for k in FLAGS), 'startup_flags_not_observed')
        require(not e['relay_workers_started'] or e['authorization_completed'], 'relay_before_authorization')
        require(not e['proxy_initialized'] or e['transport_started'], 'proxy_before_transport')
        self.last=e

def validate_numeric(v):
    if type(v) is dict and v.get('schema')==4:return validate_snapshot(v)
    return validate_v3(v)

class DiagnosticStartupFailed(RuntimeError):
    def __init__(self,stage,failure,classified):
        self.safe=dict(RESULT='DiagnosticStartupFailed', STARTUP_STAGE=stage,
            STARTUP_FAILURE_CLASS=failure, CLASSIFIED_EVENT_OBSERVED=classified, READY=False)
        super().__init__('diagnostic_startup_failed')

def startup_outcome(records, process_exited=False):
    stream=StartupStream()
    for r in records:
        if 'startup' in r:stream.accept(r['startup'])
    last=stream.last
    if last and last['startup_result']=='failure':
        raise DiagnosticStartupFailed(last['startup_stage'],last['failure_class'],True)
    if process_exited:
        raise DiagnosticStartupFailed(last['startup_stage'] if last else 'process_start', 'unknown_startup_failure',False)
    return last

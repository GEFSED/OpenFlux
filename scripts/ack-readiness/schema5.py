"""Explicit schema 5: unchanged startup/ACK data plus closed bootstrap metadata."""
import schema4
from schema3 import require

SCHEMA = 5
ENUMS = {
 'http_status_class': {'2XX','3XX','4XX','5XX','OTHER'},
 'content_type_class': {'HTML','JSON','TEXT','OTHER','MISSING','INVALID'},
 'content_encoding_class': {'IDENTITY','GZIP_AUTO_DECODED','GZIP_ENCODED','BR_ENCODED','DEFLATE_ENCODED','OTHER_ENCODED'},
 'body_read_error_class': {'NONE','UNEXPECTED_EOF','TIMEOUT','CONNECTION_RESET','DECOMPRESSION_ERROR','OTHER_IO_ERROR'},
 'final_route_class': {'YANDEX_DOCS','YANDEX_AUTH','YANDEX_CAPTCHA_OR_CHALLENGE','YANDEX_PERMISSION_OR_ERROR','OTHER_YANDEX','UNEXPECTED_HOST_CLASS','UNKNOWN_ROUTE'},
 'response_result': {'CLIENT_CONFIG_FOUND','CLIENT_CONFIG_NOT_FOUND_2XX','CLIENT_CONFIG_NOT_FOUND_NON2XX','EMPTY_BODY','BODY_READ_FAILED','BODY_READ_PARTIAL','UNSUPPORTED_ENCODING','REDIRECT','REDIRECT_LIMIT','NETWORK_ERROR','UNKNOWN_RESPONSE'},
 'client_config_parse_result': {'NOT_ATTEMPTED','OK','ERROR'},
}
BOOLS = set('''body_read_complete client_config_searched client_config_matched
client_config_match_count_capped client_config_search_on_non2xx is_html
has_client_config_pattern has_expected_docs_bootstrap_structure has_known_auth_structure
has_known_challenge_structure has_known_permission_error_structure structure_scan_complete'''.split())
INTS = {'schema','ordinal','http_status_code','body_bytes_read','client_config_match_count'}
KEYS = set(ENUMS)|BOOLS|INTS|{'event'}

def validate_bootstrap(v):
    require(type(v) is dict and v.keys()==KEYS,'unknown_or_missing_bootstrap_field')
    require(all(type(v[k]) is int and 0<=v[k]<2**64 for k in INTS),'invalid_bootstrap_number')
    require(v['schema']==5 and v['event']=='bootstrap_response','unsupported_bootstrap_envelope')
    require(1<=v['ordinal']<=11,'bootstrap_ordinal_limit')
    require(all(type(v[k]) is bool for k in BOOLS),'invalid_bootstrap_boolean')
    require(all(type(v[k]) is str and v[k] in options for k,options in ENUMS.items()),'unknown_bootstrap_enum')
    code=v['http_status_code']
    require(code==0 or 100<=code<=999,'invalid_http_status_code')
    status=str(code//100)+'XX' if 200<=code<600 else 'OTHER'
    require(v['http_status_class']==status,'http_status_class_mismatch')
    require(v['client_config_match_count']<=64,'match_count_limit')
    require(not v['client_config_match_count_capped'] or v['client_config_match_count']==64,'match_cap_mismatch')
    require(v['has_client_config_pattern']==(v['client_config_match_count']>0),'pattern_count_mismatch')
    require(v['client_config_matched']==(v['client_config_searched'] and v['has_client_config_pattern']),'search_match_mismatch')
    require(v['client_config_search_on_non2xx']==(v['client_config_searched'] and status!='2XX'),'non2xx_search_mismatch')
    require(not v['client_config_searched'] or status!='3XX','redirect_cannot_be_searched')
    require((v['client_config_parse_result']!='NOT_ATTEMPTED')==v['client_config_matched'],'parse_search_mismatch')
    require(v['final_route_class']!='YANDEX_CAPTCHA_OR_CHALLENGE' or v['has_known_challenge_structure'],'challenge_without_structure')
    terminal=code==0
    if terminal:
        require(v['response_result'] in ('NETWORK_ERROR','REDIRECT_LIMIT','UNKNOWN_RESPONSE'),'terminal_result_mismatch')
        require(v['body_bytes_read']==0 and not any(v[k] for k in BOOLS),'terminal_without_response')
        require(v['content_type_class']=='MISSING' and v['content_encoding_class']=='IDENTITY','terminal_metadata_mismatch')
        require(v['client_config_match_count']==0,'terminal_matches')
        require(v['response_result']!='REDIRECT_LIMIT' or v['body_read_error_class']=='NONE','redirect_limit_read_error')
    else:
        require(v['body_read_complete']==(v['body_read_error_class']=='NONE'),'read_complete_mismatch')
        if not v['body_read_complete']:expected='BODY_READ_PARTIAL' if v['body_bytes_read'] else 'BODY_READ_FAILED'
        elif v['content_encoding_class'] not in ('IDENTITY','GZIP_AUTO_DECODED'):expected='UNSUPPORTED_ENCODING'
        elif status=='3XX':expected='REDIRECT'
        elif v['body_bytes_read']==0:expected='EMPTY_BODY'
        elif v['client_config_matched']:expected='CLIENT_CONFIG_FOUND'
        elif v['client_config_searched']:expected='CLIENT_CONFIG_NOT_FOUND_2XX' if status=='2XX' else 'CLIENT_CONFIG_NOT_FOUND_NON2XX'
        else:expected='UNKNOWN_RESPONSE'
        require(v['response_result']==expected,'bootstrap_result_mismatch')
    return v

def validate_startup(e):
    require(type(e) is dict and type(e.get('schema')) is int and e['schema']==5,'unsupported_startup_schema5')
    schema4.validate_startup(dict(e,schema=4))
    return e

def validate_snapshot(e):
    require(type(e) is dict and type(e.get('schema')) is int and e['schema']==5,'unsupported_snapshot_schema5')
    schema4.validate_snapshot(dict(e,schema=4));return e

class StartupStream(schema4.StartupStream):
    def accept(self,e):
        validate_startup(e);super().accept(dict(e,schema=4))

class BootstrapStream:
    def __init__(self):self.ordinal=0;self.closed=False;self.last=None
    def accept(self,e,startup):
        validate_bootstrap(e)
        require(startup is not None and startup['startup_result']!='failure' and not startup['transport_started'],'bootstrap_outside_startup')
        require(e['ordinal']==self.ordinal+1 and not self.closed,'bootstrap_gap_duplicate_or_after_final')
        self.ordinal=e['ordinal'];self.last=e
        self.closed=e['http_status_class']!='3XX'

def validate_numeric(v):
    if type(v) is dict and v.get('schema')==5:return validate_snapshot(v)
    return schema4.validate_numeric(v)

DiagnosticStartupFailed=schema4.DiagnosticStartupFailed
def startup_outcome(records,process_exited=False):
    stream=None
    for r in records:
        if 'startup' not in r:continue
        e=r['startup']
        if stream is None:stream=StartupStream() if e.get('schema')==5 else schema4.StartupStream()
        stream.accept(e)
    last=stream.last if stream else None
    if last and last['startup_result']=='failure':
        raise DiagnosticStartupFailed(last['startup_stage'],last['failure_class'],True)
    if process_exited:raise DiagnosticStartupFailed(last['startup_stage'] if last else 'process_start','unknown_startup_failure',False)
    return last

def bootstrap_outcome(records,process_exited=False):
    """A classified failure is useful evidence, NEVER readiness success."""
    responses=[r['bootstrap'] for r in records if 'bootstrap' in r]
    for e in responses:validate_bootstrap(e)
    try:last=startup_outcome(records,process_exited)
    except DiagnosticStartupFailed as error:
        return dict(error.safe,BOOTSTRAP_RESPONSES=responses)
    return dict(RESULT='BootstrapObserved' if responses else 'BootstrapEvidenceMissing',READY=False,BOOTSTRAP_RESPONSES=responses,STARTUP_STAGE=last['startup_stage'] if last else 'process_start')

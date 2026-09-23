"""Closed schema 6. Structure evidence never authorizes a request or readiness."""
import schema4
import schema5
from schema3 import require

SCHEMA = 6
ENUMS = {
 'final_path_class': set('''DOCS_ROOT DOCS_EDITOR_LIKE DOCS_VIEWER_LIKE DOCS_PUBLIC_SHARE_LIKE
 DOCS_ERROR_LIKE DOCS_UNKNOWN_PATH_CLASS YANDEX_AUTH_PATH_CLASS YANDEX_CHALLENGE_PATH_CLASS
 OTHER_YANDEX_PATH_CLASS UNKNOWN_PATH_CLASS'''.split()),
 'root_container_class': set('NONE KNOWN_PUBLIC_DOCS_ROOT KNOWN_EDITOR_ROOT GENERIC_APP_ROOT MULTIPLE_ROOTS UNKNOWN'.split()),
 'initial_store_structure_class': set('ABSENT PUBLIC_LANDING_KNOWN_SHAPE EDITOR_COMPATIBLE_REQUIRED_FIELDS_PRESENT EDITOR_REQUIRED_FIELDS_ABSENT MALFORMED UNKNOWN_SHAPE'.split()),
 'other_inline_bootstrap_structure_class': set('ABSENT REQUIRED_OPENFLUX_FIELDS_PRESENT POSSIBLE_BOOTSTRAP_WITHOUT_REQUIRED_FIELDS MULTIPLE_CANDIDATES MALFORMED UNKNOWN'.split()),
 'browser_compatibility_marker': {'YES','NO','UNKNOWN'},
 'structure_scan_limit_reason': set('NONE HTML_SIZE TOKEN_COUNT SCRIPT_COUNT INLINE_JSON_SIZE DEPTH OTHER'.split()),
 'page_structure_class': set('''LEGACY_EDITOR_CLIENT_CONFIG PUBLIC_LANDING_INITIAL_STORE
 EDITOR_LIKE_INLINE_BOOTSTRAP EDITOR_LIKE_NO_REQUIRED_BOOTSTRAP BROWSER_COMPATIBILITY
 AUTH_PAGE CHALLENGE_PAGE PERMISSION_ERROR_PAGE GENERIC_DOCS_SHELL AMBIGUOUS_STRUCTURE UNKNOWN_DOCS_HTML'''.split()),
}
SCRIPT_COUNTS=set('script_total_count script_inline_count script_external_count script_json_count script_module_count candidate_container_count'.split())
COUNTS=SCRIPT_COUNTS|set('form_count iframe_count noscript_count stylesheet_count'.split())
BOOLS=set('initial_store_present other_inline_bootstrap_present required_auth_field_shape_present optional_ttl_field_present structural_classification_conflict'.split())
MARKERS=set('legacy_client_config public_initial_store editor_root generic_root'.split())
FAMILIES=set('YANDEX_STATIC_PSF YANDEX_STATIC_DOCS YANDEX_STATIC_GENERIC OTHER_YANDEX_STATIC THIRD_PARTY_STATIC RELATIVE_STATIC UNKNOWN_STATIC_FAMILY'.split())
NEW_KEYS=set(ENUMS)|COUNTS|BOOLS|{'expected_docs_app_markers','static_resource_family_classes'}
KEYS=schema5.KEYS|NEW_KEYS

def validate_bootstrap(v):
    require(type(v) is dict and v.keys()==KEYS,'unknown_or_missing_schema6_field')
    require(type(v['schema']) is int and v['schema']==6,'unsupported_schema6')
    schema5.validate_bootstrap(dict({k:v[k] for k in schema5.KEYS},schema=5))
    require(all(type(v[k]) is str and v[k] in options for k,options in ENUMS.items()),'invalid_structure_enum')
    require(all(type(v[k]) is bool for k in BOOLS),'invalid_structure_boolean')
    require(all(type(v[k]) is int and 0<=v[k]<=65536 for k in COUNTS),'invalid_structure_count')
    require(all(v[k]<=1024 for k in SCRIPT_COUNTS),'script_count_limit')
    m=v['expected_docs_app_markers'];f=v['static_resource_family_classes']
    require(type(m) is dict and m.keys()==MARKERS and all(type(x) is bool for x in m.values()),'invalid_marker_contract')
    require(type(f) is dict and f.keys()==FAMILIES and all(type(x) is int and 0<=x<=1024 for x in f.values()),'invalid_resource_family_contract')
    total=v['script_total_count']
    require(v['script_inline_count']+v['script_external_count']==total,'script_partition_mismatch')
    require(v['script_json_count']+v['script_module_count']<=total,'script_type_count_mismatch')
    require(sum(f.values())==v['script_external_count'],'resource_family_count_mismatch')
    require(v['candidate_container_count']<=v['script_inline_count'],'candidate_container_count_mismatch')
    require(not v['optional_ttl_field_present'] or v['required_auth_field_shape_present'],'ttl_without_required_shape')
    require(not v['required_auth_field_shape_present'] or v['candidate_container_count']>0,'shape_without_candidate')
    complete=v['structure_scan_complete']
    require(complete==(v['structure_scan_limit_reason']=='NONE'),'scan_limit_mismatch')
    require(not complete or v['body_read_complete'],'partial_body_cannot_prove_absence')
    require(not complete or v['body_bytes_read']<=1048576,'html_limit_mismatch')
    limit=v['structure_scan_limit_reason']
    require(limit!='HTML_SIZE' or v['body_bytes_read']>1048576,'unreached_html_limit')
    require(limit!='SCRIPT_COUNT' or total==1024,'unreached_script_limit')
    require(limit!='INLINE_JSON_SIZE' or v['body_bytes_read']>786432,'unreached_json_limit')
    require(v['initial_store_present']==(v['initial_store_structure_class']!='ABSENT'),'initial_store_presence_mismatch')
    require(v['other_inline_bootstrap_present']==(v['other_inline_bootstrap_structure_class']!='ABSENT'),'other_container_presence_mismatch')
    require(m['public_initial_store']==v['initial_store_present'],'initial_marker_mismatch')
    if complete:
        require(m['legacy_client_config']==v['has_expected_docs_bootstrap_structure'],'legacy_marker_changed')
    require(not m['legacy_client_config'] or total>0,'legacy_without_script')
    require(not m['public_initial_store'] or total>0,'initial_store_without_script')
    require(not v['other_inline_bootstrap_present'] or v['candidate_container_count']>0,'other_without_candidate')
    initial=v['initial_store_structure_class'];other=v['other_inline_bootstrap_structure_class']
    require(initial!='EDITOR_COMPATIBLE_REQUIRED_FIELDS_PRESENT' or v['required_auth_field_shape_present'],'initial_shape_mismatch')
    require(other!='REQUIRED_OPENFLUX_FIELDS_PRESENT' or v['required_auth_field_shape_present'],'other_shape_mismatch')
    require(other!='MULTIPLE_CANDIDATES' or v['candidate_container_count']>=2,'multiple_candidate_count')
    root=v['root_container_class']
    if root=='NONE':require(not m['editor_root'] and not m['generic_root'],'root_missing')
    if root=='KNOWN_EDITOR_ROOT':require(m['editor_root'] and not m['generic_root'],'editor_root_mismatch')
    if root=='GENERIC_APP_ROOT':require(m['generic_root'] and not m['editor_root'],'generic_root_mismatch')
    if root=='KNOWN_PUBLIC_DOCS_ROOT':require(m['generic_root'] and not m['editor_root'] and initial=='PUBLIC_LANDING_KNOWN_SHAPE','public_root_mismatch')
    if root=='MULTIPLE_ROOTS':require(m['generic_root'] or m['editor_root'],'multiple_root_mismatch')
    # Recompute positive classes, not a permissive list of arbitrary results.
    classes=[]
    if m['legacy_client_config']:classes.append('LEGACY_EDITOR_CLIENT_CONFIG')
    if initial=='PUBLIC_LANDING_KNOWN_SHAPE':classes.append('PUBLIC_LANDING_INITIAL_STORE')
    if initial=='EDITOR_COMPATIBLE_REQUIRED_FIELDS_PRESENT' or other=='REQUIRED_OPENFLUX_FIELDS_PRESENT':classes.append('EDITOR_LIKE_INLINE_BOOTSTRAP')
    if complete and m['editor_root'] and not v['required_auth_field_shape_present'] and not m['legacy_client_config'] and initial!='PUBLIC_LANDING_KNOWN_SHAPE':classes.append('EDITOR_LIKE_NO_REQUIRED_BOOTSTRAP')
    if v['browser_compatibility_marker']=='YES':classes.append('BROWSER_COMPATIBILITY')
    for flag,page in [('has_known_auth_structure','AUTH_PAGE'),('has_known_challenge_structure','CHALLENGE_PAGE'),('has_known_permission_error_structure','PERMISSION_ERROR_PAGE')]:
        if v[flag]:classes.append(page)
    conflict=v['structural_classification_conflict']
    require(not (len(classes)>1 or other=='MULTIPLE_CANDIDATES') or conflict,'conflict_hidden')
    require(not conflict or len(classes)>1 or other=='MULTIPLE_CANDIDATES' or (v['initial_store_present'] and initial=='UNKNOWN_SHAPE'),'conflict_without_evidence')
    page=('AMBIGUOUS_STRUCTURE' if conflict else 'UNKNOWN_DOCS_HTML' if not complete else
          classes[0] if classes else 'GENERIC_DOCS_SHELL' if m['generic_root'] else 'UNKNOWN_DOCS_HTML')
    require(v['page_structure_class']==page,'page_class_mismatch')
    browser=('YES' if 'BROWSER_COMPATIBILITY' in classes else 'NO' if complete and page not in ('UNKNOWN_DOCS_HTML','AMBIGUOUS_STRUCTURE') else 'UNKNOWN')
    require(v['browser_compatibility_marker']==browser,'browser_evidence_mismatch')
    if v['http_status_code']==0:
        require(not any(v[k] for k in COUNTS|BOOLS) and not any(m.values()) and sum(f.values())==0,'terminal_structure_without_response')
        require(page=='UNKNOWN_DOCS_HTML' and root=='NONE' and v['structure_scan_limit_reason']=='OTHER','terminal_structure_mismatch')
    return v

def validate_startup(v):
    require(type(v) is dict and type(v.get('schema')) is int and v['schema']==6,'unsupported_startup_schema6')
    schema4.validate_startup(dict(v,schema=4));return v

def validate_snapshot(v):
    require(type(v) is dict and type(v.get('schema')) is int and v['schema']==6,'unsupported_snapshot_schema6')
    schema4.validate_snapshot(dict(v,schema=4));return v

class StartupStream(schema4.StartupStream):
    def accept(self,e):validate_startup(e);super().accept(dict(e,schema=4))

class BootstrapStream(schema5.BootstrapStream):
    def accept(self,e,startup):
        validate_bootstrap(e)
        require(startup is not None and startup['startup_result']!='failure' and not startup['transport_started'],'bootstrap_outside_startup')
        require(e['ordinal']==self.ordinal+1 and not self.closed,'bootstrap_gap_duplicate_or_after_final')
        self.ordinal=e['ordinal'];self.last=e;self.closed=e['http_status_class']!='3XX'

DiagnosticStartupFailed=schema4.DiagnosticStartupFailed

def startup_outcome(records,process_exited=False):
    startup=[r['startup'] for r in records if 'startup' in r]
    if not startup or startup[0].get('schema')!=6:return schema5.startup_outcome(records,process_exited)
    stream=StartupStream()
    for e in startup:stream.accept(e)
    last=stream.last
    if last['startup_result']=='failure':raise DiagnosticStartupFailed(last['startup_stage'],last['failure_class'],True)
    if process_exited:raise DiagnosticStartupFailed(last['startup_stage'],'unknown_startup_failure',False)
    return last

def bootstrap_outcome(records,process_exited=False):
    responses=[r['bootstrap'] for r in records if 'bootstrap' in r]
    for e in responses:validate_bootstrap(e)
    try:last=startup_outcome(records,process_exited)
    except DiagnosticStartupFailed as error:return dict(error.safe,BOOTSTRAP_RESPONSES=responses)
    return dict(RESULT='BootstrapObserved' if responses else 'BootstrapEvidenceMissing',READY=False,BOOTSTRAP_RESPONSES=responses,STARTUP_STAGE=last['startup_stage'] if last else 'process_start')

def validate_numeric(v):
    if type(v) is dict and v.get('schema')==6:return validate_snapshot(v)
    return schema5.validate_numeric(v)

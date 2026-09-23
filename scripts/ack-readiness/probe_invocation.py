"""One-shot identity ledger. Metadata only; no provider or control operations.

Historical NRestarts is never an epoch. Count all target-unit invocation IDs and
PID1's structured UNIT_STARTED events from the immutable cursor, including
short-lived invocations missed between /proc samples. Missing evidence fails
closed. This is not an audit against a hostile root deleting journal entries.
"""
import json
import re
from probe_boundary import ProbeFailure, counter, guard_context, NOT_OBSERVED

UNIT_STARTED = '39f53479d3a045ac8e11786248231fbf'
UNIT_STARTING = '7d4958e842da4a758f6c1cdc7b36dcc5'
UNIT_RESTART_SCHEDULED = '5eb03494b6584870a536b337290809b3'
FIELDS = ('__CURSOR','__MONOTONIC_TIMESTAMP','_BOOT_ID','_PID','_EXE',
          '_SYSTEMD_UNIT','_SYSTEMD_INVOCATION_ID','UNIT','INVOCATION_ID',
          'MESSAGE_ID','JOB_ID')


def fail(code):raise ProbeFailure(code)


def read_ledger(command, boundary):
    """Verify the original cursor still exists; never substitute --since/now.

    journalctl may seek to a nearby record for a stale cursor; equality matters.
    --sync ensures already-submitted manager events are visible at final checks.
    No MESSAGE, CMDLINE, URL, environment or body is exported.
    """
    command('journalctl','--sync')
    cursor=boundary['scope']['cursor']
    try:
        anchor=json.loads(command('journalctl','--no-pager','--all','-o','json',
            '--cursor='+cursor,'-n','1','--output-fields=__CURSOR').strip())
        if anchor.get('__CURSOR')!=cursor:fail('JOURNAL_BOUNDARY_UNAVAILABLE')
        raw=command('journalctl','--no-pager','--all','-o','json',
            '--after-cursor='+cursor,'-u',boundary['target_unit'],
            '--output-fields='+','.join(FIELDS))
        rows=[json.loads(line) for line in raw.splitlines()]
        if not all(type(row) is dict for row in rows):fail('INVOCATION_JOURNAL_MALFORMED')
        return rows
    except (ValueError,TypeError):fail('INVOCATION_JOURNAL_MALFORMED')


class InvocationEpoch:
    def __init__(self,boundary):
        self.boundary=boundary
        self.control_calls=0
        self.invocations=set()
        self.started=set()
        self.starting=set()
        self.scheduled=set()
        self.seen=set()
        self.authorized=None
        self.pid=None
        self.exec_start=None
        self.post_baseline=None
        self.current=None

    def consume_start(self):
        # Consumed BEFORE issuing the command, even when spawn/SSH fails.
        if self.control_calls:raise ProbeFailure('SECOND_CONTROL_START_FORBIDDEN','OPERATOR_CONTROL_FAILURE')
        self.control_calls=1

    def add_invocation(self,value):
        if not re.fullmatch('[a-f0-9]{32}',value or ''):fail('INVOCATION_ID_UNAVAILABLE')
        if value==self.boundary['baseline_invocation']:
            fail('HISTORICAL_INVOCATION_AFTER_BOUNDARY')
        self.invocations.add(value)
        if len(self.invocations)>1:fail('ADDITIONAL_INVOCATION_OBSERVED')
        if not self.control_calls:fail('INVOCATION_BEFORE_AUTHORIZED_START')

    def journal(self,rows):
        present=set()
        for row in rows:
            try:
                t=int(row['__MONOTONIC_TIMESTAMP'])
                cursor=row['__CURSOR']
                if type(cursor) is not str or not cursor:raise ValueError()
            except (KeyError,TypeError,ValueError):fail('INVOCATION_JOURNAL_MALFORMED')
            if cursor==self.boundary['scope']['cursor']:continue
            if row.get('_BOOT_ID')!=self.boundary['context']['boot_id']:fail('BOOT_ID_CHANGED')
            if t<self.boundary['scope']['start_monotonic_us']:continue
            present.add(cursor)
            if cursor in self.seen:continue
            manager=(row.get('_PID')=='1' and row.get('_EXE') in
                     ('/usr/lib/systemd/systemd','/lib/systemd/systemd') and
                     row.get('UNIT')==self.boundary['target_unit'])
            app=row.get('_SYSTEMD_UNIT')==self.boundary['target_unit']
            if app:
                self.add_invocation(row.get('_SYSTEMD_INVOCATION_ID'))
            elif manager:
                ident=row.get('INVOCATION_ID')
                # A stop record can refer to the historical invocation. A start
                # record cannot. Only metadata from trusted PID1 is authoritative.
                msg=row.get('MESSAGE_ID')
                if msg in (UNIT_STARTING,UNIT_STARTED):
                    if ident:self.add_invocation(ident)
                    (self.started if msg==UNIT_STARTED else self.starting).add(cursor)
                    if len(self.started)>1 or len(self.starting)>1:fail('ADDITIONAL_INVOCATION_OBSERVED')
                    if not self.control_calls:fail('INVOCATION_BEFORE_AUTHORIZED_START')
                elif msg==UNIT_RESTART_SCHEDULED:
                    self.scheduled.add(cursor)
        if self.seen-present:fail('INVOCATION_JOURNAL_HISTORY_LOST')
        self.seen=present

    def observe(self,state,context,rows):
        guard_context(self.boundary['context'],context)
        self.journal(rows) # second invocation has priority over secondary counter
        if state.get('Restart')!='no':fail('RESTART_GUARD_CHANGED')
        inv=state.get('InvocationID','')
        if inv and inv!=self.boundary['baseline_invocation']:
            self.add_invocation(inv)
            pid=state.get('MainPID','0')
            stamp=state.get('ExecMainStartTimestampMonotonic')
            if not str(stamp or '').isdigit() or int(stamp)<self.boundary['scope']['start_monotonic_us']:
                fail('EXEC_START_BOUNDARY_MISMATCH')
            if self.authorized is None:
                # Establish only after the process has actually been spawned;
                # a pending start/job with no ExecMain PID is not a new epoch.
                exec_pid=pid if pid!='0' else state.get('ExecMainPID','0')
                if not re.fullmatch('[1-9][0-9]*',exec_pid):return
                self.authorized=inv;self.pid=exec_pid;self.exec_start=stamp
                self.post_baseline=counter(state['NRestarts'])
            if inv!=self.authorized:fail('ADDITIONAL_INVOCATION_OBSERVED')
            if stamp!=self.exec_start or (pid!='0' and pid!=self.pid):fail('AUTHORIZED_PROCESS_IDENTITY_CHANGED')
        elif self.authorized is not None:
            fail('AUTHORIZED_INVOCATION_CONTEXT_LOST')
        self.current=counter(state['NRestarts'])
        if self.post_baseline is not None:
            if self.current<self.post_baseline:fail('POST_START_NRESTARTS_EPOCH_INVALIDATED')
            if self.current>self.post_baseline:fail('AUTOMATIC_RESTART_COUNTER_ADVANCED')
        if self.scheduled:fail('AUTOMATIC_RESTART_SCHEDULED')

    def finish(self):
        if (self.control_calls!=1 or self.authorized is None or
            self.invocations!={self.authorized} or len(self.started)!=1):
            fail('ONE_SHOT_INVOCATION_PROOF_INCOMPLETE')

    def fields(self):
        delta=(self.current-self.post_baseline if self.post_baseline is not None
               and self.current is not None and self.current>=self.post_baseline else NOT_OBSERVED)
        return dict(CONTROL_START_CALL_COUNT=self.control_calls,AUTHORIZED_START_COUNT=self.control_calls,
            OBSERVED_INVOCATION_COUNT=len(self.invocations),
            UNAUTHORIZED_ADDITIONAL_INVOCATIONS=max(0,len(self.invocations)-1,len(self.started)-1,len(self.starting)-1),
            AUTHORIZED_INVOCATION_ID=self.authorized or NOT_OBSERVED,AUTHORIZED_MAINPID=self.pid or NOT_OBSERVED,
            AUTHORIZED_EXEC_START_TIMESTAMP_MONOTONIC=self.exec_start or NOT_OBSERVED,
            POST_START_NRESTARTS_BASELINE=self.post_baseline if self.post_baseline is not None else NOT_OBSERVED,
            CURRENT_NRESTARTS=self.current if self.current is not None else NOT_OBSERVED,
            AUTOMATIC_RESTART_COUNTER_DELTA=delta,AUTOMATIC_RESTART_DELTA=delta,
            OBSERVED_SYSTEMD_STARTED_EVENTS=len(self.started),SCHEDULED_RESTART_EVENTS=len(self.scheduled))

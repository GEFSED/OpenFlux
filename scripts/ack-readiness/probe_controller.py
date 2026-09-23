"""Local durable evidence controller. Execution needs separate user authorization.

No deployment, key management or retry implementation. The SSH worker must
already have separately grounded inputs. This file is NEVER run by CI against
a VPS; tests use pipes and an injected mock worker.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys

from probe_boundary import LocalEvidence, ProbeFailure, atomic_write


def receive(process, directory):
    store = LocalEvidence(directory)
    history = []
    write_error = None
    for line in process.stdout:
        try:
            event = json.loads(line)
            if type(event) is not dict:
                raise ProbeFailure('MALFORMED_WORKER_EVENT')
            if event.get('kind') == 'durable_evidence':
                try:
                    answer = store.accept(event['name'], event['payload'])
                except (ProbeFailure, OSError, ValueError, KeyError) as error:
                    answer = {'ack': 'REJECTED'}
                    write_error = error.code if isinstance(error, ProbeFailure) else 'LOCAL_EVIDENCE_FAILED'
                process.stdin.write(json.dumps(answer) + '\n')
                process.stdin.flush()
            else:
                # Worker output is already safe structured metadata; raw stderr
                # is never displayed or saved by the controller.
                history.append(event)
                atomic_write(store.root / 'worker-evidence.json', history)
        except (ValueError, KeyError, OSError, ProbeFailure):
            write_error = 'LOCAL_EVIDENCE_FAILED'
            # EOF makes the worker fail closed if it has not started yet.
            process.stdin.close()
            # Continue draining output, allowing the worker's cleanup to finish.
    code = process.wait(timeout=15)
    if write_error:
        raise ProbeFailure(write_error, 'OPERATOR_CONTROL_FAILURE')
    return code


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--authorized-one-shot', action='store_true', required=True)
    parser.add_argument('--remote-directory', required=True)
    parser.add_argument('--evidence-directory', type=Path, required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'/tmp/openflux-schema6-isolated-34ba35a\.[A-Za-z0-9]+', args.remote_directory):
        raise ProbeFailure('INVALID_REMOTE_TASK_DIRECTORY')
    # Refuse reuse before even connecting; no automatic resumption or retry.
    if args.evidence_directory.exists():
        raise ProbeFailure('EVIDENCE_ALREADY_EXISTS', 'OPERATOR_CONTROL_FAILURE')
    store = LocalEvidence(args.evidence_directory)
    atomic_write(store.root / 'controller-created.json', {'maximum_start_count': 1}, exclusive=True)
    process = subprocess.Popen(['ssh', '-o', 'BatchMode=yes', 'openflux-vps',
        'python3', '-u', args.remote_directory + '/isolated_probe.py', 'run-with-durable-controller'],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
        text=True, encoding='utf-8', bufsize=1)
    return receive(process, args.evidence_directory)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except ProbeFailure as error:
        print(json.dumps({'ERROR_CATEGORY': error.category, 'CONTROL_FAILURE_CLASS': error.code}))
        sys.exit(1)

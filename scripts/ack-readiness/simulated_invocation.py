"""Linux test process: emit unedited Engine fixtures, no network or systemd."""
import json
import sys
from fixtures import fixture

for command in sys.stdin:
    name = command.strip()
    if name == 'banner':
        print('written by p1neappleXpress', flush=True)
    elif name == 'suppressed':
        print('[ACK-LOG] suppressed=1', flush=True)
    else:
        values = fixture('idle' if name == 'malformed' else name)
        if name == 'malformed':
            values['unknown_required_structure'] = 1
        print('2026/09/23 12:00:00.000000 [ACK-DIAG] ' +
              json.dumps(values, separators=(',', ':')), flush=True)

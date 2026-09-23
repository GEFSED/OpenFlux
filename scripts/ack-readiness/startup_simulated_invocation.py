"""Linux child, no network: replay exact Go-generated startup serializer output."""
import json
import os
from pathlib import Path
import sys

name=sys.argv[1]
lines=json.loads((Path(os.environ['ACK_STARTUP_FIXTURE_DIR'])/(name+'.json')).read_text())
if sys.stdin.readline().strip()!='GO':raise SystemExit(2)
for line in lines:print(line,flush=True)
if name=='success':
    for _ in range(8):print(lines[-1],flush=True)
    if sys.stdin.readline().strip()!='STOP':raise SystemExit(2)
    raise SystemExit(0)
raise SystemExit(1)

"""Offline Linux child: emit Go-produced safe fixtures, never authorize."""
import json
import sys
for line in json.loads(sys.stdin.readline()):print(line,flush=True)
raise SystemExit(1)

"""Test-only access to Go-generated fixtures. Missing export is a test failure."""
import json
import os
from pathlib import Path


def fixture(name='idle'):
    return json.loads((Path(os.environ['ACK_SCHEMA_FIXTURE_DIR']) / (name + '.json')).read_text())

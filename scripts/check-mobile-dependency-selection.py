"""Linux CI: compare full original/current selected module versions.

Resolve the ORIGINAL mobile graph in a temporary module, never edit the checkout.
This catches any accidental network dependency upgrade caused by instrumentation.
"""
import json
from pathlib import Path
import subprocess
import tempfile

BASE='643e3cac04fcda79f55500e8b091144aac9d555b'
root=Path.cwd()

def original(path):
    return subprocess.check_output(['git','show',BASE+':'+path],text=True)

def graph(directory,mode):
    text=subprocess.check_output(['go','list','-mod='+mode,'-m','-json','all'],cwd=directory,text=True)
    decoder=json.JSONDecoder();values={}
    while text.strip():
        obj,end=decoder.raw_decode(text.lstrip());text=text.lstrip()[end:]
        values[obj['Path']]=obj.get('Version','local')
    return values

with tempfile.TemporaryDirectory(prefix='original-mobile-graph-') as d:
    target=Path(d)
    text=original('mobile/go.mod').replace('replace universal-bypass-tool => ..',
                                         'replace universal-bypass-tool => '+str(root))
    (target/'go.mod').write_text(text)
    (target/'go.sum').write_text(original('mobile/go.sum'))
    before=graph(target,'mod')
    after=graph(root/'mobile','readonly')
    assert before==after,'selected_dependency_graph_changed'
    assert after['golang.org/x/net']=='v0.59.0','unexpected_HTML_dependency'
    print('PASS: complete selected mobile module graph unchanged; x/net already v0.59.0')

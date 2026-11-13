#!/usr/bin/env python3
"""aprs-deviceid の tocalls.yaml から src/web/tocalls.json を作り直す。
使い方: curl -o tocalls.yaml https://raw.githubusercontent.com/aprsorg/aprs-deviceid/refs/heads/main/tocalls.yaml
        python3 scripts/update-tocalls.py tocalls.yaml  (要 PyYAML)
"""
import json, sys, yaml
d = yaml.safe_load(open(sys.argv[1], encoding='utf-8'))
out = {
    "source": "aprs-deviceid (https://github.com/aprsorg/aprs-deviceid) by Hessu OH7LZB, CC BY-SA 2.0",
    "classes": {c['class']: c['shown'] for c in d['classes']},
    "tocalls": [[t['tocall'], t.get('vendor', ''), t.get('model', ''), t.get('class', '')] for t in d['tocalls']],
}
json.dump(out, open('src/web/tocalls.json', 'w', encoding='utf-8'), ensure_ascii=False, separators=(',', ':'))

#!/usr/bin/env python3
"""Convert Go benchmark output to an HTML dashboard."""
import sys
import re
import json
from datetime import datetime

def parse_bench(lines):
    results = []
    for line in lines:
        m = re.match(r'(Benchmark\S+)\s+(\d+)\s+([\d.]+)\s+ns/op(?:\s+([\d.]+)\s+B/op)?(?:\s+(\d+)\s+allocs/op)?', line)
        if m:
            results.append({
                'name': m.group(1),
                'iterations': int(m.group(2)),
                'ns_per_op': float(m.group(3)),
                'bytes_per_op': float(m.group(4)) if m.group(4) else None,
                'allocs_per_op': int(m.group(5)) if m.group(5) else None,
            })
    return results

def to_html(results, timestamp):
    rows = ''
    for r in results:
        rows += f'''<tr>
            <td>{r['name']}</td>
            <td>{r['iterations']:,}</td>
            <td>{r['ns_per_op']:.1f}</td>
            <td>{r['bytes_per_op'] if r['bytes_per_op'] is not None else '-'}</td>
            <td>{r['allocs_per_op'] if r['allocs_per_op'] is not None else '-'}</td>
        </tr>\n'''
    
    return f'''<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>relay — Benchmark Dashboard</title>
<style>
  body {{ font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 2rem; }}
  h1 {{ color: #24292f; }}
  table {{ border-collapse: collapse; width: 100%; }}
  th, td {{ border: 1px solid #d0d7de; padding: 8px 12px; text-align: left; }}
  th {{ background: #f6f8fa; font-weight: 600; }}
  tr:nth-child(even) {{ background: #f6f8fa; }}
  .ts {{ color: #57606a; font-size: 0.9em; }}
</style>
</head>
<body>
<h1>relay — Benchmark Results</h1>
<p class="ts">Generated: {timestamp} · <a href="https://github.com/jhonsferg/relay">jhonsferg/relay</a></p>
<table>
<thead><tr><th>Benchmark</th><th>Iterations</th><th>ns/op</th><th>B/op</th><th>allocs/op</th></tr></thead>
<tbody>
{rows}
</tbody>
</table>
</body>
</html>'''

if __name__ == '__main__':
    path = sys.argv[1] if len(sys.argv) > 1 else '-'
    lines = open(path).readlines() if path != '-' else sys.stdin.readlines()
    results = parse_bench(lines)
    print(to_html(results, datetime.utcnow().strftime('%Y-%m-%d %H:%M UTC')))

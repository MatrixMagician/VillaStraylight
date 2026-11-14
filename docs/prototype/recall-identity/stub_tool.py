"""Stub of villa-tools-recall: serves villa's generated OpenAPI document and records every request.

It enforces the two gates the real unit enforces before any search (internal/toolserver): the
connection's bearer, and X-Villa-User-Id present exactly once matching ^[A-Za-z0-9_-]{1,64}$.
Each request is appended to OUT/tool-requests.jsonl with its headers, so the capture shows what
Open WebUI sent, including a request the gates refuse.

    python3 stub_tool.py <spec.json> <bearer> <out-dir> [port]
"""

import json
import re
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

SPEC_PATH, BEARER, OUT = sys.argv[1], sys.argv[2], sys.argv[3]
PORT = int(sys.argv[4]) if len(sys.argv) > 4 else 8093
IDENTITY = re.compile(r'^[A-Za-z0-9_-]{1,64}$')

with open(SPEC_PATH, 'rb') as f:
    SPEC = f.read()


def identity_verdict(values):
    if len(values) != 1:
        return f'refused: header present {len(values)} times'
    if not IDENTITY.match(values[0]):
        return 'refused: does not match ^[A-Za-z0-9_-]{1,64}$'
    return 'accepted'


class Handler(BaseHTTPRequestHandler):
    def _record(self, status, verdict, query):
        row = {
            'ts': time.time(),
            'method': self.command,
            'path': urlsplit(self.path).path,
            'query': query,
            'headers': [[k, v] for k, v in self.headers.items()],
            'x_villa_user_id': self.headers.get_all('X-Villa-User-Id') or [],
            'bearer_ok': self.headers.get('Authorization') == f'Bearer {BEARER}',
            'identity_verdict': verdict,
            'status': status,
        }
        with open(f'{OUT}/tool-requests.jsonl', 'a') as f:
            f.write(json.dumps(row) + '\n')

    def _send(self, status, body, ctype='application/json'):
        self.send_response(status)
        self.send_header('Content-Type', ctype)
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        parts = urlsplit(self.path)
        query = parse_qs(parts.query)
        if parts.path == '/healthz':
            return self._send(200, b'{"ok":true}')
        bearer_ok = self.headers.get('Authorization') == f'Bearer {BEARER}'
        if parts.path == '/openapi.json':
            status = 200 if bearer_ok else 401
            self._record(status, 'n/a', query)
            return self._send(status, SPEC if bearer_ok else b'{"error":"unauthorized"}')
        if parts.path == '/recall/search':
            verdict = identity_verdict(self.headers.get_all('X-Villa-User-Id') or [])
            if not bearer_ok:
                status, body = 401, {'error': 'unauthorized'}
            elif verdict != 'accepted':
                status, body = 403, {'error': 'identity required'}
            else:
                status = 200
                who = self.headers.get('X-Villa-User-Id')
                body = {
                    'results': [
                        {
                            'chat_id': 'stub-chat',
                            'content': f'<<stub recall result for {who}>>',
                            'score': 1.0,
                            'truncated': False,
                            'guard': {'detected': False, 'rules': []},
                        }
                    ]
                }
            self._record(status, verdict, query)
            return self._send(status, json.dumps(body).encode())
        self._record(404, 'n/a', query)
        return self._send(404, b'{"error":"not found"}')

    def do_POST(self):
        self._record(405, 'n/a', {})
        self._send(405, b'{"error":"method not allowed"}')

    def log_message(self, fmt, *args):
        sys.stderr.write('stub_tool: ' + fmt % args + '\n')


ThreadingHTTPServer(('0.0.0.0', PORT), Handler).serve_forever()

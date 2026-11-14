"""Stub OpenAI-compatible model: one recall tool call on the first turn, plain text after a tool result.

It answers /v1/models and /v1/chat/completions, streaming and not. When the request offers a
tool whose name ends in villa_recall_search and holds no tool result yet, it returns ONE call to
that tool with q set to the last user message, which carries the probe marker. Otherwise it
returns text, quoting the tool result when there is one. Every request body is appended to
OUT/model-requests.jsonl.

    python3 stub_model.py <out-dir> [port]
"""

import json
import sys
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

OUT = sys.argv[1]
PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 8080
MODEL = 'stub-model'
TOOL_SUFFIX = 'villa_recall_search'


def text_of(content):
    if isinstance(content, list):
        return ' '.join(p.get('text', '') for p in content if isinstance(p, dict))
    return content or ''


def decide(body):
    messages = body.get('messages') or []
    names = [(t.get('function') or {}).get('name', '') for t in body.get('tools') or []]
    target = next((n for n in names if n.endswith(TOOL_SUFFIX)), None)
    tool_results = [text_of(m.get('content')) for m in messages if m.get('role') == 'tool']
    users = [text_of(m.get('content')) for m in messages if m.get('role') == 'user']
    if target and not tool_results:
        marker = (users[-1] if users else 'no-user-message')[-200:]
        return {'tool': target, 'args': json.dumps({'q': marker})}, names
    if tool_results:
        return {'text': 'stub final answer; the tool returned: ' + tool_results[-1][:300]}, names
    return {'text': 'stub answer with no tool'}, names


class Handler(BaseHTTPRequestHandler):
    def _json(self, status, obj):
        body = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.rstrip('/') in ('/v1/models', '/models'):
            return self._json(200, {'object': 'list', 'data': [{'id': MODEL, 'object': 'model', 'owned_by': 'stub'}]})
        return self._json(404, {'error': 'not found'})

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get('Content-Length') or 0))
        if not self.path.rstrip('/').endswith('/chat/completions'):
            return self._json(404, {'error': 'not found'})
        body = json.loads(raw or b'{}')
        answer, names = decide(body)
        with open(f'{OUT}/model-requests.jsonl', 'a') as f:
            f.write(json.dumps({'ts': time.time(), 'stream': bool(body.get('stream')), 'offered_tools': names,
                                'answer': answer, 'body': body}) + '\n')
        cid = 'chatcmpl-' + uuid.uuid4().hex[:12]
        if 'tool' in answer:
            call = {'id': 'call_' + uuid.uuid4().hex[:12], 'type': 'function',
                    'function': {'name': answer['tool'], 'arguments': answer['args']}}
            message, finish = {'role': 'assistant', 'content': None, 'tool_calls': [call]}, 'tool_calls'
        else:
            message, finish = {'role': 'assistant', 'content': answer['text']}, 'stop'

        if not body.get('stream'):
            return self._json(200, {'id': cid, 'object': 'chat.completion', 'created': int(time.time()), 'model': MODEL,
                                    'choices': [{'index': 0, 'message': message, 'finish_reason': finish}],
                                    'usage': {'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 2}})

        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.send_header('Cache-Control', 'no-cache')
        self.end_headers()

        def chunk(delta, finish_reason=None):
            obj = {'id': cid, 'object': 'chat.completion.chunk', 'created': int(time.time()), 'model': MODEL,
                   'choices': [{'index': 0, 'delta': delta, 'finish_reason': finish_reason}]}
            self.wfile.write(f'data: {json.dumps(obj)}\n\n'.encode())
            self.wfile.flush()

        if 'tool_calls' in message:
            call = message['tool_calls'][0]
            chunk({'role': 'assistant', 'tool_calls': [{'index': 0, 'id': call['id'], 'type': 'function',
                                                        'function': {'name': call['function']['name'], 'arguments': ''}}]})
            chunk({'tool_calls': [{'index': 0, 'function': {'arguments': call['function']['arguments']}}]})
        else:
            chunk({'role': 'assistant', 'content': message['content']})
        chunk({}, finish)
        self.wfile.write(b'data: [DONE]\n\n')
        self.wfile.flush()

    def log_message(self, fmt, *args):
        sys.stderr.write('stub_model: ' + fmt % args + '\n')


ThreadingHTTPServer(('0.0.0.0', PORT), Handler).serve_forever()

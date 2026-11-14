"""Driver for the recall-identity capture. Runs inside a container on the scratch network.

    python3 drive.py wait
    python3 drive.py setup
    python3 drive.py measure <variant>

setup creates three accounts: A (first signup, so admin), B (admin, added by A) and C (user,
added by A). measure signs in as each, reads the account's id from GET /api/v1/auths/, and sends
one chat per request shape with a unique marker. The stub model copies the marker into the tool
call's q, so the stub tool server's log row for that call is attributable to one account and
one shape. Results append to OUT/results.jsonl.
"""

import json
import sys
import time
import urllib.error
import urllib.request
import uuid

BASE = 'http://proto-owui-webui:8080'
OUT = '/h/out'
MODEL = 'stub-model'
SERVER_TOOL_ID = 'server:villa-recall'
PASSWORD = 'proto-Passw0rd!'
ACCOUNTS = [
    ('A', 'Account A', 'a@proto.invalid', 'admin'),
    ('B', 'Account B', 'b@proto.invalid', 'admin'),
    ('C', 'Account C', 'c@proto.invalid', 'user'),
]


def call(method, path, token=None, body=None, timeout=120):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header('Content-Type', 'application/json')
    if token:
        req.add_header('Authorization', 'Bearer ' + token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, r.read().decode('utf-8', 'replace')
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode('utf-8', 'replace')


def signin(email):
    status, raw = call('POST', '/api/v1/auths/signin', body={'email': email, 'password': PASSWORD})
    assert status == 200, f'signin {email}: {status} {raw[:300]}'
    return json.loads(raw)['token']


def whoami(token):
    status, raw = call('GET', '/api/v1/auths/', token)
    assert status == 200, f'GET /api/v1/auths/: {status} {raw[:300]}'
    me = json.loads(raw)
    return me['id'], me['role']


def wait():
    for _ in range(240):
        try:
            status, _ = call('GET', '/health', timeout=5)
            if status == 200:
                print('open webui healthy')
                return
        except Exception:
            pass
        time.sleep(1)
    sys.exit('open webui never became healthy')


def setup():
    key, name, email, _ = ACCOUNTS[0]
    status, raw = call('POST', '/api/v1/auths/signup', body={'name': name, 'email': email, 'password': PASSWORD})
    assert status == 200, f'signup A: {status} {raw[:300]}'
    admin = json.loads(raw)['token']
    for key, name, email, role in ACCOUNTS[1:]:
        status, raw = call('POST', '/api/v1/auths/add', admin,
                           {'name': name, 'email': email, 'password': PASSWORD, 'role': role})
        assert status == 200, f'add {key}: {status} {raw[:300]}'
    rows = []
    for key, _, email, want_role in ACCOUNTS:
        uid, role = whoami(signin(email))
        assert role == want_role, f'{key} has role {role}, want {want_role}'
        rows.append({'account': key, 'email': email, 'id': uid, 'role': role})
    with open(f'{OUT}/accounts.json', 'w') as f:
        json.dump(rows, f, indent=1)
    print(json.dumps(rows, indent=1))


def user_message(content):
    return {'id': str(uuid.uuid4()), 'parentId': None, 'childrenIds': [], 'role': 'user',
            'content': content, 'timestamp': int(time.time()), 'models': [MODEL]}


def body_for(shape, content):
    base = {'model': MODEL, 'messages': [{'role': 'user', 'content': content}],
            'tool_ids': [SERVER_TOOL_ID], 'params': {'function_calling': 'native'}}
    if shape == 'villa-verify-current':
        return {**base, 'stream': False}
    if shape == 'stream-no-chat':
        return {**base, 'stream': True}
    um = user_message(content)
    chat = {**base, 'stream': True, 'parent_id': None, 'id': str(uuid.uuid4()), 'user_message': um}
    if shape == 'new-chat-no-session':
        return chat
    if shape == 'new-chat-session':
        return {**chat, 'session_id': 'proto-session-' + uuid.uuid4().hex[:8]}
    raise ValueError(shape)


SHAPES = ['villa-verify-current', 'stream-no-chat', 'new-chat-no-session', 'new-chat-session']


def tool_rows():
    try:
        with open(f'{OUT}/tool-requests.jsonl') as f:
            return [json.loads(line) for line in f if line.strip()]
    except FileNotFoundError:
        return []


def rows_for(marker):
    return [r for r in tool_rows() if r['path'] == '/recall/search' and marker in ' '.join(r['query'].get('q', []))]


def final_message(token, chat_id, message_id):
    status, raw = call('GET', f'/api/v1/chats/{chat_id}', token)
    if status != 200:
        return f'GET chat: {status}'
    msg = (json.loads(raw).get('chat', {}).get('history', {}).get('messages', {}) or {}).get(message_id) or {}
    out = msg.get('output') or []
    texts = [c.get('text', '') for item in out if item.get('type') == 'message' for c in item.get('content') or []]
    return {'content': (msg.get('content') or '')[:300], 'output_text': ' '.join(texts)[:300],
            'output_types': [item.get('type') for item in out], 'done': msg.get('done')}


def measure(variant):
    with open(f'{OUT}/accounts.json') as f:
        accounts = json.load(f)
    for acct in accounts:
        token = signin(acct['email'])
        uid, role = whoami(token)
        assert uid == acct['id'], 'id changed across restarts'
        for shape in SHAPES:
            marker = f'probe-{variant}-{acct["account"]}-{shape}-{uuid.uuid4().hex[:6]}'
            body = body_for(shape, f'recall something. marker {marker}')
            status, raw = call('POST', '/api/chat/completions', token, body)
            chat_id = None
            try:
                parsed = json.loads(raw)
                chat_id = parsed.get('chat_id') if isinstance(parsed, dict) else None
            except ValueError:
                parsed = None
            finish = None
            if isinstance(parsed, dict) and parsed.get('choices'):
                finish = parsed['choices'][0].get('finish_reason')
            if 'data:' in raw and 'tool_calls' in raw:
                finish = 'tool_calls (in SSE stream)'
            deadline = time.time() + 30
            calls = rows_for(marker)
            while not calls and time.time() < deadline and shape.startswith('new-chat'):
                time.sleep(1)
                calls = rows_for(marker)
            time.sleep(2)
            calls = rows_for(marker)
            final = final_message(token, chat_id, body['id']) if chat_id and 'id' in body else None
            row = {
                'variant': variant, 'account': acct['account'], 'role': role, 'owui_user_id': uid,
                'shape': shape, 'http_status': status, 'response_excerpt': raw[:400],
                'finish_reason_to_caller': finish, 'chat_id': chat_id,
                'tool_server_calls': [{'x_villa_user_id': c['x_villa_user_id'], 'identity_verdict': c['identity_verdict'],
                                       'bearer_ok': c['bearer_ok'], 'status': c['status'],
                                       'header_names': sorted(k for k, _ in c['headers'])} for c in calls],
                'header_equals_own_id': [c['x_villa_user_id'] == [uid] for c in calls],
                'final_assistant_message': final,
            }
            with open(f'{OUT}/results.jsonl', 'a') as f:
                f.write(json.dumps(row) + '\n')
            print(f'{variant:8} {acct["account"]} {role:5} {shape:22} http={status} calls={len(calls)} '
                  f'hdr={[c["x_villa_user_id"] for c in calls]} own={uid} match={row["header_equals_own_id"]}')


if __name__ == '__main__':
    {'wait': wait, 'setup': setup, 'measure': lambda: measure(sys.argv[2])}[sys.argv[1]]()

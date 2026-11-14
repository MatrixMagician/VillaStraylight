# Recall identity capture

The blocking measurement in ADR-0042 "Not established in the tree" (issue #363). The question
is whether the pinned Open WebUI, running a registered OpenAPI tool server itself, sends
`X-Villa-User-Id` set to each signed-in account's own `user.id`, rendered from the
connection's `{"X-Villa-User-Id": "{{USER_ID}}"}` template.

Measured on the dev host on 2026-11-14 at `ghcr.io/open-webui/open-webui:main@sha256:1a6399d2…8dc8b924`
(image revision `0a7c15832fb30b1903753e83f81dc7d27e5b0944`). The header was correct for every
account Open WebUI let through to the tool server.

## How it runs

`run.sh <scratch-dir>` starts three scratch containers on an internal network, `proto-owui`
(10.99.9.0/24). The first is the pinned Open WebUI. The second is `stub_model.py`, which returns
one `villa_recall_search` call and then text. The third is `stub_tool.py`, which answers on
villa's real address alias `villa-tools-recall:8093`. The Open WebUI container gets the
live unit's `Environment=` lines, less the ones naming live services. It adds
`BYPASS_MODEL_ACCESS_CONTROL=True`, which only lets the non-admin account reach the model.
`gen_villa_bytes.go` writes the OpenAPI document (`toolserver.OpenAPI`) and the
`TOOL_SERVER_CONNECTIONS` value (`orchestrate.RenderToolEnvFiles`) from villa's own code. So
the `villa` variant is byte-for-byte what villa renders. Every `proto-owui-*` object is removed
on exit, and the bearer is redacted from `captured/`.

There are three accounts. A signs up first and becomes admin, B is an admin added by A, and C is
a user added by A. There are three connection variants: `villa`, `noheaders` (the template
removed) and `granted` (villa's connection with a public read `access_grants`). Each account
sends four request shapes in each variant (`drive.py`).

## Result (`captured/results.jsonl`, `captured/tool-requests.jsonl`)

| variant | account | own `user.id` | tool-server calls | `X-Villa-User-Id` received |
|---|---|---|---|---|
| villa | A admin | 8110375a-52b1-4082-a29a-2a211073553b | 2 | 8110375a-52b1-4082-a29a-2a211073553b (both) |
| villa | B admin | 1c9c7c0e-8e93-4fcb-9d52-fe8ca8c1fd17 | 2 | 1c9c7c0e-8e93-4fcb-9d52-fe8ca8c1fd17 (both) |
| villa | C user | 475dbed4-3fc7-4258-ada0-8cbec383f33f | 0 | none. The tool is not offered: "Access denied to tool server villa-recall" |
| noheaders | A, B | as above | 2 each | absent. The stub answered 403, as the real unit does |
| noheaders | C | as above | 0 | none (access denied) |
| granted | A, B, C | as above | 2 each | each account's own id |

The two calls per account come from the two shapes that carry a chat id and a message id.
The shapes without one, `villa-verify-current` (villa's present `ToolCompletionBody`) and
`stream-no-chat`, made no tool-server call for any account. In both, Open WebUI handed
`finish_reason: "tool_calls"` back to the caller. That reproduces #363.

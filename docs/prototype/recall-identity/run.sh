#!/usr/bin/env bash
# Recall-identity capture (ADR-0042 "Not established in the tree", issue #363).
#
# Runs the pinned Open WebUI image against two stubs on a private internal network, in three
# variants of the one tool-server connection, and leaves the captures in $RUN/out. Touches no
# villa unit, volume or network: every object it creates is named proto-owui-* and is removed
# on exit.
#
#   docs/prototype/recall-identity/run.sh <scratch-run-dir>
set -euo pipefail

REPO=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
HERE="$REPO/docs/prototype/recall-identity"
RUN=${1:?usage: run.sh <scratch-run-dir>}
IMG=$(cd "$REPO" && go run ./docs/prototype/recall-identity/print_owui_image.go)
NET=proto-owui
VOL=proto-owui-data
BEARER=proto-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
SECRET=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')

cleanup() {
	if [ -d "${RUN:-}/out" ]; then
		for c in proto-owui-webui proto-owui-model proto-owui-tool; do
			podman logs "$c" >>"$RUN/out/$c.log" 2>&1 || true
			podman inspect "$c" --format '{{.Name}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}} status={{.State.Status}}' >>"$RUN/out/container-state.txt" 2>&1 || true
		done
		sed -i "s/$BEARER/<bearer>/g" "$RUN"/out/* "$RUN"/connections.*.json 2>/dev/null || true
	fi
	podman rm -f proto-owui-webui proto-owui-model proto-owui-tool >/dev/null 2>&1 || true
	podman volume rm -f "$VOL" >/dev/null 2>&1 || true
	podman network rm -f "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

rm -rf "$RUN" && mkdir -p "$RUN/out"
cp "$HERE"/stub_model.py "$HERE"/stub_tool.py "$HERE"/drive.py "$RUN"/
(cd "$REPO" && go run ./docs/prototype/recall-identity/gen_villa_bytes.go "$RUN" "$BEARER")

python3 - "$RUN" <<'PY'
import json, sys
run = sys.argv[1]
villa = json.load(open(f'{run}/connections.villa.json'))
noheaders = [{k: v for k, v in c.items() if k != 'headers'} for c in villa]
granted = [{**c, 'config': {**c['config'], 'access_grants': [{'principal_type': 'user', 'principal_id': '*', 'permission': 'read'}]}} for c in villa]
for name, conns in (('noheaders', noheaders), ('granted', granted)):
    json.dump(conns, open(f'{run}/connections.{name}.json', 'w'), separators=(',', ':'))
PY

echo "image: $IMG"
podman network create --internal --subnet 10.99.9.0/24 "$NET" >/dev/null
podman volume create "$VOL" >/dev/null

podman run -d --name proto-owui-model --network "$NET" -v "$RUN:/h:z" --entrypoint python3 "$IMG" \
	/h/stub_model.py /h/out 8080 >/dev/null
podman run -d --name proto-owui-tool --network "$NET" --network-alias villa-tools-recall -v "$RUN:/h:z" \
	--entrypoint python3 "$IMG" /h/stub_tool.py /h/recall-openapi.json "$BEARER" /h/out 8093 >/dev/null

drive() { podman run --rm --network "$NET" -v "$RUN:/h:z" --entrypoint python3 "$IMG" /h/drive.py "$@"; }

start_webui() {
	podman logs proto-owui-webui >>"$RUN/out/proto-owui-webui.log" 2>&1 || true
	podman rm -f proto-owui-webui >/dev/null 2>&1 || true
	echo "--- webui variant $1" >>"$RUN/out/proto-owui-webui.log"
	# The Environment= lines of villa-openwebui.container, less the ones naming live services
	# (qdrant, villa-embed, searxng, websafe). BYPASS_MODEL_ACCESS_CONTROL is the one addition:
	# it lets the non-admin account reach the stub model, and is not read by the tool-server
	# access check (has_connection_access), which is the gate under test.
	podman run -d --name proto-owui-webui --network "$NET" -v "$VOL:/app/backend/data:Z" \
		-e WEBUI_SECRET_KEY="$SECRET" \
		-e OPENAI_API_BASE_URL=http://proto-owui-model:8080/v1 -e OPENAI_API_KEY=stub \
		-e ENABLE_OPENAI_API=True -e ENABLE_OLLAMA_API=False \
		-e ANONYMIZED_TELEMETRY=False -e DO_NOT_TRACK=True -e SCARF_NO_ANALYTICS=True \
		-e OFFLINE_MODE=True -e ENABLE_VERSION_UPDATE_CHECK=False -e HF_HUB_OFFLINE=1 \
		-e WEBUI_AUTH=True -e ENABLE_MEMORIES=True -e ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True \
		-e RAG_EMBEDDING_ENGINE=openai -e RAG_OPENAI_API_BASE_URL=http://proto-owui-model:8080/v1 \
		-e RAG_OPENAI_API_KEY=stub -e RAG_EMBEDDING_MODEL_AUTO_UPDATE=False \
		-e ENABLE_PERSISTENT_CONFIG=False \
		-e BYPASS_MODEL_ACCESS_CONTROL=True \
		-e TOOL_SERVER_CONNECTIONS="$(cat "$RUN/connections.$1.json")" \
		"$IMG" >/dev/null
	drive wait
}

start_webui villa
drive setup
drive measure villa
start_webui noheaders
drive measure noheaders
start_webui granted
drive measure granted

echo "captures in $RUN/out"

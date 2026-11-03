#!/usr/bin/env bash
# PROTOTYPE (#375): real Qwen Code launched with the contract villa really emitted (CONTRACT_DIR/{env,settings}.out
# from `villa code --agent qwen` against a fake qwen). Model-request counts prove each session reached the model.
cd "$(dirname "$0")"
n() { podman logs proto-llama 2>&1 | grep -c "slot      release"; }
for m in default villa-contract villa-contract-override; do
  ./seed.sh >/dev/null; b=$(n)
  python3 -I interactive.py "$m" 2>&1 | sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g' | tail -1
  echo "   model requests: $(( $(n) - b ))"
done
podman logs proto-sink > results/interactive/sink-accept.log 2>&1
echo ACCEPT-DONE

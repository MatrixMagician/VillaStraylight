#!/usr/bin/env bash
# PROTOTYPE (#372) positive control for run.sh's capture: the same container IP makes one
# deliberate off-box request; the capture must see it, or a "0 packets" result means nothing.
cd "$(dirname "$0")"; IP=10.89.2.240
podman unshare --rootless-netns sh -c "exec tcpdump -i any -n -U -w $PWD/results/control.pcap \"src host $IP and (not dst net 10.89.2.0/24 or dst port 53)\"" 2>/dev/null & TD=$!
sleep 2
podman run --rm --network villa --ip $IP docker.io/library/node:22-slim node -e 'fetch("https://example.com").then(r=>console.log("status",r.status)).catch(e=>console.log("err",e.cause&&e.cause.code||e.message))'
sleep 2; pkill -x tcpdump; wait $TD 2>/dev/null
echo "control: $(tcpdump -nr results/control.pcap 2>/dev/null | wc -l) packets"; tcpdump -nr results/control.pcap 2>/dev/null | head -5

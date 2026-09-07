---
status: accepted
---

# The KV cache stays f16 on gfx1151

llama-server can hold the KV cache in `q8_0` instead of `f16` (`-ctk q8_0 -ctv
q8_0`). Villa renders `-fa 1` and no cache type, so every unit runs f16. The pitch
for `q8_0` is that token generation on gfx1151 is memory-bandwidth bound, so a
cache half the size should read faster at long context and halve the KV term of
the fit inequality. Measured on the dev host on 2026-09-07 with the pinned ROCm
7.2.4 image (llama-server build 9536), the Quadlet arguments minus speculation,
greedy, 256 generated tokens, `ignore_eos`, warm numbers, a 34-token prompt and a
15.5k-token prompt:

| model | ctx | KV | tg short | tg 16k prompt | pp 16k | GTT |
|-------|-----|----|----------|---------------|--------|-----|
| Qwen3.6-35B-A3B UD-Q4_K_M | 32k | f16 | 48.8 | 44.9 | 827 | 21.7 GB |
| Qwen3.6-35B-A3B UD-Q4_K_M | 32k | q8_0 | 47.7 | 40.7 | 827 | 21.5 GB |
| Qwen3.6-35B-A3B UD-Q4_K_M | 128k | f16 | 47.4 | 44.5 | 833 | 23.7 GB |
| Qwen3.6-35B-A3B UD-Q4_K_M | 128k | q8_0 | 47.5 | 39.8 | 827 | 22.8 GB |
| Qwen3-Coder-30B-A3B UD-Q4_K_XL | 32k | f16 | 68.0 | 45.8 | 841 | 20.1 GB |
| Qwen3-Coder-30B-A3B UD-Q4_K_XL | 32k | q8_0 | 64.6 | 35.5 | 831 | 18.6 GB |
| Qwen3-Coder-30B-A3B UD-Q4_K_XL | 128k | f16 | 68.4 | 46.1 | 830 | 29.4 GB |
| Qwen3-Coder-30B-A3B UD-Q4_K_XL | 128k | q8_0 | 65.3 | 36.1 | 828 | 23.8 GB |

`q8_0` loses on every row. On a short prompt it costs 2 to 5 percent of tg. On a
16k prompt, the case the bandwidth argument was for, it costs 9 to 10 percent on
the hybrid model and 22 percent on the full-attention coder. Prompt processing
is unchanged. The memory it returns is real but small on the hybrid entry, 0.2
to 0.9 GB, because only 10 of that model's 40 blocks hold a KV cache at all (ADR
0007). The 5.6 GB it returns on the coder at 128k buys nothing, since the coder
runs at its 64k agent context and already fits.

The flash-attention kernel on this backend dequantizes the cache on every step,
and that work costs more than the halved read saves. The bandwidth model was
right about what is read and wrong about what it costs to read it. This is a
property of the pinned kernel, not of the models, so it can be re-measured when
the image pin moves.

So villa offers no KV cache type. The config vocabulary does not gain a
`kv_cache` field, the render stays byte-identical, and the catalog carries no
`kv_q8_safe` flag. The probe that produced the table is the same one ADR 0006
used: the pinned image on port 8081 with the unit's arguments plus the candidate
flag, reading `timings` from `/completion`. `q4_0` was never a candidate, since
its quality cost is known to be large.

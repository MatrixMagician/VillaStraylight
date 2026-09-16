---
description: Keep changed code within the CRAP ceiling
globs: ["**/*.go"]
priority: 85
---
- Before calling a Go change done, run `node "/home/oliverh/.claude/plugins/cache/eigenwise-toolshed/quartermaster/0.11.1/bin/quartermaster.js" crap --project "/home/oliverh/repos/github/MatrixMagician/VillaStraylight"`.
- Keep every changed or new function under the ceiling (6, ratcheted against `main`). Cover it or split it.
- Exit 2 means a prerequisite is missing (`lizard`, `gcov2lcov`, or the LCOV file). Follow the printed install hint, then rerun the gate. Do not skip it.

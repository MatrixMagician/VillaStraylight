---
description: Dev-host rebuild and restart after a Go change
priority: 70
---
Two traps follow a Go change on this host, and both fail silently rather than loudly.

**Build static.** `make build` links `./villa` dynamically. `villa-websafe` and every task VM
bind-mount that exact file into a distroless image, so the next websafe restart crash-loops with
"No such file or directory". Build with `make build-static` — the same CGO-free gate CI enforces.

**Restart the dashboard.** `villa status` and `villa recommend` run fresh from `./villa`, but
`villa-dashboard.service` is long-lived and its `ExecStart` points at this working tree's binary. A
dashboard change is not live until `systemctl --user restart villa-dashboard.service`. No config
change and no `villa up` are needed for a dashboard-only change, and websafe does not need restarting
(the running container keeps the old inode).

Because the unit runs the working-tree binary, a `git checkout` plus a rebuild silently changes what
the live stack serves. Know which branch is checked out before you rebuild.

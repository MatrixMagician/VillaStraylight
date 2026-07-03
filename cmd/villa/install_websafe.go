package main

// install_websafe.go holds the v1.5 GROUNDED-FETCH (villa-websafe) install wiring constant
// the `villa install` verb uses when the PERSISTED web_search_enabled is true (Phase-31
// GUARD-01 / GROUND-01). It mirrors install_searxng.go's searxngServiceName: villa-websafe is
// started additively when web search is opted in, gated on its rendered unit being present in
// the written plan, AFTER the 0600 websafe.env bearer file is written (so the unit's
// EnvironmentFile= target exists on first boot — the SAME secret file the OWUI unit references,
// WebsafeSecretEnvFilePath).

// websafeServiceName is the systemd service the villa-websafe .container generates (Quadlet
// maps villa-websafe.container → villa-websafe.service). Its start is gated on the rendered
// unit being present in the written plan AND on the PERSISTED web_search_enabled, and it is
// started only AFTER the 0600 websafe.env secret file is written (its EnvironmentFile= target).
const websafeServiceName = "villa-websafe.service"

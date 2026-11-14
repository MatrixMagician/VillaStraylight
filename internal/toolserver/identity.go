package toolserver

// identity.go names a set to the code that talks to its unit from outside: the route of its
// one tool, the operation id its request log line carries, and the id its Open WebUI
// connection is registered under. The first two come from the tool table; the third is the
// one place the registration id is spelled.

// SearchPath is the route of the set's tool, or "" for an unknown set.
func SearchPath(set Set) string {
	if ts := toolsFor(set); len(ts) == 1 {
		return ts[0].Path
	}
	return ""
}

// OperationID is the set's tool name, as the OpenAPI document and the request log line
// ("tool=<id> status=...") spell it, or "" for an unknown set.
func OperationID(set Set) string {
	if ts := toolsFor(set); len(ts) == 1 {
		return ts[0].OperationID
	}
	return ""
}

// ConnectionID is the id the set's connection is registered under in Open WebUI's
// TOOL_SERVER_CONNECTIONS (info.id), or "" for an unknown set. A chat selects the connection
// as "server:<id>".
func ConnectionID(set Set) string {
	switch set {
	case SetWeb:
		return "villa-web"
	case SetRecall:
		return "villa-recall"
	}
	return ""
}

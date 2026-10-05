package backup

// registry.go is the ONE ordered list of archive entries (ADR-0020). Backup,
// readAndVerify and Restore's capture / forward-write / rollback steps iterate it
// instead of spelling each entry out, so a new file entry is a row here, a path in
// the cmd tier's two maps, and nothing else.
//
// The order is the order members are written to the tar (manifest.json first, then
// these rows), so an archive's layout is a function of this list.
//
// The two volume rows and config.toml are listed so the tar order lives in one
// place, but they are NOT written by the file loops: quiesce, the tri-state Unknown
// refusal and clean-recreate-before-import are different in kind and stay explicit
// code in Backup and Restore.

// Kind says how an entry is read at backup and applied at restore.
type Kind int

const (
	// KindVolume is a podman volume exported to a tar (explicit code: quiesce,
	// streamed checksum, clean-recreate on restore).
	KindVolume Kind = iota
	// KindConfig is config.toml (explicit code: parsed and saved through SaveConfig).
	KindConfig
	// KindFile is a plain file copied in and written back (the table-driven loops).
	KindFile
)

// Row is one archive entry.
type Row struct {
	// Name is the tar member name (the Entry* constant).
	Name string
	// Label is what an error or rollback message calls the entry ("restore
	// settings.yml"). It is not always the Name.
	Label string
	Kind  Kind
	// Required entries abort a backup when absent; optional ones are skipped.
	Required bool
}

// registry lists every entry in tar order.
var registry = []Row{
	{Name: EntryOpenWebUIVolume, Label: "owui volume", Kind: KindVolume, Required: true},
	{Name: EntryConfig, Label: "config.toml", Kind: KindConfig, Required: true},
	{Name: EntryUsage, Label: "usage.json", Kind: KindFile},
	{Name: EntryBenchReports, Label: "bench-reports.jsonl", Kind: KindFile},
	{Name: EntryQdrantVolume, Label: "qdrant volume", Kind: KindVolume},
	{Name: EntryRecallState, Label: "recall-state.json", Kind: KindFile},
	{Name: EntryCrushConfig, Label: "crush.json", Kind: KindFile},
	{Name: EntrySearxngSettings, Label: "settings.yml", Kind: KindFile},
	{Name: EntryEvalBaselines, Label: "eval-baselines.json", Kind: KindFile},
}

// EntryNamesForTest lists the tar member name of every registry row in tar order, so
// a test in another package can assert each entry is documented without the registry
// itself being exported (ADR-0020).
func EntryNamesForTest() []string {
	names := make([]string, 0, len(registry))
	for _, r := range registry {
		names = append(names, r.Name)
	}
	return names
}

// fileRows are the registry's KindFile rows, in order.
var fileRows = rowsOfKind(KindFile)

func rowsOfKind(k Kind) []Row {
	var rows []Row
	for _, r := range registry {
		if r.Kind == k {
			rows = append(rows, r)
		}
	}
	return rows
}

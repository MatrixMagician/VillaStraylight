package backup

// restore_read.go is the pure read+verify pass (step 1 of Restore): it parses
// manifest.json (the FIRST entry), verifies every other entry's SHA-256 against it,
// and maps the verified bytes onto the registry. Zero side effects — it only reads
// the injected archive stream.

import (
	"bytes"
	"fmt"
)

// extracted holds the verified, tar-slip-guarded archive payload after the read
// pass: the parsed manifest, the raw config.toml bytes, the two volume tars, and
// the file entries. An optional entry's presence is its key in files (or
// qdrantPresent), which distinguishes an absent entry from an empty one; every
// downstream mutation of an optional entry is gated on it.
type extracted struct {
	manifest   Manifest
	config     []byte
	owuiVolume []byte
	// qdrantVolume is the OPTIONAL Phase-23 memory volume tar; qdrantPresent gates
	// EVERY qdrant mutation downstream.
	qdrantVolume  []byte
	qdrantPresent bool
	// files holds the registry's KindFile entries the archive carries, by entry
	// name. They are SHA-256-verified through the SAME readAndVerify pass as every
	// other entry (no parallel reader).
	files map[string][]byte
}

// readAndVerify performs the pure read+verify pass: it parses manifest.json (FIRST
// entry) and verifies every subsequent entry's SHA-256 against the manifest,
// returning the extracted, tar-slip-guarded payload. A manifest whose schema_version
// is unreadable (<=0) or NEWER than this villa supports is a fail-closed BLOCK; a
// per-entry SHA-256 mismatch wraps ErrChecksumMismatch.
func readAndVerify(in RestoreInput) (extracted, error) {
	c, err := collectArchive(in)
	if err != nil {
		return extracted{}, err
	}
	if err := c.verify(); err != nil {
		return extracted{}, err
	}
	return c.extract()
}

// archiveCollector gathers the archive's members as readArchive hands them over.
type archiveCollector struct {
	manifest Manifest
	seen     bool
	idx      int
	files    map[string][]byte
}

// collectArchive reads every member. readArchive applies the tar-slip guard to every
// entry name before handing it to the collector, so a malicious "../escape" /
// absolute entry is refused here, before any side effect.
func collectArchive(in RestoreInput) (*archiveCollector, error) {
	if in.OpenArchive == nil {
		return nil, fmt.Errorf("nil archive opener")
	}
	rc, err := in.OpenArchive()
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = rc.Close() }()

	c := &archiveCollector{files: map[string][]byte{}}
	if err := readArchive(rc, c.add); err != nil {
		return nil, err
	}
	if !c.seen {
		return nil, fmt.Errorf("archive has no %s entry", EntryManifest)
	}
	return c, nil
}

// add is the readArchive callback: the manifest is parsed and schema-gated, every
// other member is collected.
func (c *archiveCollector) add(name string, data []byte) error {
	idx := c.idx
	c.idx++
	if name == EntryManifest {
		return c.addManifest(idx, data)
	}
	return c.addEntry(name, data)
}

// addManifest parses the manifest. Manifest-first on READ: the manifest MUST be the
// FIRST tar member so it is parsed + schema-gated before any subsequent body is
// trusted. An out-of-position manifest is refused (and a second manifest is a
// duplicate).
func (c *archiveCollector) addManifest(idx int, data []byte) error {
	if idx != 0 {
		return fmt.Errorf("archive %s must be the FIRST entry (found at position %d)", EntryManifest, idx)
	}
	m, err := parseManifest(data)
	if err != nil {
		return err
	}
	c.manifest, c.seen = m, true
	return checkManifestSchema(m)
}

// checkManifestSchema schema-gates the manifest BEFORE any further entry body is
// read: fail-closed BLOCK on an unreadable/incompatible schema, mirroring
// usage.Load's fail-closed-on-future discipline.
func checkManifestSchema(m Manifest) error {
	if m.SchemaVersion <= 0 || m.SchemaVersion > backupSchemaVersion {
		return fmt.Errorf("manifest schema_version %d is unreadable or newer than this villa supports (%d)",
			m.SchemaVersion, backupSchemaVersion)
	}
	return nil
}

// addEntry collects a non-manifest member. Every one arrives AFTER the manifest: if
// the manifest was not the first member, addManifest already refused it; a data
// entry before it means there was no leading manifest. A duplicate name is refused
// explicitly (a silent last-write-wins would make verify order-dependent).
func (c *archiveCollector) addEntry(name string, data []byte) error {
	if !c.seen {
		return fmt.Errorf("archive %s must be the FIRST entry — entry %q precedes it", EntryManifest, name)
	}
	if _, dup := c.files[name]; dup {
		return fmt.Errorf("archive contains duplicate entry %q", name)
	}
	c.files[name] = data
	return nil
}

// verify checks the collected members against the manifest's checksum list.
func (c *archiveCollector) verify() error {
	want := map[string]string{}
	for _, e := range c.manifest.Entries {
		want[e.Name] = e.SHA256
	}
	if err := c.rejectUnlisted(want); err != nil {
		return err
	}
	return c.verifyListed(want)
}

// rejectUnlisted refuses any collected entry NOT listed in the manifest: the archive
// must contain EXACTLY the manifest-described members — an extra/unexpected entry
// would otherwise be accepted-and-ignored, which is not what the manifest claims.
func (c *archiveCollector) rejectUnlisted(want map[string]string) error {
	for name := range c.files {
		if _, listed := want[name]; !listed {
			return fmt.Errorf("archive contains entry %q not listed in the manifest", name)
		}
	}
	return nil
}

// verifyListed verifies every manifest-listed entry's SHA-256 against the collected
// bytes. A missing entry or a mismatch is archive corruption.
func (c *archiveCollector) verifyListed(want map[string]string) error {
	for name, csum := range want {
		data, ok := c.files[name]
		if !ok {
			return fmt.Errorf("manifest lists entry %q but the archive does not contain it", name)
		}
		if err := verify(bytes.NewReader(data), csum); err != nil {
			return fmt.Errorf("entry %q: %w", name, err)
		}
	}
	return nil
}

// extract maps the verified members onto the typed payload. config.toml and the owui
// volume tar are REQUIRED; every other registry entry is optional.
func (c *archiveCollector) extract() (extracted, error) {
	ex := extracted{manifest: c.manifest, files: map[string][]byte{}}
	var ok bool
	if ex.config, ok = c.files[EntryConfig]; !ok {
		return ex, fmt.Errorf("archive is missing the required %s entry", EntryConfig)
	}
	if ex.owuiVolume, ok = c.files[EntryOpenWebUIVolume]; !ok {
		return ex, fmt.Errorf("archive is missing the required %s entry", EntryOpenWebUIVolume)
	}
	ex.qdrantVolume, ex.qdrantPresent = c.files[EntryQdrantVolume]
	for _, row := range fileRows {
		if b, present := c.files[row.Name]; present {
			ex.files[row.Name] = b
		}
	}
	return ex, nil
}

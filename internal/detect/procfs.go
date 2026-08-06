package detect

// procfs.go holds the one line-scanning primitive the /proc readers share. Both
// /proc/cpuinfo and /proc/meminfo are line-oriented "key: value" files, and both
// must degrade the same way: an unreadable file, an absent key and an interrupted
// read are three distinct answers, and none of them may become a bare zero or a
// panic.
//
// The scan lives here so the distinction that is easiest to get wrong — a read
// that fails partway is not the same as a key that is not there, and neither is
// the same as a file that would not open — is made once rather than
// remembered at each reader.

import (
	"bufio"
	"os"
	"strings"
)

// lineResult reports which of the four outcomes a procfs scan reached.
type lineResult int

const (
	// lineFound: a line with the requested prefix was read.
	lineFound lineResult = iota
	// lineAbsent: the file was read cleanly to the end without the prefix.
	lineAbsent
	// lineUnopenable: the file could not be opened at all.
	lineUnopenable
	// lineReadFailed: the read failed partway. Distinct from lineAbsent, which it
	// would otherwise be silently mistaken for, and from lineUnopenable, since the
	// file did open.
	lineReadFailed
)

// findLine returns the first line of path that starts with prefix, along with
// which outcome was reached and the underlying error when there was one.
func findLine(path, prefix string) (line string, res lineResult, err error) {
	f, err := os.Open(path) //nolint:gosec // fixed procfs path, or a test fixture
	if err != nil {
		return "", lineUnopenable, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), prefix) {
			return sc.Text(), lineFound, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", lineReadFailed, err
	}
	return "", lineAbsent, nil
}

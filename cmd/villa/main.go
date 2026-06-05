// Command villa is the VillaStraylight control plane: a single static Go binary
// that detects an AMD Strix Halo (gfx1151) host, recommends a memory-fitting
// model/quant/context, and gates installs behind a preflight check. Phase 1
// delivers the read-only `detect` slice.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "villa:", err)
		os.Exit(1)
	}
}

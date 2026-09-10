// Package sandbox carries the workspace agent's image build context as embedded
// files, so `villa sandbox build` can build the image from the shipped binary on
// a host that has no checkout of this repository.
//
// The embed set is the build context and nothing else: the Containerfile, the
// pip hash-lock, and the three office scripts it copies to /usr/local/bin.
// testdata/ is the on-hardware suite's fixtures and is deliberately excluded —
// it would enter the image's layer cache and change nothing about the image.
package sandbox

import "embed"

// Context is the build context `villa sandbox build` writes to a temporary
// directory before invoking podman. Its layout is the layout of this directory,
// which is what lets the Containerfile's COPY lines stay relative.
//
//go:embed Containerfile requirements.txt scripts
var Context embed.FS

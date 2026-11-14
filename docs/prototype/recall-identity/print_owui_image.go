//go:build ignore

// print_owui_image.go prints the compiled-in Open WebUI pin, so run.sh runs the image villa
// vets and never a tag it typed.
package main

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

func main() { fmt.Println(orchestrate.OpenWebUIImage()) }

// Command genicon renders the application icon, winres/icon.svg, with
// Inkscape into the PNG files that go-winres puts in the executable and
// release.sh packs into the installer icon.
//
// Run from the repository root (needs inkscape):
//
//	go run ./tools/genicon
package main

import (
	"fmt"
	"log"
	"os/exec"
)

const svg = "winres/icon.svg"

func main() {
	for size, file := range map[int]string{
		256: "winres/icon.png",
		48:  "winres/icon48.png",
		32:  "winres/icon32.png",
		16:  "winres/icon16.png",
	} {
		px := fmt.Sprint(size)
		cmd := exec.Command("inkscape", svg, "--export-type=png", "--export-filename="+file, "-w", px, "-h", px)
		if b, err := cmd.CombinedOutput(); err != nil {
			log.Fatalf("inkscape: %v\n%s", err, b)
		}
	}
}

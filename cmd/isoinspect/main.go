// isoinspect lists or prints files inside an ISO image (debugging aid).
package main

import (
	"fmt"
	"io"
	"os"

	"ipxemultiboot/internal/iso9660"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: isoinspect <iso> [ls <dir> | cat <file>]")
		os.Exit(2)
	}
	img, err := iso9660.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer img.Close()
	cmd, arg := "ls", "/"
	if len(os.Args) >= 4 {
		cmd, arg = os.Args[2], os.Args[3]
	}
	switch cmd {
	case "ls":
		fmt.Printf("volume: %q\n", img.VolumeID())
		entries, err := img.ReadDir(arg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, e := range entries {
			kind := "f"
			if e.IsDir {
				kind = "d"
			}
			fmt.Printf("%s %12d %s\n", kind, e.Size, e.Name)
		}
	case "cat":
		e, err := img.Stat(arg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		io.Copy(os.Stdout, img.Reader(e))
	}
}

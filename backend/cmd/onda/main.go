package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/starmito/onda/internal/api"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Onda " + api.Version + " — Audio separation tool")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  onda serve [--addr :PORT]   Start API server")
		fmt.Println("  onda pipeline [flags]       Run separation pipeline")
		fmt.Println("  onda version                Show version")
		os.Exit(0)
	}

	switch os.Args[1] {
	case "serve":
		serveFlags := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := serveFlags.String("addr", ":3000", "Listen address")
		serveFlags.Parse(os.Args[2:])

		srv := api.NewServer(*addr)
		fmt.Printf("Onda API server listening on %s\n", *addr)
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	case "pipeline":
		cmd := exec.Command("bash", append([]string{"/pipeline.sh"}, os.Args[2:]...)...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
	case "version":
		fmt.Println(api.Version)
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

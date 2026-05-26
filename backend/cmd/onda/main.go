package main

import (
	"fmt"
	"os"

	"github.com/starmito/onda/internal/api"
	"github.com/starmito/onda/internal/cli"
	"github.com/starmito/onda/internal/pipeline"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Onda v2.0.0-alpha — Audio separation tool")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  onda pipeline [flags]   Run separation pipeline")
		fmt.Println("  onda version            Show version")
		fmt.Println()
		fmt.Println("Próximamente (Fase 2):")
		fmt.Println("  onda serve              Start API server")
		fmt.Println("  onda models             List available models and presets")
		os.Exit(0)
	}

	switch os.Args[1] {
	case "serve":
		srv := api.NewServer(":3000")
		fmt.Println("Onda API server listening on :3000")
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	case "pipeline":
		flags, err := cli.ParsePipelineFlags(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := pipeline.Run(flags); err != nil {
			fmt.Fprintf(os.Stderr, "Pipeline failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Pipeline completed: preset=%s vocal=%s input=%s output=%s\n",
			flags.Preset, flags.VocalModel, flags.Input, flags.Output)
	case "models":
		fmt.Println("onda models — not implemented yet")
	case "version":
		fmt.Println("v2.0.0-alpha")
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

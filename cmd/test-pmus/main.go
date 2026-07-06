package main

import (
	"context"
	"fmt"
	"os"

	"pdc/store"
)

func main() {
	s, err := store.NewStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "store init: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	pmus, err := s.GetAllPMUs(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetAllPMUs: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ok: %d pmus\n", len(pmus))
	for _, p := range pmus {
		fmt.Printf("  %s %s:%d\n", p.Name, p.IP, p.Port)
	}
}

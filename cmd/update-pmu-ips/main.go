// One-off utility: update IP for all registered PMUs in Postgres.
// Usage: go run ./cmd/update-pmu-ips [new-ip]
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"pdc/store"
)

func main() {
	newIP := "172.24.108.1"
	if len(os.Args) > 1 && os.Args[1] != "" {
		newIP = os.Args[1]
	}

	ctx := context.Background()
	db, err := store.NewStore()
	if err != nil {
		log.Fatalf("store init: %v", err)
	}
	defer db.Close()

	pmus, err := db.GetAllPMUs(ctx)
	if err != nil {
		log.Fatalf("get pmus: %v", err)
	}
	if len(pmus) == 0 {
		log.Println("no registered PMUs found")
		return
	}

	for _, pmu := range pmus {
		oldIP := pmu.IP
		pmu.IP = newIP
		if err := db.SavePMU(ctx, pmu); err != nil {
			log.Fatalf("save %s: %v", pmu.Name, err)
		}
		fmt.Printf("updated %s: %s:%d -> %s:%d\n", pmu.Name, oldIP, pmu.Port, pmu.IP, pmu.Port)
	}

	fmt.Printf("\nDone. Updated %d device(s) to IP %s.\n", len(pmus), newIP)
	fmt.Println("Restart PDC (or re-register via POST /api/pmus) for receivers to use the new IP.")
}

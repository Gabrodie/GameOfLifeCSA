package main

import (
	"fmt"
	"net/rpc"
	"os"
	"testing"

	"uk.ac.bris.cs/gameoflife/gol"
)

const distBenchTurns = 1000

func BenchmarkDistributedGOL(b *testing.B) {
	// Disable program output for cleaner benchmark results
	os.Stdout = nil

	// Connect to the server once
	client, err := rpc.Dial("tcp", "34.237.56.190:7000")
	if err != nil {
		b.Fatalf("Failed to connect to server: %v", err)
	}
	defer client.Close()

	// Prepare a world matching your benchmark size
	w, h := 512, 512
	world := make([][]uint8, h)
	for i := range world {
		world[i] = make([]uint8, w)
	}

	// Test scalability from 1 → 6 workers
	for workers := 1; workers <= 6; workers++ {

		// Configure worker count on server
		var cfgResp gol.WorkerConfigResponse
		err := client.Call(
			"GameOfLifeServer.ConfigureWorkers",
			gol.WorkerConfigRequest{NumWorkers: workers},
			&cfgResp,
		)
		if err != nil {
			b.Fatalf("ConfigureWorkers failed: %v", err)
		}

		benchName := fmt.Sprintf("512x512x%dturns-%dworkers", distBenchTurns, workers)

		b.Run(benchName, func(b *testing.B) {

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				req := gol.GolRequest{
					World:       world,
					ImageWidth:  w,
					ImageHeight: h,
					Turns:       distBenchTurns,
				}
				var res gol.GolResponse

				if err := client.Call("GameOfLifeServer.AdvanceWorld", req, &res); err != nil {
					b.Fatalf("AdvanceWorld RPC failed: %v", err)
				}
			}

		})
	}
}

//steps for benchmarking

//go test -run ^$ -bench . -benchtime 1x -count 5 | tee results.out
//go run golang.org/x/perf/cmd/benchstat -format csv results.out | tee results.csv

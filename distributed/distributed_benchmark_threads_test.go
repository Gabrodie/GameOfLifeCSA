package main

import (
    "fmt"
    "net/rpc"
    "os"
    "testing"
    "uk.ac.bris.cs/gameoflife/gol"
)

const distBenchTurns = 1000

func BenchmarkDistributedThreads(b *testing.B) {
    // Disable all program output during benchmarking
    os.Stdout = nil

    // Connect to distributed server
    client, err := rpc.Dial("tcp", "34.237.56.190:7000")
    if err != nil {
        b.Fatalf("Failed to connect to server: %v", err)
    }
    defer client.Close()

    // Fixed number of distributed workers
    const fixedWorkers = 3

    // Configure the worker count on the server
    var workerResp gol.WorkerConfigResponse
    if err := client.Call(
        "GameOfLifeServer.ConfigureWorkers",
        gol.WorkerConfigRequest{NumWorkers: fixedWorkers},
        &workerResp,
    ); err != nil {
        b.Fatalf("ConfigureWorkers failed: %v", err)
    }

    // Benchmark world
    w, h := 512, 512
    world := make([][]uint8, h)
    for i := range world {
        world[i] = make([]uint8, w)
    }

    // Test scalability from 1 → 6 threads per worker node
    for threads := 1; threads <= 6; threads++ {

        // Configure thread count on server
        var threadResp gol.ThreadConfigResponse
        if err := client.Call(
            "GameOfLifeServer.ConfigureWorkerThreads",
            gol.ThreadConfigRequest{NumThreads: threads},
            &threadResp,
        ); err != nil {
            b.Fatalf("ConfigureWorkerThreads failed: %v", err)
        }

        benchName := fmt.Sprintf("512x512x%dturns-%dthreads", distBenchTurns, threads)

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

package main

import (
	"net"
	"net/rpc"
	"sync"
	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/util"
	"runtime"
)

type Worker struct{}

func (w *Worker) Step(req gol.WorkerStepRequest, res *gol.WorkerStepResponse) error {
    chunk := req.Chunk
    h := len(chunk)
    if h < 3 {
        res.NewInner = [][]uint8{}
        res.Flipped = nil
        return nil
    }

    width := req.Width
	
    innerHeight := h - 2 // h-1 as worker processes rows [startY:endY)
	// there is a halo row on the bottom so take off another, hence h-2

    // create output
    newInner := make([][]uint8, innerHeight)
    for i := range newInner {
        newInner[i] = make([]uint8, width)
    }

    // parallelism inside worker node, number of threads passed in from broker
    numThreads := req.NumThreads
	if numThreads <= 0 {
    	numThreads = runtime.NumCPU() // if number of threads isn't specified, use all available
	}

    rowsPerThread := innerHeight / numThreads
    extra := innerHeight % numThreads

    var wg sync.WaitGroup
    start := 0

    for t := 0; t < numThreads; t++ {
        rows := rowsPerThread
		// distribute extra rows across threads
        if t < extra {
            rows++
        }
        end := start + rows

        wg.Add(1)
        go func(startY, endY int) {
            defer wg.Done()
            for y := startY; y < endY; y++ {
                chunkY := y + 1
                for x := 0; x < width; x++ {
                    alive := gol.CountAliveNeighbors(chunk, x, chunkY, width, h)
                    current := chunk[chunkY][x]
                    next := current

                    if current != 0 {
                        if alive < 2 || alive > 3 {
                            next = 0
                        } else {
                            next = 255
                        }
                    } else {
                        if alive == 3 {
                            next = 255
                        } else {
                            next = 0
                        }
                    }

                    newInner[y][x] = next
                }
            }
        }(start, end)

        start = end
    }

	// wait group waits until all goroutines finish
    wg.Wait()

    var flipped []util.Cell
    for y := 0; y < innerHeight; y++ {
        for x := 0; x < width; x++ {
			// y+1 as chunk offset by halo row
            if newInner[y][x] != chunk[y+1][x] {
                flipped = append(flipped, util.Cell{
                    X: x,
                    Y: req.StartY + y,
                })
            }
        }
    }

    res.NewInner = newInner
    res.Flipped = flipped
    return nil
}

func main() {
    port := ":7000"

    rpc.Register(new(Worker))

    l, err := net.Listen("tcp", port)
    if err != nil {
        panic(err)
    }
    defer l.Close()

    rpc.Accept(l)
}

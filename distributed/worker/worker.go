package main

import (
	"net"
	"net/rpc"
	"os"

	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/util"
)

type Worker struct{}

func (w *Worker) Step(req gol.WorkerStepRequest, res *gol.WorkerStepResponse) error {
	chunk := req.Chunk
	h := len(chunk)
	if h < 3 {
		// must have at least 1 inner row + 2 halo rows
		res.NewInner = [][]uint8{}
		res.Flipped = nil
		return nil
	}

	width := req.Width

	newChunk := make([][]uint8, h)
	for i := 0; i < h; i++ {
		newChunk[i] = make([]uint8, width)
	}

	var flipped []util.Cell

	// y=1..h-2 are the "real" rows, 0 and h-1 are halos
	for y := 1; y < h-1; y++ {
		for x := 0; x < width; x++ {
			alive := gol.CountAliveNeighbors(chunk, x, y, width, h)
			current := chunk[y][x]
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

			if next != current {
				flipped = append(flipped, util.Cell{
					X: x,
					// map the y position here to the y position in the full world
					// worker chunk indexes are offset by +1 because of the top halo so need to -1
					Y: req.StartY + (y - 1),
				})
			}

			newChunk[y][x] = next
		}
	}

	// remove halos before returning
	innerHeight := h - 2
	res.NewInner = make([][]uint8, innerHeight)
	for y := 0; y < innerHeight; y++ {
		// again offset because of halo rows
		res.NewInner[y] = newChunk[y+1]
	}
	res.Flipped = flipped
	return nil
}

func main() {
	port := ":7000"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}

	rpc.Register(new(Worker))
	l, err := net.Listen("tcp", port)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	rpc.Accept(l)
}

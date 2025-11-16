package main

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/util"
)

type GameOfLifeServer struct {
    currentAlive   []util.Cell
    flipped        []util.Cell
    completedTurns int
    lock           sync.Mutex
    paused         bool
    pauseReq       chan bool
    quitReq        chan struct{}
    killReq        chan struct{}
    World          [][]uint8

    workerAddrs []string
    workers     []*rpc.Client
}

// AdvanceWorld is RPC method that clients call to advance the world
func (s *GameOfLifeServer) AdvanceWorld(request gol.GolRequest, response *gol.GolResponse) error {
    world := request.World

    s.lock.Lock()
    s.completedTurns = 0
    s.currentAlive = nil
    s.flipped = nil
    s.World = world
    s.paused = false
    s.lock.Unlock()

    if s.pauseReq == nil {
        s.pauseReq = make(chan bool, 1)
    }
    if s.quitReq == nil {
        s.quitReq = make(chan struct{})
    }
    if s.killReq == nil {
        s.killReq = make(chan struct{})
    }

    for i := 0; i < request.Turns; i++ {
        var quit bool

        // pause / quit handling unchanged
        select {
        case pause := <-s.pauseReq:
            for pause {
                select {
                case pause = <-s.pauseReq:
                case <-s.quitReq:
                    quit = true
                    pause = false
                }
            }
        default:
        }
        if quit {
            break
        }

        s.lock.Lock()
        newWorld, flipped := s.distributedStep(world)
        world = newWorld
        s.World = newWorld
        s.currentAlive = gol.CalculateAliveCells(newWorld)
        s.completedTurns = i + 1
        s.flipped = flipped
        s.lock.Unlock()
    }

    s.lock.Lock()
    response.World = world
    response.Alive = gol.CalculateAliveCells(world)
    response.CompletedTurns = s.completedTurns
    s.lock.Unlock()

    return nil
}


// GetStatus returns alive cells and completed turns
func (s *GameOfLifeServer) GetStatus(request gol.StatusRequest, response *gol.StatusResponse) error {
	s.lock.Lock()
	response.AliveCount = len(s.currentAlive)
	response.CompletedTurns = s.completedTurns
	response.FlippedCells = s.flipped
	s.lock.Unlock()
	return nil
}

// Pause toggles paused/unpaused state
func (s *GameOfLifeServer) Pause(request gol.PauseRequest, response *gol.PauseResponse) error {
	s.lock.Lock()
	s.paused = !s.paused
	newState := s.paused
	s.lock.Unlock()

	if s.pauseReq != nil {
		s.pauseReq <- newState
	}

	response.Paused = newState
	s.lock.Lock()
	response.CompletedTurns = s.completedTurns
	s.lock.Unlock()

	return nil
}

func (s *GameOfLifeServer) Save(request gol.SaveRequest, response *gol.SaveResponse) error {
	wasPaused := s.paused

	//If not paused, request a pause first

	s.pauseReq <- true // safely pause AdvanceWorld loop

	//Perform the save (guaranteed safe snapshot)
	s.lock.Lock()
	response.CompletedTurns = s.completedTurns
	response.World = make([][]uint8, len(s.World))
	for i := range s.World {
		response.World[i] = append([]uint8(nil), s.World[i]...) // deep copy
	}
	s.lock.Unlock()

	// if we paused just for saving, resume again
	if !wasPaused {
		fmt.Println("Resuming after save...")
		s.pauseReq <- false

		s.lock.Lock()
		s.paused = false
		s.lock.Unlock()
	}

	return nil
}

func (s *GameOfLifeServer) Quit(request gol.QuitRequest, response *gol.QuitResponse) error {
	s.pauseReq <- true
	s.quitReq <- struct{}{}
	return nil
}

func (s *GameOfLifeServer) Kill(request gol.KillRequest, response *gol.KillResponse) error {
	s.pauseReq <- true
	s.quitReq <- struct{}{}
	//delay kill to ensure nil is returned
	go func() {
		// allow RPC to flush and client to receive response
		// small delay to ensure return nil is done
		time.Sleep(50 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// helper: deep-copy a world to avoid data races when sharing snapshots between goroutines
// this function was very important to avoid data races. u dont want any of the go routines to read or write directly to world as it is unstable
// func copyWorld(wrld [][]uint8) [][]uint8 {
// 	if wrld == nil {
// 		return nil
// 	}
// 	h := len(wrld)
// 	if h == 0 {
// 		return [][]uint8{}
// 	}
// 	w := len(wrld[0])
// 	dst := make([][]uint8, h)
// 	for y := 0; y < h; y++ {
// 		dst[y] = make([]uint8, w)
// 		copy(dst[y], wrld[y])
// 	}
// 	return dst
// }

func main() {
    // addresses passed as args:
    // go run server.go localhost:7000 localhost:7001 ...
    workerAddrs := os.Args[1:]
    if len(workerAddrs) == 0 {
        fmt.Println("No worker addresses supplied")
        os.Exit(1)
    }

    srv := &GameOfLifeServer{
        workerAddrs: workerAddrs,
    }

    // dial workers
    for _, addr := range workerAddrs {
        cl, err := rpc.Dial("tcp", addr)
        if err != nil {
            panic(err)
        }
        srv.workers = append(srv.workers, cl)
    }

    rpc.Register(srv)
    listener, _ := net.Listen("tcp", ":6000")
    defer listener.Close()
    rpc.Accept(listener)
}

func (s *GameOfLifeServer) distributedStep(world [][]uint8) ([][]uint8, []util.Cell) {
    height := len(world)
    width := len(world[0])
    nWorkers := len(s.workers)

    rowsPerWorker := height / nWorkers
    extra := height % nWorkers

    // channel for worker results (just like parallel version)
    type result struct {
        startY int
        section [][]uint8
        flipped []util.Cell
    }
    results := make(chan result, nWorkers)

    startRow := 0

    // spawn RPC workers
    for i := 0; i < nWorkers; i++ {
        // determine row range for this worker
        rows := rowsPerWorker
        if i < extra {
            rows++
        }
        endRow := startRow + rows

        // build halo chunk 
        topHalo := world[(startRow-1+height)%height]     // row above start
        bottomHalo := world[endRow%height]               // row below end

        chunk := make([][]uint8, 0, rows+2)
        chunk = append(chunk, topHalo)                   // halo above
        chunk = append(chunk, world[startRow:endRow]...) // worker rows
        chunk = append(chunk, bottomHalo)                // halo below

        req := gol.WorkerStepRequest{
            Chunk:  chunk,
            StartY: startRow,
            Width:  width,
            Height: len(chunk),
        }

        worker := s.workers[i]

        // make goroutine for RPC call
        go func(start, end int) {
            var resp gol.WorkerStepResponse
            err := worker.Call("Worker.Step", req, &resp)
            if err != nil {
                panic(err)
            }

            // send back results
            results <- result{
                startY:  start,
                section: resp.NewInner,
                flipped: resp.Flipped,
            }
        }(startRow, endRow)
		
		// startRow for the next worker is endRow for this one 
        startRow = endRow
    }

    // build new world
    newWorld := make([][]uint8, height)
    for i := range newWorld {
        newWorld[i] = make([]uint8, width)
    }

    var allFlipped []util.Cell

    // collect all results 
    for i := 0; i < nWorkers; i++ {
        r := <-results

        // copy worker’s computed rows into the new world
        for j := range r.section {
            newWorld[r.startY+j] = r.section[j]
        }

        if len(r.flipped) > 0 {
            allFlipped = append(allFlipped, r.flipped...)
        }
    }

    return newWorld, allFlipped
}



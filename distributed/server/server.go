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

	workers        []*rpc.Client
	allWorkers     []*rpc.Client
	activeWorkers  int
	activeWorkerThreads int
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

        // pause / quit handling
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

        newWorld, flipped := s.distributedStep(world)

        s.lock.Lock()
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
    // Hardcode worker private IPs
    workerAddrs := []string{
        "54.147.138.250:7000", // GOLEC2-2
        "18.232.42.255:7000",  // GOLEC2-3
        "98.91.115.1:7000",    // GOLEC2-4
    }

    srv := &GameOfLifeServer{}

    // dial all workers up front
    for _, addr := range workerAddrs {
        cl, err := rpc.Dial("tcp", addr)
        if err != nil {
            panic(err)
        }
        srv.allWorkers = append(srv.allWorkers, cl)
    }

    // by default use all workers
    srv.activeWorkers = len(srv.allWorkers)
    srv.workers = srv.allWorkers[:srv.activeWorkers]

    // register server
    rpc.Register(srv)

    // listen for client RPCs
    listener, err := net.Listen("tcp", ":7000")
    if err != nil {
        panic(err)
    }
    defer listener.Close()

    rpc.Accept(listener)
}


func (s *GameOfLifeServer) distributedStep(world [][]uint8) ([][]uint8, []util.Cell) {
    height := len(world)
    width := len(world[0])

    s.lock.Lock()
    nWorkers := s.activeWorkers

    workers := make([]*rpc.Client, nWorkers)
    copy(workers, s.workers[:nWorkers])
    s.lock.Unlock()

    rowsPerWorker := height / nWorkers
    extra := height % nWorkers

    type result struct {
        startY  int
        section [][]uint8
        flipped []util.Cell
    }

    results := make(chan result, nWorkers)
    startRow := 0

    for i := 0; i < nWorkers; i++ {
        rows := rowsPerWorker
        if i < extra {
            rows++
        }
        endRow := startRow + rows

        // build chunk with halo rows
        topHalo := world[(startRow-1+height)%height]
        bottomHalo := world[endRow%height]

        chunk := make([][]uint8, 0, rows+2)
        chunk = append(chunk, topHalo)
        chunk = append(chunk, world[startRow:endRow]...)
        chunk = append(chunk, bottomHalo)

        // build request
        req := gol.WorkerStepRequest{
            Chunk:  chunk,
            StartY: startRow,
            Width:  width,
            Height: len(chunk),
			NumThreads: s.activeWorkerThreads,
        }

        // make a local copy of req so goroutines don't share it
        localReq := req
        worker := workers[i]

        go func(start, end int, r gol.WorkerStepRequest) {
            var resp gol.WorkerStepResponse
            if err := worker.Call("Worker.Step", r, &resp); err != nil {
                panic(err)
            }

            results <- result{
                startY:  start,
                section: resp.NewInner,
                flipped: resp.Flipped,
            }
        }(startRow, endRow, localReq)

        startRow = endRow
    }

    newWorld := make([][]uint8, height)
    for i := range newWorld {
        newWorld[i] = make([]uint8, width)
    }

    var allFlipped []util.Cell

    for i := 0; i < nWorkers; i++ {
        r := <-results

        for j := range r.section {
            newWorld[r.startY+j] = r.section[j]
        }

        if len(r.flipped) > 0 {
            allFlipped = append(allFlipped, r.flipped...)
        }
    }

    return newWorld, allFlipped
}


func (s *GameOfLifeServer) ConfigureWorkers(req gol.WorkerConfigRequest, res *gol.WorkerConfigResponse) error {
    // clamp N
    if req.NumWorkers < 1 {
        req.NumWorkers = 1
    }
    if req.NumWorkers > len(s.allWorkers) {
        req.NumWorkers = len(s.allWorkers)
    }

    s.lock.Lock()
    s.activeWorkers = req.NumWorkers
    s.workers = s.allWorkers[:req.NumWorkers]
    s.lock.Unlock()

    return nil
}

func (s *GameOfLifeServer) ConfigureWorkerThreads(req gol.ThreadConfigRequest, res *gol.ThreadConfigResponse) error {
    s.lock.Lock()
    s.activeWorkerThreads = req.NumThreads
    s.lock.Unlock()
    return nil
}

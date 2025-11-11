package main

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"sync"

	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/util"
)

type GameOfLifeServer struct {
	currentAlive   []util.Cell
	flipped        []util.Cell
	completedTurns int
	lock           sync.Mutex
	paused         bool
	pauseReq       chan bool // shared pause/unpause channel
	quitReq        chan struct{}
	killReq        chan struct{}
	World          [][]uint8
}

// AdvanceWorld is RPC method that clients call to advance the world
func (s *GameOfLifeServer) AdvanceWorld(request gol.GolRequest, response *gol.GolResponse) error {
	world := request.World

	s.lock.Lock()
	s.paused = false
	s.lock.Unlock()

	// create pause channel only once
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

		select {
		case pause := <-s.pauseReq:
			for pause {
				// wait here until unpaused
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
		newWorld, flipped := gol.CalculateNextState(world)
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
	select {
	case <-s.killReq:
		os.Exit(0)
	default:
	}

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
	fmt.Println("✅ World saved at turn", s.completedTurns)
	s.lock.Unlock()

	// 🔹 Step 3: If we paused just for saving, resume again
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
	s.killReq <- struct{}{}
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
	rpc.Register(new(GameOfLifeServer))

	listener, _ := net.Listen("tcp", ":6000")
	defer listener.Close()

	// no logging here, matches your original
	rpc.Accept(listener)
}

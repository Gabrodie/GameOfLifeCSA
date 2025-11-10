package main

import (
	"net"
	"net/rpc"
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
		s.pauseReq = make(chan bool)
	} else if s.quitReq == nil {
		s.quitReq = make(chan struct{})
	}

	for i := 0; i < request.Turns; i++ {
		s.lock.Lock()
		isPaused := s.paused
		s.lock.Unlock()
		var quit bool

		if isPaused {
			for {
				pause := <-s.pauseReq // blocks until signal received
				s.lock.Lock()
				s.paused = pause
				s.lock.Unlock()
				if !pause {
					break // resume
				}
			}
		}
		select {
		case <-s.quitReq:
			quit = true
		default:
		}
		if quit == true {
			break
		}

		s.lock.Lock()
		var flipped []util.Cell
		s.World, flipped = gol.CalculateNextState(world)
		s.currentAlive = gol.CalculateAliveCells(world)
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
	s.lock.Lock()
	if !s.paused {
		s.paused = true
		if s.pauseReq != nil {
			// Unlock before sending so we don’t deadlock AdvanceWorld
			s.lock.Unlock()
			s.pauseReq <- true
			s.lock.Lock()
		}
	}
	// Now we are guaranteed paused, so World is stable
	response.CompletedTurns = s.completedTurns
	response.World = make([][]uint8, len(s.World))
	for i := range s.World {
		response.World[i] = append([]uint8(nil), s.World[i]...) // deep copy
	}
	s.lock.Unlock()

	return nil
}

func (s *GameOfLifeServer) Quit(request gol.QuitRequest, response *gol.QuitResponse) error {
	s.lock.Lock()
	if !s.paused {
		s.paused = true
		if s.pauseReq != nil {
			// Unlock before sending so we don’t deadlock AdvanceWorld
			s.lock.Unlock()
			s.pauseReq <- true
			s.lock.Lock()
		}
	}
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

	// 🔹 Reverted: no error check on Listen
	listener, _ := net.Listen("tcp", ":6000")
	defer listener.Close()

	// no logging here, matches your original
	rpc.Accept(listener)
}

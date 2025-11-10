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
}

// advanceWorld is rpc method that clients call to advance the world
func (s *GameOfLifeServer) AdvanceWorld(request gol.GolRequest, response *gol.GolResponse) error {
	// copy the incoming world state
	world := request.World

	// run the simulation for the requested number of turns
	for i := 0; i < request.Turns; i++ {
		var flipped []util.Cell
		world, flipped = gol.CalculateNextState(world)
		s.currentAlive = gol.CalculateAliveCells(world)
		s.completedTurns = i + 1
		s.flipped = flipped

	}

	s.lock.Lock()
	// fill in the response that will be sent back the client
	response.World = world
	response.Alive = gol.CalculateAliveCells(world)
	s.lock.Unlock()

	// no errors occurred so return nil
	return nil
}

func (s *GameOfLifeServer) GetStatus(request gol.StatusRequest, response *gol.StatusResponse) error {
	s.lock.Lock()
	response.AliveCount = len(s.currentAlive)
	response.CompletedTurns = s.completedTurns
	response.FlippedCells = s.flipped
	s.lock.Unlock()
	return nil
}

func main() {
	// register the GameOfLifeServer type so its methods can be called over RPC
	rpc.Register(new(GameOfLifeServer))

	// listen for tcp connections on port 6000
	listener, _ := net.Listen("tcp", ":6000")

	defer listener.Close()

	// block forever until called by client
	rpc.Accept(listener)
}

package main

import (
    "net"
    "net/rpc"
    "uk.ac.bris.cs/gameoflife/gol"
)


type GameOfLifeServer struct{}

// advanceWorld is rpc method that clients call to advance the world 
func (s *GameOfLifeServer) AdvanceWorld(request gol.GolRequest, response *gol.GolResponse) error {
    // copy the incoming world state
    world := request.World

    // run the simulation for the requested number of turns.
    for i := 0; i < request.Turns; i++ {
        world, _ = gol.CalculateNextState(world)
    }

    // fill in the response that will be sent back the client
    response.World = world
    response.Alive = gol.CalculateAliveCells(world)

    // no errors occurred so return nil
    return nil
}

func main() {
    // register the GameOfLifeServer type so its methods can be called over RPC.
    rpc.Register(new(GameOfLifeServer))

    // listen for tcp connections on port 6000.
    listener, err := net.Listen("tcp", ":6000")
    if err != nil {
        panic(err)
    }
    defer listener.Close()

    // block forever until called by client
    rpc.Accept(listener)
}

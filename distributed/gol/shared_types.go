package gol

import (
	"uk.ac.bris.cs/gameoflife/util"
)


//shared types so both node and controller can use
type GolRequest struct {
	World       [][]uint8
    ImageWidth  int
    ImageHeight int
    Turns       int
}

type GolResponse struct {
    World [][]uint8
    Alive []util.Cell
}

type StatusRequest struct {
}

type StatusResponse struct {
    AliveCount int
    CompletedTurns int
}
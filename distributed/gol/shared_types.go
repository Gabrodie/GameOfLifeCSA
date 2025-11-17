package gol

import (
	"uk.ac.bris.cs/gameoflife/util"
)

// shared types so both node and controller can use
type GolRequest struct {
	World       [][]uint8
	ImageWidth  int
	ImageHeight int
	Turns       int
}

type GolResponse struct {
	World          [][]uint8
	Alive          []util.Cell
	CompletedTurns int
}

type StatusRequest struct {
}

type StatusResponse struct {
	AliveCount     int
	CompletedTurns int
	FlippedCells   []util.Cell
}

type PauseRequest struct {
}

type PauseResponse struct {
	Paused         bool
	CompletedTurns int
}

type SaveRequest struct{}

type SaveResponse struct {
	CompletedTurns int
	World          [][]uint8
}

type QuitRequest struct{}

type QuitResponse struct{}

type KillRequest struct{}

type KillResponse struct{}

type WorkerStepRequest struct {
    // Chunk includes 1 halo row above and 1 below
    Chunk [][]uint8

    // Global starting row index of the *inner* chunk (not including the top halo)
    StartY int

    Width  int
    Height int // total height of Chunk including halos
}

type WorkerStepResponse struct {
    // NewInner will be len = Height-2 (no halos), rows that correspond to StartY..StartY+innerHeight-1
    NewInner [][]uint8

    // Flipped cells with global coordinates (Broker doesn’t need to shift them)
    Flipped []util.Cell
}

type WorkerConfigRequest struct {
    NumWorkers int
}

type WorkerConfigResponse struct{}

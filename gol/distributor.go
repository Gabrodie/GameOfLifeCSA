package gol

import (
	"fmt"

	"uk.ac.bris.cs/gameoflife/util"
)

//test comment

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {

	// TODO: Create a 2D slice to store the world.
	c.ioCommand <- ioInput
	c.ioFilename <- fmt.Sprintf("%vx%v", p.ImageWidth, p.ImageHeight)
	world := make([][]uint8, p.ImageHeight)
	for i := range world {
		world[i] = make([]uint8, p.ImageWidth)
		for j := range world[i] {
			world[i][j] = <-c.ioInput
		}
	}

	turn := 0
	c.events <- StateChange{turn, Executing}

	// TODO: Execute all turns of the Game of Life.
	for turn := 0; turn < p.Turns; turn++ {
		var flipped []util.Cell
		world, flipped = CalculateNextState(world)
		if len(flipped) > 0 {
			c.events <- CellsFlipped{CompletedTurns: turn + 1, Cells: flipped}
		}

		c.events <- TurnComplete{CompletedTurns: turn + 1}
	}

	// TODO: Report the final state using FinalTurnCompleteEvent.
	aliveCells := calculateAliveCells(world)
	c.events <- FinalTurnComplete{
		CompletedTurns: turn,
		Alive:          aliveCells,
	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}

func CalculateNextState(world [][]uint8) ([][]uint8, []util.Cell) {
	height := len(world)
	width := len(world[0])
	newWorld := make([][]uint8, height)
	var flipped []util.Cell

	for y := 0; y < height; y++ {
		newWorld[y] = make([]uint8, width)
		for x := 0; x < width; x++ {
			aliveNeighbors := countAliveNeighbors(world, x, y, width, height)
			current := world[y][x]
			next := current
			if current != 0 {
				// Cell is currently alive
				if aliveNeighbors < 2 {
					next = 0

				} else if aliveNeighbors == 2 || aliveNeighbors == 3 {
					next = 255
				} else {
					next = 0
				}
			} else {
				// dead cell
				if aliveNeighbors == 3 {
					next = 255
				} else {
					next = 0
				}
			}
			if next != current {
				flipped = append(flipped, util.Cell{X: x, Y: y})
			}
			newWorld[y][x] = next
		}

	}
	return newWorld, flipped
}

func countAliveNeighbors(world [][]uint8, x, y, width, height int) int {
	sum := 0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dy == 0 && dx == 0 {
				continue
			}
			ny := (y + dy + height) % height
			nx := (x + dx + width) % width
			if world[ny][nx] != 0 {
				sum++
			}
		}
	}
	return sum
}

func calculateAliveCells(world [][]uint8) []util.Cell {
	var alive []util.Cell
	for y := 0; y < len(world); y++ {
		for x := 0; x < len(world[y]); x++ {
			if world[y][x] != 0 {
				alive = append(alive, util.Cell{X: x, Y: y})
			}
		}
	}
	return alive
}

package gol

import (
	"fmt"
	"time"

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

// new struct so workers can send both their section, flipped and which row they started at
type workerResult struct {
	section [][]uint8
	flipped []util.Cell
	startY  int
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

	// ticker function
	aliveCountChan := make(chan AliveCellsCount, 1)
	done := make(chan struct{})
	ticker := time.NewTicker(2 * time.Second)

	// Ticker goroutine sends AliveCellsCount events every 2 seconds
	go func() {
		defer ticker.Stop()

		// store latest turn and alive count known by ticker
		currentTurn := 0
		currentAlive := len(calculateAliveCells(world)) // initial world count

		for {
			select {
			case <-done:
				return

			case <-ticker.C:
				// if new turn info waiting, take it
				select {
				case ac := <-aliveCountChan:
					currentTurn = ac.CompletedTurns
					currentAlive = ac.CellsCount
				default:
				}

				// send AliveCellsCount (non-blocking)
				select {
				case c.events <- AliveCellsCount{CompletedTurns: currentTurn, CellsCount: currentAlive}:
				default:
					// GUI not ready? drop to avoid deadlock
				}
			}
		}
	}()

	// TODO: Execute all turns of the Game of Life.
	for turn := 0; turn < p.Turns; turn++ {

		// find how many rows each thread should get. this might not be correct due to int. div but next part fixes
		rowsPerThread := p.ImageHeight / p.Threads
		results := make(chan workerResult, p.Threads)

		for t := 0; t < p.Threads; t++ {
			startRow := t * rowsPerThread
			endRow := (t + 1) * rowsPerThread
			if t == p.Threads-1 {
				// if p.ImageHeight not divisble by number of threads set last thread to take last row, otherwise last few rows wont get any worker
				endRow = p.ImageHeight
			}
			go worker(startRow, endRow, world, p.ImageWidth, p.ImageHeight, results)
		}

		newWorld := make([][]uint8, p.ImageHeight)

		var allFlipped []util.Cell
		// collect results from workers and create new world
		for t := 0; t < p.Threads; t++ {
			// read each section from the channel
			workerResults := <-results
			for i := range workerResults.section {
				/* iterate through the worker's rows. So if worker handled rows 10–15 then
				newWorld[10 + 0] = section[0]
				newWorld[10 + 1] = section[1] etc*/
				newWorld[workerResults.startY+i] = workerResults.section[i]
			}
			allFlipped = append(allFlipped, workerResults.flipped...)
		}

		if len(allFlipped) > 0 {
			c.events <- CellsFlipped{CompletedTurns: turn + 1, Cells: allFlipped}
		}

		// update world to newly computed world
		world = newWorld

		// notify GUI turn is done
		c.events <- TurnComplete{CompletedTurns: turn + 1}

		// send updated alive count for ticker to use
		alive := len(calculateAliveCells(world))
		select {
		case aliveCountChan <- AliveCellsCount{CompletedTurns: turn + 1, CellsCount: alive}:
		default:
			// drop if ticker hasn't consumed yet
		}
	}

	// stop ticker loop
	close(done)

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

// each worker takes their own chunk of rows and processes them
func worker(startRow int, endRow int, world [][]uint8, width int, height int, result chan<- workerResult) {
	newSection := make([][]uint8, endRow-startRow)
	var flipped []util.Cell

	// same logic in CalculateNextState()
	for y := startRow; y < endRow; y++ {
		newSection[y-startRow] = make([]uint8, width)
		for x := 0; x < width; x++ {
			aliveNeighbors := countAliveNeighbors(world, x, y, width, height)
			current := world[y][x]
			next := current

			if current != 0 {
				// Cell is currently active
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
			newSection[y-startRow][x] = next
		}
	}
	// send results to the channel
	result <- workerResult{
		section: newSection,
		flipped: flipped,
		startY:  startRow,
	}
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

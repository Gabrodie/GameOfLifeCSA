package gol

import (
	"fmt"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

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

// new struct to hold all info after completed turns
type turnData struct {
	CompletedTurns int
	CellsCount     int
	world          [][]uint8
	Alive          []util.Cell
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels, keyPresses <-chan rune) {

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
	initialAlive := calculateAliveCells(world)
	if len(initialAlive) > 0 {
		c.events <- CellsFlipped{CompletedTurns: 0, Cells: initialAlive}
	}
	c.events <- StateChange{0, Executing}

	// channels used for sharing the latest snapshot with ticker and keypress goroutine
	snapshotTicker := make(chan turnData, 1) // ticker reads latest snapshot
	snapshotKey := make(chan turnData, 1)    // keypress goroutine reads latest snapshot for save/quit
	quitReq := make(chan struct{}, 1)        // keypress signals quit to main loop
	done := make(chan struct{})              // closed by main once simulation finishes
	pauseReq := make(chan bool, 1)           // unimplemented: can be used to signal pause/resume

	// Ticker goroutine sends AliveCellsCount events every 2 seconds
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		// store latest turn and alive count known by ticker
		currentTurn := 0
		currentAlive := len(initialAlive) // initial world count

		for {
			select {
			case <-done:
				return

			case <-ticker.C:
				// if new turn info waiting, take it
				select {
				case ac := <-snapshotTicker:
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

	// keyPresses handling goroutine: handles pause/save/quit using latest snapshot
	go func() {
		paused := false
		var latestSnapshot turnData
		for {
			select {
			case data := <-snapshotKey:
				// update latest snapshot
				latestSnapshot = data
			case key := <-keyPresses:
				// allow toggle pause anytime
				if key == 'p' {
					paused = !paused
					if paused {
						c.events <- StateChange{latestSnapshot.CompletedTurns, Paused}
						pauseReq <- true

					} else {
						c.events <- StateChange{latestSnapshot.CompletedTurns, Executing}
						pauseReq <- false
					}
					continue
				}

				// when paused, 's' saves and 'q' requests quit; 'q' can also be allowed when not paused if desired
				if key == 's' {
					// save state using latestSnapshot.world (deep copy already provided by main)
					pauseReq <- true
					c.ioCommand <- ioOutput
					c.ioFilename <- fmt.Sprintf("%vx%vx%v", p.ImageWidth, p.ImageHeight, latestSnapshot.CompletedTurns)
					for y := 0; y < p.ImageHeight; y++ {
						for x := 0; x < p.ImageWidth; x++ {
							val := latestSnapshot.world[y][x]
							c.ioOutput <- val
						}
					}
					c.ioCommand <- ioCheckIdle
					<-c.ioIdle
					c.events <- ImageOutputComplete{CompletedTurns: latestSnapshot.CompletedTurns, Filename: fmt.Sprintf("%vx%vx%v", p.ImageWidth, p.ImageHeight, latestSnapshot.CompletedTurns)}

					pauseReq <- false
					continue
				}

				if key == 'q' {
					// signal main loop to quit (non-blocking)
					pauseReq <- true
					select {
					case quitReq <- struct{}{}:
					default:
					}
					return
				}
			default:
				// small sleep to avoid busy loop when idle
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// TODO: Execute all turns of the Game of Life.
	latestTurn := 0
	for turn := 0; turn < p.Turns; turn++ {
		var quit bool

		// check for quit request
		select {
		case pause := <-pauseReq:
			for pause {
				// wait here until unpaused
				select {
				case pause = <-pauseReq:
				case <-quitReq:
					quit = true
					pause = false
				}
			}
		default:
		}
		if quit {
			break
		}

		// find how many rows each thread should get. this might not be correct due to int. div but next part fixes
		rowsPerThread := p.ImageHeight / p.Threads
		results := make(chan workerResult, p.Threads)
		worldCpyWorkers := copyWorld(world)

		for t := 0; t < p.Threads; t++ {
			startRow := t * rowsPerThread
			endRow := (t + 1) * rowsPerThread
			if t == p.Threads-1 {
				// if p.ImageHeight not divisble by number of threads set last thread to take last row, otherwise last few rows wont get any worker
				endRow = p.ImageHeight
			}
			go worker(startRow, endRow, worldCpyWorkers, p.ImageWidth, p.ImageHeight, results)
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

		// send updated alive count for ticker and latest snapshot for keypress
		alive := calculateAliveCells(world)
		// create deep copy of world for safe sharing
		worldCopy := copyWorld(world)
		snap := turnData{CompletedTurns: turn + 1, CellsCount: len(alive), world: worldCopy, Alive: alive}
		latestTurn = turn + 1

		// non-blocking updates to ticker and keypress snapshot channels
		select {
		case snapshotTicker <- snap:
		default:
		}
		select {
		case snapshotKey <- snap:
		default:
		}
	}

	// stop ticker goroutine
	close(done)

	// TODO: Report the final state using FinalTurnCompleteEvent.
	aliveCells := calculateAliveCells(world)
	c.events <- FinalTurnComplete{
		CompletedTurns: latestTurn,
		Alive:          aliveCells,
	}

	// Make sure that the Io has finished any output before exiting.
	// Send the final image to the io goroutine.
	c.ioCommand <- ioOutput
	c.ioFilename <- fmt.Sprintf("%vx%vx%v", p.ImageWidth, p.ImageHeight, latestTurn)
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			val := world[y][x]
			c.ioOutput <- val
		}
	}

	// Wait for the io goroutine to finish.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	// Notify the GUI that the image output is complete.
	c.events <- ImageOutputComplete{CompletedTurns: latestTurn, Filename: fmt.Sprintf("%vx%vx%v", p.ImageWidth, p.ImageHeight, latestTurn)}

	// Notify the GUI that we are quitting.
	c.events <- StateChange{latestTurn, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}

// helper: deep-copy a world to avoid data races when sharing snapshots between goroutines
// this function was very important to avoid data races. u dont want any of the go routines to read or write directly to world as it is unstable
func copyWorld(wrld [][]uint8) [][]uint8 {
	if wrld == nil {
		return nil
	}
	h := len(wrld)
	if h == 0 {
		return [][]uint8{}
	}
	w := len(wrld[0])
	dst := make([][]uint8, h)
	for y := 0; y < h; y++ {
		dst[y] = make([]uint8, w)
		copy(dst[y], wrld[y])
	}
	return dst
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

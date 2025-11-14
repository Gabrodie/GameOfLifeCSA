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
	keyPresses <-chan rune
}

type workerResult struct {
	section [][]uint8
	flipped []util.Cell
	startY  int
}

type turnData struct {
	CompletedTurns int
	CellsCount     int
	world          [][]uint8
	Alive          []util.Cell
}

// counts alive cells surounding cells of every cell
func countAliveNeighbors(world [][]uint8, x, y, width, height int) int {
	sum := 0
	//3 rows
	for dy := -1; dy <= 1; dy++ {
		//3 columns
		for dx := -1; dx <= 1; dx++ {
			//ignoring middle cell
			if dy == 0 && dx == 0 {
				continue
			}
			//uses modulo for wrap around function(eg. when y =0, y + dy + height = height -1 ... ny = height -1 % height = height -1)
			ny := (y + dy + height) % height
			nx := (x + dx + width) % width
			if world[ny][nx] != 0 {
				sum++
			}
		}
	}
	return sum
}

func countAliveNeighborsFast(world [][]uint8, x, y, width, height int) int {
	sum := 0
	//no more modulo
	// Row above
	ny := y - 1
	//wrap around
	if ny < 0 {
		ny = height - 1
	}
	// Left
	nx := x - 1
	//wrap around
	if nx < 0 {
		nx = width - 1
	}
	if world[ny][nx] != 0 {
		sum++
	}

	// Middle
	nx = x
	if world[ny][nx] != 0 {
		sum++
	}

	// Right
	nx = x + 1
	if nx >= width {
		nx = 0
	}
	if world[ny][nx] != 0 {
		sum++
	}

	// Same row
	ny = y

	//left
	nx = x - 1
	if nx < 0 {
		nx = width - 1
	}
	if world[ny][nx] != 0 {
		sum++
	}

	//right
	nx = x + 1
	if nx >= width {
		nx = 0
	}
	if world[ny][nx] != 0 {
		sum++
	}

	// Row below
	ny = y + 1
	if ny >= height {
		ny = 0
	}

	//left
	nx = x - 1
	if nx < 0 {
		nx = width - 1
	}
	if world[ny][nx] != 0 {
		sum++
	}

	//middle
	nx = x
	if world[ny][nx] != 0 {
		sum++
	}

	//right
	nx = x + 1
	if nx >= width {
		nx = 0
	}
	if world[ny][nx] != 0 {
		sum++
	}

	return sum
}

// CalculateNextState was implemnted for serial implementation (intentionally unchanged).
func CalculateNextState(world [][]uint8) ([][]uint8, []util.Cell) {
	height := len(world)
	width := len(world[0])
	newWorld := make([][]uint8, height)
	var flipped []util.Cell

	for y := 0; y < height; y++ {
		newWorld[y] = make([]uint8, width)
		for x := 0; x < width; x++ {
			aliveNeighbors := countAliveNeighborsFast(world, x, y, width, height)
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

// each worker takes their own chunk of rows and processes them
func worker(startRow int, endRow int, world [][]uint8, width int, height int, result chan<- workerResult) {
	newSection := make([][]uint8, endRow-startRow)
	var flipped []util.Cell

	for y := startRow; y < endRow; y++ {
		newSection[y-startRow] = make([]uint8, width)
		for x := 0; x < width; x++ {
			aliveNeighbors := countAliveNeighborsFast(world, x, y, width, height)
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

// calculates Alive Cells in a each state of the world
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

// deep-copy a world to avoid data races when sharing snapshots between goroutines
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

// writeImage writes the world to the io goroutine and waits for it to become idle.
// Extracted helper to avoid duplicated image-output code.
func writeImage(c distributorChannels, world [][]uint8, w, h, turn int) {
	c.ioCommand <- ioOutput
	c.ioFilename <- formatFilename(w, h, turn)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c.ioOutput <- world[y][x]
		}
	}
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- ImageOutputComplete{CompletedTurns: turn, Filename: formatFilename(w, h, turn)}
}

// formatFilename centralises filename formatting.
func formatFilename(w, h, turn int) string {
	return fmt.Sprintf("%vx%vx%v", w, h, turn)
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

	initialAlive := calculateAliveCells(world)
	if len(initialAlive) > 0 {
		c.events <- CellsFlipped{CompletedTurns: 0, Cells: initialAlive}
	}
	c.events <- StateChange{0, Executing}

	// channels used to share latest snapshot with ticker and keypress goroutine
	// ticker reads latest snapshot
	snapshotTicker := make(chan turnData, 1)
	// keypress uses latest snapshot for save/quit
	snapshotKey := make(chan turnData, 1)
	// keypress signals quit to main loop
	quitReq := make(chan struct{}, 1)
	// closed by main once simulation finishes
	done := make(chan struct{})
	// used to signal pause/resume to main loop
	pauseReq := make(chan bool, 1)

	//ticker go routine that sends alivecellcount every 2 seconds
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		currentTurn := 0
		currentAlive := len(initialAlive)

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// update currentTurn/currentAlive if a newer snapshot is available
				select {
				case ac := <-snapshotTicker:
					currentTurn = ac.CompletedTurns
					currentAlive = ac.CellsCount
				default:
				}

				// send AliveCellsCount event non-blocking
				select {
				case c.events <- AliveCellsCount{CompletedTurns: currentTurn, CellsCount: currentAlive}:
				default:
				}
			}
		}
	}()

	// go rountine that handles keypresses 'q', 'p' and 's'
	go func() {
		paused := false
		var latestSnapshot turnData

		for {
			select {
			case data := <-snapshotKey:
				latestSnapshot = data

			case key := <-c.keyPresses:
				// toggle pause instantly
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

				// when paused, 's' saves (but we allow save anytime)
				if key == 's' {
					// signal pause to main loop to ensure snapshot stability
					pauseReq <- true
					// use helper to write image (removes duplication)
					writeImage(c, latestSnapshot.world, p.ImageWidth, p.ImageHeight, latestSnapshot.CompletedTurns)
					// resume
					pauseReq <- false
					continue
				}

				// 'q' requests quit
				if key == 'q' {
					// ensure main loop pauses and then request quit
					pauseReq <- true
					select {
					case quitReq <- struct{}{}:
					default:
					}
					return
				}

			default:
				// avoid busy loop
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// TODO: Execute all turns of the Game of Life.
	//Advancing state of the world Loop
	latestTurn := 0
	for turn := 0; turn < p.Turns; turn++ {
		var quit bool

		// check for pause/quit requests
		select {
		case pause := <-pauseReq:
			for pause {
				select {
				case pause = <-pauseReq:
					// continue loop until unpaused
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

		// compute rows per worker and spawn workers
		rowsPerThread := p.ImageHeight / p.Threads
		results := make(chan workerResult, p.Threads)
		worldCopyWorkers := copyWorld(world)

		for t := 0; t < p.Threads; t++ {
			startRow := t * rowsPerThread
			endRow := (t + 1) * rowsPerThread
			if t == p.Threads-1 {
				// last worker takes remaining rows
				endRow = p.ImageHeight
			}
			go worker(startRow, endRow, worldCopyWorkers, p.ImageWidth, p.ImageHeight, results)
		}

		// collect worker results and build new world
		newWorld := make([][]uint8, p.ImageHeight)
		var allFlipped []util.Cell

		for t := 0; t < p.Threads; t++ {
			workerResults := <-results
			for i := range workerResults.section {
				newWorld[workerResults.startY+i] = workerResults.section[i]
			}
			if len(workerResults.flipped) > 0 {
				allFlipped = append(allFlipped, workerResults.flipped...)
			}
		}

		if len(allFlipped) > 0 {
			c.events <- CellsFlipped{CompletedTurns: turn + 1, Cells: allFlipped}
		}

		// update world and notify GUI
		world = newWorld
		c.events <- TurnComplete{CompletedTurns: turn + 1}

		// create snapshot for ticker and keypress goroutines
		alive := calculateAliveCells(world)
		worldCopy := copyWorld(world) // safe deep copy for sharing
		snap := turnData{CompletedTurns: turn + 1, CellsCount: len(alive), world: worldCopy, Alive: alive}
		latestTurn = turn + 1

		// non-blocking update of snapshots
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
	c.events <- FinalTurnComplete{CompletedTurns: latestTurn, Alive: aliveCells}

	// send final image
	writeImage(c, world, p.ImageWidth, p.ImageHeight, latestTurn)

	// notify GUI and close
	c.events <- StateChange{latestTurn, Quitting}
	close(c.events)
}

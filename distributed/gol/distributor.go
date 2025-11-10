package gol

import (
	"fmt"
	"net/rpc"
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

// // new struct so workers can send both their section, flipped and which row they started at
// type workerResult struct {
// 	section [][]uint8
// 	flipped []util.Cell
// 	startY  int
// }

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels, keyPresses <-chan rune) {

	// connect to the AWS server via rpc (localhost for now to test)
	client, err := rpc.Dial("tcp", "localhost:6000")
	if err != nil {
		panic(err)
	}
	defer client.Close()

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
	initialAlive := CalculateAliveCells(world)
	if len(initialAlive) > 0 {
		c.events <- CellsFlipped{CompletedTurns: 0, Cells: initialAlive}
	}
	c.events <- StateChange{0, Executing}

	// channels used for sharing the latest snapshot with ticker and keypress goroutine
	// snapshotTicker := make(chan turnData, 1) // ticker reads latest snapshot
	// snapshotKey := make(chan turnData, 1) // keypress goroutine reads latest snapshot for save/quit
	// quitReq := make(chan struct{}, 1)     // keypress signals quit to main loop
	done := make(chan struct{}) // closed by main once simulation finishes
	// pauseReq := make(chan bool, 1)        // unimplemented: can be used to signal pause/resume

	// Ticker goroutine sends AliveCellsCount events every 2 seconds
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		// store latest turn and alive count known by ticker
		// currentTurn := 0
		// currentAlive := len(initialAlive) // initial world count

		for {
			select {
			case <-done:
				return

			case <-ticker.C:
				var status StatusResponse
				err := client.Call("GameOfLifeServer.GetStatus", StatusRequest{}, &status)
				if err == nil {
					c.events <- AliveCellsCount{CompletedTurns: status.CompletedTurns, CellsCount: status.AliveCount}
					c.events <- CellsFlipped{CompletedTurns: status.CompletedTurns, Cells: status.FlippedCells}
					c.events <- TurnComplete{CompletedTurns: status.CompletedTurns}
				}

			}
		}
	}()

	// keyPresses handling goroutine: handles pause/save/quit using latest snapshot
	go func() {
		paused := false

		for {
			select {
			case key := <-keyPresses:
				// allow toggle pause anytime
				if key == 'p' {
					paused = !paused
					var PauseResponse PauseResponse
					err := client.Call("GameOfLifeServer.Pause", PauseRequest{}, &PauseResponse)
					if err != nil {
						panic(err)
					}
					if paused {
						c.events <- StateChange{PauseResponse.CompletedTurns, Paused}

					} else {
						c.events <- StateChange{PauseResponse.CompletedTurns, Executing}
					}
					continue
				}

				// when paused, 's' saves and 'q' requests quit; 'q' can also be allowed when not paused if desired
				if key == 's' {
					// save state using latestSnapshot.world (deep copy already provided by main)
					var SaveResponse SaveResponse
					err := client.Call("GameOfLifeServer.Save", SaveRequest{}, &SaveResponse)
					if err != nil {
						panic(err)
					}
					c.ioCommand <- ioOutput
					c.ioFilename <- fmt.Sprintf("%vx%vx%v", p.ImageWidth, p.ImageHeight, SaveResponse.CompletedTurns)

					for y := 0; y < p.ImageHeight; y++ {
						for x := 0; x < p.ImageWidth; x++ {
							val := SaveResponse.World[y][x]
							c.ioOutput <- val
						}
					}
					c.ioCommand <- ioCheckIdle
					<-c.ioIdle
					c.events <- ImageOutputComplete{CompletedTurns: SaveResponse.CompletedTurns, Filename: fmt.Sprintf("%vx%vx%v", p.ImageWidth, p.ImageHeight, SaveResponse.CompletedTurns)}

					continue
				}

				if key == 'q' {
					var QuitResponse SaveResponse
					err := client.Call("GameOfLifeServer.Quit", QuitRequest{}, &QuitResponse)
					if err != nil {
						panic(err)
					}
				}

			default:
				// small sleep to avoid busy loop when idle
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// TODO: Execute all turns of the Game of Life.

	// prepare the request struct
	req := GolRequest{
		World:       world,
		ImageWidth:  p.ImageWidth,
		ImageHeight: p.ImageHeight,
		Turns:       p.Turns,
	}
	var res GolResponse

	// call the remote function (defined in server.go)
	err = client.Call("GameOfLifeServer.AdvanceWorld", req, &res)
	if err != nil {
		panic(err)
	}

	// collect the returned result and continue like usual
	world = res.World
	aliveCells := res.Alive
	latestTurn := res.CompletedTurns

	// stop ticker goroutine
	close(done)

	// TODO: Report the final state using FinalTurnCompleteEvent.
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

// // each worker takes their own chunk of rows and processes them
// func worker(startRow int, endRow int, world [][]uint8, width int, height int, result chan<- workerResult) {
// 	newSection := make([][]uint8, endRow-startRow)
// 	var flipped []util.Cell

// 	// same logic in CalculateNextState()
// 	for y := startRow; y < endRow; y++ {
// 		newSection[y-startRow] = make([]uint8, width)
// 		for x := 0; x < width; x++ {
// 			aliveNeighbors := CountAliveNeighbors(world, x, y, width, height)
// 			current := world[y][x]
// 			next := current

// 			if current != 0 {
// 				// Cell is currently active
// 				if aliveNeighbors < 2 {
// 					next = 0
// 				} else if aliveNeighbors == 2 || aliveNeighbors == 3 {
// 					next = 255
// 				} else {
// 					next = 0
// 				}
// 			} else {
// 				// dead cell
// 				if aliveNeighbors == 3 {
// 					next = 255
// 				} else {
// 					next = 0
// 				}
// 			}

// 			if next != current {
// 				flipped = append(flipped, util.Cell{X: x, Y: y})
// 			}
// 			newSection[y-startRow][x] = next
// 		}
// 	}
// 	// send results to the channel
// 	result <- workerResult{
// 		section: newSection,
// 		flipped: flipped,
// 		startY:  startRow,
// 	}
// }

func CalculateNextState(world [][]uint8) ([][]uint8, []util.Cell) {
	height := len(world)
	width := len(world[0])
	newWorld := make([][]uint8, height)
	var flipped []util.Cell

	for y := 0; y < height; y++ {
		newWorld[y] = make([]uint8, width)
		for x := 0; x < width; x++ {
			aliveNeighbors := CountAliveNeighbors(world, x, y, width, height)
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

// capitalised so they can be exported (both functions below were lowercase previously)
func CountAliveNeighbors(world [][]uint8, x, y, width, height int) int {
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

func CalculateAliveCells(world [][]uint8) []util.Cell {
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

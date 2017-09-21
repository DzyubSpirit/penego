package main // import "git.yo2.cz/drahoslav/penego"

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"
	"github.com/pkg/profile"
	"github.com/fsnotify/fsnotify"
	"github.com/sqweek/dialog"
	"git.yo2.cz/drahoslav/penego/gui"
	"git.yo2.cz/drahoslav/penego/net"
)

const EXAMPLE = `
	g (1)
	e ( ) "exit"
	----
	g -> [exp(1s)] -> g, 2*e
`
const (
	quitIcon = '\uf00d'
	playIcon = '\uf04b'
	pauseIcon = '\uf04c'
	resetIcon = '\uf021'
)

func alwaysIcon(icon rune) (func() rune) {
	return func() rune {
		return icon
	}
}

type State int

const (
	Initial State = iota
	Running
	Paused
	Stopped
	Splash
	Idle
	Exit
)

type TimeFlow int

const (
	// no waits, just jum to the end of simulation
	NoFlow TimeFlow = iota
	// render as fast as reality, or proportionally faster/slower
	ContinuousFlow
	// render continuously, with fixed waits between events, independent of simulation time
	NaturalFlow
)

func (flow TimeFlow) String() string {
	return map[TimeFlow]string{
		NoFlow:         "no",
		ContinuousFlow: "continuous",
		NaturalFlow:    "natural",
	}[flow]
}

func (flow *TimeFlow) Set(name string) error {
	val, ok := map[string]TimeFlow{
		"no":         NoFlow,
		"continuous": ContinuousFlow,
		"natural":    NaturalFlow,
	}[name]
	if !ok {
		return fmt.Errorf("may be: no, continuous, natural")
	}
	*flow = val
	return nil
}

func main() {
	if os.Getenv("PROFILE") != "" {
		defer profile.Start(profile.CPUProfile, profile.ProfilePath(".")).Stop()
	}

	var (
		network net.Net
		err     error
	)

	// flags

	var (
		startTime  = time.Duration(0)
		endTime    = time.Hour * 24 * 1e5
		timeFlow   = ContinuousFlow
		timeSpeed  = uint(10)
		truerandom = false
		noclose    = true
		verbose    = false
		autostart  = false
	)

	flag.DurationVar(&startTime, "start", startTime, "start time of simulation")
	flag.DurationVar(&endTime, "end", endTime, "end time of simulation")
	flag.Var(&timeFlow, "flow", "type of time flow\n\tno, continuous, or natural")
	flag.UintVar(&timeSpeed, "speed", timeSpeed, "time flow acceleration\n\tdifferent meaning for different -flow\n\t")
	flag.BoolVar(&truerandom, "truerandom", truerandom, "seed pseudorandom generator with true random seed on start")
	flag.BoolVar(&noclose, "noclose", noclose, "preserve window after simulation ends")
	flag.BoolVar(&verbose, "v", verbose, "be more verbose")
	flag.BoolVar(&autostart, "autostart", autostart, "automatic start")
	flag.Parse()

	////////////////////////////////

	// load network from file if given filename

	pnstring := EXAMPLE

	read := func(filename string) (pnstring string) {
		filecontent, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Fatal(err)
			return
		}
		pnstring = string(filecontent)
		return
	}
	parse := func(pnstring string) (network net.Net) {
		network, err = net.Parse(pnstring)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s", err)
			return
		}
		if verbose {
			fmt.Println(network)
		}
		return
	}

	filename := flag.Arg(0)

	if len(filename) > 0 {
		pnstring = read(filename)
	} else {
		fmt.Println("No pn file specified, using example")
	}
	network = parse(pnstring)

	////////////////////////////////

	gui.Run(func(screen *gui.Screen) { // runs this anon func in goroutine

		var state State = Splash

		// how to draw
		var drawNet = getDrawNet(network)

		var onStateChange = func(before, now time.Duration) {
			switch timeFlow {
			case NoFlow:
			case NaturalFlow:
				time.Sleep((now - before) / time.Duration(timeSpeed))
			case ContinuousFlow:
				time.Sleep(time.Second / time.Duration(timeSpeed))
			}
			if verbose {
				fmt.Println(now, network.Places())
			}
			screen.SetTitle(now.String())
			screen.ForceRedraw(false) // block
		}

		var sim net.Simulation

		reload := func(filename string) {
			pnstring = read(filename)
			network = parse(pnstring)
			drawNet = getDrawNet(network)
			sim.Stop()
			state = Initial
		}

		watchFile := makeFileWatcher(reload)

		playPause := func() {
			switch state {
			case Paused:
				state = Running
			case Running:
				state = Paused
				sim.Pause()
			}
		}
		reset := func() {
			switch state {
			case Running, Paused, Idle:
				sim.Stop()
				state = Initial
			}
		}
		quit := func() {
			screen.SetShouldClose(true)
		}

		open := func() {
			go func() {
				filename, err := dialog.File().Filter("Penego notation", "pn").SetStartDir(".").Load()
				if verbose {
					fmt.Println(filename)
				}
				if err != nil {
					return
				}
				watchFile(filename)
				reload(filename)
			}()
		}

		// TODO modifiers
		screen.RegisterControl("Q", alwaysIcon(quitIcon), "quit", quit)
		screen.RegisterControl("O", alwaysIcon('\uf15b'), "open", open)
		screen.RegisterControl("R", alwaysIcon(resetIcon), "reset", reset)
		screen.RegisterControl("space", func() rune {
			if state != Running {
				return playIcon
			} else {
				return pauseIcon
			}
		}, "play/pause", playPause)

		watchFile(filename)

		for state != Exit {
			switch state {
			case Splash:
				// show splash for 2 seconds
				screen.SetRedrawFuncToSplash()
				time.Sleep(time.Second * 1)
				state = Initial
			case Initial:
				sim = net.NewSimulation(startTime, endTime, network)
				sim.DoEveryStateChange(onStateChange)
				if truerandom {
					net.TrueRandomSeed()
				}
				screen.SetRedrawFunc(drawNet)
				if autostart {
					state = Running
				} else {
					state = Paused
				}
				screen.SetTitle(sim.GetNow().String() + " init")
			case Running:
				sim.Run()             ////////////////// <--
				if state != Running { // paused or stopped
					continue
				}
				screen.SetTitle(sim.GetNow().String() + " done")
				screen.ForceRedraw(true)
				if verbose {
					fmt.Println("----")
				}
				if noclose {
					state = Idle
				} else {
					state = Exit
				}
			case Paused:
				time.Sleep(time.Millisecond * 20)
				screen.SetTitle(sim.GetNow().String() + " paused")
			case Idle:
				time.Sleep(time.Millisecond * 20)
			}
		}

	}) // returns when func returns

}

func makeFileWatcher(callback func(string)) func(string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	// defer watcher.Close() // TODO call somewhere
	var currentFile = ""

	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if (event.Op & fsnotify.Write) == fsnotify.Write {
					callback(currentFile)
				}
			case err := <-watcher.Errors:
				fmt.Fprintf(os.Stderr, "%s", err)
			}
		}
	}()

	return func(file string) {
		if currentFile == file {
			return
		}
		if currentFile != "" {
			err = watcher.Remove(currentFile)
			if err != nil {
				log.Fatal(err)
				return
			}
		}
		err = watcher.Add(file)
		if err != nil {
			log.Fatal(err)
			return
		}
		currentFile = file
	}
}

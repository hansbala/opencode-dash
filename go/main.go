package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"golang.org/x/term"
)

type options struct {
	once       bool
	sessions   bool
	noColor    bool
	forceColor bool
	width      int
	height     int
}

type terminal struct {
	state  *term.State
	active bool
}

func (t *terminal) enter() error {
	enableVT()
	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	t.state = state
	t.active = true
	fmt.Print("\x1b[?1049h\x1b[2J\x1b[H\x1b[?25l")
	return nil
}

func (t *terminal) leave() {
	fmt.Print("\x1b[?25h\x1b[?1049l")
	if t.active {
		_ = term.Restore(int(os.Stdin.Fd()), t.state)
		t.active = false
	}
}

func (t *terminal) size() (int, int) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 || height <= 0 {
		return 100, 32
	}
	return width, height
}

type fetchResult struct {
	snapshot *snapshot
	sessions []sessionSummary
	err      error
}

func main() {
	opts := parseOptions()
	color := opts.forceColor || (!opts.noColor && term.IsTerminal(int(os.Stdout.Fd())))

	if opts.once {
		runOnce(opts, color)
		return
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "opencode-dash: interactive mode needs a terminal (use --once for scripts)")
		os.Exit(1)
	}
	runInteractive(opts, color)
}

func runOnce(opts options, color bool) {
	width, height := opts.width, opts.height
	if width == 0 || height == 0 {
		terminalWidth, terminalHeight := (&terminal{}).size()
		if width == 0 {
			width = terminalWidth
		}
		if height == 0 {
			height = terminalHeight
		}
	}

	snapshot, err := fetchSnapshot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	sessions, _ := fetchSessions(12)
	fmt.Println(render(snapshot, sessions, opts.sessions, width, height, color, ""))
}

func runInteractive(opts options, color bool) {
	terminalUI := &terminal{}
	if err := terminalUI.enter(); err != nil {
		fmt.Fprintf(os.Stderr, "opencode-dash: %v\n", err)
		os.Exit(1)
	}
	defer terminalUI.leave()

	keys := make(chan byte, 32)
	go readKeys(keys)

	results := make(chan fetchResult, 1)
	fetching := false
	request := func() {
		if fetching {
			return
		}
		fetching = true
		go func() {
			snapshot, err := fetchSnapshot()
			var sessions []sessionSummary
			if err == nil {
				sessions, _ = fetchSessions(12)
			}
			results <- fetchResult{snapshot: snapshot, sessions: sessions, err: err}
		}()
	}

	var snapshot *snapshot
	var sessions []sessionSummary
	var errMessage string
	showSessions := false

	draw := func() {
		width, height := terminalUI.size()
		fmt.Print("\x1b[H" + render(snapshot, sessions, showSessions, width, height, color, errMessage) + "\x1b[J")
	}

	draw()
	request()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	refresh := time.NewTicker(30 * time.Second)
	defer refresh.Stop()

	for {
		select {
		case key, ok := <-keys:
			if !ok {
				return
			}
			switch key {
			case 'q', 'Q', 3:
				return
			case 'r', 'R':
				request()
			case 's', 'S', 'l', 'L':
				showSessions = !showSessions
				draw()
			case 'o', 'O':
				if snapshot != nil && snapshot.usageURL != "" {
					openURL(snapshot.usageURL)
				}
			}
		case result := <-results:
			fetching = false
			if result.err != nil {
				errMessage = result.err.Error()
			} else {
				snapshot = result.snapshot
				sessions = result.sessions
				errMessage = ""
			}
			draw()
		case <-refresh.C:
			request()
		case <-ticker.C:
			draw()
		}
	}
}

func readKeys(ch chan<- byte) {
	buffer := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buffer)
		if err != nil {
			close(ch)
			return
		}
		for _, b := range buffer[:n] {
			ch <- b
		}
	}
}

func openURL(target string) {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{target}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", target}
	default:
		command = "xdg-open"
		args = []string{target}
	}
	_ = exec.Command(command, args...).Start()
}

func parseOptions() options {
	opts := options{}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		opts.noColor = true
	}

	args := os.Args[1:]
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--once", "-1":
			opts.once = true
		case "--sessions":
			opts.sessions = true
			opts.once = true
		case "--no-color":
			opts.noColor = true
		case "--color":
			opts.forceColor = true
		case "--width":
			if index+1 < len(args) {
				if value, err := strconv.Atoi(args[index+1]); err == nil {
					opts.width = value
					index++
				}
			}
		case "--height":
			if index+1 < len(args) {
				if value, err := strconv.Atoi(args[index+1]); err == nil {
					opts.height = value
					index++
				}
			}
		case "--help", "-h":
			printHelp()
			os.Exit(0)
		}
	}
	return opts
}

func printHelp() {
	fmt.Print(`opencode-dash — terminal dashboard for OpenCode usage

Usage: opencode-dash [--once] [--sessions] [--no-color] [--width N] [--height N]

Flags:
  --once       print one frame and exit
  --sessions   print the recent-sessions view and exit
  --no-color   disable ANSI color

Keys:
  q  quit
  r  refresh
  s  toggle recent sessions
  o  open the web dashboard
`)
}

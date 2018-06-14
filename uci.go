/*
This file is part of the uci package.
Copyright (C) 2018 David Hughes

uci is free software: you can redistribute it and/or modify it under
the terms of the GNU General Public License as published by the Free Software
Foundation, either version 3 of the License, or (at your option) any later
version.

This program is distributed in the hope that it will be useful, but WITHOUT ANY
WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
PARTICULAR PURPOSE.  See the GNU General Public License for more details.

You should have received a copy of the GNU General Public License along with
this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package uci

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"text/scanner"
	"time"
)

// EngOption is a slice of option names and values
// Only the first two options are set with options specified by the GUI
//
// Because any variety of options can be specified by the engine, all fields
// are recorded as strings. If more information about a specific engine is
// known, these values can be parsed into native types at discretion
type EngOption struct {
	Name  string // name of the option
	Value string // value of the option

	Type    string   // type of the option
	Default string   // default value of option
	Min     string   // min possible value of option
	Max     string   // max possible value of option
	Var     []string // predefined values of this parameter
}

// BestMove stores the most recent bestmove and ponder
type BestMove struct {
	BestMove string
	Ponder   string
}

// Score is the score returned by the engine
type Score struct {
	Val        int  // score in centipawns or mate in moves
	Lowerbound bool // true if the score is a lowerbound
	Upperbound bool // true if the score is an upperbound
	Mate       bool // false if val in centipawns, true if val is mate in moves
}

// Info returned from the engine
type Info struct {
	Depth          int      // search depth in plies
	SelDepth       int      // selective search depth in plies
	Time           int      // the time searched in ms
	Nodes          int      // nodes searched
	NodesPerSecond int      // nodes per second searched
	PV             []string // the best line found
	MultiPV        int      // multipv ranking, 0 if multipv not set
	Score          Score    // score
	CurrMove       string   // currently searching this move
	CurrMoveNumber int      // currently searching this move number
	HashFull       int      // the hash is x permill full
	TBHits         int      // number of positions found in the endgame table bases
	SBHits         int      // number of positions found in the shredder endgame databases
	CPULoad        int      // CPU usage of the engine in permill
	String         string   // any string str which will be displayed be the engine
	Refutation     []string // first move refuted by the line of remaining moves
	CurrLine       []string // current line the engine is calculating
}

// EngChans are the channels used by the engine
type EngChans struct {
	infoDone chan bool
	readyOK  chan bool
	bestMove chan BestMove
}

// Engine holds information about the engine executable, the communication to
// the engine, and information returned from the engine
type Engine struct {
	cmd    *exec.Cmd     // interface for the external engine program
	stdout *bufio.Reader // engine stdout buffer
	stdin  *bufio.Writer // engine stdin buffer

	name   string // name specified by the engine
	author string // author specified by the engine
	dName  string // displayName specified by the GUI

	defaultOptions []EngOption // options returned when sending uci to engine
	setOptions     []EngOption // options set by GUI

	infoBuf      []Info   // information returned by the engine
	infoBufCap   int      // max capacity of the slice, or 0 if none specified
	lastBestMove BestMove // most recent bestmove
	sync.RWMutex          // embedded mutex for editing the info buf and bestmove

	chans EngChans // channels used by the engine
}

// PrintInfo prints the name, author defaultOptions, and SetOptions
func (e *Engine) PrintInfo() {
	fmt.Printf("Name: %s\n", e.name)
	fmt.Printf("Author: %s\n", e.author)
	fmt.Printf("Display Name: %s\n\n", e.dName)

	fmt.Println("Default Options:")
	for _, v := range e.defaultOptions {
		fmt.Printf("%+v\n", v)
	}
	fmt.Println()

	fmt.Println("Set Options:")
	for _, v := range e.setOptions {
		fmt.Printf("%+v\n", v)
	}
}

// parses the output of the uci command
func (e *Engine) parseUCILine(s []string) {
	f := func(s []string) (string, int) {
		keywords := []string{"name", "type", "default", "min", "max", "var"}
		ret := ""
		var i int

	Outer:
		for _, v := range s {
			for _, k := range keywords {
				if v == k {
					break Outer
				}
			}
			i++
		}

		ret = strings.Join(s[:i], " ")
		if ret == "<empty>" {
			ret = ""
		}
		return ret, i
	}

	lineOptions := EngOption{}

	for i := 0; i < len(s); i++ {
		var skip int

		switch s[i] {
		case "name":
			lineOptions.Name, skip = f(s[i+1:])
			i += skip
		case "type":
			lineOptions.Type, skip = f(s[i+1:])
			i += skip
		case "default":
			lineOptions.Default, skip = f(s[i+1:])
			i += skip
		case "min":
			lineOptions.Min, skip = f(s[i+1:])
			i += skip
		case "max":
			lineOptions.Max, skip = f(s[i+1:])
			i += skip
		case "var":
			opt, skip := f(s[i+1:])
			lineOptions.Var = append(lineOptions.Var, opt)
			i += skip
		}
	}

	e.defaultOptions = append(e.defaultOptions, lineOptions)
}

// UCI sends the uci command to the engine and sets up values in the Engine
// struct
func (e *Engine) UCI() error {
	_, err := e.stdin.WriteString(fmt.Sprintf("uci\n"))
	if err != nil {
		return err
	}

	err = e.stdin.Flush()
	if err != nil {
		return err
	}

Loop:
	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return err
		}

		lineSlice := strings.Fields(line)

		if len(lineSlice) == 0 {
			continue
		}

		switch lineSlice[0] {
		case "id":
			if lineSlice[1] == "name" {
				e.name = strings.Join(lineSlice[2:], " ")
			} else if lineSlice[1] == "author" {
				e.author = strings.Join(lineSlice[2:], " ")
			}
		case "option":
			e.parseUCILine(lineSlice[1:])
		case "uciok":
			break Loop
		}
	}

	if e.dName == "" {
		e.dName = e.name
	}

	return nil
}

// SetDisplayName sets the display name of the engine
func (e *Engine) SetDisplayName(displayName string) {
	e.dName = displayName
}

// SendCommand sends a generic string to the engine without guarantee that
// the command was accepted. The input command should not include a newline.
func (e *Engine) SendCommand(command string) error {
	_, err := e.stdin.WriteString(command + "\n")
	if err != nil {
		return err
	}

	err = e.stdin.Flush()
	if err != nil {
		return err
	}

	return nil
}

// SendFEN updates the engine position with a FEN string
func (e *Engine) SendFEN(fen string) error {
	return e.SendCommand(fmt.Sprintf("position fen %s", fen))
}

// SendUCINewGame sends a ucinewgame command to the engine
func (e *Engine) SendUCINewGame() error {
	return e.SendCommand("ucinewgame")
}

// SendStop sends a stop command to the engine
func (e *Engine) SendStop() error {
	return e.SendCommand("stop")
}

// SendQuit sends a quit command to the engine and waits for the program to
// exit
func (e *Engine) SendQuit() error {
	if err := e.SendCommand("quit"); err != nil {
		return err
	}

	if err := e.cmd.Wait(); err != nil {
		return err
	}

	return nil
}

// SendPonderHit sends a ponderhit command to the engine
// func (e *Engine) SendPonderHit() error {
// return e.SendCommand("ponderhit")
// }

// SendOption sends an option to the engine
func (e *Engine) SendOption(name, value string) error {
	var sendString string

	if value == "" {
		sendString = fmt.Sprintf("setoption name %s", name)
	} else {
		sendString = fmt.Sprintf("setoption name %s value %s", name, value)
	}

	if err := e.SendCommand(sendString); err != nil {
		return err
	}

	setOption := EngOption{}
	setOption.Name = name
	setOption.Value = value

	// Updates the options sent to the engine, and overwrites previously
	// set option with the same name
	mergeSetOptions := func(prev []EngOption, new EngOption) []EngOption {
		for i, v := range prev {
			if v.Name == setOption.Name {
				prev = append(prev[:i], prev[i+1:]...)
				break
			}
		}

		prev = append(prev, new)
		return prev
	}

	e.setOptions = mergeSetOptions(e.setOptions, setOption)

	return nil
}

// WaitReadyOK sends isready to engine and waits for readyok
// sets a 5s timeout and checks every 10ms for the readyok response
//
// Note: while isready can be sent to the engine at any time, even while the
// engine is calculating, this function throws away any other output from the
// engine while waiting for isready, so this should be used with care
func (e *Engine) WaitReadyOK() error {
	if err := e.SendCommand("isready"); err != nil {
		return err
	}

	timeout := time.After(5 * time.Second)
	tick := time.Tick(10 * time.Millisecond)

	for {
		select {
		case <-timeout:
			return errors.New("timed out")
		case <-tick:
			if e.chans.readyOK != nil {
				continue
			}

			line, err := e.stdout.ReadString('\n')
			if err != nil {
				return err
			}

			if line == "readyok\n" {
				return nil
			}
		case <-e.chans.readyOK:
			return nil
		}
	}
}

// WaitCollection waits for the info collection goroutine to finish
func (e *Engine) WaitCollection(timeout time.Duration) error {
	if e.chans.infoDone == nil {
		return errors.New("infoDone channel not made")
	}

	timer := time.After(timeout)

	select {
	case <-e.chans.infoDone:
		return nil
	case <-timer:
		return errors.New("timed out")
	}
}

// WaitBestMove waits for the bestmove to be sent
func (e *Engine) WaitBestMove(timeout time.Duration) (BestMove, error) {
	if e.chans.bestMove == nil {
		return BestMove{}, errors.New("bestMove channel not made")
	}

	timer := time.After(timeout)

	select {
	case b := <-e.chans.bestMove:
		return b, nil
	case <-timer:
		return BestMove{}, errors.New("timed out")
	}
}

// GetInfo returns the last info lines returned by the engine, or all lines if
// last is negative
func (e *Engine) GetInfo(last int) []Info {
	var ret []Info

	e.RLock()
	defer e.RUnlock()

	if last < 0 || last > e.infoBufCap {
		ret = make([]Info, len(e.infoBuf))
		copy(ret, e.infoBuf)
	} else {
		ret = make([]Info, last)
		copy(ret, e.infoBuf[len(e.infoBuf)-last:])
	}

	return ret

}

// parses the stdout of the engine
func (e *Engine) parseStdout(line string) error {
	if strings.HasPrefix(line, "readyok") {
		e.chans.readyOK <- true
		return nil
	}

	if strings.HasPrefix(line, "bestmove") {
		e.Lock()

		lineSlice := strings.Split(line, " ")

		e.lastBestMove.BestMove = lineSlice[1]

		if len(lineSlice) > 2 {
			e.lastBestMove.Ponder = lineSlice[3]
		} else {
			e.lastBestMove.Ponder = ""
		}

		b := BestMove{e.lastBestMove.BestMove, e.lastBestMove.Ponder}

		e.Unlock()

		// explicitly empty the channel
	Loop:
		for {
			select {
			case <-e.chans.bestMove:
			default:
				break Loop
			}
		}

		e.chans.bestMove <- b
		return nil
	}

	var err error
	rd := strings.NewReader(line)
	s := scanner.Scanner{}
	s.Init(rd)
	s.Mode = scanner.ScanIdents | scanner.ScanChars | scanner.ScanInts

	atoi := func(dest int, s scanner.Scanner) error {
		s.Scan()
		dest, err = strconv.Atoi(s.TokenText())
		return err
	}

	info := Info{}
	var StringSlice []string
	for s.Scan() != scanner.EOF {
		switch s.TokenText() {
		case "info":
		case "depth":
			if err = atoi(info.Depth, s); err != nil {
				return err
			}
		case "seldepth":
			if err = atoi(info.SelDepth, s); err != nil {
				return err
			}
		case "time":
			if err = atoi(info.Time, s); err != nil {
				return err
			}
		case "nodes":
			if err = atoi(info.Nodes, s); err != nil {
				return err
			}
		case "nps":
			if err = atoi(info.NodesPerSecond, s); err != nil {
				return err
			}
		case "pv": // assumes pv is at the end of the line
			for s.Scan() != scanner.EOF {
				info.PV = append(info.PV, s.TokenText())
			}
		case "multipv":
			if err = atoi(info.MultiPV, s); err != nil {
				return err
			}
		case "score":
			s.Scan()
			switch s.TokenText() {
			case "cp":
				s.Scan()
			case "mate":
				info.Score.Mate = true
				s.Scan()
			}
			neg := 1
			if s.TokenText() == "-" {
				neg = -1
				s.Scan()
			}
			info.Score.Val, err = strconv.Atoi(s.TokenText())
			if err != nil {
				return err
			}
			info.Score.Val *= neg
		case "currmove":
			s.Scan()
			info.CurrMove = s.TokenText()
		case "currmovenumber":
			if err = atoi(info.CurrMoveNumber, s); err != nil {
				return err
			}
		case "hashfull":
			if err = atoi(info.HashFull, s); err != nil {
				return err
			}
		case "tbhits":
			if err = atoi(info.TBHits, s); err != nil {
				return err
			}
		case "sbhits":
			if err = atoi(info.SBHits, s); err != nil {
				return err
			}
		case "cpuload":
			if err = atoi(info.CPULoad, s); err != nil {
				return err
			}
		case "string":
			for s.Scan() != scanner.EOF {
				StringSlice = append(StringSlice, s.TokenText())
			}
		case "refutation": // assumes refutation at end of line
			for s.Scan() != scanner.EOF {
				info.Refutation = append(info.Refutation, s.TokenText())
			}
		case "currline":
			for s.Scan() != scanner.EOF {
				info.CurrLine = append(info.CurrLine, s.TokenText())
			}
		}
	}

	// put string slice into a single string separated by spaces
	info.String = strings.Join(StringSlice, " ")

	e.Lock()
	defer e.Unlock()

	// TODO check performance of this
	if len(e.infoBuf) > e.infoBufCap && e.infoBufCap != 0 {
		e.infoBuf = append(e.infoBuf[len(e.infoBuf)-e.infoBufCap:], info)
	} else {
		e.infoBuf = append(e.infoBuf, info)
	}

	return nil
}

// StartInfoCollection starts a goroutine that continually parses information
// sent by the engine
//
// Once info collection has started it cannot be stopped
//
// TODO: handle error better
func (e *Engine) StartInfoCollection() error {
	e.chans.infoDone = make(chan bool)
	e.chans.readyOK = make(chan bool)
	e.chans.bestMove = make(chan BestMove, 16)

	go func() error {
		for {
			line, err := e.stdout.ReadString('\n')
			if err == io.EOF {
				fmt.Println("closed")
				return nil
			} else if err != nil {
				log.Fatalf("%v\n", err)
			}

			e.parseStdout(strings.Trim(line, "\n"))
		}
	}()

	return nil
}

// NewEngineFromPath returns an Engine it has spun up given a path and
// connected communication to. If the displayName is not specified (empty
// string), the displayName will be set to the name given by the engine when
// UCI() is called.
func NewEngineFromPath(path, displayName string, infoBufCap int) (*Engine, error) {
	eng := Engine{}
	eng.cmd = exec.Command(path)

	stdin, err := eng.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := eng.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := eng.cmd.Start(); err != nil {
		return nil, err
	}

	eng.stdin = bufio.NewWriter(stdin)
	eng.stdout = bufio.NewReader(stdout)
	eng.dName = displayName

	if infoBufCap < 0 {
		return nil, errors.New("infoBufCap can't be negative")
	}

	eng.infoBufCap = infoBufCap

	return &eng, nil
}

// EngConfig holds the information specified in the config file
type EngConfig []struct {
	DisplayName string `json:"displayName"` // name to display for the engine
	Path        string `json:"path"`        // path to engine executable
	InfoBufCap  int    `json:"infoBufCap"`  // max capacity for the info buffer
	UCIOptions  []struct {
		Name  string `json:"name"`  // name of engine option
		Value string `json:"value"` // value of engine option
	}
}

// parses the specified config file
func (ec *EngConfig) parseConfig(filename string) error {
	raw, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	if err = json.Unmarshal(raw, &ec); err != nil {
		return err
	}

	// checks parsed data
	// the path for each engine must be specified
	// if each UCIoption must have a name specified, but value is optional
	for _, c := range *ec {
		if c.Path == "" {
			return errors.New("no path specified for engine in config file")
		}

		for _, o := range c.UCIOptions {
			if o.Name == "" && o.Value != "" {
				return errors.New("engine option value specified without name")
			}
		}
	}

	return nil
}

// NewEnginesFromConfig sets up all engines described in a JSON config file
func NewEnginesFromConfig(path string) ([]*Engine, error) {
	config := EngConfig{}
	engs := []*Engine{}

	if err := config.parseConfig(path); err != nil {
		return nil, err
	}

	for _, c := range config {
		eng, err := NewEngineFromPath(c.Path, c.DisplayName, c.InfoBufCap)
		if err != nil {
			return nil, err
		}

		if err = eng.UCI(); err != nil {
			return nil, err
		}

		for _, o := range c.UCIOptions {
			if err = eng.SendOption(o.Name, o.Value); err != nil {
				return nil, err
			}
		}

		if err = eng.WaitReadyOK(); err != nil {
			return nil, err
		}

		engs = append(engs, eng)
	}

	return engs, nil
}

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
	"io/ioutil"
	"os/exec"
	"strings"
)

// EngOption is a slice of option names and values
type EngOption struct {
	Name  string
	Value string

	Type    string
	Default string
	Min     string
	Max     string
	Var     []string
}

// Engine holds information about the engine executable, the communication to
// the engine, and information returned from the engine
type Engine struct {
	cmd    *exec.Cmd
	stdout *bufio.Reader
	stdin  *bufio.Writer

	name   string
	author string
	dName  string

	defaultOptions []EngOption
	setOptions     []EngOption
}

// PrintInfo prints the name, authror, defaultOptions, and SetOptions
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
	err := e.SendCommand(fmt.Sprintf("position fen %s", fen))
	return err
}

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

// NewEngineFromPath returns an Engine it has spun up given a path and
// connected communication to. If the displayName is not specified (empty
// string), the displayName will be set to the name given by the engine when
// UCI() is called.
func NewEngineFromPath(path, displayName string) (*Engine, error) {
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

	return &eng, nil
}

// EngConfig holds the information specified in the config file
type EngConfig []struct {
	DisplayName string `json:"displayName"`
	Path        string `json:"path"`
	UCIOptions  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
}

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
		eng, err := NewEngineFromPath(c.Path, c.DisplayName)
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

		engs = append(engs, eng)
	}

	return engs, nil
}

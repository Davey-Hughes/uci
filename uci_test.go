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
	"io/ioutil"
	"testing"
)

type ConfigTTOutput struct {
	err error
}

type ConfigTT struct {
	name   string
	path   string
	output ConfigTTOutput
}

// Tests functions for initializing engines
func TestNewEngines(t *testing.T) {
	// no file error
	_, nofileerr := ioutil.ReadFile("nofile")

	tt := []ConfigTT{
		{
			name: "good config file",
			path: "../engines.json",
			output: ConfigTTOutput{
				err: nil,
			},
		},
		{
			name: "no file",
			path: "nofile",
			output: ConfigTTOutput{
				err: nofileerr,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := NewEnginesFromConfig(tc.path)
			_ = eng

			if err != nil && err.Error() != tc.output.err.Error() {
				t.Fatalf("%s test should produce \"%v\" error", tc.name, tc.output.err)
			}
		})
	}
}

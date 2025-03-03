// Copyright 2022 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"time"
	_ "unsafe"
)

var globalStartUnixTimeNS TimeNano
var globalStartMonoTimeNS TimeMono

func init() {
	globalStartUnixTimeNS = unixtimeNS()
	globalStartMonoTimeNS = monotimeNS()
}

// `time.Now()` contain two syscalls in Linux.
// One is `CLOCK_REALTIME` and another is `CLOCK_MONOTONIC`.
// Separate it into two functions: walltime() and nanotime(), which can improve duration calculation.
// PS: runtime.walltime() hav been removed from linux-amd64

//go:linkname nanotime runtime.nanotime
func nanotime() int64

type TimeMono = uint64

// MonotimeNS used to calculate duration.
func monotimeNS() TimeMono {
	return TimeMono(nanotime())
}

type TimeNano = uint64

// unixtimeNS save time.Time as uint64
func unixtimeNS() TimeNano {
	t := time.Now()
	sec, nsec := t.Unix(), t.Nanosecond()
	return TimeNano(sec*1e9 + int64(nsec))
}

func NowNS() TimeNano {
	mono := monotimeNS()
	return TimeNano((mono - globalStartMonoTimeNS) + globalStartUnixTimeNS)
}

// Now generate `hasMonotonic=0` time.Time.
// warning: It should NOT compare with time.Time, which generated by time.Now()
func Now() time.Time {
	nowNS := NowNS()
	sec, nesc := nowNS/1e9, nowNS%1e9
	return time.Unix(int64(sec), int64(nesc))
}

// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	done := make(chan bool)
	ticker := time.NewTicker(500 * time.Millisecond)

	wg.Go(func() {
		for {
			select {
			case <-done:
				fmt.Println("Ticker stopped, worker exiting...")
				return
			case t := <-ticker.C:
				fmt.Println("Tick at", t)
			}
		}
	})

	// Run for 2 seconds then stop
	time.Sleep(2 * time.Second)
	ticker.Stop()
	done <- true

	wg.Wait() // Ensure goroutine finishes before main exits
	fmt.Println("Program finished")
}

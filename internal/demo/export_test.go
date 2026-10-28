package demo

import "time"

func init() { history = 6 * time.Hour } // keeps TestDemoFleet fast under -race

package main

import (
	"log"
)

var Logf func(string, ...any) = func(format string, v ...any) {}

func SetVerbose(v bool) {
	if v {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		Logf = func(format string, v ...any) {
			log.Printf(format, v...)
		}
	} else {
		Logf = func(format string, v ...any) {}
	}
}

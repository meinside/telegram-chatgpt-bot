package main

// main.go

import (
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) <= 1 {
		printUsage()
	} else {
		confFilepath := os.Args[1]

		if conf, err := loadConfig(confFilepath); err == nil {
			runBot(conf)
		} else {
			log.Printf("failed to load config: %s", err)
		}
	}
}

// print usage string
func printUsage() {
	fmt.Printf(`
Usage: %s [config_filepath]
`, os.Args[0])
}

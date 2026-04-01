package main

import (
	"log"
	"os"

	"forum/internal/app"
)

func main() {
	if err := app.Execute(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("tpod development")
		return
	}

	fmt.Println("usage: tpod version")
}

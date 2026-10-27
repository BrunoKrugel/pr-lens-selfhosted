package main

import (
	"flag"
	"log"
	"net/http"

	"canvas-server/internal/canvas"
)

func main() {
	base := flag.String("base-url", "http://localhost:8080", "public base URL the CLI-facing URLs are rooted at")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	log.Printf("canvas server listening on %s, serving URLs under %s", *addr, *base)
	log.Fatal(http.ListenAndServe(*addr, canvas.New(*base)))
}

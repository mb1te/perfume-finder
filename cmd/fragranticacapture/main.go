package main

import (
	"context"
	"flag"
	"log"
	"parfumes_finder/internal/capture"
	"time"
)

func main() {
	topic := flag.String("topic-url", "https://www.fragrantica.ru/board/viewtopic.php?id=235155", "")
	pages := flag.Int("pages", 16, "")
	output := flag.String("output", "data/fragrantica/topic-235155", "")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := capture.Capture(ctx, *topic, *pages, *output); err != nil {
		log.Fatal(err)
	}
}

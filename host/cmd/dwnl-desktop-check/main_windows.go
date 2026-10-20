package main

import (
	"context"
	"dwnl/host/internal/desktopipc"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	state := flag.String("state", "", "State root")
	method := flag.String("method", "snapshot", "Method")
	payload := flag.String("payload", "{}", "JSON")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	start := time.Now()
	response, err := desktopipc.Call(ctx, *state, desktopipc.Request{Version: 2, ID: "development-check", Method: *method, Payload: json.RawMessage(*payload)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, _ := json.Marshal(map[string]any{"elapsedMs": float64(time.Since(start).Microseconds()) / 1000, "response": response})
	fmt.Println(string(data))
	if response.Error != nil {
		os.Exit(2)
	}
}

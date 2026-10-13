// A typical consumer of the orders topic, written against v2: it decodes
// each event into a struct and keeps revenue per currency. Run it on events
// from a v3 producer to see what a breaking change looks like downstream.
//
//	go run ./examples/consumer < examples/events/orders-v2.ndjson
//	go run ./examples/consumer < examples/events/orders-v3.ndjson
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

type Order struct {
	OrderID    string    `json:"order_id"`
	Amount     int64     `json:"amount"` // cents
	Currency   string    `json:"currency"`
	CreatedAt  time.Time `json:"created_at"`
	CustomerID string    `json:"customer_id,omitempty"`
}

func main() {
	revenue := map[string]int64{}
	sc := bufio.NewScanner(os.Stdin)
	for n := 1; sc.Scan(); n++ {
		var o Order
		if err := json.Unmarshal(sc.Bytes(), &o); err != nil {
			fmt.Fprintf(os.Stderr, "consumer: event %d: %v\n", n, err)
			os.Exit(1)
		}
		revenue[o.Currency] += o.Amount
	}
	var cs []string
	for c := range revenue {
		cs = append(cs, c)
	}
	sort.Strings(cs)
	for _, c := range cs {
		fmt.Printf("%s %d.%02d\n", c, revenue[c]/100, revenue[c]%100)
	}
}
